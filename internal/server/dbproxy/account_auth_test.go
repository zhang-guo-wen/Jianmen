//go:build ignore

package dbproxy

import (
	"encoding/binary"
	"testing"
)

func TestMySQLAccountAuthAcceptsAllowedUser(t *testing.T) {
	auth, err := newAccountAuth("mysql", []string{"app"})
	if err != nil {
		t.Fatalf("newAccountAuth returned error: %v", err)
	}
	ready, err := auth.ObserveClientBytes(mysqlLoginPacket("app"))
	if err != nil {
		t.Fatalf("ObserveClientBytes returned error: %v", err)
	}
	if !ready || !auth.Ready() || auth.Observation().User != "app" {
		t.Fatalf("unexpected auth state ready=%v user=%q", ready, auth.Observation().User)
	}
}

func TestMySQLAccountAuthRejectsDeniedUser(t *testing.T) {
	auth, err := newAccountAuth("mysql", []string{"app"})
	if err != nil {
		t.Fatalf("newAccountAuth returned error: %v", err)
	}
	if _, err := auth.ObserveClientBytes(mysqlLoginPacket("root")); err == nil {
		t.Fatal("ObserveClientBytes accepted denied user")
	}
}

func TestPostgresAccountAuthAcceptsAllowedUser(t *testing.T) {
	auth, err := newAccountAuth("postgres", []string{"app"})
	if err != nil {
		t.Fatalf("newAccountAuth returned error: %v", err)
	}
	ready, err := auth.ObserveClientBytes(postgresStartupPacket("app", "appdb"))
	if err != nil {
		t.Fatalf("ObserveClientBytes returned error: %v", err)
	}
	if !ready || !auth.Ready() || auth.Observation().User != "app" {
		t.Fatalf("unexpected auth state ready=%v user=%q", ready, auth.Observation().User)
	}
}

func TestPostgresAccountAuthAllowsTLSWhenNotEnforced(t *testing.T) {
	auth, err := newAccountAuth("postgres", nil)
	if err != nil {
		t.Fatalf("newAccountAuth returned error: %v", err)
	}
	ready, err := auth.ObserveClientBytes(postgresSSLRequestPacket())
	if err != nil {
		t.Fatalf("ObserveClientBytes returned error: %v", err)
	}
	if !ready || !auth.Ready() || auth.Observation().Observation != "hidden_by_tls" {
		t.Fatalf("unexpected auth state ready=%v observation=%#v", ready, auth.Observation())
	}
}

func TestPostgresAccountAuthRejectsDeniedUser(t *testing.T) {
	auth, err := newAccountAuth("postgres", []string{"app"})
	if err != nil {
		t.Fatalf("newAccountAuth returned error: %v", err)
	}
	if _, err := auth.ObserveClientBytes(postgresStartupPacket("postgres", "appdb")); err == nil {
		t.Fatal("ObserveClientBytes accepted denied user")
	}
}

func mysqlLoginPacket(username string) []byte {
	payload := make([]byte, 0, 64)
	capabilities := uint32(mysqlClientProtocol41)
	cap := make([]byte, 4)
	binary.LittleEndian.PutUint32(cap, capabilities)
	payload = append(payload, cap...)
	payload = append(payload, 0, 0, 0, 1)
	payload = append(payload, 33)
	payload = append(payload, make([]byte, 23)...)
	payload = append(payload, []byte(username)...)
	payload = append(payload, 0)

	packet := make([]byte, 4, 4+len(payload))
	packet[0] = byte(len(payload))
	packet[1] = byte(len(payload) >> 8)
	packet[2] = byte(len(payload) >> 16)
	packet[3] = 1
	packet = append(packet, payload...)
	return packet
}

func postgresStartupPacket(username, database string) []byte {
	payload := make([]byte, 0, 64)
	protocol := make([]byte, 4)
	binary.BigEndian.PutUint32(protocol, 196608)
	payload = append(payload, protocol...)
	payload = append(payload, []byte("user")...)
	payload = append(payload, 0)
	payload = append(payload, []byte(username)...)
	payload = append(payload, 0)
	payload = append(payload, []byte("database")...)
	payload = append(payload, 0)
	payload = append(payload, []byte(database)...)
	payload = append(payload, 0)
	payload = append(payload, 0)

	packet := make([]byte, 4, 4+len(payload))
	binary.BigEndian.PutUint32(packet[:4], uint32(4+len(payload)))
	packet = append(packet, payload...)
	return packet
}

func postgresSSLRequestPacket() []byte {
	packet := make([]byte, 8)
	binary.BigEndian.PutUint32(packet[:4], 8)
	binary.BigEndian.PutUint32(packet[4:8], 80877103)
	return packet
}
