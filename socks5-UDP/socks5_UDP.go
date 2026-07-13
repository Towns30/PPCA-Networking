package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

func GetAddr(conn net.Conn) (string, error) {
	buf := make([]byte, 1)
	_, err := io.ReadFull(conn, buf)
	if err != nil {
		return "", err
	}
	host := ""
	switch buf[0] {
	case 0x01: // IPv4，len = 4 byte
		addr_buf := make([]byte, 4)
		_, err = io.ReadFull(conn, addr_buf)
		if err != nil {
			return "", err
		}
		host = net.IP(addr_buf).String()
	case 0x03: // domianname, we need to read domainname length followed
		domainlen_buf := make([]byte, 1)
		_, err = io.ReadFull(conn, domainlen_buf)
		if err != nil {
			return "", err
		}
		domainlen := int(domainlen_buf[0])
		domain_buf := make([]byte, domainlen)
		_, err = io.ReadFull(conn, domain_buf)
		if err != nil {
			return "", err
		}
		host = string(domain_buf)
	case 0x04: // IPv6，len = 16 byte
		addr_buf := make([]byte, 16)
		_, err = io.ReadFull(conn, addr_buf)
		if err != nil {
			return "", err
		}
		host = net.IP(addr_buf).String()
	default:
		return "", fmt.Errorf("Wrong address type %d", buf[0])
	}
	port_buf := make([]byte, 2)
	_, err = io.ReadFull(conn, port_buf)
	if err != nil {
		return "", err
	}
	port := binary.BigEndian.Uint16(port_buf)
	addr := net.JoinHostPort(host, strconv.Itoa(int(port)))
	fmt.Println("connect target:", addr)
	return addr, nil
}

func Methods(conn net.Conn) error {
	// Methods request format
	// +----+----------+----------+
	// |VER | NMETHODS | METHODS  |
	// +----+----------+----------+
	// | 1  |    1     | 1 to 255 |
	// +----+----------+----------+

	buf := make([]byte, 2)
	_, err := io.ReadFull(conn, buf)
	if err != nil {
		return err
	}
	if buf[0] != byte(0x05) { // unsupported SOCKS5 version
		return fmt.Errorf("unsupported socks version: %d", buf[0])
	}
	num := int(buf[1])
	buf_methods := make([]byte, num)
	_, err = io.ReadFull(conn, buf_methods)
	if err != nil {
		return err
	}
	hasNoAuth := false
	for _, method := range buf_methods {
		if method == byte(0x00) {
			hasNoAuth = true
			break
		}
	}
	if !hasNoAuth { // unsupport NO AUTH
		conn.Write([]byte{0x05, 0xff})
		return fmt.Errorf("Sorry, we only support NO AUTH")
	}
	// SOCKS5 reply format
	// +----+--------+
	// |VER | METHOD |
	// +----+--------+
	// | 1  |   1    |
	// +----+--------+

	conn.Write([]byte{0x05, 0x00}) // SOCKS5 reply
	return nil
}
func TCPConnect(conn net.Conn, addr string) error {
	conn_server, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
		return err
	}
	// SOCKS5 reply format
	// +----+-----+-------+------+----------+----------+
	// |VER | REP |  RSV  | ATYP | BND.ADDR | BND.PORT |
	// +----+-----+-------+------+----------+----------+
	// | 1  |  1  | X'00' |  1   | Variable |    2     |
	// +----+-----+-------+------+----------+----------+
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	err = Transmit(conn, conn_server)
	if err != nil {
		return err
	}
	return nil
}
func BuildUDPResponse(server_udp_addr *net.UDPAddr, data []byte, data_len int) ([]byte, error) {

	ipv4 := server_udp_addr.IP.To4()
	port := server_udp_addr.Port
	if ipv4 != nil { // IPv4
		udp_buf := make([]byte, 10)
		udp_buf[0] = byte(0x00)
		udp_buf[1] = byte(0x00)
		udp_buf[2] = byte(0x00)
		udp_buf[3] = byte(0x01)
		copy(udp_buf[4:8], ipv4)
		binary.BigEndian.PutUint16(udp_buf[8:10], uint16(port))
		return append(udp_buf, data[:data_len]...), nil
	} else { // IPv6
		ipv6 := server_udp_addr.IP.To16()
		udp_buf := make([]byte, 22)
		udp_buf[0] = byte(0x00)
		udp_buf[1] = byte(0x00)
		udp_buf[2] = byte(0x00)
		udp_buf[3] = byte(0x04)
		copy(udp_buf[4:20], ipv6)
		binary.BigEndian.PutUint16(udp_buf[20:22], uint16(port))
		return append(udp_buf, data[:data_len]...), nil
	}
}
func HandleUDP(client_udp_conn *net.UDPConn) error {
	// UDP package format
	// +----+------+------+----------+----------+----------+
	// |RSV | FRAG | ATYP | DST.ADDR | DST.PORT |   DATA   |
	// +----+------+------+----------+----------+----------+
	// | 2  |  1   |  1   | Variable |    2     | Variable |
	// +----+------+------+----------+----------+----------+

	buf := make([]byte, 65535)
	for {
		n, clientAddr, err := client_udp_conn.ReadFromUDP(buf)
		if err != nil {
			return err
		}
		fmt.Println("receive package from ", clientAddr)
		fmt.Printf("raw packet: %x\n", buf[:n])
		// unpackage
		if n < 4 {
			return fmt.Errorf("format error")
		}
		if buf[0] != byte(0x00) || buf[1] != byte(0x00) {
			return fmt.Errorf("format error")
		}
		// we make it simple
		if buf[2] != byte(0x00) {
			return fmt.Errorf("it is a big package, we can't handle it in this version")
		}
		pos := 4
		host := ""
		switch buf[3] {
		case 0x01: // IPv4, 4 byte
			if n < 8 {
				return fmt.Errorf("format error")
			}
			host = net.IP(buf[4:8]).String()
			pos = 8
		case 0x03: // domainname, we need to read length first
			if n < 5 {
				return fmt.Errorf("format error")
			}
			domain_len := int(buf[4])
			if n < 5+domain_len {
				return fmt.Errorf("format error")
			}
			host = string(buf[5 : 5+domain_len])
			pos = 5 + domain_len
		case 0x04: // IPv6, 16 byte
			if n < 20 {
				return fmt.Errorf("format error")
			}
			host = net.IP(buf[4:20]).String()
			pos = 20
		default:
			return fmt.Errorf("ATYP error")
		}
		if n < pos+2 {
			return fmt.Errorf("format error")
		}
		port := binary.BigEndian.Uint16(buf[pos : pos+2])
		data := buf[pos+2 : n]
		server_addr := net.JoinHostPort(host, strconv.Itoa(int(port)))
		// connect target server
		server_udp_addr, err := net.ResolveUDPAddr("udp", server_addr)
		if err != nil {
			return err
		}
		server_udp_conn, err := net.DialUDP("udp", nil, server_udp_addr)
		if err != nil {
			return err
		}
		_, err = server_udp_conn.Write(data)
		if err != nil {
			server_udp_conn.Close()
			return err
		}
		server_udp_conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		reply_data := make([]byte, 65535)
		data_len, err := server_udp_conn.Read(reply_data)
		if err != nil {
			server_udp_conn.Close()
			net_err, ok := err.(net.Error)
			if ok && net_err.Timeout() {
				continue
			}
			return err
		}
		err = server_udp_conn.Close()
		if err != nil {
			return err
		}
		reply_buf, err := BuildUDPResponse(server_udp_addr, reply_data, data_len)
		if err != nil {
			return err
		}
		client_udp_conn.WriteToUDP(reply_buf, clientAddr)
	}
}
func UDPConnect(conn net.Conn) error {
	// distribute port for UDP listening
	client_udp_conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return err
	}
	local_addr := client_udp_conn.LocalAddr().(*net.UDPAddr)
	port := local_addr.Port
	fmt.Println("UDP relay port: ", port)
	tcp_local := conn.LocalAddr().(*net.TCPAddr)
	relay_IP := tcp_local.IP.To4()
	if relay_IP == nil {
		client_udp_conn.Close()
		return fmt.Errorf("only ipv4 is supported now")
	}
	reply := []byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0x00, 0x00}
	copy(reply[4:8], relay_IP)
	binary.BigEndian.PutUint16(reply[8:10], uint16(port))
	_, err = conn.Write(reply)
	if err != nil {
		return err
	}
	fmt.Println("UDP relay listening on: ", client_udp_conn.LocalAddr())
	go func() {
		buf := make([]byte, 1)
		for {
			_, err = conn.Read(buf)
			if err != nil {
				client_udp_conn.Close()
				return
			}
		}
	}()
	err = HandleUDP(client_udp_conn)
	if err != nil && !errors.Is(err, net.ErrClosed) {
		return err
	}
	return nil
}
func Divide(conn net.Conn) error {
	// CONNECT request format
	// +----+-----+-------+------+----------+----------+
	// |VER | CMD |  RSV  | ATYP | DST.ADDR | DST.PORT |
	// +----+-----+-------+------+----------+----------+
	// | 1  |  1  | X'00' |  1   | Variable |    2     |
	// +----+-----+-------+------+----------+----------+

	buf := make([]byte, 3)
	_, err := io.ReadFull(conn, buf)
	if err != nil {
		return err
	}
	if buf[0] != byte(0x05) { // support SOCKS version
		return fmt.Errorf("unsupported socks version: %d", buf[0])
	}
	if buf[2] != byte(0x00) {
		return fmt.Errorf("wrong format in buf[2] %d", buf[2])
	}
	addr, err := GetAddr(conn)
	if err != nil {
		return err
	}
	if buf[1] == byte(0x01) { // TCP connect server

		return TCPConnect(conn, addr)
	}
	if buf[1] == byte(0x03) { // UDP ASSOCIATE
		return UDPConnect(conn)
	}
	return fmt.Errorf("we need right command in buf[1]")
}
func Pass(conn_1 net.Conn, conn_2 net.Conn, err_chan chan error) {
	_, err := io.Copy(conn_1, conn_2)
	err_chan <- err
}
func Transmit(conn_client net.Conn, conn_server net.Conn) error {
	ch := make(chan error, 2)
	go Pass(conn_client, conn_server, ch)
	go Pass(conn_server, conn_client, ch)
	err := <-ch
	conn_client.Close()
	conn_server.Close()
	return err
}
func HandleClient(conn net.Conn) {
	defer conn.Close()
	err := Methods(conn)
	if err != nil {
		fmt.Println("error occurs", err)
		return
	}
	err = Divide(conn)
	if err != nil {
		fmt.Println("error occurs: ", err)
	}
}

func main() {
	listener, err := net.Listen("tcp", ":1080") // listening on all 1080 port
	fmt.Println("SOCKS5 proxy listening on :1080")
	if err != nil {
		fmt.Println("error occurs", err)
		return
	}
	defer listener.Close()
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println("error occurs", err)
			continue
		}
		fmt.Println("Wellcome new client: ", conn.RemoteAddr())

		go HandleClient(conn) // listening on all 1080 port
	}
}
