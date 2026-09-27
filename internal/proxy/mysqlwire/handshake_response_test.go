package mysqlwire

import (
	"encoding/binary"
	"testing"
)

func TestParseHandshakeReadsCachingSHA2PluginAndSalt(t *testing.T) {
	capabilities := ClientProtocol41 | ClientSecureConnection | ClientPluginAuth | ClientSSL
	payload := []byte{0x0a}
	payload = append(payload, []byte("8.0.39\x00")...)
	connectionID := make([]byte, 4)
	binary.LittleEndian.PutUint32(connectionID, 42)
	payload = append(payload, connectionID...)
	payload = append(payload, []byte("12345678")...)
	payload = append(payload, 0)
	lower := make([]byte, 2)
	binary.LittleEndian.PutUint16(lower, uint16(capabilities))
	payload = append(payload, lower...)
	payload = append(payload, 45)
	payload = append(payload, 0x02, 0x00)
	upper := make([]byte, 2)
	binary.LittleEndian.PutUint16(upper, uint16(capabilities>>16))
	payload = append(payload, upper...)
	payload = append(payload, 21)
	payload = append(payload, make([]byte, 10)...)
	payload = append(payload, []byte("abcdefghijkl\x00")...)
	payload = append(payload, []byte("caching_sha2_password\x00")...)

	handshake, err := ParseHandshake(payload)
	if err != nil {
		t.Fatalf("parse handshake: %v", err)
	}
	if handshake.ConnectionID != 42 ||
		handshake.AuthPluginName != "caching_sha2_password" ||
		string(handshake.AuthData) != "12345678abcdefghijkl" {
		t.Fatalf("unexpected handshake: %#v", handshake)
	}
}

func TestBuildHandshakeResponse41OmitsDatabaseFieldWithoutCapability(t *testing.T) {
	handshake := Handshake{
		CapabilityFlags: ClientProtocol41 | ClientSecureConnection | ClientPluginAuth | ClientSSL | ClientConnectWithDB,
		CharacterSet:    45,
		AuthPluginName:  "caching_sha2_password",
		AuthData:        []byte("12345678901234567890"),
	}
	packet, err := BuildHandshakeResponse41(
		handshake,
		LoginOptions{
			Username: "app", Password: "secret", AuthPlugin: handshake.AuthPluginName,
			Sequence: 2, TLS: true,
		},
	)
	if err != nil {
		t.Fatalf("build handshake response: %v", err)
	}
	payload := packet[4:]
	capabilities := binary.LittleEndian.Uint32(payload[:4])
	if capabilities&ClientConnectWithDB != 0 {
		t.Fatalf("empty database advertised CLIENT_CONNECT_WITH_DB: %#x", capabilities)
	}
	if capabilities&ClientSSL == 0 {
		t.Fatalf("TLS login omitted CLIENT_SSL: %#x", capabilities)
	}
	position := 32 + len("app") + 1
	authLength := int(payload[position])
	position += 1 + authLength
	if got := cStringAt(payload, position); got != handshake.AuthPluginName {
		t.Fatalf("field after auth response = %q, want plugin %q", got, handshake.AuthPluginName)
	}
}

func TestBuildHandshakeResponse41IncludesDatabaseOnlyWithCapability(t *testing.T) {
	handshake := Handshake{
		CapabilityFlags: ClientProtocol41 | ClientSecureConnection | ClientPluginAuth | ClientConnectWithDB,
		CharacterSet:    45,
		AuthPluginName:  "mysql_native_password",
		AuthData:        []byte("12345678901234567890"),
	}
	packet, err := BuildHandshakeResponse41(
		handshake,
		LoginOptions{
			Username: "app", Password: "secret", Database: "orders",
			AuthPlugin: handshake.AuthPluginName, Sequence: 1,
		},
	)
	if err != nil {
		t.Fatalf("build handshake response: %v", err)
	}
	payload := packet[4:]
	capabilities := binary.LittleEndian.Uint32(payload[:4])
	if capabilities&ClientConnectWithDB == 0 {
		t.Fatalf("database login omitted CLIENT_CONNECT_WITH_DB: %#x", capabilities)
	}
	position := 32 + len("app") + 1
	authLength := int(payload[position])
	position += 1 + authLength
	if got := cStringAt(payload, position); got != "orders" {
		t.Fatalf("database field = %q, want orders", got)
	}
	position += len("orders") + 1
	if got := cStringAt(payload, position); got != handshake.AuthPluginName {
		t.Fatalf("plugin field = %q, want %q", got, handshake.AuthPluginName)
	}
}

func TestBuildHandshakeResponse41RejectsDatabaseWithoutServerCapability(t *testing.T) {
	handshake := Handshake{
		CapabilityFlags: ClientProtocol41 | ClientSecureConnection | ClientPluginAuth,
		CharacterSet:    45,
		AuthPluginName:  "mysql_native_password",
		AuthData:        []byte("12345678901234567890"),
	}
	if _, err := BuildHandshakeResponse41(
		handshake,
		LoginOptions{
			Username: "app", Password: "secret", Database: "orders",
			AuthPlugin: handshake.AuthPluginName, Sequence: 1,
		},
	); err == nil {
		t.Fatal("database was silently omitted without CLIENT_CONNECT_WITH_DB")
	}
}

func cStringAt(payload []byte, position int) string {
	end := position
	for end < len(payload) && payload[end] != 0 {
		end++
	}
	return string(payload[position:end])
}
