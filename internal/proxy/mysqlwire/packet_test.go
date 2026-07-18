package mysqlwire

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestEncodePacketEnforcesProtocolPayloadLimit(t *testing.T) {
	payload := make([]byte, MaxPacketPayloadBytes)
	packet, err := EncodePacket(7, payload)
	if err != nil {
		t.Fatalf("encode maximum payload: %v", err)
	}
	if len(packet) != MaxPacketPayloadBytes+4 ||
		packet[0] != 0xff || packet[1] != 0xff || packet[2] != 0xff ||
		packet[3] != 7 {
		t.Fatalf("maximum packet header is invalid: len=%d header=%x", len(packet), packet[:4])
	}
	if _, err := EncodePacket(0, make([]byte, MaxPacketPayloadBytes+1)); err == nil {
		t.Fatal("oversized payload was truncated into a three-byte packet length")
	}
}

func TestWritePacketRejectsOversizedPayloadBeforeWriting(t *testing.T) {
	conn := &countingConn{}
	err := WritePacket(
		context.Background(),
		conn,
		0,
		make([]byte, MaxPacketPayloadBytes+1),
	)
	if err == nil {
		t.Fatal("oversized payload was written")
	}
	if conn.writes != 0 {
		t.Fatalf("oversized payload reached connection: writes=%d", conn.writes)
	}
}

func TestReadPacketCancellationInterruptsSilentConnection(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := ReadPacket(ctx, client, 1024)
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled packet read unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("context cancellation did not interrupt packet read")
	}
}

func TestWritePacketCancellationInterruptsBlockedConnection(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- WritePacket(ctx, client, 0, make([]byte, 1024))
	}()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled packet write unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("context cancellation did not interrupt packet write")
	}
}

type countingConn struct {
	writes int
}

func (c *countingConn) Read([]byte) (int, error)         { return 0, nil }
func (c *countingConn) Write(p []byte) (int, error)      { c.writes++; return len(p), nil }
func (c *countingConn) Close() error                     { return nil }
func (c *countingConn) LocalAddr() net.Addr              { return nil }
func (c *countingConn) RemoteAddr() net.Addr             { return nil }
func (c *countingConn) SetDeadline(time.Time) error      { return nil }
func (c *countingConn) SetReadDeadline(time.Time) error  { return nil }
func (c *countingConn) SetWriteDeadline(time.Time) error { return nil }
