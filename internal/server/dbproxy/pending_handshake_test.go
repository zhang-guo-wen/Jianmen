package dbproxy

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/online"
)

func TestNewGatewayInitializesPendingHandshakeLimit(t *testing.T) {
	gateway := NewGateway(config.DatabaseGatewayConfig{}, nil, "", nil, nil, nil, nil, nil)
	if gateway.pendingHandshakes == nil {
		t.Fatal("NewGateway() did not initialize the pending handshake limiter")
	}
	if got := cap(gateway.pendingHandshakes.slots); got != defaultPendingHandshakeLimit {
		t.Fatalf("pending handshake capacity = %d, want %d", got, defaultPendingHandshakeLimit)
	}
}

func TestPendingHandshakeLimitRejectsUnifiedAndIndependentImmediately(t *testing.T) {
	limiter := newPendingHandshakeLimiter(1)
	held, acquired := limiter.tryAcquire()
	if !acquired {
		t.Fatal("failed to reserve the only pending handshake slot")
	}
	defer held.release()
	gateway := &Gateway{
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		pendingHandshakes: limiter,
	}

	tests := []struct {
		name string
		run  func(net.Conn)
	}{
		{
			name: "independent",
			run: func(connection net.Conn) {
				gateway.handleProtocolConnectionWithTimeout(
					context.Background(),
					connection,
					databaseProtocolMySQL,
					config.DatabaseProtocolListener{},
					time.Second,
				)
			},
		},
		{
			name: "unified",
			run: func(connection net.Conn) {
				gateway.handleUnifiedConnectionWithTimeout(
					context.Background(),
					connection,
					config.DatabaseUnifiedListener{},
					time.Second,
					200*time.Millisecond,
				)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, client := net.Pipe()
			defer client.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer server.Close()
				test.run(server)
			}()
			if err := client.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			var response [1]byte
			if read, err := client.Read(response[:]); err == nil || read != 0 {
				t.Fatalf("saturated %s listener returned %x, want immediate close", test.name, response[:read])
			}
			waitPendingHandshakeTest(t, done)
		})
	}
}

func TestPendingHandshakeSlotIsReleasedAfterFailedHandshake(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Gateway, net.Conn)
		act  func(*testing.T, net.Conn)
	}{
		{
			name: "independent MySQL EOF",
			run: func(gateway *Gateway, connection net.Conn) {
				gateway.handleProtocolConnectionWithTimeout(
					context.Background(),
					connection,
					databaseProtocolMySQL,
					config.DatabaseProtocolListener{},
					time.Second,
				)
			},
			act: func(t *testing.T, connection net.Conn) {
				t.Helper()
				if _, err := readMySQLPacket(connection); err != nil {
					t.Fatalf("read independent MySQL greeting: %v", err)
				}
			},
		},
		{
			name: "unified unknown preface",
			run: func(gateway *Gateway, connection net.Conn) {
				gateway.handleUnifiedConnectionWithTimeout(
					context.Background(),
					connection,
					config.DatabaseUnifiedListener{},
					time.Second,
					200*time.Millisecond,
				)
			},
			act: func(t *testing.T, connection net.Conn) {
				t.Helper()
				if _, err := connection.Write([]byte{'?'}); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limiter := newPendingHandshakeLimiter(1)
			gateway := &Gateway{
				logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
				pendingHandshakes: limiter,
			}
			server, client := net.Pipe()
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer server.Close()
				test.run(gateway, server)
			}()
			test.act(t, client)
			_ = client.Close()
			waitPendingHandshakeTest(t, done)

			lease, acquired := limiter.tryAcquire()
			if !acquired {
				t.Fatal("failed handshake leaked its pending slot")
			}
			lease.release()
		})
	}
}

func TestPendingHandshakeSlotIsReleasedBeforeLongLivedRelay(t *testing.T) {
	limiter := newPendingHandshakeLimiter(1)
	handshakeLease, acquired := limiter.tryAcquire()
	if !acquired {
		t.Fatal("failed to acquire pending handshake slot")
	}
	client, clientPeer := net.Pipe()
	upstream, upstreamPeer := net.Pipe()
	defer clientPeer.Close()
	defer upstreamPeer.Close()
	gateway := &Gateway{
		replayDir:         t.TempDir(),
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		onlineSessions:    online.NewRegistry(),
		pendingHandshakes: limiter,
	}
	connection := &gatewayConn{
		protocol:     "redis",
		accountID:    "account-1",
		instanceID:   "instance-1",
		accountName:  "account-name",
		accountUser:  "upstream-user",
		instanceName: "redis-instance",
		userID:       "user-1",
		upstream:     upstream,
		upstreamAddr: "127.0.0.1:6379",
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		gateway.finishProtocolConnection(
			context.Background(),
			client,
			connection,
			databaseProtocolRedis,
			handshakeLease,
		)
	}()

	var relayLease *pendingHandshakeLease
	deadline := time.Now().Add(time.Second)
	for relayLease == nil && time.Now().Before(deadline) {
		if lease, ok := limiter.tryAcquire(); ok {
			relayLease = lease
			break
		}
		time.Sleep(time.Millisecond)
	}
	if relayLease == nil {
		t.Fatal("long-lived relay kept consuming the pending handshake slot")
	}
	relayLease.release()
	select {
	case <-done:
		t.Fatal("relay ended before its peer connections were closed")
	default:
	}

	_ = clientPeer.Close()
	_ = upstreamPeer.Close()
	waitPendingHandshakeTest(t, done)
}

func waitPendingHandshakeTest(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("pending handshake test handler did not stop")
	}
}
