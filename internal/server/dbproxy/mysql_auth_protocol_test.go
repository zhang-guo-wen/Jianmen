package dbproxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"log/slog"
	"net"
	"os"
	"strings"
	"testing"

	"jianmen/internal/dbtls"
	"jianmen/internal/model"
)

func TestMySQLCachingSHA2AuthMoreDataParsesCombinedAndSplitPackets(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    byte
		ok      bool
	}{
		{name: "combined fast authentication", payload: []byte{0x01, 0x03}, want: 0x03, ok: true},
		{name: "combined full authentication", payload: []byte{0x01, 0x04}, want: 0x04, ok: true},
		{name: "split fast authentication continuation", payload: []byte{0x03}, want: 0x03, ok: true},
		{name: "split full authentication continuation", payload: []byte{0x04}, want: 0x04, ok: true},
		{name: "incomplete marker", payload: []byte{0x01}, ok: false},
		{name: "unrelated packet", payload: []byte{0x00}, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := MySQLCachingSHA2AuthMoreData(tt.payload)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("MySQLCachingSHA2AuthMoreData(%x) = (%#x, %v), want (%#x, %v)", tt.payload, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestMySQLResponseSequenceFollowsServerPacket(t *testing.T) {
	if got := mysqlResponseSequence(3); got != 4 {
		t.Fatalf("mysqlResponseSequence(3) = %d, want 4", got)
	}
	if got := mysqlResponseSequence(255); got != 0 {
		t.Fatalf("mysqlResponseSequence(255) = %d, want 0", got)
	}
}

func TestMySQLTLSLoginCarriesSSLClientCapability(t *testing.T) {
	handshake := &MySQLHandshake{
		CharacterSet: 45, AuthPluginName: "caching_sha2_password",
		AuthData: []byte("12345678901234567890"),
		CapabilityFlags: mysqlClientProtocol41 | mysqlClientSecureConnection |
			mysqlClientPluginAuth | mysqlClientSSL | mysqlClientConnectWithDB,
	}
	packet, err := BuildMySQLUpstreamLogin(handshake, "app", "secret", handshake.AuthPluginName, 2)
	if err != nil {
		t.Fatalf("build upstream login: %v", err)
	}
	if len(packet) < 8 {
		t.Fatalf("login packet too short: %x", packet)
	}
	capabilities := binary.LittleEndian.Uint32(packet[4:8])
	if capabilities&mysqlClientSSL == 0 {
		t.Fatalf("TLS login capabilities = 0x%x, missing CLIENT_SSL", capabilities)
	}
	if maximumPacket := binary.LittleEndian.Uint32(packet[8:12]); maximumPacket != 1<<24 {
		t.Fatalf("TLS login max packet = %d, want %d to match SSLRequest", maximumPacket, 1<<24)
	}
	if capabilities&mysqlClientConnectWithDB != 0 {
		t.Fatalf("login without database advertised CLIENT_CONNECT_WITH_DB: %#x", capabilities)
	}
	payload := packet[4:]
	position := 32 + len("app") + 1
	authLength := int(payload[position])
	position += 1 + authLength
	plugin, _ := splitCString(payload[position:])
	if string(plugin) != handshake.AuthPluginName {
		t.Fatalf("field after auth response = %q, want plugin %q", plugin, handshake.AuthPluginName)
	}
}

func TestMySQLGatewayAndUpstreamLoginPreserveInitialDatabase(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	writeResult := make(chan error, 1)
	go func() {
		_, err := sendFakeMySQLHandshake(server)
		writeResult <- err
	}()
	greeting, err := readMySQLPacket(client)
	if err != nil {
		t.Fatalf("read fake MySQL handshake: %v", err)
	}
	if err := <-writeResult; err != nil {
		t.Fatalf("write fake MySQL handshake: %v", err)
	}
	fakeHandshake, err := ParseMySQLHandshake(greeting.payload)
	if err != nil {
		t.Fatalf("parse fake MySQL handshake: %v", err)
	}
	if fakeHandshake.CapabilityFlags&mysqlClientConnectWithDB == 0 {
		t.Fatal("fake MySQL handshake did not advertise CLIENT_CONNECT_WITH_DB")
	}

	upstreamHandshake := &MySQLHandshake{
		CapabilityFlags: mysqlClientProtocol41 | mysqlClientSecureConnection |
			mysqlClientPluginAuth | mysqlClientSSL | mysqlClientConnectWithDB,
		CharacterSet:   45,
		AuthPluginName: "caching_sha2_password",
		AuthData:       []byte("12345678901234567890"),
	}
	login, err := buildMySQLUpstreamLogin(
		upstreamHandshake,
		"app",
		"secret",
		" appdb ",
		upstreamHandshake.AuthPluginName,
		2,
	)
	if err != nil {
		t.Fatalf("build MySQL upstream database login: %v", err)
	}
	parser := &MySQLLoginParser{}
	observation, ready, err := parser.Observe(login)
	if err != nil {
		t.Fatalf("parse MySQL upstream database login: %v", err)
	}
	if !ready || observation.User != "app" || observation.Database != " appdb " {
		t.Fatalf("unexpected MySQL upstream login observation: ready=%v observation=%#v", ready, observation)
	}
}

func TestBuildMySQLUpstreamLoginRejectsInvalidHandshake(t *testing.T) {
	base := &MySQLHandshake{
		ProtocolVersion: 10,
		CharacterSet:    45,
		AuthPluginName:  "mysql_native_password",
		AuthData:        []byte("12345678901234567890"),
		CapabilityFlags: mysqlClientProtocol41 | mysqlClientSecureConnection | mysqlClientPluginAuth,
	}
	tests := []struct {
		name      string
		handshake *MySQLHandshake
	}{
		{
			name: "missing protocol 41",
			handshake: &MySQLHandshake{
				CharacterSet: 45, AuthPluginName: base.AuthPluginName, AuthData: base.AuthData,
				CapabilityFlags: mysqlClientSecureConnection | mysqlClientPluginAuth,
			},
		},
		{
			name: "missing secure connection",
			handshake: &MySQLHandshake{
				CharacterSet: 45, AuthPluginName: base.AuthPluginName, AuthData: base.AuthData,
				CapabilityFlags: mysqlClientProtocol41 | mysqlClientPluginAuth,
			},
		},
		{
			name: "unknown authentication plugin",
			handshake: &MySQLHandshake{
				CharacterSet: 45, AuthPluginName: "unsupported", AuthData: base.AuthData,
				CapabilityFlags: base.CapabilityFlags,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packet, err := BuildMySQLUpstreamLogin(tt.handshake, "app", "secret", tt.handshake.AuthPluginName, 1)
			if err == nil {
				t.Fatalf("BuildMySQLUpstreamLogin() = %x, nil error; want fail-closed error", packet)
			}
			if packet != nil {
				t.Fatalf("BuildMySQLUpstreamLogin() packet = %x on error, want nil", packet)
			}
		})
	}
}

func TestMySQLAuthSwitchResponseUsesServerPacketSequence(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	serverResult := make(chan error, 1)
	go func() {
		packet, err := readMySQLPacket(server)
		if err != nil {
			serverResult <- err
			return
		}
		if packet.seq != 4 {
			serverResult <- errUnexpectedMySQLFullAuthPacket(packet.seq, packet.payload)
			return
		}
		_, err = server.Write(mysqlPacketWithSeq(5, []byte{0x00}))
		serverResult <- err
	}()
	request := &mysqlPacket{seq: 3, payload: append([]byte{0xfe}, append([]byte("mysql_native_password\x00"), []byte("12345678901234567890")...)...)}
	response, err := (&Gateway{}).handleMySQLAuthSwitch(client, &model.DatabaseAccount{Password: model.NewEncryptedField("secret")}, nil, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.payload) != 1 || response.payload[0] != 0x00 {
		t.Fatalf("final packet = %x, want OK", response.payload)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestMySQLAuthSwitchRejectsMissingPluginTerminator(t *testing.T) {
	request := &mysqlPacket{seq: 3, payload: []byte{0xfe, 'b', 'a', 'd'}}
	if _, err := (&Gateway{}).handleMySQLAuthSwitch(
		nil,
		&model.DatabaseAccount{Password: model.NewEncryptedField("secret")},
		nil,
		request,
	); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("handleMySQLAuthSwitch() error = %v, want malformed request", err)
	}
}

func TestMySQLAuthSwitchReadsFragmentedServerResponse(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	serverResult := make(chan error, 1)
	go func() {
		if _, err := readMySQLPacket(server); err != nil {
			serverResult <- err
			return
		}
		writeFragments(server, mysqlPacketWithSeq(5, []byte{0x00}), 1, 1, 2)
		serverResult <- nil
	}()
	request := &mysqlPacket{
		seq:     3,
		payload: append([]byte{0xfe}, append([]byte("mysql_native_password\x00"), []byte("12345678901234567890")...)...),
	}
	response, err := (&Gateway{}).handleMySQLAuthSwitch(
		client,
		&model.DatabaseAccount{Password: model.NewEncryptedField("secret")},
		nil,
		request,
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.seq != 5 || len(response.payload) != 1 || response.payload[0] != 0 {
		t.Fatalf("response = seq %d payload %x", response.seq, response.payload)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestMySQLCachingSHA2FullAuthWritesTLSProtectedPasswordAfterServerSequence(t *testing.T) {
	certificateFile, keyFile := writeListenerCertificate(t)
	certificatePEM, err := os.ReadFile(certificateFile)
	if err != nil {
		t.Fatal(err)
	}
	serverRaw, clientRaw := net.Pipe()
	defer serverRaw.Close()
	defer clientRaw.Close()
	serverResult := make(chan error, 1)
	go func() {
		certificate, loadErr := tls.LoadX509KeyPair(certificateFile, keyFile)
		if loadErr != nil {
			serverResult <- loadErr
			return
		}
		server := tls.Server(serverRaw, &tls.Config{Certificates: []tls.Certificate{certificate}})
		if handshakeErr := server.Handshake(); handshakeErr != nil {
			serverResult <- handshakeErr
			return
		}
		packet, readErr := readMySQLPacket(server)
		if readErr != nil {
			serverResult <- readErr
			return
		}
		if packet.seq != 8 || string(packet.payload) != "secret\x00" {
			serverResult <- errUnexpectedMySQLFullAuthPacket(packet.seq, packet.payload)
			return
		}
		_, writeErr := server.Write(mysqlPacketWithSeq(9, []byte{0x00}))
		serverResult <- writeErr
	}()
	client, err := dbtls.HandshakeClient(context.Background(), clientRaw, dbtls.Config{Mode: dbtls.ModeVerifyCA, CAPEM: string(certificatePEM)}, "localhost:3306")
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	packet, err := (&Gateway{logger: logger}).handleMySQLCachingSha2FullAuth(client, "secret", 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(packet.payload) != 1 || packet.payload[0] != 0x00 {
		t.Fatalf("final packet = %x, want OK", packet.payload)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
	if output := logs.String(); !strings.Contains(output, `"event":"caching_sha2_full_auth"`) {
		t.Fatalf("full-auth debug event was not emitted: %s", output)
	} else if strings.Contains(output, "secret") {
		t.Fatalf("full-auth debug event leaked the password: %s", output)
	}
}

func errUnexpectedMySQLFullAuthPacket(sequence byte, payload []byte) error {
	return &mysqlFullAuthPacketError{sequence: sequence, payload: payload}
}

type mysqlFullAuthPacketError struct {
	sequence byte
	payload  []byte
}

func (err *mysqlFullAuthPacketError) Error() string {
	return "unexpected MySQL full-auth packet"
}
