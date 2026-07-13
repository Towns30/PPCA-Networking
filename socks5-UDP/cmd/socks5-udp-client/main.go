package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"time"
)

func splitHostPort(address string) (string, int, error) {
	// 使用 net.SplitHostPort 才能正确处理带方括号的 IPv6 地址，例如 [::1]:9999。
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 0 || port > 65535 {
		return "", 0, fmt.Errorf("invalid port %q", portText)
	}
	return host, port, nil
}

func readAddress(r io.Reader, atyp byte) (string, error) {
	// SOCKS5 中 IPv4 和 IPv6 长度固定，Domain 则先放一个长度字节。
	var n int
	switch atyp {
	case 0x01:
		n = net.IPv4len
	case 0x03:
		length := []byte{0}
		if _, err := io.ReadFull(r, length); err != nil {
			return "", err
		}
		n = int(length[0])
	case 0x04:
		n = net.IPv6len
	default:
		return "", fmt.Errorf("unsupported ATYP 0x%02x", atyp)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	if atyp == 0x03 {
		return string(b), nil
	}
	return net.IP(b).String(), nil
}

func parseSocks5Reply(r io.Reader) (string, int, error) {
	// UDP ASSOCIATE 回复：VER | REP | RSV | ATYP | BND.ADDR | BND.PORT。
	header := make([]byte, 4)
	if _, err := io.ReadFull(r, header); err != nil {
		return "", 0, err
	}
	if header[0] != 0x05 || header[2] != 0x00 {
		return "", 0, fmt.Errorf("invalid SOCKS5 reply header %x", header)
	}
	if header[1] != 0x00 {
		return "", 0, fmt.Errorf("UDP ASSOCIATE failed: REP=0x%02x", header[1])
	}
	host, err := readAddress(r, header[3])
	if err != nil {
		return "", 0, err
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(r, portBytes); err != nil {
		return "", 0, err
	}
	// SOCKS5 的端口始终使用网络字节序（大端序）。
	return host, int(binary.BigEndian.Uint16(portBytes)), nil
}

func buildUDPPacket(target, atyp string, data []byte) ([]byte, error) {
	host, port, err := splitHostPort(target)
	if err != nil {
		return nil, err
	}
	// UDP 请求头的前三字节固定为 RSV=00 00、FRAG=00，表示不使用分片。
	packet := []byte{0x00, 0x00, 0x00}
	switch atyp {
	case "ipv4":
		// ATYP=01 后紧跟 4 字节 IPv4 地址。
		ip := net.ParseIP(host).To4()
		if ip == nil {
			return nil, fmt.Errorf("%q is not an IPv4 address", host)
		}
		packet = append(packet, 0x01)
		packet = append(packet, ip...)
	case "domain":
		// ATYP=03 后是域名长度和域名原始字节。这里绝不能先解析域名，
		// 否则代理收到的就会是 IP，而不是需要由代理解析的 Domain。
		if len(host) == 0 || len(host) > 255 {
			return nil, fmt.Errorf("domain length must be 1..255 bytes")
		}
		packet = append(packet, 0x03, byte(len(host)))
		packet = append(packet, []byte(host)...)
	case "ipv6":
		// ATYP=04 后紧跟 16 字节 IPv6 地址。
		ip := net.ParseIP(host)
		if ip == nil || ip.To4() != nil {
			return nil, fmt.Errorf("%q is not an IPv6 address", host)
		}
		packet = append(packet, 0x04)
		packet = append(packet, ip.To16()...)
	default:
		return nil, fmt.Errorf("invalid -atyp %q (want ipv4, domain, or ipv6)", atyp)
	}
	// 地址后依次追加两字节大端序端口和真正的 UDP 数据。
	packet = binary.BigEndian.AppendUint16(packet, uint16(port))
	return append(packet, data...), nil
}

func parseUDPPacket(packet []byte) ([]byte, error) {
	// 代理返回的数据仍有 SOCKS5 UDP 头，不能直接拿整个 packet 和发送内容比较。
	if len(packet) < 4 {
		return nil, errors.New("short UDP packet")
	}
	if packet[0] != 0x00 || packet[1] != 0x00 {
		return nil, errors.New("invalid RSV")
	}
	if packet[2] != 0x00 {
		return nil, errors.New("fragmented UDP packet is unsupported")
	}
	// pos 从地址字段开始，根据 ATYP 跳过不同长度的地址。
	pos := 4
	switch packet[3] {
	case 0x01:
		pos += net.IPv4len
	case 0x03:
		if len(packet) < pos+1 {
			return nil, errors.New("short domain address")
		}
		pos += 1 + int(packet[pos])
	case 0x04:
		pos += net.IPv6len
	default:
		return nil, fmt.Errorf("unsupported ATYP 0x%02x", packet[3])
	}
	if len(packet) < pos+2 {
		return nil, errors.New("short UDP address or port")
	}
	// 本测试只关心 DATA，但仍按大端序读取并验证 PORT 字段存在。
	_ = binary.BigEndian.Uint16(packet[pos : pos+2])
	return packet[pos+2:], nil
}

func run() int {
	socks := flag.String("socks", "127.0.0.1:1080", "SOCKS5 proxy address")
	target := flag.String("target", "127.0.0.1:9999", "UDP target address")
	atyp := flag.String("atyp", "ipv4", "target address type: ipv4, domain, or ipv6")
	count := flag.Int("count", 50, "number of packets")
	timeout := flag.Duration("timeout", 5*time.Second, "per-packet read and write timeout")
	flag.Parse()
	if *count < 0 {
		fmt.Fprintln(os.Stderr, "-count must not be negative")
		return 2
	}

	// 第一步：建立 TCP 控制连接。UDP association 的生命周期依赖这条连接。
	tcpConn, err := net.DialTimeout("tcp", *socks, *timeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// defer 会在 run 返回时执行，所以全部 UDP 测试结束前 TCP 不会被关闭。
	defer tcpConn.Close()

	// 第二步：方法协商。05=SOCKS5，01=一个方法，00=NO AUTH。
	if _, err := tcpConn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	method := make([]byte, 2)
	if _, err := io.ReadFull(tcpConn, method); err != nil || method[0] != 0x05 || method[1] != 0x00 {
		fmt.Fprintf(os.Stderr, "NO AUTH negotiation failed: reply=%x error=%v\n", method, err)
		return 1
	}
	// 第三步：请求 UDP ASSOCIATE。03 是命令，后面的 0.0.0.0:0 表示
	// 客户端不指定固定的 UDP 来源地址，让代理返回实际 relay 地址。
	associate := []byte{0x05, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	if _, err := tcpConn.Write(associate); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	relayHost, relayPort, err := parseSocks5Reply(tcpConn)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	socksHost, _, err := splitHostPort(*socks)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// 有些代理回复 0.0.0.0 或 ::。它们不是可连接的目的地址，
	// 此时改用 SOCKS5 TCP 代理的主机地址，但保留代理给出的 UDP 端口。
	if ip := net.ParseIP(relayHost); ip != nil && ip.IsUnspecified() {
		relayHost = socksHost
	}
	relay, err := net.ResolveUDPAddr("udp", net.JoinHostPort(relayHost, strconv.Itoa(relayPort)))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// 第四步：创建一个 UDP socket。所有数字都通过同一个 association 发送。
	udpConn, err := net.ListenUDP("udp", nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer udpConn.Close()

	passed := 0
	buf := make([]byte, 65535)
	width := len(strconv.Itoa(*count))
	for i := 1; i <= *count; i++ {
		// 每次只发送一个十进制数字字符串，等待响应后再进入下一轮。
		sent := strconv.Itoa(i)
		packet, err := buildUDPPacket(*target, *atyp, []byte(sent))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		_ = udpConn.SetWriteDeadline(time.Now().Add(*timeout))
		if _, err := udpConn.WriteToUDP(packet, relay); err != nil {
			fmt.Printf("[%0*d/%d] send=%s receive=ERROR FAIL\n", width, i, *count, sent)
			continue
		}
		_ = udpConn.SetReadDeadline(time.Now().Add(*timeout))
		n, _, err := udpConn.ReadFromUDP(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				fmt.Printf("[%0*d/%d] send=%s receive=TIMEOUT FAIL\n", width, i, *count, sent)
			} else {
				fmt.Printf("[%0*d/%d] send=%s receive=ERROR FAIL\n", width, i, *count, sent)
			}
			continue
		}
		// 去掉代理添加的 SOCKS5 UDP 头，只比较里面的 DATA。
		data, err := parseUDPPacket(buf[:n])
		if err != nil {
			fmt.Printf("[%0*d/%d] send=%s receive=INVALID FAIL\n", width, i, *count, sent)
			continue
		}
		received := string(data)
		if received == sent {
			passed++
			fmt.Printf("[%0*d/%d] send=%s receive=%s PASS\n", width, i, *count, sent, received)
		} else {
			fmt.Printf("[%0*d/%d] send=%s receive=%s FAIL\n", width, i, *count, sent, received)
		}
	}

	fmt.Printf("Total: %d\nPassed: %d\nFailed: %d\n", *count, passed, *count-passed)
	if passed == *count {
		fmt.Println("SOCKS5 UDP test success")
		return 0
	}
	fmt.Println("SOCKS5 UDP test failed")
	return 1
}

func main() {
	os.Exit(run())
}
