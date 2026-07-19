package dbproxy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/dbtls"
)

const defaultUnifiedDetectionTimeout = 200 * time.Millisecond

type unifiedPrefaceKind uint8

const (
	unifiedPrefaceMySQL unifiedPrefaceKind = iota + 1
	unifiedPrefacePostgreSQL
	unifiedPrefaceRedis
	unifiedPrefaceTLS
)

type unifiedPreface struct {
	kind      unifiedPrefaceKind
	firstByte byte
}

func (g *Gateway) listenAndServeUnifiedListener(ctx context.Context) error {
	listenerConfig := g.cfg.Unified
	if !listenerConfig.Enabled {
		return errors.New("database gateway unified listener is disabled")
	}
	if err := validateUnifiedListenerTLS(listenerConfig); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", listenerConfig.Address)
	if err != nil {
		return fmt.Errorf("listen unified database gateway on %s: %w", listenerConfig.Address, err)
	}
	g.logger.Info("database gateway listening", "protocol", "unified", "addr", listener.Addr().String())
	return g.serveUnifiedListener(ctx, listener, listenerConfig)
}

func (g *Gateway) serveUnifiedListener(
	ctx context.Context,
	listener net.Listener,
	listenerConfig config.DatabaseUnifiedListener,
) error {
	defer listener.Close()
	activeConnections := newActiveConnectionSet()
	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
			activeConnections.closeAll()
		case <-stopped:
		}
	}()

	var waitGroup sync.WaitGroup
	for {
		client, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				waitGroup.Wait()
				return nil
			}
			activeConnections.closeAll()
			waitGroup.Wait()
			return fmt.Errorf("accept unified database connection: %w", err)
		}
		if !activeConnections.add(client) {
			_ = client.Close()
			continue
		}
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			defer activeConnections.remove(client)
			defer client.Close()
			g.handleUnifiedConnection(ctx, client, listenerConfig)
		}()
	}
}

func (g *Gateway) handleUnifiedConnection(
	ctx context.Context,
	client net.Conn,
	listenerConfig config.DatabaseUnifiedListener,
) {
	detectionTimeout := time.Duration(listenerConfig.DetectionTimeoutMS) * time.Millisecond
	if detectionTimeout <= 0 {
		detectionTimeout = defaultUnifiedDetectionTimeout
	}
	g.handleUnifiedConnectionWithTimeout(
		ctx,
		client,
		listenerConfig,
		protocolHandshakeTimeout,
		detectionTimeout,
	)
}

func (g *Gateway) handleUnifiedConnectionWithTimeout(
	ctx context.Context,
	client net.Conn,
	listenerConfig config.DatabaseUnifiedListener,
	handshakeTimeout time.Duration,
	detectionTimeout time.Duration,
) {
	handshakeLease, acquired := g.tryAcquirePendingHandshake()
	if !acquired {
		return
	}
	defer handshakeLease.release()

	handshakeDeadline := time.Now().Add(handshakeTimeout)
	if err := client.SetDeadline(handshakeDeadline); err != nil {
		g.logger.Warn("database gateway failed to set unified handshake deadline", "error", err)
		return
	}
	preface, err := detectUnifiedPreface(client, handshakeDeadline, detectionTimeout)
	if err != nil {
		return
	}

	protocolConfig := unifiedProtocolConfig(listenerConfig)
	var connection *gatewayConn
	var protocol databaseProtocol
	switch preface.kind {
	case unifiedPrefaceMySQL:
		protocol = databaseProtocolMySQL
		connection = g.handleMySQLWithListener(ctx, client, protocolConfig)
	case unifiedPrefacePostgreSQL:
		protocol = databaseProtocolPostgreSQL
		prefixed := &postgresPrefixedConn{Conn: client, prefix: []byte{preface.firstByte}}
		connection = g.handlePostgresConnection(ctx, prefixed, protocolConfig)
	case unifiedPrefaceRedis:
		protocol = databaseProtocolRedis
		connection = g.handleUnifiedPlaintextRedis(ctx, client, listenerConfig, preface.firstByte)
	case unifiedPrefaceTLS:
		protocol, connection = g.handleUnifiedTLS(ctx, client, listenerConfig, preface.firstByte)
	default:
		return
	}
	g.finishProtocolConnection(client, connection, protocol, handshakeLease)
}

func detectUnifiedPreface(
	client net.Conn,
	handshakeDeadline time.Time,
	detectionTimeout time.Duration,
) (unifiedPreface, error) {
	detectionDeadline := time.Now().Add(detectionTimeout)
	if detectionDeadline.After(handshakeDeadline) {
		detectionDeadline = handshakeDeadline
	}
	if err := client.SetReadDeadline(detectionDeadline); err != nil {
		return unifiedPreface{}, fmt.Errorf("set unified detection deadline: %w", err)
	}
	var first [1]byte
	read, readErr := client.Read(first[:])
	if err := client.SetReadDeadline(handshakeDeadline); err != nil {
		return unifiedPreface{}, fmt.Errorf("restore unified handshake deadline: %w", err)
	}
	if read == 0 {
		var netErr net.Error
		if errors.As(readErr, &netErr) && netErr.Timeout() {
			return unifiedPreface{kind: unifiedPrefaceMySQL}, nil
		}
		if readErr == nil {
			readErr = io.ErrNoProgress
		}
		return unifiedPreface{}, readErr
	}

	switch first[0] {
	case 0:
		return unifiedPreface{kind: unifiedPrefacePostgreSQL, firstByte: first[0]}, nil
	case '*':
		return unifiedPreface{kind: unifiedPrefaceRedis, firstByte: first[0]}, nil
	case 0x16:
		return unifiedPreface{kind: unifiedPrefaceTLS, firstByte: first[0]}, nil
	default:
		return unifiedPreface{}, fmt.Errorf("unsupported unified database protocol preface 0x%02x", first[0])
	}
}

func (g *Gateway) handleUnifiedPlaintextRedis(
	ctx context.Context,
	client net.Conn,
	listenerConfig config.DatabaseUnifiedListener,
	firstByte byte,
) *gatewayConn {
	if unifiedTLSConfigured(listenerConfig) || !isLoopbackPeer(client.RemoteAddr()) {
		_, _ = client.Write([]byte("-NOAUTH TLS is required for remote AUTH\r\n"))
		return nil
	}
	return g.handleRedisAfterTransport(ctx, client, firstByte)
}

func (g *Gateway) handleUnifiedTLS(
	ctx context.Context,
	client net.Conn,
	listenerConfig config.DatabaseUnifiedListener,
	firstByte byte,
) (databaseProtocol, *gatewayConn) {
	if !unifiedTLSConfigured(listenerConfig) {
		return "", nil
	}
	tlsConfig, err := unifiedListenerTLSConfig(listenerConfig)
	if err != nil {
		g.logger.Error("load unified database listener certificate", "error", err)
		return "", nil
	}
	prefixed := &postgresPrefixedConn{Conn: client, prefix: []byte{firstByte}}
	secured := tls.Server(prefixed, tlsConfig)
	if err := secured.HandshakeContext(ctx); err != nil {
		return "", nil
	}
	var decryptedFirst [1]byte
	if _, err := io.ReadFull(secured, decryptedFirst[:]); err != nil {
		return "", nil
	}
	switch secured.ConnectionState().NegotiatedProtocol {
	case postgresTLSALPN:
		if decryptedFirst[0] != 0 {
			return "", nil
		}
		return databaseProtocolPostgreSQL,
			g.handlePostgresAfterTLS(ctx, secured, decryptedFirst[0])
	case "":
		if decryptedFirst[0] != '*' {
			return "", nil
		}
		return databaseProtocolRedis,
			g.handleRedisAfterTransport(ctx, secured, decryptedFirst[0])
	default:
		return "", nil
	}
}

func validateUnifiedListenerTLS(listener config.DatabaseUnifiedListener) error {
	certConfigured := strings.TrimSpace(listener.CertFile) != ""
	keyConfigured := strings.TrimSpace(listener.KeyFile) != ""
	if certConfigured != keyConfigured {
		return errors.New("unified database listener certificate and key must be configured together")
	}
	if !certConfigured {
		return nil
	}
	if _, err := dbtls.LoadServerIdentity(listener.CertFile, listener.CAFile, listener.ServerName); err != nil {
		return fmt.Errorf("load unified database listener TLS: validate listener identity: %w", err)
	}
	if _, err := unifiedListenerTLSConfig(listener); err != nil {
		return fmt.Errorf("load unified database listener TLS: %w", err)
	}
	return nil
}

func unifiedListenerTLSConfig(listener config.DatabaseUnifiedListener) (*tls.Config, error) {
	tlsConfig, err := databaseListenerTLSConfig(unifiedProtocolConfig(listener))
	if err != nil {
		return nil, err
	}
	tlsConfig.NextProtos = []string{postgresTLSALPN}
	return tlsConfig, nil
}

func unifiedProtocolConfig(listener config.DatabaseUnifiedListener) config.DatabaseProtocolListener {
	return config.DatabaseProtocolListener{
		Enabled:    listener.Enabled,
		Address:    listener.Address,
		CertFile:   listener.CertFile,
		KeyFile:    listener.KeyFile,
		CAFile:     listener.CAFile,
		ServerName: listener.ServerName,
	}
}

func unifiedTLSConfigured(listener config.DatabaseUnifiedListener) bool {
	return strings.TrimSpace(listener.CertFile) != "" &&
		strings.TrimSpace(listener.KeyFile) != ""
}
