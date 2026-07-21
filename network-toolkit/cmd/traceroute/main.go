package main

import (
	"bytes"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"time"

	"golang.org/x/net/ipv4"
)

type Mode int

const (
	ModeUDP Mode = iota
	ModeICMP
)

type Config struct {
	max_hops_  int
	nqueries_  int
	timeout_   time.Duration
	mode_      Mode
	first_ttl_ int
	host_      string
}

type QueryInfo struct {
	source_IP_    *net.IP
	receive_time_ time.Time
}

type MesInfo struct {
	ttl_   int
	query_ int
}

type ReplyInfo struct {
	source_IP_    *net.IP
	ttl_          int
	query_        int
	receive_time_ time.Time
	type_         int // 0: Time Exceeded; 1: destination reached
}

func ParseArgs() (Config, error) {
	max_hops := flag.Int("m", 30, "maximum number of hops")
	nqueries := flag.Int("q", 3, "number of probes per hop")
	timeout := flag.Float64("w", 3.0, "timeout in seconds")
	useICMP := flag.Bool("I", false, "use ICMP Echo mode")
	first_ttl := flag.Int("f", 1, "starting TTL")
	flag.Parse()

	if *max_hops < 1 || *max_hops > 255 {
		return Config{}, fmt.Errorf("max hops must be between 1 and 255")
	}
	if *nqueries <= 0 {
		return Config{}, fmt.Errorf("number of queries must be larger than 0")
	}
	if math.IsNaN(*timeout) || math.IsInf(*timeout, 0) || *timeout <= 0 {
		return Config{}, fmt.Errorf("timeout must be a finite number larger than 0")
	}
	if *first_ttl < 1 || *first_ttl > *max_hops {
		return Config{}, fmt.Errorf("first TTL must be between 1 and max hops")
	}
	if flag.NArg() != 1 {
		return Config{}, fmt.Errorf("config length error")
	}

	mode := ModeUDP
	if *useICMP {
		mode = ModeICMP
	}

	return Config{
		max_hops_:  *max_hops,
		nqueries_:  *nqueries,
		timeout_:   time.Duration(*timeout * float64(time.Second)),
		mode_:      mode,
		first_ttl_: *first_ttl,
		host_:      flag.Arg(0),
	}, nil
}

func CheckPackage(mode Mode, receive_package []byte, package_len int, payload []byte, nqueries int) (int, int, int, error) { // return (receive_type, ttl, query, err)
	// UDP mode: the received packet is either ICMP Time Exceeded or ICMP
	// Destination Unreachable. Both cases have the same layout; only the ICMP
	// Type at the beginning is different:
	//
	// byte offset        0               1               2               3
	//                   +---------------+---------------+-------------------------------+
	// ICMP          0   | Type=11 or 3  |     Code      |           Checksum            |
	//                   +---------------+---------------+-------------------------------+
	//               4   |                            Unused                             |
	//                   +-------+-------+---------------+-------------------------------+
	// Original IPv4 8   |Version|  IHL  |   DSCP/ECN    |         Total Length          |
	//                   +-------+-------+---------------+-----+-------------------------+
	//              12   |        Identification         |Flags|     Fragment Offset     |
	//                   +---------------+---------------+-------------------------------+
	//              16   |      TTL      | Protocol=17   |        Header Checksum        |
	//                   +---------------+---------------+-------------------------------+
	//              20   |                        Source Address                         |
	//                   +---------------------------------------------------------------+
	//              24   |                     Destination Address                       |
	//                   +---------------------------------------------------------------+
	//              28   |          Options + Padding (present only if IHL > 5)         |
	//                   +-------------------------------+-------------------------------+
	// Original UDP      |
	//       8 + IHL*4   |          Source Port          |       Destination Port        |
	//                   +-------------------------------+-------------------------------+
	//                   |            Length             |           Checksum            |
	//                   +-------------------------------+-------------------------------+
	//                   |                       Original UDP Payload                    |
	//                   +---------------------------------------------------------------+
	//
	// The ICMP header is always 8 bytes long, so receive_package[8] is the first
	// byte of the original IPv4 header. The high 4 bits of this byte contain the
	// Version and the low 4 bits contain the IHL. IHL is measured in 4-byte
	// words, so:
	//
	//     ihl               = receive_package[8] & 0x0f
	//     ip_header_length  = int(ihl) * 4
	//     udp_header_start  = 8 + ip_header_length
	//
	// For example, if the first byte is 0x45, IHL is 5, the IPv4 header is 20
	// bytes long, and the UDP header starts at byte 28 of the complete ICMP
	// packet. If IPv4 options are present, the UDP header starts later. Finally,
	// ttl and query are decoded from the UDP Destination Port, while receive_type
	// is determined from the ICMP Type.
	if mode == ModeUDP {
		if package_len < 0 || package_len > len(receive_package) {
			return 0, 0, 0, fmt.Errorf("invalid received package length")
		}
		if nqueries <= 0 {
			return 0, 0, 0, fmt.Errorf("number of queries must be larger than 0")
		}
		if len(payload) > 65535-8 {
			return 0, 0, 0, fmt.Errorf("UDP payload is too large")
		}

		packet := receive_package[:package_len]
		const icmp_header_len = 8
		const min_ipv4_header_len = 20
		const udp_header_len = 8
		if len(packet) < icmp_header_len+min_ipv4_header_len+udp_header_len {
			return 0, 0, 0, fmt.Errorf("received ICMP package is too short")
		}

		receive_type := 0
		switch packet[0] {
		case 11: // Time Exceeded
			if packet[1] != 0 { // TTL exceeded in transit
				return 0, 0, 0, fmt.Errorf("unexpected ICMP Time Exceeded code %d", packet[1])
			}
		case 3: // Destination Unreachable
			receive_type = 1
		default:
			return 0, 0, 0, fmt.Errorf("unexpected ICMP type %d", packet[0])
		}
		if CheckSum(packet, len(packet)) != 0 {
			return 0, 0, 0, fmt.Errorf("ICMP checksum error")
		}

		quoted_ip_start := icmp_header_len
		quoted_ip := packet[quoted_ip_start:]
		version_and_ihl := quoted_ip[0]
		version := version_and_ihl >> 4
		if version != 4 {
			return 0, 0, 0, fmt.Errorf("quoted packet is not IPv4")
		}
		// IHL is stored in the low 4 bits and is measured in 4-byte words.
		// It cannot be assumed to be 20 bytes because IPv4 options may exist.
		ihl := version_and_ihl & 0x0f
		ip_header_len := int(ihl) * 4
		if ip_header_len < min_ipv4_header_len || ip_header_len+udp_header_len > len(quoted_ip) {
			return 0, 0, 0, fmt.Errorf("invalid quoted IPv4 header length")
		}
		if quoted_ip[9] != 17 { // IP protocol number for UDP
			return 0, 0, 0, fmt.Errorf("quoted packet is not UDP")
		}

		// quoted_ip no longer contains the 8-byte ICMP header, so ip_header_len
		// is the UDP header offset in quoted_ip. Its offset in packet would be
		// quoted_ip_start + ip_header_len.
		udp_header_start := ip_header_len
		udp_header := quoted_ip[udp_header_start : udp_header_start+udp_header_len]
		udp_length := int(udp_header[4])<<8 | int(udp_header[5])
		if udp_length != udp_header_len+len(payload) {
			return 0, 0, 0, fmt.Errorf("quoted UDP length does not match probe payload")
		}

		// use dst port to calculate ttl and query
		const base_port = 33434
		destination_port := int(udp_header[2])<<8 | int(udp_header[3])
		probe_number := destination_port - base_port
		if probe_number < nqueries { // ttl starts at 1, so these ports were never sent
			return 0, 0, 0, fmt.Errorf("quoted UDP destination port is not a probe port")
		}
		ttl := probe_number / nqueries
		query := probe_number % nqueries
		if ttl < 1 || ttl > 255 {
			return 0, 0, 0, fmt.Errorf("decoded TTL is out of range")
		}
		return receive_type, ttl, query, nil
	}

	// ICMP mode has two possible packet layouts.
	//
	// Case 1: an intermediate router returns ICMP Time Exceeded. The outer ICMP
	// packet contains the original IPv4 header and the beginning of the original
	// ICMP Echo Request:
	//
	// byte offset        0               1               2               3
	//                   +---------------+---------------+-------------------------------+
	// ICMP Error    0   |    Type=11    |    Code=0     |           Checksum            |
	//                   +---------------+---------------+-------------------------------+
	//               4   |                            Unused                             |
	//                   +-------+-------+---------------+-------------------------------+
	// Original IPv4 8   |Version|  IHL  |   DSCP/ECN    |         Total Length          |
	//                   +-------+-------+---------------+-----+-------------------------+
	//              12   |        Identification         |Flags|     Fragment Offset     |
	//                   +---------------+---------------+-------------------------------+
	//              16   |      TTL      |  Protocol=1   |        Header Checksum        |
	//                   +---------------+---------------+-------------------------------+
	//              20   |                        Source Address                         |
	//                   +---------------------------------------------------------------+
	//              24   |                     Destination Address                       |
	//                   +---------------------------------------------------------------+
	//              28   |          Options + Padding (present only if IHL > 5)         |
	//                   +---------------+---------------+-------------------------------+
	// Original ICMP     |
	//       8 + IHL*4   |    Type=8     |    Code=0     |           Checksum            |
	// Echo Request      +-------------------------------+-------------------------------+
	//                   |          Identifier           |        Sequence Number        |
	//                   +-------------------------------+-------------------------------+
	//                   |                       Original ICMP Payload                   |
	//                   +---------------------------------------------------------------+
	//
	// Case 2: the destination returns an ICMP Echo Reply directly, so there is no
	// quoted IPv4 header between the ICMP header and its payload:
	//
	// byte offset        0               1               2               3
	//                   +---------------+---------------+-------------------------------+
	// ICMP Reply    0   |    Type=0     |    Code=0     |           Checksum            |
	//                   +-------------------------------+-------------------------------+
	//               4   |          Identifier           |        Sequence Number        |
	//                   +-------------------------------+-------------------------------+
	//               8   |                              Payload                          |
	//                   +---------------------------------------------------------------+
	//
	// For Time Exceeded, the original ICMP header starts after the outer 8-byte
	// ICMP header and the variable-length original IPv4 header:
	//
	//     ihl                  = receive_package[8] & 0x0f
	//     ip_header_length     = int(ihl) * 4
	//     original_icmp_start  = 8 + ip_header_length
	//
	// In both cases, Identifier is used to make sure the reply belongs to this
	// process. Sequence Number was encoded as ttl*nqueries+query when the probe
	// was sent, so it is used to recover ttl and query here.
	if mode == ModeICMP {
		if package_len < 0 || package_len > len(receive_package) {
			return 0, 0, 0, fmt.Errorf("invalid received package length")
		}
		if nqueries <= 0 {
			return 0, 0, 0, fmt.Errorf("number of queries must be larger than 0")
		}

		packet := receive_package[:package_len]
		const icmp_header_len = 8
		const min_ipv4_header_len = 20
		if len(packet) < icmp_header_len {
			return 0, 0, 0, fmt.Errorf("received ICMP package is too short")
		}
		if CheckSum(packet, len(packet)) != 0 {
			return 0, 0, 0, fmt.Errorf("ICMP checksum error")
		}

		expected_identifier := uint16(os.Getpid() & 0xffff)
		sequence_number := 0
		receive_type := 0

		switch packet[0] {
		case 0: // Echo Reply from the destination
			if packet[1] != 0 {
				return 0, 0, 0, fmt.Errorf("unexpected ICMP Echo Reply code %d", packet[1])
			}
			// check identifier
			identifier := uint16(packet[4])<<8 | uint16(packet[5])
			if identifier != expected_identifier {
				return 0, 0, 0, fmt.Errorf("ICMP identifier does not match this process")
			}
			// check payload
			if !bytes.Equal(packet[icmp_header_len:], payload) {
				return 0, 0, 0, fmt.Errorf("ICMP payload does not match probe payload")
			}
			sequence_number = int(packet[6])<<8 | int(packet[7])
			receive_type = 1

		case 11: // Time Exceeded from an intermediate router
			if packet[1] != 0 {
				return 0, 0, 0, fmt.Errorf("unexpected ICMP Time Exceeded code %d", packet[1])
			}
			if len(packet) < icmp_header_len+min_ipv4_header_len+icmp_header_len {
				return 0, 0, 0, fmt.Errorf("received ICMP Time Exceeded package is too short")
			}

			quoted_ip_start := icmp_header_len
			quoted_ip := packet[quoted_ip_start:]
			version_and_ihl := quoted_ip[0]
			version := version_and_ihl >> 4
			// check version
			if version != 4 {
				return 0, 0, 0, fmt.Errorf("quoted packet is not IPv4")
			}
			// calculate ip header len
			ihl := version_and_ihl & 0x0f
			ip_header_len := int(ihl) * 4
			if ip_header_len < min_ipv4_header_len || ip_header_len+icmp_header_len > len(quoted_ip) {
				return 0, 0, 0, fmt.Errorf("invalid quoted IPv4 header length")
			}
			if quoted_ip[9] != 1 { // IP protocol number for ICMP
				return 0, 0, 0, fmt.Errorf("quoted packet is not ICMP")
			}

			original_total_len := int(quoted_ip[2])<<8 | int(quoted_ip[3])
			expected_total_len := ip_header_len + icmp_header_len + len(payload)
			if original_total_len != expected_total_len {
				return 0, 0, 0, fmt.Errorf("quoted IPv4 length does not match probe package")
			}

			original_icmp_start := ip_header_len
			original_icmp := quoted_ip[original_icmp_start:]
			if original_icmp[0] != 8 || original_icmp[1] != 0 {
				return 0, 0, 0, fmt.Errorf("quoted ICMP package is not an Echo Request")
			}
			// check identifier
			identifier := uint16(original_icmp[4])<<8 | uint16(original_icmp[5])
			if identifier != expected_identifier {
				return 0, 0, 0, fmt.Errorf("quoted ICMP identifier does not match this process")
			}
			// check payload
			quoted_payload := original_icmp[icmp_header_len:]
			if len(quoted_payload) > len(payload) || !bytes.Equal(quoted_payload, payload[:len(quoted_payload)]) {
				return 0, 0, 0, fmt.Errorf("quoted ICMP payload does not match probe payload")
			}
			sequence_number = int(original_icmp[6])<<8 | int(original_icmp[7])

		default:
			return 0, 0, 0, fmt.Errorf("unexpected ICMP type %d", packet[0])
		}

		probe_number := sequence_number
		if probe_number < nqueries {
			return 0, 0, 0, fmt.Errorf("ICMP sequence number is not a probe number")
		}
		ttl := probe_number / nqueries
		query := probe_number % nqueries
		if ttl < 1 || ttl > 255 {
			return 0, 0, 0, fmt.Errorf("decoded TTL is out of range")
		}
		return receive_type, ttl, query, nil
	}

	return 0, 0, 0, fmt.Errorf("unknown traceroute mode")
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

func BuildPackage(payload []byte, seq int) ([]byte, int, error) { // return (package, package_len, err)
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

func SendUDPPackages(udp_send_conn *net.UDPConn, addr *net.IPAddr, ttl int, nqueries int, payload []byte) error { // send all query for one ttl at one time
	for query := 0; query < nqueries; query++ {
		port := ttl*nqueries + query + 33434
		_, err := udp_send_conn.WriteToUDP(payload, &net.UDPAddr{IP: addr.IP, Port: port, Zone: "ip4"})
		if err != nil {
			return err
		}
	}
	return nil
}

func SendICMPPackages(icmp_send_conn *net.IPConn, addr *net.IPAddr, ttl int, nqueries int, payload []byte) error { // send all query for one ttl at one time
	for query := 0; query < nqueries; query++ {
		seq := ttl*nqueries + query
		send_icmp_package, n, err := BuildPackage(payload, seq)
		if err != nil {
			return err
		}
		icmp_send_conn.WriteTo(send_icmp_package[:n], addr)
	}
	return nil
}

func ParseIPv4Package(ipv4_package []byte) ([]byte, *net.IP, error) { // return
	// 0                 1                 2                 3                 4
	// +--------+--------+--------+--------+--------+--------+--------+--------+
	// |Version |  IHL   | DSCP/ECN        |       Total Length                 |
	// +--------+--------+--------+--------+--------+--------+--------+--------+
	// |          Identification           |Flags| Fragment Offset              |
	// +--------+--------+--------+--------+--------+--------+--------+--------+
	// |  TTL            |     Protocol    |     Header Checksum                 |
	// +--------+--------+--------+--------+--------+--------+--------+--------+
	// |                  Source IP Address                                    |
	// +--------+--------+--------+--------+--------+--------+--------+--------+
	// |                Destination IP Address                                 |
	// +--------+--------+--------+--------+--------+--------+--------+--------+
	// |             Options (if IHL is greater than 5) ...                     |
	// +------------------------------------------------------------------------+
	// |                  ICMP Header + Payload ...                             |
	// +------------------------------------------------------------------------+
	if len(ipv4_package) < 20 {
		return nil, nil, fmt.Errorf("IPv4 ipv4_package too short")
	}
	version := ipv4_package[0] >> 4
	if version != 4 {
		return nil, nil, fmt.Errorf("not an IPv4 ipv4_package")
	}
	header_len := int(ipv4_package[0]&0x0f) * 4
	if header_len < 20 || header_len > len(ipv4_package) {
		return nil, nil, fmt.Errorf("invalid IPv4 header length")
	}
	source_IP := append(net.IP(nil), ipv4_package[12:16]...)
	if ipv4_package[9] != 1 {
		return nil, nil, fmt.Errorf("not an ICMP ipv4_package")
	}
	return ipv4_package[header_len:], &source_IP, nil
}

func ReceivePackages(mode Mode, icmp_receive_conn *net.IPConn, udp_receive_conn *net.IPConn, ttl int, nqueries int, payload []byte, reply_ch chan ReplyInfo, stop_ch <-chan struct{}, done_ch chan<- struct{}) {
	defer close(done_ch)

	receive_conn := icmp_receive_conn
	if mode == ModeUDP {
		receive_conn = udp_receive_conn
	}
	for {
		select {
		case <-stop_ch:
			return
		default:
		}
		if err := receive_conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
			return
		}
		receive_buf := make([]byte, 65535)
		n, _, _, _, err := receive_conn.ReadMsgIP(receive_buf, nil)
		if err != nil {
			continue
		}
		receive_time := time.Now()

		// On Linux an IPv4 raw socket returns the outer IPv4 header as part of
		// the packet. CheckPackage parses an ICMP message, so strip that header
		// before handing the packet to it. Without this step packet[0] is usually
		// 0x45 (IPv4 + IHL), not ICMP Type 11/3, and every valid reply is dropped.
		icmp_packet, source_IP, err := ParseIPv4Package(receive_buf[:n])
		if err != nil {
			continue
		}
		receive_type, reply_ttl, query, err := CheckPackage(mode, icmp_packet, len(icmp_packet), payload, nqueries)
		if err != nil {
			continue
		}
		// A delayed reply from the preceding hop may still be queued in the raw
		// socket when the next receive loop starts.
		if reply_ttl != ttl {
			continue
		}
		select {
		case reply_ch <- ReplyInfo{source_IP_: source_IP, type_: receive_type, ttl_: reply_ttl, query_: query, receive_time_: receive_time}:
		case <-stop_ch:
			return
		}
	}
}

func PrintStates(ttl int, states map[int]QueryInfo, send_time time.Time, nqueries int) {
	fmt.Printf("%2d ", ttl)
	last_addr := ""
	for query := 0; query < nqueries; query++ {
		state, exists := states[query]
		if !exists || state.source_IP_ == nil || state.receive_time_.IsZero() {
			fmt.Print(" *")
			continue
		}
		addr := state.source_IP_.String()
		if addr != last_addr {
			fmt.Printf("  %s (%s)", addr, addr)
			last_addr = addr
		}
		rtt_milliseconds := float64(state.receive_time_.Sub(send_time)) / float64(time.Millisecond)
		fmt.Printf("  %.3f ms", rtt_milliseconds)
	}
	fmt.Println()
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

func main() {
	config, err := ParseArgs()
	if err != nil {
		fmt.Println(err)
		return
	}
	addr, err := ResolveHost(config.host_)
	if err != nil {
		fmt.Println(err)
		return
	}
	var udp_send_conn *net.UDPConn
	var icmp_conn *net.IPConn
	var udp_receive_conn *net.IPConn
	if config.mode_ == ModeUDP {
		udp_send_conn, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
		if err != nil {
			fmt.Println(err)
			return
		}
		defer udp_send_conn.Close()
		udp_receive_conn, err = net.ListenIP("ip4:icmp", &net.IPAddr{IP: net.IPv4zero})
		if err != nil {
			fmt.Println(err)
			return
		}
		defer udp_receive_conn.Close()
	} else {
		icmp_conn, err = net.ListenIP("ip4:icmp", &net.IPAddr{IP: net.IPv4zero})
		if err != nil {
			fmt.Println(err)
			return
		}
		defer icmp_conn.Close()
	}
	payload := make([]byte, 16)
	for i := 0; i < 16; i++ {
		payload[i] = byte(0x66)
	}
	fmt.Printf("traceroute to %s (%s), %d hops max, %d byte packets\n",
		config.host_, addr.IP.String(), config.max_hops_, 20+8+len(payload))
	arrive_flag := false
	for ttl := config.first_ttl_; ttl <= config.max_hops_ && !arrive_flag; ttl++ {
		states := make(map[int]QueryInfo)
		reply_ch := make(chan ReplyInfo)
		receive_stop_ch := make(chan struct{})
		receive_done_ch := make(chan struct{})

		// Start receiving before sending. More importantly, the preceding
		// iteration waits for its receiver to exit, so only one goroutine ever
		// reads from the raw ICMP socket and no reply can be stolen by an old hop.
		go ReceivePackages(config.mode_, icmp_conn, udp_receive_conn, ttl, config.nqueries_, payload, reply_ch, receive_stop_ch, receive_done_ch)

		// send
		send_time := time.Now()
		timeout_timer := time.NewTimer(config.timeout_)
		if config.mode_ == ModeUDP {
			ipv4.NewPacketConn(udp_send_conn).SetTTL(ttl)
			SendUDPPackages(udp_send_conn, addr, ttl, config.nqueries_, payload)
		} else {
			ipv4.NewPacketConn(icmp_conn).SetTTL(ttl)
			SendICMPPackages(icmp_conn, addr, ttl, config.nqueries_, payload)
		}
		// receive
		collecting := true
		for len(states) < config.nqueries_ && collecting {
			select {
			case <-timeout_timer.C:
				collecting = false
			case <-receive_done_ch:
				collecting = false
			case reply := <-reply_ch:
				if reply.type_ == 1 {
					arrive_flag = true // exit large routine
				}
				if _, exists := states[reply.query_]; exists {
					continue
				}
				states[reply.query_] = QueryInfo{source_IP_: reply.source_IP_, receive_time_: reply.receive_time_}
			}
		}
		close(receive_stop_ch)
		timeout_timer.Stop()
		// receive goroutine has been closed
		// defer close(done_ch) has been execute
		<-receive_done_ch
		PrintStates(ttl, states, send_time, config.nqueries_)
	}
}
