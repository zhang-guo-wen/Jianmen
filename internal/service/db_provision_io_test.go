package service

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"jianmen/internal/proxy/mysqlwire"
)

func TestMySQLExecDeadlineBoundsSilentUpstream(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	commandReceived := make(chan struct{})
	go func() {
		if _, err := mysqlwire.ReadPacket(context.Background(), server, 1024); err == nil {
			close(commandReceived)
		}
	}()
	started := time.Now()
	err := mysqlExecWithTimeout(
		context.Background(),
		client,
		"CREATE USER 'jm_test'@'10.0.0.8'",
		50*time.Millisecond,
	)
	if !errors.Is(err, errMySQLStatementOutcomeUncertain) {
		t.Fatalf("silent upstream error = %v, want uncertain statement outcome", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("silent upstream blocked past command deadline: %s", elapsed)
	}
	select {
	case <-commandReceived:
	default:
		t.Fatal("command was not sent before read deadline")
	}
}

func TestMySQLExecCancellationInterruptsSilentUpstream(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	commandReceived := make(chan struct{})
	go func() {
		if _, err := mysqlwire.ReadPacket(context.Background(), server, 1024); err == nil {
			close(commandReceived)
		}
	}()
	done := make(chan error, 1)
	go func() {
		done <- mysqlExecWithTimeout(ctx, client, "SET @a = 1", time.Minute)
	}()
	select {
	case <-commandReceived:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("server did not receive command")
	}
	select {
	case err := <-done:
		if !errors.Is(err, errMySQLStatementOutcomeUncertain) {
			t.Fatalf("canceled exec error = %v, want uncertain outcome", err)
		}
	case <-time.After(time.Second):
		t.Fatal("context cancellation did not interrupt mysqlExec")
	}
}

func TestMySQLQueryCancellationInterruptsSilentUpstream(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	commandReceived := make(chan struct{})
	go func() {
		if _, err := mysqlwire.ReadPacket(context.Background(), server, 1024); err == nil {
			close(commandReceived)
		}
	}()
	done := make(chan error, 1)
	go func() {
		_, err := mysqlQueryWithTimeout(ctx, client, "SHOW DATABASES", time.Minute)
		done <- err
	}()
	select {
	case <-commandReceived:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("server did not receive query")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled query unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("context cancellation did not interrupt mysqlQuery")
	}
}

func TestMySQLExecClassifiesPreWriteCancellationAsNotSent(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := mysqlExecWithTimeout(ctx, client, "CREATE USER 'x'@'10.0.0.8'", time.Second)
	if !errors.Is(err, errMySQLStatementNotSent) {
		t.Fatalf("pre-write cancellation error = %v, want statement-not-sent", err)
	}
}

func TestWriteMySQLRawPacketCancellationInterruptsBlockedLoginWrite(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	// The upstream has already sent its handshake.  It then stops reading the
	// login packet, which makes the client-side write block until cancellation.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- writeMySQLRawPacket(ctx, client, make([]byte, 1024))
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled login write error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("context cancellation did not interrupt blocked mysql login write")
	}
}
