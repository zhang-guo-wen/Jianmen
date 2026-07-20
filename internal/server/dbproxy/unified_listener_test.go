package dbproxy

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"jianmen/internal/config"
)

func TestUnifiedListenerSilentConnectionBecomesMySQL(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		(&Gateway{logger: slog.Default()}).handleUnifiedConnectionWithTimeout(
			context.Background(),
			server,
			config.DatabaseUnifiedListener{},
			500*time.Millisecond,
			40*time.Millisecond,
		)
	}()

	if err := client.SetReadDeadline(time.Now().Add(15 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var early [1]byte
	if _, err := client.Read(early[:]); err == nil {
		t.Fatal("unified listener sent a MySQL greeting before its detection window elapsed")
	}
	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := readMySQLPacket(client); err != nil {
		t.Fatalf("read delayed MySQL greeting: %v", err)
	}
	_ = client.Close()
	waitUnifiedHandler(t, done)
}

func TestIndependentMySQLGreetingIsImmediateWhileUnifiedWaits(t *testing.T) {
	const detectionTimeout = 200 * time.Millisecond
	independentServer, independentClient := net.Pipe()
	defer independentClient.Close()
	independentDone := make(chan struct{})
	go func() {
		defer close(independentDone)
		defer independentServer.Close()
		(&Gateway{logger: slog.Default()}).handleProtocolConnectionWithTimeout(
			context.Background(),
			independentServer,
			databaseProtocolMySQL,
			config.DatabaseProtocolListener{},
			time.Second,
		)
	}()

	unifiedServer, unifiedClient := net.Pipe()
	defer unifiedClient.Close()
	unifiedDone := runUnifiedPipeHandler(
		unifiedServer,
		config.DatabaseUnifiedListener{},
		time.Second,
		detectionTimeout,
	)
	startedAt := time.Now()

	if err := independentClient.SetReadDeadline(startedAt.Add(detectionTimeout / 2)); err != nil {
		t.Fatal(err)
	}
	if _, err := readMySQLPacket(independentClient); err != nil {
		t.Fatalf("independent MySQL greeting waited for the unified detection window: %v", err)
	}

	if err := unifiedClient.SetReadDeadline(startedAt.Add(detectionTimeout / 2)); err != nil {
		t.Fatal(err)
	}
	var early [1]byte
	if _, err := unifiedClient.Read(early[:]); err == nil {
		t.Fatal("unified MySQL greeting arrived before the detection window")
	}
	if err := unifiedClient.SetReadDeadline(startedAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := readMySQLPacket(unifiedClient); err != nil {
		t.Fatalf("read unified MySQL greeting: %v", err)
	}
	elapsed := time.Since(startedAt)
	if elapsed < detectionTimeout-20*time.Millisecond {
		t.Fatalf("unified MySQL greeting delay = %v, want about %v", elapsed, detectionTimeout)
	}
	if elapsed > detectionTimeout+500*time.Millisecond {
		t.Fatalf("unified MySQL greeting delay = %v, exceeds detection window by too much", elapsed)
	}

	_ = independentClient.Close()
	_ = unifiedClient.Close()
	waitUnifiedHandler(t, independentDone)
	waitUnifiedHandler(t, unifiedDone)
}

func TestUnifiedListenerPostgresPrefaceIsReplayedOnce(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	done := runUnifiedPipeHandler(
		server,
		config.DatabaseUnifiedListener{},
		500*time.Millisecond,
		100*time.Millisecond,
	)

	request := make([]byte, 8)
	binary.BigEndian.PutUint32(request[:4], 8)
	binary.BigEndian.PutUint32(request[4:], postgresSSLRequestCode)
	if _, err := client.Write(request); err != nil {
		t.Fatal(err)
	}
	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var response [1]byte
	if _, err := client.Read(response[:]); err != nil {
		t.Fatalf("read PostgreSQL SSL refusal: %v", err)
	}
	if response[0] != 'N' {
		t.Fatalf("PostgreSQL SSL response = %q, want N", response[0])
	}
	waitUnifiedHandler(t, done)
}

func TestUnifiedListenerPartialPostgresNeverFallsBackToMySQL(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	done := runUnifiedPipeHandler(
		server,
		config.DatabaseUnifiedListener{},
		80*time.Millisecond,
		20*time.Millisecond,
	)
	if _, err := client.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	if err := client.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var response [1]byte
	if read, err := client.Read(response[:]); err == nil || read != 0 {
		t.Fatalf("partial PostgreSQL preface produced %x, want connection close without MySQL fallback", response[:read])
	}
	waitUnifiedHandler(t, done)
}

func TestUnifiedListenerMalformedPrefacesNeverBecomeMySQL(t *testing.T) {
	certFile, keyFile := writeListenerCertificate(t)
	tests := []struct {
		name           string
		payload        []byte
		listenerConfig config.DatabaseUnifiedListener
	}{
		{name: "unknown first byte", payload: []byte{'?'}},
		{
			name:           "incomplete TLS ClientHello",
			payload:        []byte{0x16},
			listenerConfig: config.DatabaseUnifiedListener{CertFile: certFile, KeyFile: keyFile},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, client := net.Pipe()
			defer client.Close()
			done := runUnifiedPipeHandler(
				server,
				test.listenerConfig,
				80*time.Millisecond,
				20*time.Millisecond,
			)
			if _, err := client.Write(test.payload); err != nil {
				t.Fatal(err)
			}
			if err := client.SetReadDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			var response [1]byte
			if read, err := client.Read(response[:]); err == nil || read != 0 {
				t.Fatalf("malformed preface returned %x, want close without MySQL greeting", response[:read])
			}
			waitUnifiedHandler(t, done)
		})
	}
}

func TestUnifiedListenerAllowsLoopbackPlaintextRedisWithoutCertificate(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	loopback := &remoteAddrConn{Conn: server, remote: "127.0.0.1:12345"}
	done := runUnifiedPipeHandler(
		loopback,
		config.DatabaseUnifiedListener{},
		500*time.Millisecond,
		100*time.Millisecond,
	)
	if _, err := client.Write([]byte("*1\r\n$4\r\nPING\r\n")); err != nil {
		t.Fatal(err)
	}
	response := readUnifiedResponse(t, client)
	if response != "-NOAUTH Authentication required.\r\n" {
		t.Fatalf("Redis response = %q", response)
	}
	waitUnifiedHandler(t, done)
}

func TestUnifiedOptionalTLSListenerAllowsPlaintextRedisWithCertificate(t *testing.T) {
	certFile, keyFile := writeListenerCertificate(t)
	server, client := net.Pipe()
	defer client.Close()
	done := runUnifiedPipeHandler(
		server,
		config.DatabaseUnifiedListener{CertFile: certFile, KeyFile: keyFile},
		500*time.Millisecond,
		100*time.Millisecond,
		config.DatabaseGatewayClientTLSModeOptional,
	)
	if _, err := client.Write([]byte("*1\r\n$4\r\nPING\r\n")); err != nil {
		t.Fatal(err)
	}
	response := readUnifiedResponse(t, client)
	if response != "-NOAUTH Authentication required.\r\n" {
		t.Fatalf("Redis response = %q", response)
	}
	waitUnifiedHandler(t, done)
}

func TestUnifiedRequiredTLSListenerRejectsPlaintextRedis(t *testing.T) {
	certFile, keyFile := writeListenerCertificate(t)
	server, client := net.Pipe()
	defer client.Close()
	loopback := &remoteAddrConn{Conn: server, remote: "127.0.0.1:12345"}
	done := runUnifiedPipeHandler(
		loopback,
		config.DatabaseUnifiedListener{CertFile: certFile, KeyFile: keyFile},
		500*time.Millisecond,
		100*time.Millisecond,
		config.DatabaseGatewayClientTLSModeRequired,
	)
	// Required TLS rejects as soon as the RESP marker is visible.
	if _, err := client.Write([]byte{'*'}); err != nil {
		t.Fatal(err)
	}
	response := readUnifiedResponse(t, client)
	if response != "-NOAUTH TLS is required for remote AUTH\r\n" {
		t.Fatalf("Redis TLS-required response = %q", response)
	}
	waitUnifiedHandler(t, done)
}

func TestUnifiedListenerRoutesRedisAfterSharedTLS(t *testing.T) {
	certFile, keyFile := writeListenerCertificate(t)
	server, client := net.Pipe()
	defer client.Close()
	done := runUnifiedPipeHandler(
		server,
		config.DatabaseUnifiedListener{CertFile: certFile, KeyFile: keyFile},
		time.Second,
		200*time.Millisecond,
	)
	secured := tls.Client(client, &tls.Config{
		InsecureSkipVerify: true, // the generated test identity is self-signed
		MinVersion:         tls.VersionTLS12,
	})
	if err := secured.Handshake(); err != nil {
		t.Fatalf("Redis shared TLS handshake: %v", err)
	}
	if _, err := secured.Write([]byte("*1\r\n$4\r\nPING\r\n")); err != nil {
		t.Fatal(err)
	}
	response := readUnifiedResponse(t, secured)
	if response != "-NOAUTH Authentication required.\r\n" {
		t.Fatalf("Redis shared TLS response = %q", response)
	}
	waitUnifiedHandler(t, done)
}

func TestUnifiedListenerRoutesPostgresDirectTLSByALPN(t *testing.T) {
	certFile, keyFile := writeListenerCertificate(t)
	server, client := net.Pipe()
	defer client.Close()
	done := runUnifiedPipeHandler(
		server,
		config.DatabaseUnifiedListener{CertFile: certFile, KeyFile: keyFile},
		time.Second,
		200*time.Millisecond,
	)
	secured := tls.Client(client, &tls.Config{
		InsecureSkipVerify: true, // the generated test identity is self-signed
		MinVersion:         tls.VersionTLS12,
		NextProtos:         []string{postgresTLSALPN},
	})
	if err := secured.Handshake(); err != nil {
		t.Fatalf("PostgreSQL shared TLS handshake: %v", err)
	}
	if got := secured.ConnectionState().NegotiatedProtocol; got != postgresTLSALPN {
		t.Fatalf("negotiated ALPN = %q, want %q", got, postgresTLSALPN)
	}
	startup := make([]byte, 9)
	binary.BigEndian.PutUint32(startup[:4], uint32(len(startup)))
	binary.BigEndian.PutUint32(startup[4:8], postgresProtocolVersion30)
	if _, err := secured.Write(startup); err != nil {
		t.Fatal(err)
	}
	if err := secured.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var response [1]byte
	if _, err := secured.Read(response[:]); err != nil {
		t.Fatalf("read PostgreSQL authentication rejection: %v", err)
	}
	if response[0] != 'E' {
		t.Fatalf("PostgreSQL response kind = %q, want E", response[0])
	}
	waitUnifiedHandler(t, done)
}

func TestUnifiedListenerRejectsTLSProtocolMismatch(t *testing.T) {
	certFile, keyFile := writeListenerCertificate(t)
	tests := []struct {
		name       string
		nextProtos []string
		payload    []byte
	}{
		{
			name:       "PostgreSQL ALPN with Redis payload",
			nextProtos: []string{postgresTLSALPN},
			payload:    []byte("*1\r\n$4\r\nPING\r\n"),
		},
		{
			name:    "empty ALPN with PostgreSQL payload",
			payload: postgresInvalidStartupForUnifiedTest(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, client := net.Pipe()
			defer client.Close()
			done := runUnifiedPipeHandler(
				server,
				config.DatabaseUnifiedListener{CertFile: certFile, KeyFile: keyFile},
				time.Second,
				200*time.Millisecond,
			)
			secured := tls.Client(client, &tls.Config{
				InsecureSkipVerify: true, // the generated test identity is self-signed
				MinVersion:         tls.VersionTLS12,
				NextProtos:         test.nextProtos,
			})
			if err := secured.Handshake(); err != nil {
				t.Fatalf("shared TLS handshake: %v", err)
			}
			_, _ = secured.Write(test.payload)
			if err := secured.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				waitUnifiedHandler(t, done)
				return
			}
			var response [1]byte
			if read, err := secured.Read(response[:]); err == nil || read != 0 {
				t.Fatalf("protocol mismatch returned %x instead of closing", response[:read])
			}
			waitUnifiedHandler(t, done)
		})
	}
}

func TestUnifiedListenerCancellationClosesSilentConnections(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gateway := &Gateway{logger: slog.Default()}
	done := make(chan error, 1)
	go func() {
		done <- gateway.serveUnifiedListener(
			ctx,
			listener,
			config.DatabaseUnifiedListener{DetectionTimeoutMS: 200},
		)
	}()
	client, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unified listener returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("unified listener did not stop after cancellation")
	}
	if err := client.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var value [1]byte
	if _, err := client.Read(value[:]); err == nil {
		t.Fatal("silent unified client remained open after cancellation")
	}
}

func TestUnifiedTCPListenerRoutesThreeProtocolsConcurrently(t *testing.T) {
	certFile, keyFile := writeListenerCertificate(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	gateway := &Gateway{
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		pendingHandshakes: newPendingHandshakeLimiter(8),
	}
	listenerConfig := config.DatabaseUnifiedListener{
		CertFile:           certFile,
		KeyFile:            keyFile,
		DetectionTimeoutMS: 40,
	}
	done := make(chan error, 1)
	go func() {
		done <- gateway.serveUnifiedListener(ctx, listener, listenerConfig)
	}()

	address := listener.Addr().String()
	start := make(chan struct{})
	results := make(chan error, 3)
	routes := []func(string) error{
		verifyUnifiedMySQLRoute,
		verifyUnifiedPostgresRoute,
		verifyUnifiedRedisRoute,
	}
	for _, route := range routes {
		go func(route func(string) error) {
			<-start
			results <- route(address)
		}(route)
	}
	close(start)
	for range routes {
		if err := <-results; err != nil {
			cancel()
			<-done
			t.Fatal(err)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unified TCP listener returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("unified TCP listener did not stop")
	}
}

func TestUnifiedModeIgnoresIndependentListenerBindings(t *testing.T) {
	certFile, keyFile := writeListenerCertificate(t)
	blocked, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer blocked.Close()
	unifiedAddress := reserveTCPAddress(t)
	ctx, cancel := context.WithCancel(context.Background())
	gateway := &Gateway{
		cfg: config.DatabaseGatewayConfig{
			Enabled: true,
			Mode:    config.DatabaseGatewayModeUnified,
			Unified: config.DatabaseUnifiedListener{
				Enabled:            true,
				Address:            unifiedAddress,
				CertFile:           certFile,
				KeyFile:            keyFile,
				ServerName:         "localhost",
				DetectionTimeoutMS: 20,
			},
			MySQL: config.DatabaseProtocolListener{
				Enabled: true,
				Address: blocked.Addr().String(),
			},
		},
		logger: slog.Default(),
	}
	done := make(chan error, 1)
	go func() {
		done <- gateway.ListenAndServe(ctx)
	}()
	client := dialEventually(t, unifiedAddress)
	if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := readMySQLPacket(client); err != nil {
		t.Fatalf("read unified MySQL greeting: %v", err)
	}
	_ = client.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ListenAndServe() returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("unified ListenAndServe did not stop")
	}
}

func TestValidateUnifiedListenerTLSRejectsMismatchedFiles(t *testing.T) {
	err := validateUnifiedListenerTLS(config.DatabaseUnifiedListener{CertFile: "server.crt"})
	if err == nil || !strings.Contains(err.Error(), "configured together") {
		t.Fatalf("validateUnifiedListenerTLS() error = %v", err)
	}
}

func runUnifiedPipeHandler(
	server net.Conn,
	listenerConfig config.DatabaseUnifiedListener,
	handshakeTimeout time.Duration,
	detectionTimeout time.Duration,
	clientTLSModes ...string,
) <-chan struct{} {
	clientTLSMode := config.DatabaseGatewayClientTLSModeOptional
	if len(clientTLSModes) > 0 {
		clientTLSMode = clientTLSModes[0]
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		(&Gateway{
			cfg:    config.DatabaseGatewayConfig{ClientTLSMode: clientTLSMode},
			logger: slog.Default(),
		}).handleUnifiedConnectionWithTimeout(
			context.Background(),
			server,
			listenerConfig,
			handshakeTimeout,
			detectionTimeout,
		)
	}()
	return done
}

func waitUnifiedHandler(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("unified connection handler did not stop")
	}
}

func readUnifiedResponse(t *testing.T, connection net.Conn) string {
	t.Helper()
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 128)
	read, err := connection.Read(buffer)
	if err != nil {
		t.Fatalf("read unified response: %v", err)
	}
	return string(buffer[:read])
}

func postgresInvalidStartupForUnifiedTest() []byte {
	startup := make([]byte, 9)
	binary.BigEndian.PutUint32(startup[:4], uint32(len(startup)))
	binary.BigEndian.PutUint32(startup[4:8], postgresProtocolVersion30)
	return startup
}

func reserveTCPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func dialEventually(t *testing.T, address string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		connection, err := net.DialTimeout("tcp", address, 50*time.Millisecond)
		if err == nil {
			return connection
		}
		if !errors.Is(err, net.ErrClosed) && time.Now().After(deadline) {
			t.Fatalf("dial unified listener: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func verifyUnifiedMySQLRoute(address string) error {
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		return fmt.Errorf("dial unified MySQL route: %w", err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(time.Second)); err != nil {
		return err
	}
	handshake, err := readMySQLPacket(connection)
	if err != nil {
		return fmt.Errorf("read unified MySQL greeting: %w", err)
	}
	if len(handshake.payload) == 0 || handshake.payload[0] != 10 {
		return fmt.Errorf("unified MySQL greeting payload = %x", handshake.payload)
	}
	return nil
}

func verifyUnifiedPostgresRoute(address string) error {
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		return fmt.Errorf("dial unified PostgreSQL route: %w", err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(time.Second)); err != nil {
		return err
	}
	request := make([]byte, 8)
	binary.BigEndian.PutUint32(request[:4], 8)
	binary.BigEndian.PutUint32(request[4:], postgresSSLRequestCode)
	if _, err := connection.Write(request); err != nil {
		return fmt.Errorf("write PostgreSQL SSLRequest: %w", err)
	}
	var negotiation [1]byte
	if _, err := io.ReadFull(connection, negotiation[:]); err != nil {
		return fmt.Errorf("read PostgreSQL SSL response: %w", err)
	}
	if negotiation[0] != 'S' {
		return fmt.Errorf("PostgreSQL SSL response = %q, want S", negotiation[0])
	}
	secured := tls.Client(connection, &tls.Config{
		InsecureSkipVerify: true, // the generated test identity is self-signed
		MinVersion:         tls.VersionTLS12,
		NextProtos:         []string{postgresTLSALPN},
	})
	if err := secured.Handshake(); err != nil {
		return fmt.Errorf("PostgreSQL TLS handshake: %w", err)
	}
	if _, err := secured.Write(postgresInvalidStartupForUnifiedTest()); err != nil {
		return fmt.Errorf("write PostgreSQL startup: %w", err)
	}
	var response [1]byte
	if _, err := io.ReadFull(secured, response[:]); err != nil {
		return fmt.Errorf("read PostgreSQL route response: %w", err)
	}
	if response[0] != 'E' {
		return fmt.Errorf("PostgreSQL route response = %q, want E", response[0])
	}
	return nil
}

func verifyUnifiedRedisRoute(address string) error {
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		return fmt.Errorf("dial unified Redis route: %w", err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(time.Second)); err != nil {
		return err
	}
	secured := tls.Client(connection, &tls.Config{
		InsecureSkipVerify: true, // the generated test identity is self-signed
		MinVersion:         tls.VersionTLS12,
	})
	if err := secured.Handshake(); err != nil {
		return fmt.Errorf("Redis TLS handshake: %w", err)
	}
	if _, err := secured.Write([]byte("*1\r\n$4\r\nPING\r\n")); err != nil {
		return fmt.Errorf("write Redis command: %w", err)
	}
	var response [64]byte
	read, err := secured.Read(response[:])
	if err != nil {
		return fmt.Errorf("read Redis route response: %w", err)
	}
	if got := string(response[:read]); got != "-NOAUTH Authentication required.\r\n" {
		return fmt.Errorf("Redis route response = %q", got)
	}
	return nil
}

func FuzzDetectUnifiedPreface(f *testing.F) {
	f.Add([]byte(nil), false)
	f.Add([]byte{0}, false)
	f.Add([]byte("*1\r\n"), true)
	f.Add([]byte{0x16, 0x03, 0x03}, false)
	f.Add([]byte{'?'}, true)

	f.Fuzz(func(t *testing.T, input []byte, eofWithByte bool) {
		connection := &fuzzUnifiedConnection{
			input:       input,
			eofWithByte: eofWithByte,
		}
		preface, err := detectUnifiedPreface(
			connection,
			time.Now().Add(time.Second),
			defaultUnifiedDetectionTimeout,
		)
		if len(input) == 0 {
			if err != nil {
				t.Fatalf("silent connection detection error = %v", err)
			}
			if preface.kind != unifiedPrefaceMySQL {
				t.Fatalf("silent connection kind = %d, want MySQL", preface.kind)
			}
			return
		}

		if preface.kind == unifiedPrefaceMySQL {
			t.Fatalf("non-empty preface %x fell back to MySQL", input[0])
		}
		if connection.consumed != 1 {
			t.Fatalf("detector consumed %d bytes, want exactly one", connection.consumed)
		}
		var expected unifiedPrefaceKind
		switch input[0] {
		case 0:
			expected = unifiedPrefacePostgreSQL
		case '*':
			expected = unifiedPrefaceRedis
		case 0x16:
			expected = unifiedPrefaceTLS
		default:
			if err == nil {
				t.Fatalf("unknown preface 0x%02x was accepted", input[0])
			}
			return
		}
		if err != nil {
			t.Fatalf("known preface 0x%02x detection error = %v", input[0], err)
		}
		if preface.kind != expected || preface.firstByte != input[0] {
			t.Fatalf(
				"preface 0x%02x classified as kind=%d byte=0x%02x, want kind=%d",
				input[0],
				preface.kind,
				preface.firstByte,
				expected,
			)
		}
	})
}

type fuzzUnifiedConnection struct {
	net.Conn
	input       []byte
	consumed    int
	eofWithByte bool
}

func (connection *fuzzUnifiedConnection) Read(buffer []byte) (int, error) {
	if connection.consumed >= len(connection.input) {
		return 0, fuzzUnifiedTimeout{}
	}
	buffer[0] = connection.input[connection.consumed]
	connection.consumed++
	if connection.eofWithByte {
		return 1, io.EOF
	}
	return 1, nil
}

func (connection *fuzzUnifiedConnection) SetReadDeadline(time.Time) error {
	return nil
}

type fuzzUnifiedTimeout struct{}

func (fuzzUnifiedTimeout) Error() string   { return "fuzz timeout" }
func (fuzzUnifiedTimeout) Timeout() bool   { return true }
func (fuzzUnifiedTimeout) Temporary() bool { return true }
