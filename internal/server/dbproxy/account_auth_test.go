package dbproxy

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net"
	"strings"
	"testing"
)

func TestMySQLLoginParserObservesUser(t *testing.T) {
	parser := &MySQLLoginParser{}
	observation, ready, err := parser.Observe(mysqlLoginPacket("app"))
	if err != nil {
		t.Fatalf("Observe returned error: %v", err)
	}
	if !ready || observation.User != "app" || !observation.MetadataVisible {
		t.Fatalf("unexpected observation ready=%v observation=%#v", ready, observation)
	}
}

func TestPostgresLoginParserObservesUser(t *testing.T) {
	parser := &postgresLoginParser{}
	observation, ready, err := parser.Observe(postgresStartupPacket("app", "appdb"))
	if err != nil {
		t.Fatalf("Observe returned error: %v", err)
	}
	if !ready || observation.User != "app" || observation.Database != "appdb" || !observation.MetadataVisible {
		t.Fatalf("unexpected observation ready=%v observation=%#v", ready, observation)
	}
}

func TestPostgresLoginParserAllowsTLSWhenMetadataHidden(t *testing.T) {
	parser := &postgresLoginParser{}
	observation, ready, err := parser.Observe(postgresSSLRequestPacket())
	if err != nil {
		t.Fatalf("Observe returned error: %v", err)
	}
	if !ready || !observation.TLSRequested || observation.MetadataVisible || observation.Observation != "hidden_by_tls" {
		t.Fatalf("unexpected observation ready=%v observation=%#v", ready, observation)
	}
}

func TestFakeMySQLHandshakeAdvertisesCompleteAuthPluginName(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	errCh := make(chan error, 1)
	go func() {
		_, err := sendFakeMySQLHandshake(server)
		errCh <- err
	}()

	packet, err := readMySQLPacket(client)
	if err != nil {
		t.Fatalf("read fake handshake: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("send fake handshake: %v", err)
	}

	handshake, err := ParseMySQLHandshake(packet.payload)
	if err != nil {
		t.Fatalf("parse fake handshake: %v", err)
	}
	if handshake.AuthPluginName != "mysql_native_password" {
		t.Fatalf("auth plugin = %q, want mysql_native_password", handshake.AuthPluginName)
	}
}

func TestGenerateMySQLAuthSaltUsesConnectorCompatibleASCII(t *testing.T) {
	for iteration := 0; iteration < 100; iteration++ {
		salt, err := generateMySQLAuthSalt(20)
		if err != nil {
			t.Fatalf("generate salt: %v", err)
		}
		if len(salt) != 20 {
			t.Fatalf("salt length = %d, want 20", len(salt))
		}
		for _, value := range salt {
			if !strings.ContainsRune(mysqlAuthSaltAlphabet, rune(value)) {
				t.Fatalf("salt contains byte 0x%02x outside the safe alphabet", value)
			}
		}
	}
}

func TestWriteMySQLClientAuthErrorUsesHandshakeResponseSequence(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- writeMySQLClientAuthError(server, 2)
	}()

	header := make([]byte, 4)
	if _, err := io.ReadFull(client, header); err != nil {
		t.Fatalf("read auth error header: %v", err)
	}
	if header[3] != 2 {
		t.Fatalf("auth error sequence = %d, want 2", header[3])
	}
	payloadLength := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	payload := make([]byte, payloadLength)
	if _, err := io.ReadFull(client, payload); err != nil {
		t.Fatalf("read auth error payload: %v", err)
	}
	if len(payload) < 9 || payload[0] != 0xff || binary.LittleEndian.Uint16(payload[1:3]) != 1045 {
		t.Fatalf("unexpected auth error payload: %x", payload)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("write auth error: %v", err)
	}
}

func TestMySQLClientAuthResponseSequenceFollowsFinalLoginPacket(t *testing.T) {
	tests := []struct {
		name     string
		loginSeq byte
		want     byte
	}{
		{name: "plaintext login", loginSeq: 1, want: 2},
		{name: "TLS login after SSL request", loginSeq: 2, want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mysqlClientAuthResponseSequence(tt.loginSeq); got != tt.want {
				t.Fatalf("mysqlClientAuthResponseSequence(%d) = %d, want %d", tt.loginSeq, got, tt.want)
			}
		})
	}
}

func TestBuildPostgresMD5PasswordResponseUsesUsernameSaltAndPassword(t *testing.T) {
	got := BuildPostgresPasswordResponse(5, "user_dba", "secret", []byte{1, 2, 3, 4})

	h1 := md5.Sum([]byte("secret" + "user_dba"))
	h1Hex := hex.EncodeToString(h1[:])
	h2Input := append([]byte(h1Hex), 1, 2, 3, 4)
	h2 := md5.Sum(h2Input)
	want := "md5" + hex.EncodeToString(h2[:])

	if got != want {
		t.Fatalf("password response = %q, want %q", got, want)
	}
}

func TestBuildPostgresUpstreamStartupMessageIncludesDatabase(t *testing.T) {
	msg := BuildPostgresUpstreamStartupMessage("user_dba", "appdb")
	params := postgresStartupParams(msg[8:])

	if params["user"] != "user_dba" {
		t.Fatalf("user = %q, want user_dba", params["user"])
	}
	if params["database"] != "appdb" {
		t.Fatalf("database = %q, want appdb", params["database"])
	}
}

func TestPostgresUpstreamDatabaseDefaultsWhenClientUsesProxyUsername(t *testing.T) {
	got := postgresUpstreamDatabase("D000200001")
	if got != "postgres" {
		t.Fatalf("database = %q, want postgres", got)
	}
}

func TestShouldForwardPostgresAuthMessageOnlyForAuthOk(t *testing.T) {
	md5Challenge := []byte{'R', 0, 0, 0, 12, 0, 0, 0, 5, 1, 2, 3, 4}
	if shouldForwardPostgresAuthMessage(md5Challenge) {
		t.Fatal("MD5 authentication challenge must not be forwarded to client")
	}

	authOK := []byte{'R', 0, 0, 0, 8, 0, 0, 0, 0}
	if !shouldForwardPostgresAuthMessage(authOK) {
		t.Fatal("AuthenticationOk should be forwarded to client")
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
