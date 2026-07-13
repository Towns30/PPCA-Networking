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

	addr, err := net.ResolveUDPAddr("udp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	buf := make([]byte, 65535)
	for {
		n, client, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Fatal(err)
		}
		if _, err := conn.WriteToUDP(buf[:n], client); err != nil {
			log.Printf("client=%s error=%v", client, err)
			continue
		}
		fmt.Printf("client=%s data=%s\n", client, buf[:n])
	}
}
