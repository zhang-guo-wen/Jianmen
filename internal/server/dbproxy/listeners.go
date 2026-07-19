package dbproxy

import (
	"context"
	"crypto/tls"
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
	switch g.cfg.EffectiveMode() {
	case config.DatabaseGatewayModeUnified:
		return g.listenAndServeUnifiedListener(ctx)
	case config.DatabaseGatewayModeIndependent:
	default:
		return fmt.Errorf("unsupported database gateway mode %q", g.cfg.Mode)
	}

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
	handshakeLease, acquired := g.tryAcquirePendingHandshake()
	if !acquired {
		return
	}
	defer handshakeLease.release()

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
	g.finishProtocolConnection(ctx, client, connection, protocol, handshakeLease)
}

func (g *Gateway) finishProtocolConnection(
	listenerCtx context.Context,
	client net.Conn,
	connection *gatewayConn,
	protocol databaseProtocol,
	handshakeLease *pendingHandshakeLease,
) {
	if connection != nil {
		connectionCtx, cancelConnection := context.WithCancel(listenerCtx)
		defer cancelConnection()
		defer connection.releasePostgresCancel()
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
		handshakeLease.release()
		g.handleGatewayConn(connectionCtx, client, connection)
	}
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
	return g.handleRedisAfterTransport(ctx, client, firstByte[0])
}

func (g *Gateway) handleRedisAfterTransport(ctx context.Context, client net.Conn, firstByte byte) *gatewayConn {
	connection := g.handleRedis(ctx, client, firstByte)
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
