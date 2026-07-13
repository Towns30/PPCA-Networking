package main

import (
	"flag"
	"fmt"
	"log"
	"net"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:9999", "UDP listen address")
	flag.Parse()

	// ResolveUDPAddr 同时支持 127.0.0.1:9999 和 [::1]:9999。
	addr, err := net.ResolveUDPAddr("udp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	buf := make([]byte, 65535) // UDP 数据报的最大长度不会超过 65535 字节。
	for {
		// ReadFromUDP 除了返回数据，也会返回发送者地址，回包时要用这个地址。
		n, client, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Fatal(err)
		}
		// 不修改收到的数据，直接把有效的前 n 个字节发回去。
		if _, err := conn.WriteToUDP(buf[:n], client); err != nil {
			log.Printf("client=%s error=%v", client, err)
			continue
		}
		fmt.Printf("client=%s data=%s\n", client, buf[:n])
	}
}
