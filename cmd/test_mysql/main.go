package main

import (
	"fmt"
	"io"
	"net"
	"time"

	"jianmen/internal/server/dbproxy"
)

func main() {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:13306", 10*time.Second)
	if err != nil {
		fmt.Println("dial:", err)
		return
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		fmt.Println("read handshake:", err)
		return
	}
	fmt.Printf("read %d bytes handshake\n", n)

	hsPayloadLen := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16
	if 4+hsPayloadLen > n {
		remaining := make([]byte, 4+hsPayloadLen-n)
		io.ReadFull(conn, remaining)
		buf = append(buf[:n], remaining...)
	}
	hsPayload := buf[4 : 4+hsPayloadLen]
	hs, err := dbproxy.ParseMySQLHandshake(hsPayload)
	if err != nil {
		fmt.Println("parse:", err)
		return
	}
	fmt.Printf("auth plugin: %s\n", hs.AuthPluginName)

	loginPkt := dbproxy.BuildMySQLUpstreamLogin(hs, "app", "app123", hs.AuthPluginName, 1)
	conn.Write(loginPkt)
	fmt.Println("login sent")

	n2, err := conn.Read(buf)
	if err != nil {
		fmt.Println("read auth response:", err)
		return
	}
	fmt.Printf("auth response: %d bytes, payload[4]=0x%02x\n", n2, buf[4])
	payloadLen := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16
	fmt.Printf("payload len: %d\n", payloadLen)

	if buf[4] == 0x00 {
		fmt.Println("AUTH OK!")
	} else if buf[4] == 0xff {
		fmt.Println("AUTH DENIED:", dbproxy.ParseMySQLErrorMessage(buf[4:4+payloadLen]))
	} else if buf[4] == 0xfe {
		fmt.Println("AUTH SWITCH requested")
	} else if buf[4] == 0x01 {
		fmt.Println("caching_sha2 fast auth, phase 2")
	}
}
