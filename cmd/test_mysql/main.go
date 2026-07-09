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
	conn.SetDeadline(time.Now().Add(15 * time.Second))

	buf := make([]byte, 65536)
	n, err := conn.Read(buf)
	if err != nil {
		fmt.Println("read handshake:", err)
		return
	}

	hsPayloadLen := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16
	if 4+hsPayloadLen > n {
		remaining := make([]byte, 4+hsPayloadLen-n)
		io.ReadFull(conn, remaining)
		buf = append(buf[:n], remaining...)
	}
	hsPayload := buf[4 : 4+hsPayloadLen]
	hs, _ := dbproxy.ParseMySQLHandshake(hsPayload)

	loginPkt := dbproxy.BuildMySQLUpstreamLogin(hs, "app", "app123", hs.AuthPluginName, 1)
	conn.Write(loginPkt)

	// Read auth result
	_, _ = conn.Read(buf)
	payloadLen := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16

	if buf[4] == 0xfe {
		// Auth switch - extract plugin name and auth data
		payload := buf[5 : 4+payloadLen]
		nullPos := 0
		for nullPos < len(payload) && payload[nullPos] != 0 {
			nullPos++
		}
		newPlugin := string(payload[:nullPos])
		authData := payload[nullPos+1:]
		fmt.Printf("Auth switch to %s, auth_data_len=%d\n", newPlugin, len(authData))

		// mysql_native_password uses 20-byte salt in auth switch
		// authData may have null terminator at end
		salt := authData
		if len(salt) > 0 && salt[len(salt)-1] == 0 {
			salt = salt[:len(salt)-1]
		}
		fmt.Printf("salt bytes (%d): %x\n", len(salt), salt)

		resp := dbproxy.BuildMySQLAuthResponse(newPlugin, "app123", salt)
		if resp == nil {
			fmt.Println("NULL auth response!")
			return
		}
		fmt.Printf("auth response len=%d\n", len(resp))

		// Build response packet (length 3 bytes, seq 3, payload)
		respPkt := make([]byte, 4+len(resp))
		respPkt[0] = byte(len(resp))
		respPkt[1] = byte(len(resp) >> 8)
		respPkt[2] = byte(len(resp) >> 16)
		respPkt[3] = 3 // client seq after auth switch
		copy(respPkt[4:], resp)
		conn.Write(respPkt)

		// Read final result
		n3, _ := conn.Read(buf)
		fmt.Printf("final response: %d bytes, payload[4]=0x%02x\n", n3, buf[4])
		payloadLen2 := int(buf[0]) | int(buf[1])<<8 | int(buf[2])<<16
		if buf[4] == 0x00 {
			fmt.Println("AUTH OK!")
		} else if buf[4] == 0xff {
			fmt.Println("AUTH DENIED:", dbproxy.ParseMySQLErrorMessage(buf[4:4+payloadLen2]))
		}
	} else if buf[4] == 0x00 {
		fmt.Println("AUTH OK! (no switch)")
	} else if buf[4] == 0xff {
		fmt.Println("AUTH DENIED:", dbproxy.ParseMySQLErrorMessage(buf[4:4+payloadLen]))
	}
}
