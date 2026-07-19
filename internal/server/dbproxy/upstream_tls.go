package dbproxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"jianmen/internal/dbtls"
	"jianmen/internal/model"
)

var errVerifiedTLSRequired = errors.New("verified TLS is required for database authentication")

func upstreamTLSPolicy(instance model.DatabaseInstance) (dbtls.Config, string, error) {
	mode, err := dbtls.NormalizeMode(instance.TLSMode)
	if err != nil {
		return dbtls.Config{}, "", err
	}
	policy := dbtls.Config{Mode: mode, ServerName: instance.TLSServerName, CAPEM: instance.TLSCAPEM}
	if mode == dbtls.ModeDisable {
		return policy, mode, nil
	}
	_, err = dbtls.ClientConfig(policy, upstreamAddress(instance))
	if err != nil {
		return dbtls.Config{}, "", fmt.Errorf("build upstream TLS config: %w", err)
	}
	return policy, mode, nil
}

func dialUpstream(ctx context.Context, instance model.DatabaseInstance) (net.Conn, error) {
	connection, err := (&net.Dialer{Timeout: protocolHandshakeTimeout}).DialContext(ctx, "tcp", upstreamAddress(instance))
	if err != nil {
		return nil, err
	}
	secured, err := newUpstreamHandshakeConn(ctx, connection, protocolHandshakeTimeout)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	return secured, nil
}

type upstreamHandshakeConn struct {
	net.Conn
	ctx    context.Context
	done   chan struct{}
	exited chan struct{}
	once   sync.Once
}

func newUpstreamHandshakeConn(ctx context.Context, connection net.Conn, timeout time.Duration) (net.Conn, error) {
	if timeout <= 0 {
		return nil, errors.New("upstream handshake timeout must be positive")
	}
	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set upstream handshake deadline: %w", err)
	}
	wrapped := &upstreamHandshakeConn{
		Conn:   connection,
		ctx:    ctx,
		done:   make(chan struct{}),
		exited: make(chan struct{}),
	}
	go func() {
		defer close(wrapped.exited)
		select {
		case <-ctx.Done():
			_ = connection.SetDeadline(time.Now())
		case <-wrapped.done:
		}
	}()
	return wrapped, nil
}

func (connection *upstreamHandshakeConn) SetDeadline(deadline time.Time) error {
	if deadline.IsZero() {
		connection.stopContextWatcher()
		if err := connection.ctx.Err(); err != nil {
			return err
		}
	}
	return connection.Conn.SetDeadline(deadline)
}

func (connection *upstreamHandshakeConn) Close() error {
	connection.stopContextWatcher()
	return connection.Conn.Close()
}

func (connection *upstreamHandshakeConn) stopContextWatcher() {
	connection.once.Do(func() {
		close(connection.done)
	})
	<-connection.exited
}

func dialMySQLUpstream(
	ctx context.Context,
	instance model.DatabaseInstance,
	database string,
) (net.Conn, *MySQLHandshake, error) {
	connection, err := dialUpstream(ctx, instance)
	if err != nil {
		return nil, nil, err
	}
	closeOnError := func(err error) (net.Conn, *MySQLHandshake, error) {
		_ = connection.Close()
		return nil, nil, err
	}
	greeting, err := readMySQLPacket(connection)
	if err != nil {
		return closeOnError(fmt.Errorf("read MySQL greeting: %w", err))
	}
	handshake, err := ParseMySQLHandshake(greeting.payload)
	if err != nil {
		return closeOnError(fmt.Errorf("parse MySQL greeting: %w", err))
	}
	policy, mode, err := upstreamTLSPolicy(instance)
	if err != nil {
		return closeOnError(err)
	}
	if mode == dbtls.ModeDisable {
		return connection, handshake, nil
	}
	if handshake.CapabilityFlags&mysqlClientSSL == 0 {
		return closeOnError(errors.New("MySQL upstream does not support TLS"))
	}
	selectDatabase := database != "" &&
		handshake.CapabilityFlags&mysqlClientConnectWithDB != 0
	if err := writeMySQLUpstreamTLSRequest(
		connection,
		handshake.CharacterSet,
		selectDatabase,
	); err != nil {
		return closeOnError(err)
	}
	secured, err := dbtls.HandshakeClient(ctx, connection, policy, upstreamAddress(instance))
	if err != nil {
		return closeOnError(fmt.Errorf("MySQL upstream TLS handshake: %w", err))
	}
	return secured, handshake, nil
}

func writeMySQLUpstreamTLSRequest(
	connection net.Conn,
	characterSet byte,
	selectDatabase bool,
) error {
	payload := make([]byte, 32)
	capabilities := uint32(
		mysqlClientProtocol41 |
			mysqlClientSecureConnection |
			mysqlClientPluginAuth |
			mysqlClientSSL,
	)
	if selectDatabase {
		capabilities |= mysqlClientConnectWithDB
	}
	binary.LittleEndian.PutUint32(payload[:4], capabilities)
	binary.LittleEndian.PutUint32(payload[4:8], 1<<24)
	if characterSet == 0 {
		characterSet = 45
	}
	payload[8] = characterSet
	_, err := connection.Write(mysqlPacketWithSeq(1, payload))
	if err != nil {
		return fmt.Errorf("write MySQL TLS request: %w", err)
	}
	return nil
}

func dialPostgresUpstream(ctx context.Context, instance model.DatabaseInstance) (net.Conn, error) {
	connection, err := dialUpstream(ctx, instance)
	if err != nil {
		return nil, err
	}
	policy, mode, err := upstreamTLSPolicy(instance)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	if mode == dbtls.ModeDisable {
		return connection, nil
	}
	request := make([]byte, 8)
	binary.BigEndian.PutUint32(request[:4], 8)
	binary.BigEndian.PutUint32(request[4:], 80877103)
	if _, err := connection.Write(request); err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("write PostgreSQL SSL request: %w", err)
	}
	response := []byte{0}
	if _, err := readFull(connection, response); err != nil || response[0] != 'S' {
		_ = connection.Close()
		if err != nil {
			return nil, fmt.Errorf("read PostgreSQL SSL response: %w", err)
		}
		return nil, errors.New("PostgreSQL upstream refused TLS")
	}
	secured, err := dbtls.HandshakeClient(ctx, connection, policy, upstreamAddress(instance))
	if err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("PostgreSQL upstream TLS handshake: %w", err)
	}
	return secured, nil
}

func dialRedisUpstream(ctx context.Context, instance model.DatabaseInstance) (net.Conn, error) {
	policy, mode, err := upstreamTLSPolicy(instance)
	if err != nil {
		return nil, err
	}
	if mode == dbtls.ModeDisable && !isLoopbackUpstream(instance.Address) {
		return nil, errors.New("remote Redis upstream requires TLS")
	}
	connection, err := dialUpstream(ctx, instance)
	if err != nil || mode == dbtls.ModeDisable {
		return connection, err
	}
	secured, err := dbtls.HandshakeClient(ctx, connection, policy, upstreamAddress(instance))
	if err != nil {
		_ = connection.Close()
		return nil, fmt.Errorf("Redis upstream TLS handshake: %w", err)
	}
	return secured, nil
}

func isLoopbackUpstream(address string) bool {
	address = strings.TrimSpace(address)
	if strings.EqualFold(address, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(address, "[]"))
	return ip != nil && ip.IsLoopback()
}

func requireVerifiedPostgresTLS(connection net.Conn) error {
	if !dbtls.IsVerified(connection) {
		return errVerifiedTLSRequired
	}
	return nil
}
