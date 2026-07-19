package dbproxy

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"

	"jianmen/internal/config"
)

const (
	postgresSSLRequestCode    = 80877103
	postgresGSSENCRequestCode = 80877104
	postgresTLSALPN           = "postgresql"
	maxPostgresTLSRecordBytes = 18 * 1024
)

type postgresPrefixedConn struct {
	net.Conn
	prefix []byte
}

func (connection *postgresPrefixedConn) Read(buffer []byte) (int, error) {
	if len(connection.prefix) == 0 {
		return connection.Conn.Read(buffer)
	}
	read := copy(buffer, connection.prefix)
	connection.prefix = connection.prefix[read:]
	return read, nil
}

func (g *Gateway) handlePostgresConnection(
	ctx context.Context,
	client net.Conn,
	listenerConfig config.DatabaseProtocolListener,
) *gatewayConn {
	first := make([]byte, postgresCancelRequestHeaderSize)
	if _, err := readFull(client, first); err != nil {
		return nil
	}
	if isPostgresDirectTLSClientHello(first) {
		prefixed := &postgresPrefixedConn{Conn: client, prefix: append([]byte(nil), first...)}
		return g.handlePostgresTLSConnection(ctx, prefixed, listenerConfig, true)
	}
	if isPostgresGSSENCRequest(first) {
		if err := writePostgresBytes(client, []byte{'N'}); err != nil {
			return nil
		}
		if _, err := readFull(client, first); err != nil {
			return nil
		}
	}
	if isPostgresCancelRequestHeader(first) {
		message, err := readPostgresCancelRequest(client, first)
		if err == nil {
			err = g.forwardPostgresCancel(ctx, message)
		}
		if err != nil {
			g.logger.Warn("db gateway rejected PostgreSQL CancelRequest")
		}
		return nil
	}
	if !isPostgresSSLRequest(first) {
		drainPostgresPlaintextStartup(client, first)
		writePostgresTLSError(client)
		return nil
	}
	if listenerConfig.CertFile == "" || listenerConfig.KeyFile == "" {
		_ = writePostgresBytes(client, []byte{'N'})
		return nil
	}
	if err := writePostgresBytes(client, []byte{'S'}); err != nil {
		return nil
	}
	return g.handlePostgresTLSConnection(ctx, client, listenerConfig, false)
}

func (g *Gateway) handlePostgresTLSConnection(
	ctx context.Context,
	client net.Conn,
	listenerConfig config.DatabaseProtocolListener,
	direct bool,
) *gatewayConn {
	tlsConfig, err := postgresListenerTLSConfig(listenerConfig)
	if err != nil {
		g.logger.Error("load PostgreSQL listener certificate", "error", err)
		return nil
	}
	secured := tls.Server(client, tlsConfig)
	if err := secured.HandshakeContext(ctx); err != nil {
		return nil
	}
	if direct && secured.ConnectionState().NegotiatedProtocol != postgresTLSALPN {
		return nil
	}
	firstByte := make([]byte, 1)
	if _, err := io.ReadFull(secured, firstByte); err != nil {
		return nil
	}
	return g.handlePostgresAfterTLS(ctx, secured, firstByte[0])
}

func (g *Gateway) handlePostgresAfterTLS(
	ctx context.Context,
	client net.Conn,
	firstByte byte,
) *gatewayConn {
	connection := g.handlePG(ctx, client, firstByte)
	if connection != nil {
		connection.client = client
	}
	return connection
}

func postgresListenerTLSConfig(listener config.DatabaseProtocolListener) (*tls.Config, error) {
	tlsConfig, err := databaseListenerTLSConfig(listener)
	if err != nil {
		return nil, err
	}
	tlsConfig.NextProtos = []string{postgresTLSALPN}
	return tlsConfig, nil
}

func isPostgresDirectTLSClientHello(header []byte) bool {
	if len(header) < 6 || header[0] != 22 || header[1] != 3 ||
		header[2] < 1 || header[2] > 4 || header[5] != 1 {
		return false
	}
	recordLength := int(binary.BigEndian.Uint16(header[3:5]))
	return recordLength >= 4 && recordLength <= maxPostgresTLSRecordBytes
}

func isPostgresSSLRequest(header []byte) bool {
	return len(header) == 8 &&
		binary.BigEndian.Uint32(header[:4]) == 8 &&
		binary.BigEndian.Uint32(header[4:8]) == postgresSSLRequestCode
}

func isPostgresGSSENCRequest(header []byte) bool {
	return len(header) == 8 &&
		binary.BigEndian.Uint32(header[:4]) == 8 &&
		binary.BigEndian.Uint32(header[4:8]) == postgresGSSENCRequestCode
}

func writePostgresTLSError(connection net.Conn) {
	payload := []byte("SFATAL\x00MSSL is required for password authentication\x00\x00")
	_ = writePostgresMessage(connection, 'E', payload)
}

func drainPostgresPlaintextStartup(connection net.Conn, header []byte) {
	if len(header) != 8 {
		return
	}
	messageLength := int(binary.BigEndian.Uint32(header[:4]))
	if messageLength < 8 || messageLength > maxPostgresStartupMessageBytes {
		return
	}
	remainder := make([]byte, messageLength-len(header))
	_, _ = io.ReadFull(connection, remainder)
}
