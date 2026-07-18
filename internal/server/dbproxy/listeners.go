package dbproxy

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/dbtls"
)

type databaseProtocol string

const (
	databaseProtocolMySQL      databaseProtocol = "mysql"
	databaseProtocolPostgreSQL databaseProtocol = "postgresql"
	databaseProtocolRedis      databaseProtocol = "redis"
)

type protocolListener struct {
	protocol databaseProtocol
	config   config.DatabaseProtocolListener
}

type boundProtocolListener struct {
	listener net.Listener
	item     protocolListener
}

const protocolHandshakeTimeout = 5 * time.Second

func (g *Gateway) listenAndServeProtocolListeners(ctx context.Context) error {
	listeners := g.configuredProtocolListeners()
	if len(listeners) == 0 {
		return errors.New("database gateway has no enabled protocol listeners")
	}
	for _, item := range listeners {
		if err := validateProtocolListenerTLS(item); err != nil {
			return err
		}
	}

	bound := make([]boundProtocolListener, 0, len(listeners))
	for _, item := range listeners {
		listener, err := net.Listen("tcp", item.config.Address)
		if err != nil {
			for _, previous := range bound {
				_ = previous.listener.Close()
			}
			return fmt.Errorf("listen %s database gateway on %s: %w", item.protocol, item.config.Address, err)
		}
		bound = append(bound, boundProtocolListener{listener: listener, item: item})
	}

	for _, item := range bound {
		g.logger.Info("database gateway listening", "protocol", item.item.protocol, "addr", item.listener.Addr().String())
	}
	return g.serveBoundProtocolListeners(ctx, bound)
}

func (g *Gateway) serveBoundProtocolListeners(ctx context.Context, listeners []boundProtocolListener) error {
	serveCtx, cancel := context.WithCancel(ctx)
	activeConnections := newActiveConnectionSet()
	var shutdownOnce sync.Once
	shutdown := func() {
		shutdownOnce.Do(func() {
			cancel()
			for _, item := range listeners {
				_ = item.listener.Close()
			}
			activeConnections.closeAll()
		})
	}
	stop := make(chan struct{})
	go func() {
		select {
		case <-serveCtx.Done():
			shutdown()
		case <-stop:
		}
	}()
	defer func() {
		close(stop)
		shutdown()
	}()

	errs := make(chan error, len(listeners))
	for _, item := range listeners {
		go func(item boundProtocolListener) {
			errs <- g.serveProtocolListenerWithConnections(
				serveCtx, item.listener, item.item.protocol, item.item.config, activeConnections, shutdown,
			)
		}(item)
	}
	var firstErr error
	for range listeners {
		if err := <-errs; err != nil && firstErr == nil {
			firstErr = err
			shutdown()
		}
	}
	return firstErr
}

func validateProtocolListenerTLS(item protocolListener) error {
	hasCertificate := strings.TrimSpace(item.config.CertFile) != "" &&
		strings.TrimSpace(item.config.KeyFile) != ""
	if item.protocol == databaseProtocolPostgreSQL && !hasCertificate {
		return errors.New("postgresql database listener requires TLS certificate and key")
	}
	if !hasCertificate {
		return nil
	}
	if _, err := dbtls.LoadServerIdentity(
		item.config.CertFile,
		item.config.CAFile,
		item.config.ServerName,
	); err != nil {
		return fmt.Errorf("load %s database listener TLS: validate listener identity: %w", item.protocol, err)
	}
	if _, err := databaseListenerTLSConfig(item.config); err != nil {
		return fmt.Errorf("load %s database listener TLS: %w", item.protocol, err)
	}
	return nil
}

func (g *Gateway) configuredProtocolListeners() []protocolListener {
	listeners := []protocolListener{
		{protocol: databaseProtocolMySQL, config: g.cfg.MySQL},
		{protocol: databaseProtocolPostgreSQL, config: g.cfg.PostgreSQL},
		{protocol: databaseProtocolRedis, config: g.cfg.Redis},
	}
	result := make([]protocolListener, 0, len(listeners))
	for _, item := range listeners {
		if item.config.Enabled {
			result = append(result, item)
		}
	}
	return result
}

func (g *Gateway) serveProtocolListener(ctx context.Context, listener net.Listener, protocol databaseProtocol, listenerConfig config.DatabaseProtocolListener) error {
	return g.serveProtocolListenerWithConnections(ctx, listener, protocol, listenerConfig, newActiveConnectionSet(), nil)
}

func (g *Gateway) serveProtocolListenerWithConnections(ctx context.Context, listener net.Listener, protocol databaseProtocol, listenerConfig config.DatabaseProtocolListener, activeConnections *activeConnectionSet, onAcceptFailure func()) error {
	defer listener.Close()
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
	var wg sync.WaitGroup
	for {
		client, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				wg.Wait()
				return nil
			}
			if onAcceptFailure != nil {
				onAcceptFailure()
			}
			wg.Wait()
			return fmt.Errorf("accept %s database connection: %w", protocol, err)
		}
		if !activeConnections.add(client) {
			_ = client.Close()
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer activeConnections.remove(client)
			defer client.Close()
			g.handleProtocolConnection(ctx, client, protocol, listenerConfig)
		}()
	}
}

func (g *Gateway) handleProtocolConnection(ctx context.Context, client net.Conn, protocol databaseProtocol, listenerConfig config.DatabaseProtocolListener) {
	g.handleProtocolConnectionWithTimeout(ctx, client, protocol, listenerConfig, protocolHandshakeTimeout)
}

func (g *Gateway) handleProtocolConnectionWithTimeout(ctx context.Context, client net.Conn, protocol databaseProtocol, listenerConfig config.DatabaseProtocolListener, timeout time.Duration) {
	// The protocol preface, listener TLS handshake, and bastion authentication all
	// happen before a connection can enter the long-lived relay. A single deadline
	// covers that entire phase and prevents slowloris-style goroutine retention.
	if err := client.SetDeadline(time.Now().Add(timeout)); err != nil {
		g.logger.Warn("database gateway failed to set handshake deadline", "protocol", protocol, "error", err)
		return
	}
	var connection *gatewayConn
	switch protocol {
	case databaseProtocolMySQL:
		connection = g.handleMySQLWithListener(ctx, client, listenerConfig)
	case databaseProtocolPostgreSQL:
		connection = g.handlePostgresConnection(ctx, client, listenerConfig)
	case databaseProtocolRedis:
		connection = g.handleRedisConnection(ctx, client, listenerConfig)
	default:
		g.logger.Warn("unsupported database protocol listener", "protocol", protocol)
		return
	}
	if connection != nil {
		if err := client.SetDeadline(time.Time{}); err != nil {
			g.logger.Warn("database gateway failed to clear handshake deadline", "protocol", protocol, "error", err)
			_ = connection.upstream.Close()
			return
		}
		if connection.client != nil {
			if err := connection.client.SetDeadline(time.Time{}); err != nil {
				g.logger.Warn("database gateway failed to clear secured handshake deadline", "protocol", protocol, "error", err)
				_ = connection.upstream.Close()
				return
			}
		}
		g.handleGatewayConn(client, connection)
	}
}

func (g *Gateway) handlePostgresConnection(ctx context.Context, client net.Conn, listenerConfig config.DatabaseProtocolListener) *gatewayConn {
	first := make([]byte, 8)
	if _, err := readFull(client, first); err != nil {
		return nil
	}
	if isPostgresGSSENCRequest(first) {
		if _, err := client.Write([]byte{'N'}); err != nil {
			return nil
		}
		if _, err := readFull(client, first); err != nil {
			return nil
		}
	}
	if !isPostgresSSLRequest(first) {
		writePostgresTLSError(client)
		return nil
	}
	if listenerConfig.CertFile == "" || listenerConfig.KeyFile == "" {
		_, _ = client.Write([]byte{'N'})
		return nil
	}
	tlsConfig, err := databaseListenerTLSConfig(listenerConfig)
	if err != nil {
		g.logger.Error("load PostgreSQL listener certificate", "error", err)
		return nil
	}
	if _, err := client.Write([]byte{'S'}); err != nil {
		return nil
	}
	secured := tls.Server(client, tlsConfig)
	if err := secured.HandshakeContext(ctx); err != nil {
		return nil
	}
	firstByte := make([]byte, 1)
	if _, err := readFull(secured, firstByte); err != nil {
		return nil
	}
	connection := g.handlePG(ctx, secured, firstByte[0])
	if connection != nil {
		connection.client = secured
	}
	return connection
}

func (g *Gateway) handleRedisConnection(ctx context.Context, client net.Conn, listenerConfig config.DatabaseProtocolListener) *gatewayConn {
	if listenerConfig.CertFile != "" && listenerConfig.KeyFile != "" {
		tlsConfig, err := databaseListenerTLSConfig(listenerConfig)
		if err != nil {
			g.logger.Error("load Redis listener certificate", "error", err)
			return nil
		}
		secured := tls.Server(client, tlsConfig)
		if err := secured.HandshakeContext(ctx); err != nil {
			return nil
		}
		client = secured
	} else if !isLoopbackPeer(client.RemoteAddr()) {
		_, _ = client.Write([]byte("-NOAUTH TLS is required for remote AUTH\r\n"))
		return nil
	}

	firstByte := make([]byte, 1)
	if _, err := readFull(client, firstByte); err != nil {
		return nil
	}
	connection := g.handleRedis(ctx, client, firstByte[0])
	if connection != nil {
		connection.client = client
	}
	return connection
}

func databaseListenerTLSConfig(listener config.DatabaseProtocolListener) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(listener.CertFile, listener.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load listener certificate: %w", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}, nil
}

func isPostgresSSLRequest(header []byte) bool {
	return len(header) == 8 && header[0] == 0 && header[1] == 0 && header[2] == 0 && header[3] == 8 &&
		header[4] == 4 && header[5] == 210 && header[6] == 22 && header[7] == 47
}

func isPostgresGSSENCRequest(header []byte) bool {
	return len(header) == 8 && header[0] == 0 && header[1] == 0 && header[2] == 0 && header[3] == 8 &&
		header[4] == 4 && header[5] == 210 && header[6] == 22 && header[7] == 48
}

func writePostgresTLSError(conn net.Conn) {
	payload := []byte("SFATAL\x00MSSL is required for password authentication\x00\x00")
	message := make([]byte, 5+len(payload))
	message[0] = 'E'
	binary.BigEndian.PutUint32(message[1:5], uint32(4+len(payload)))
	copy(message[5:], payload)
	_, _ = conn.Write(message)
}

func isLoopbackPeer(address net.Addr) bool {
	if address == nil {
		return false
	}
	host, _, err := net.SplitHostPort(address.String())
	if err != nil {
		return false
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func readFull(conn net.Conn, buffer []byte) (int, error) {
	read := 0
	for read < len(buffer) {
		n, err := conn.Read(buffer[read:])
		read += n
		if err != nil {
			return read, err
		}
	}
	return read, nil
}
