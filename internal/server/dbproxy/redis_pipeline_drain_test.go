package dbproxy

import (
	"bytes"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestRedisRelayDrainsLegalResponseBeforeCrossChunkRejection(t *testing.T) {
	legal := redisObserverTestCommand("SET", "key", "value")
	rejected := redisObserverTestCommand("AUTH", "replacement-user", "replacement-password")
	clientGateway, clientPeer := net.Pipe()
	upstreamGateway, upstreamPeer := net.Pipe()
	defer clientPeer.Close()
	defer upstreamPeer.Close()
	sink := &captureSink{}
	done := make(chan struct{})
	go func() {
		relayGatewayConnection(clientGateway, upstreamGateway, &redisObserver{sink: sink})
		close(done)
	}()

	if _, err := clientPeer.Write(legal); err != nil {
		t.Fatal(err)
	}
	gotLegal := make([]byte, len(legal))
	if _, err := io.ReadFull(upstreamPeer, gotLegal); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotLegal, legal) {
		t.Fatalf("upstream legal bytes = %q", gotLegal)
	}
	if _, err := clientPeer.Write(rejected); err != nil {
		t.Fatal(err)
	}

	_ = clientPeer.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	var early [64]byte
	if n, err := clientPeer.Read(early[:]); err == nil || n != 0 {
		t.Fatalf("client received terminal rejection before legal response: %q, %v", early[:n], err)
	}
	_ = clientPeer.SetReadDeadline(time.Time{})

	if _, err := upstreamPeer.Write([]byte("+OK\r\n")); err != nil {
		t.Fatal(err)
	}
	_ = clientPeer.SetReadDeadline(time.Now().Add(time.Second))
	response, err := io.ReadAll(clientPeer)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(response, []byte("+OK\r\n-ERR ")) {
		t.Fatalf("client response = %q, want legal result then terminal error", response)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("relay did not terminate")
	}
	if len(sink.finished) != 1 || sink.finished[0].Status != queryStatusSuccess {
		t.Fatalf("legal audit = %#v, want real success", sink.finished)
	}
}

func TestRedisRelayDrainsAfterClientReadEOFWithDeferredRejection(t *testing.T) {
	legal := redisObserverTestCommand("SET", "key", "value")
	rejected := redisObserverTestCommand("AUTH", "replacement-user", "replacement-password")
	client := newDrainClientConn(legal, rejected)
	upstream := newDrainUpstreamConn(client.readEOF)
	done := make(chan struct{})
	go func() {
		relayGatewayConnection(client, upstream, &redisObserver{sink: &captureSink{}})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("relay did not drain after client EOF")
	}
	if got := upstream.writtenBytes(); !bytes.Equal(got, legal) {
		t.Fatalf("upstream bytes = %q, want legal command only", got)
	}
	if got := client.writtenBytes(); !bytes.HasPrefix(got, []byte("+OK\r\n-ERR ")) {
		t.Fatalf("client bytes = %q, want response before terminal error", got)
	}
}

type drainClientConn struct {
	mu        sync.Mutex
	chunks    [][]byte
	index     int
	writes    bytes.Buffer
	readEOF   chan struct{}
	closeOnce sync.Once
	closed    chan struct{}
}

func newDrainClientConn(chunks ...[]byte) *drainClientConn {
	return &drainClientConn{chunks: chunks, readEOF: make(chan struct{}), closed: make(chan struct{})}
}

func (c *drainClientConn) Read(buffer []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.index < len(c.chunks) {
		chunk := c.chunks[c.index]
		c.index++
		return copy(buffer, chunk), nil
	}
	c.closeOnce.Do(func() { close(c.readEOF) })
	return 0, io.EOF
}

func (c *drainClientConn) Write(buffer []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writes.Write(buffer)
}

func (c *drainClientConn) writtenBytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.writes.Bytes()...)
}

func (c *drainClientConn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}

func (*drainClientConn) LocalAddr() net.Addr              { return observerCopyAddr("local") }
func (*drainClientConn) RemoteAddr() net.Addr             { return observerCopyAddr("remote") }
func (*drainClientConn) SetDeadline(time.Time) error      { return nil }
func (*drainClientConn) SetReadDeadline(time.Time) error  { return nil }
func (*drainClientConn) SetWriteDeadline(time.Time) error { return nil }

type drainUpstreamConn struct {
	mu        sync.Mutex
	readEOF   <-chan struct{}
	readOnce  bool
	writes    bytes.Buffer
	closeOnce sync.Once
	closed    chan struct{}
}

func newDrainUpstreamConn(readEOF <-chan struct{}) *drainUpstreamConn {
	return &drainUpstreamConn{readEOF: readEOF, closed: make(chan struct{})}
}

func (c *drainUpstreamConn) Read(buffer []byte) (int, error) {
	<-c.readEOF
	c.mu.Lock()
	if !c.readOnce {
		c.readOnce = true
		c.mu.Unlock()
		return copy(buffer, []byte("+OK\r\n")), nil
	}
	c.mu.Unlock()
	<-c.closed
	return 0, net.ErrClosed
}

func (c *drainUpstreamConn) Write(buffer []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writes.Write(buffer)
}

func (c *drainUpstreamConn) writtenBytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.writes.Bytes()...)
}

func (c *drainUpstreamConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (*drainUpstreamConn) LocalAddr() net.Addr              { return observerCopyAddr("local") }
func (*drainUpstreamConn) RemoteAddr() net.Addr             { return observerCopyAddr("remote") }
func (*drainUpstreamConn) SetDeadline(time.Time) error      { return nil }
func (*drainUpstreamConn) SetReadDeadline(time.Time) error  { return nil }
func (*drainUpstreamConn) SetWriteDeadline(time.Time) error { return nil }

var _ net.Conn = (*drainClientConn)(nil)
var _ net.Conn = (*drainUpstreamConn)(nil)
