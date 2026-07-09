package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

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
func Connect(conn net.Conn) (net.Conn, error) {
	// CONNECT request format
	// +----+-----+-------+------+----------+----------+
	// |VER | CMD |  RSV  | ATYP | DST.ADDR | DST.PORT |
	// +----+-----+-------+------+----------+----------+
	// | 1  |  1  | X'00' |  1   | Variable |    2     |
	// +----+-----+-------+------+----------+----------+

	buf := make([]byte, 4)
	_, err := io.ReadFull(conn, buf)
	if err != nil {
		return nil, err
	}
	if buf[0] != byte(0x05) { // support SOCKS version
		return nil, fmt.Errorf("unsupported socks version: %d", buf[0])
	}
	if buf[1] != byte(0x01) {
		return nil, fmt.Errorf("we need CONNECT command in buf[1]")
	}
	if buf[2] != byte(0x00) {
		return nil, fmt.Errorf("wrong format in buf[2] %d", buf[2])
	}
	host := ""
	switch buf[3] {
	case 0x01: // IPv4，len = 4 byte
		addr_buf := make([]byte, 4)
		_, err = io.ReadFull(conn, addr_buf)
		if err != nil {
			return nil, err
		}
		host = net.IP(addr_buf).String()
	case 0x03: // domianname, we need to read domainname length followed
		domainlen_buf := make([]byte, 1)
		_, err = io.ReadFull(conn, domainlen_buf)
		if err != nil {
			return nil, err
		}
		domainlen := int(domainlen_buf[0])
		domain_buf := make([]byte, domainlen)
		_, err = io.ReadFull(conn, domain_buf)
		if err != nil {
			return nil, err
		}
		host = string(domain_buf)
	case 0x04: // IPv6，len = 16 byte
		addr_buf := make([]byte, 16)
		_, err = io.ReadFull(conn, addr_buf)
		if err != nil {
			return nil, err
		}
		host = net.IP(addr_buf).String()
	default:
		return nil, fmt.Errorf("Wrong address type %d", buf[3])
	}
	port_buf := make([]byte, 2)
	_, err = io.ReadFull(conn, port_buf)
	if err != nil {
		return nil, err
	}
	port := binary.BigEndian.Uint16(port_buf)
	addr := net.JoinHostPort(host, strconv.Itoa(int(port)))
	fmt.Println("connect target:", addr)

	// proxy connect server
	conn_server, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		conn.Write([]byte{0x05, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
		return nil, err
	}
	// SOCKS5 reply format
	// +----+-----+-------+------+----------+----------+
	// |VER | REP |  RSV  | ATYP | BND.ADDR | BND.PORT |
	// +----+-----+-------+------+----------+----------+
	// | 1  |  1  | X'00' |  1   | Variable |    2     |
	// +----+-----+-------+------+----------+----------+
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	return conn_server, nil
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
	conn_server, err := Connect(conn)
	if err != nil {
		fmt.Println("error occurs", err)
		return
	}
	err = Transmit(conn, conn_server)
	if err != nil {
		fmt.Println("error occurs", err)
		return
	}
}

func main() {
	listener, err := net.Listen("tcp", ":1080") // 监听本电脑所有IP地址的1080端口
	fmt.Println("SOCKS5 proxy listening on 127.0.0.1:1080")
	if err != nil {
		fmt.Println("error occurs", err)
		return
	}
	defer listener.Close()
	for {
		conn, err := listener.Accept()
		fmt.Println("Wellcome new client: ", conn.RemoteAddr())
		if err != nil {
			fmt.Println("error occurs", err)
			continue
		}
		go HandleClient(conn) // 开一个goroutine连接此用户
	}
}
