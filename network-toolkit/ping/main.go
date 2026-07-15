package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"os/signal"
	"time"
)

type Config struct {
	count_    int
	interval_ time.Duration
	size_     int
	timeout_  time.Duration
	host_     string
}

type ReplyPackage struct {
	receive_time_ time.Time
	addr_         *net.IPAddr
	ttl_          uint16
	seq_          uint16 // 0-based
	receive_type_ int
}

type Info struct {
	seq_          uint16 // 0-based
	send_time_    time.Time
	receive_time_ time.Time
	is_handled_   bool
}

type RTTStatistics struct {
	count_ int
	min_   time.Duration
	max_   time.Duration
	mean_  float64
	m2_    float64
}

func (statistics *RTTStatistics) Add(rtt time.Duration) {
	statistics.count_++
	if statistics.count_ == 1 {
		statistics.min_ = rtt
		statistics.max_ = rtt
	} else {
		if rtt < statistics.min_ {
			statistics.min_ = rtt
		}
		if rtt > statistics.max_ {
			statistics.max_ = rtt
		}
	}

	rtt_value := float64(rtt)
	delta := rtt_value - statistics.mean_
	statistics.mean_ += delta / float64(statistics.count_)
	delta_after_mean_update := rtt_value - statistics.mean_
	statistics.m2_ += delta * delta_after_mean_update
}

func (statistics RTTStatistics) StdDev() float64 {
	if statistics.count_ == 0 {
		return 0
	}
	return math.Sqrt(statistics.m2_ / float64(statistics.count_))
}

func ParseArgs() (Config, error) {
	count := flag.Int("c", -1, "send times, -1 represents infinity")
	interval := flag.Float64("i", 1.0, "send interval")
	size := flag.Int("s", 56, "payload size")
	timeout := flag.Float64("t", 2.0, "timeout interval")
	flag.Parse()
	if *count < -1 {
		return Config{}, fmt.Errorf("count must larger than -1")
	}
	if *interval <= 0 {
		return Config{}, fmt.Errorf("interval must larger than 0")
	}
	if *size < 0 {
		return Config{}, fmt.Errorf("size must larger than 0")
	}
	if *timeout <= 0 {
		return Config{}, fmt.Errorf("timeout must larger than 0")
	}
	if flag.NArg() != 1 {
		return Config{}, fmt.Errorf(
			"config length error",
		)
	}

	return Config{
		count_:    *count,
		interval_: time.Duration(*interval * float64(time.Second)),
		size_:     *size,
		timeout_:  time.Duration(*timeout * float64(time.Second)),
		host_:     flag.Arg(0),
	}, nil
}
func CheckSum(data []byte, data_len int) uint16 {
	var sum uint32 = 0
	for i := 0; i+1 < data_len; i += 2 {
		number := uint16(data[i])<<8 | uint16(data[i+1])
		sum += uint32(number)
	}
	if data_len%2 == 1 {
		number := uint16(data[data_len-1]) << 8
		sum += uint32(number)
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func BuildPackage(payload []byte, seq uint16) ([]byte, int, error) {
	// 0        1        2         3         4         5         6         7
	// +--------+--------+-------------------+-------------------+-------------------+
	// | Type=8 | Code=0 |     Checksum      |    Identifier     |  Sequence Number  |
	// +--------+--------+-------------------+-------------------+-------------------+
	// |                             Data ...
	// +-----------------------------------------------------------------------------
	package_buf := make([]byte, 65535)
	package_buf[0] = byte(0x08)                    // type
	package_buf[1] = byte(0x00)                    // code
	_ = copy(package_buf[2:4], []byte{0x00, 0x00}) // initial checksum
	identifier := uint16(os.Getpid() & 0xffff)     // identifier
	package_buf[4] = byte(identifier >> 8)
	package_buf[5] = byte(identifier)
	package_buf[6] = byte(seq >> 8) // sequence number
	package_buf[7] = byte(seq)
	_ = copy(package_buf[8:], []byte(payload)) // payload
	package_len := 8 + len(payload)            // checksum
	check_sum := CheckSum(package_buf, package_len)
	package_buf[2] = byte(check_sum >> 8)
	package_buf[3] = byte(check_sum)
	return package_buf, package_len, nil
}

func CheckPackage(receive_package []byte, package_len int, payload []byte) (uint16, uint16, int, error) {
	// 0        1        2         3         4         5         6         7
	// +--------+--------+-------------------+-------------------+-------------------+
	// | Type=8 | Code=0 |     Checksum      |    Identifier     |  Sequence Number  |
	// +--------+--------+-------------------+-------------------+-------------------+
	// |                             Data ...
	// +-----------------------------------------------------------------------------
	if package_len < 8 {
		return 0, 0, 0, fmt.Errorf("receive package format error")
	}
	if receive_package[1] != byte(0x00) {
		fmt.Println(receive_package)
		return 0, 0, 0, fmt.Errorf("receive package Code error")
	}
	if CheckSum(receive_package, package_len) != 0 {
		return 0, 0, 0, fmt.Errorf("Checksum error")
	}
	switch receive_package[0] {
	case 0:
		// Echo Reply
		indentifier := uint16(receive_package[4])<<8 | uint16(receive_package[5])
		seq := uint16(receive_package[6])<<8 | uint16(receive_package[7])
		if !bytes.Equal(receive_package[8:package_len], payload) {
			return 0, 0, 0, fmt.Errorf("payload is modified")
		}
		return indentifier, seq, 0, nil
	case 3:
		// Destination Unreachable
		indentifier := uint16(receive_package[4])<<8 | uint16(receive_package[5])
		seq := uint16(receive_package[6])<<8 | uint16(receive_package[7])
		if !bytes.Equal(receive_package[8:package_len], payload) {
			return 0, 0, 0, fmt.Errorf("payload is modified")
		}
		return indentifier, seq, 3, nil
	case 11:
		// Time Exceeded
		indentifier := uint16(receive_package[4])<<8 | uint16(receive_package[5])
		seq := uint16(receive_package[6])<<8 | uint16(receive_package[7])
		if !bytes.Equal(receive_package[8:package_len], payload) {
			return 0, 0, 0, fmt.Errorf("payload is modified")
		}
		return indentifier, seq, 11, nil
	default:
		// fmt.Println("the receive type is ", receive_package[0])
		return 0, 0, int(receive_package[0]), fmt.Errorf("receive package Type error")
	}

}

func SendPackage(conn *net.IPConn, payload []byte, addr *net.IPAddr, seq uint16) error {
	send_ICMP_package, n, err := BuildPackage(payload, seq)
	if err != nil {
		return err
	}
	conn.WriteTo([]byte(send_ICMP_package[:n]), addr)
	// fmt.Println("send package ", send_ICMP_package[:n], " to ", addr.String(), " seq = ", seq)
	return nil
}

func ParseIPv4Package(ipv4_package []byte) ([]byte, uint16, error) {
	// 0                 1                 2                 3                 4
	// +--------+--------+--------+--------+--------+--------+--------+--------+
	// |Version |  IHL   | DSCP/ECN        |       Total Length                 |
	// +--------+--------+--------+--------+--------+--------+--------+--------+
	// |          Identification           |Flags| Fragment Offset              |
	// +--------+--------+--------+--------+--------+--------+--------+--------+
	// |  TTL            |     Protocol    |     Header Checksum                                 |
	// +--------+--------+--------+--------+--------+--------+--------+--------+
	// |                  Source IP Address                                    |
	// +--------+--------+--------+--------+--------+--------+--------+--------+
	// |                Destination IP Address                                 |
	// +--------+--------+--------+--------+--------+--------+--------+--------+
	// |             Options（如果 IHL 大于 5）                                  |
	// +-----------------------------------------------------------------------+
	// |                  ICMP Header + Payload                                |
	// +-----------------------------------------------------------------------+
	if len(ipv4_package) < 20 {
		return nil, 0, fmt.Errorf("IPv4 ipv4_package too short")
	}
	version := ipv4_package[0] >> 4
	if version != 4 {
		return nil, 0, fmt.Errorf("not an IPv4 ipv4_package")
	}
	header_len := int(ipv4_package[0]&0x0f) * 4
	if header_len < 20 || header_len > len(ipv4_package) {
		return nil, 0, fmt.Errorf("invalid IPv4 header length")
	}
	ttl := uint16(ipv4_package[8])
	if ipv4_package[9] != 1 {
		return nil, 0, fmt.Errorf("not an ICMP ipv4_package")
	}
	return ipv4_package[header_len:], ttl, nil
}

func ReceivePackage(conn *net.IPConn, payload []byte, reply_ch chan ReplyPackage) {
	for {
		receive_buf := make([]byte, 65535)
		n, _, _, addr, err := conn.ReadMsgIP(receive_buf, nil)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				// fmt.Println("receive net closed")
				return
			}
			continue
		}
		receive_time := time.Now()
		receive_icmp_buf, ttl, err := ParseIPv4Package(receive_buf[:n])
		// fmt.Println("receive a package from ", addr.String(), " raw package is ", receive_buf[:n])
		indentifier, seq, receive_type, err := CheckPackage(receive_icmp_buf, len(receive_icmp_buf), payload)
		if receive_type == 8 {
			continue
		}
		if err != nil {
			fmt.Println(err)
			continue
		}
		if indentifier != uint16(os.Getpid()&0xffff) {
			// fmt.Println("receive other ping's message")
			continue
		}
		reply_ch <- ReplyPackage{receive_time_: receive_time, seq_: seq, ttl_: ttl, addr_: addr, receive_type_: receive_type}
	}
}

func ResolveHost(host string) (*net.IPAddr, error) {
	ip := net.ParseIP(host)
	if ip != nil {
		ipv4 := ip.To4()
		if ipv4 == nil {
			return nil, fmt.Errorf("we only support ipv4 for IP")
		}
		return &net.IPAddr{IP: ipv4}, nil
	}
	addr, err := net.ResolveIPAddr("ip4", host)
	if err != nil {
		return nil, fmt.Errorf("host error")
	}
	return addr, nil
}

func PrintSummary(send_cnt uint16, timeout_cnt uint16, host string, rtt_statistics RTTStatistics) {
	fmt.Println()
	fmt.Printf("--- %s ping statistics ---\n", host)
	fmt.Printf("%d packets transmitted, %d packets received, %.1f%% packet loss\n", send_cnt, send_cnt-timeout_cnt, float64(timeout_cnt)/float64(send_cnt)*100)
	if rtt_statistics.count_ == 0 {
		return
	}
	millisecond := float64(time.Millisecond)
	fmt.Printf(
		"round-trip min/avg/max/stddev = %.2f/%.2f/%.2f/%.2f ms\n",
		float64(rtt_statistics.min_)/millisecond,
		rtt_statistics.mean_/millisecond,
		float64(rtt_statistics.max_)/millisecond,
		rtt_statistics.StdDev()/millisecond,
	)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	config, err := ParseArgs()
	if err != nil {
		fmt.Println("parse args error")
		return
	}
	addr, err := ResolveHost(config.host_)
	if err != nil {
		fmt.Println("host error")
		return
	}
	conn, err := net.ListenIP("ip4:icmp", &net.IPAddr{IP: net.IPv4zero})
	if err != nil {
		fmt.Println("can't build conn to target server")
		return
	}
	payload := make([]byte, config.size_)
	for i := 0; i < config.size_; i++ {
		payload[i] = byte(0x66)
	}
	fmt.Printf(
		"PING %s (%s): %d data bytes\n",
		config.host_,
		addr.IP.String(),
		config.size_,
	)

	send_ticker := time.NewTicker(time.Duration(config.interval_))
	defer send_ticker.Stop()
	timeout_timer := time.NewTimer(time.Until(time.Now().Add(config.timeout_)))
	defer timeout_timer.Stop()
	var timeout_ticker_ch <-chan time.Time
	var timeout_ticker *time.Ticker
	defer func() {
		if timeout_ticker != nil {
			timeout_ticker.Stop()
		}
	}()
	states := make(map[uint16]Info)
	rtt_statistics := RTTStatistics{}
	timeout_seq := uint16(0)
	timeout_cnt := 0
	seq := uint16(0)
	// send first package
	states[seq] = Info{seq_: seq, send_time_: time.Now()}
	SendPackage(conn, payload, addr, seq)
	seq++

	reply_ch := make(chan ReplyPackage)
	go ReceivePackage(conn, payload, reply_ch)
	for {
		select {
		case <-timeout_timer.C:
			timeout_ticker = time.NewTicker(time.Duration(config.interval_))
			timeout_ticker_ch = timeout_ticker.C
			// check seq = 0
			state, exists := states[timeout_seq]
			if exists && !state.is_handled_ {
				state.is_handled_ = true
				states[timeout_seq] = state
				timeout_cnt++
			}
			timeout_seq++
		case <-ctx.Done():
			PrintSummary(seq, uint16(timeout_cnt), config.host_, rtt_statistics)
			return
		case <-send_ticker.C:
			send_time := time.Now()
			if config.count_ != -1 && int(seq) >= config.count_ {
				conn.Close()
				PrintSummary(seq, uint16(timeout_cnt), config.host_, rtt_statistics)
				return
			}
			states[seq] = Info{seq_: seq, send_time_: send_time, is_handled_: false}
			SendPackage(conn, payload, addr, seq)
			seq++
		case <-timeout_ticker_ch:
			state, exists := states[timeout_seq]
			if exists && state.is_handled_ == false {
				state.is_handled_ = true
				states[timeout_seq] = state
				timeout_cnt++
			}
			timeout_seq++
		case reply := <-reply_ch:
			info, exists := states[reply.seq_]
			if !exists {
				continue
			}
			// fix if timeout is a mistake
			// i.e. rtt is shorten than timeout
			if states[reply.seq_].is_handled_ {
				rtt := reply.receive_time_.Sub(info.send_time_)
				// fmt.Println("package received seq=", reply.seq_, "time=", rtt, "we suspect timeout")
				if rtt > config.timeout_ {
					// fmt.Println("package received seq=", reply.seq_, "time=", rtt, " but sadly timeout")
					continue
				} else {
					timeout_cnt--
				}
			}

			switch reply.receive_type_ {
			case 0:
				rtt := reply.receive_time_.Sub(info.send_time_)
				if rtt > config.timeout_ {
					info.is_handled_ = true
					states[reply.seq_] = info
					timeout_cnt++
					continue
				}
				rtt_statistics.Add(rtt)
				rtt_milliseconds := float64(rtt) / float64(time.Millisecond)
				fmt.Printf("%d bytes from %s: icmp_seq=%d ttl=%d time=%.2f ms\n", 8+config.size_, addr.IP.String(), reply.seq_, reply.ttl_, rtt_milliseconds)
				info.receive_time_ = reply.receive_time_
				info.is_handled_ = true
				states[reply.seq_] = info
			case 3:
				fmt.Printf("%d bytes from %s: icmp_seq=%d Destination Host Unreachable", 8+config.size_, addr.IP.String(), reply.seq_)
				info.is_handled_ = true
				states[reply.seq_] = info
			case 11:
				fmt.Printf("%d bytes from %s: icmp_seq=%d Time to live exceeded", 8+config.size_, addr.IP.String(), reply.seq_)
				info.is_handled_ = true
				states[reply.seq_] = info
			}
		}
	}
}
