package dbproxy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strconv"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/util"
)

func TestProtocolHandlersRejectAccountsForDifferentInstanceProtocols(t *testing.T) {
	t.Run("mysql listener rejects Redis account", func(t *testing.T) {
		gateway, resolver := newCrossProtocolGateway(t, "redis")
		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()

		done := make(chan *gatewayConn, 1)
		go func() {
			done <- gateway.handleMySQL(context.Background(), server)
		}()
		if _, err := readMySQLPacket(client); err != nil {
			t.Fatalf("read MySQL handshake: %v", err)
		}
		if _, err := client.Write(protocolValidationMySQLLogin(databaseCompactUsername())); err != nil {
			t.Fatalf("write MySQL login: %v", err)
		}
		go drainProtocolRejection(client)

		select {
		case connection := <-done:
			if connection != nil {
				t.Fatal("MySQL handler accepted a Redis instance account")
			}
		case <-time.After(time.Second):
			t.Fatal("MySQL handler did not reject cross-protocol account")
		}
		if len(resolver.mysqlContexts) != 0 {
			t.Fatal("MySQL handler authenticated before rejecting cross-protocol account")
		}
	})

	t.Run("PostgreSQL listener rejects MySQL account", func(t *testing.T) {
		gateway, resolver := newCrossProtocolGateway(t, "mysql")
		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()

		startup := postgresStartupPacket(databaseCompactUsername(), "postgres")
		done := make(chan *gatewayConn, 1)
		go func() {
			done <- gateway.handlePG(context.Background(), server, startup[0])
		}()
		clientDone := make(chan struct{})
		go func() {
			defer close(clientDone)
			if _, err := client.Write(startup[1:]); err != nil {
				return
			}
			_ = client.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			var response [256]byte
			n, err := client.Read(response[:])
			if err != nil || n == 0 || response[0] != 'R' {
				return
			}
			passwordPayload := append([]byte("bastion-password"), 0)
			_ = writePostgresMessage(client, 'p', passwordPayload)
		}()

		select {
		case connection := <-done:
			if connection != nil {
				t.Fatal("PostgreSQL handler accepted a MySQL instance account")
			}
		case <-time.After(time.Second):
			t.Fatal("PostgreSQL handler did not reject cross-protocol account")
		}
		_ = client.Close()
		<-clientDone
		if len(resolver.passwordContexts) != 0 {
			t.Fatal("PostgreSQL handler authenticated before rejecting cross-protocol account")
		}
	})

	t.Run("Redis listener rejects PostgreSQL account", func(t *testing.T) {
		gateway, resolver := newCrossProtocolGateway(t, "postgres")
		server, client := net.Pipe()
		defer server.Close()
		defer client.Close()

		command := redisAuthCommand(databaseCompactUsername(), "bastion-password")
		done := make(chan *gatewayConn, 1)
		go func() {
			done <- gateway.handleRedis(context.Background(), server, command[0])
		}()
		clientDone := make(chan struct{})
		go func() {
			defer close(clientDone)
			if _, err := client.Write(command[1:]); err != nil {
				return
			}
			drainProtocolRejection(client)
		}()

		select {
		case connection := <-done:
			if connection != nil {
				t.Fatal("Redis handler accepted a PostgreSQL instance account")
			}
		case <-time.After(time.Second):
			t.Fatal("Redis handler did not reject cross-protocol account")
		}
		_ = client.Close()
		<-clientDone
		if len(resolver.passwordContexts) != 0 {
			t.Fatal("Redis handler authenticated before rejecting cross-protocol account")
		}
	})
}

func newCrossProtocolGateway(t *testing.T, instanceProtocol string) (*Gateway, *captureDatabaseAccountResolver) {
	t.Helper()
	db := newResolvableDatabaseAccount(t, nil)
	if err := db.Model(&model.DatabaseInstance{}).
		Where("id = ?", "db-instance-1").
		Update("protocol", instanceProtocol).Error; err != nil {
		t.Fatalf("update instance protocol: %v", err)
	}
	resolver := &captureDatabaseAccountResolver{
		err:      errors.New("authentication must not run"),
		mysqlErr: errors.New("authentication must not run"),
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &Gateway{db: db, store: resolver, logger: logger}, resolver
}

func databaseCompactUsername() string {
	return util.FullUsername(util.PrefixDatabase, 1, 1)
}

func protocolValidationMySQLLogin(username string) []byte {
	packet := mysqlLoginPacket(username)
	packet = append(packet, 'x', 0)
	payloadLength := len(packet) - 4
	packet[0] = byte(payloadLength)
	packet[1] = byte(payloadLength >> 8)
	packet[2] = byte(payloadLength >> 16)
	return packet
}

func redisAuthCommand(username, password string) []byte {
	command := make([]byte, 0, 128)
	command = append(command, []byte("*3\r\n$4\r\nAUTH\r\n")...)
	command = append(command, []byte("$"+strconv.Itoa(len(username))+"\r\n"+username+"\r\n")...)
	command = append(command, []byte("$"+strconv.Itoa(len(password))+"\r\n"+password+"\r\n")...)
	return command
}

func drainProtocolRejection(connection net.Conn) {
	_ = connection.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	var buffer [256]byte
	_, _ = connection.Read(buffer[:])
}
