package dbproxy

import (
	"bytes"
	"net"
	"sync"
	"testing"
	"time"
)

func TestRelayBoundsPendingDrainAfterClientEOF(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		request  []byte
	}{
		{
			name:     "redis BLPOP zero",
			protocol: "redis",
			request:  redisObserverTestCommand("BLPOP", "queue", "0"),
		},
		{
			name:     "long mysql query",
			protocol: "mysql",
			request:  buildMySQLPacket(0, append([]byte{0x03}, []byte("select sleep(3600)")...)),
		},
		{
			name:     "postgres execute without sync",
			protocol: "postgres",
			request: bytes.Join([][]byte{
				postgresMessage('P', []byte("statement\x00select pg_sleep(3600)\x00\x00\x00")),
				postgresBindNoParameters("portal", "statement"),
				postgresMessage('E', []byte("portal\x00\x00\x00\x00\x00")),
			}, nil),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const drainTimeout = 20 * time.Millisecond
			client := newDrainClientConn(tt.request)
			upstream := newBlockingDrainUpstreamConn()
			sink := &captureSink{}
			observer := newQueryObserver(tt.protocol, sink)
			done := make(chan struct{})
			started := time.Now()

			go func() {
				relayGatewayConnectionWithDrainTimeout(client, upstream, observer, drainTimeout)
				close(done)
			}()

			select {
			case <-done:
			case <-time.After(500 * time.Millisecond):
				t.Fatal("relay did not terminate within the injected pending drain limit")
			}
			if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
				t.Fatalf("relay elapsed = %v, want bounded close near %v", elapsed, drainTimeout)
			}
			select {
			case <-upstream.readExited:
			case <-time.After(100 * time.Millisecond):
				t.Fatal("upstream read goroutine did not exit after drain timeout")
			}
			if !drainConnClosed(client.closed) || !drainConnClosed(upstream.closed) {
				t.Fatal("drain timeout did not close both client and upstream")
			}
			if got := upstream.writtenBytes(); !bytes.Equal(got, tt.request) {
				t.Fatalf("upstream request = %x, want %x", got, tt.request)
			}
			if len(sink.finished) != 1 {
				t.Fatalf("finished audit events = %d, want 1", len(sink.finished))
			}
			if finish := sink.finished[0]; finish.Status != queryStatusError ||
				finish.ErrorCode != observerErrorDrainTimeout {
				t.Fatalf("timeout audit = %#v", finish)
			}
			if len(client.writtenBytes()) == 0 {
				t.Fatal("client did not receive a fixed terminal protocol error")
			}
		})
	}
}

func TestDefaultPendingDrainTimeoutIsFinite(t *testing.T) {
	if relayPendingDrainTimeout <= 0 || relayPendingDrainTimeout > 30*time.Second {
		t.Fatalf("default pending drain timeout = %v, want finite positive operational bound", relayPendingDrainTimeout)
	}
}

type blockingDrainUpstreamConn struct {
	mu           sync.Mutex
	writes       bytes.Buffer
	closed       chan struct{}
	readExited   chan struct{}
	closeOnce    sync.Once
	readExitOnce sync.Once
}

func newBlockingDrainUpstreamConn() *blockingDrainUpstreamConn {
	return &blockingDrainUpstreamConn{
		closed:     make(chan struct{}),
		readExited: make(chan struct{}),
	}
}

func (c *blockingDrainUpstreamConn) Read([]byte) (int, error) {
	<-c.closed
	c.readExitOnce.Do(func() { close(c.readExited) })
	return 0, net.ErrClosed
}

func (c *blockingDrainUpstreamConn) Write(buffer []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writes.Write(buffer)
}

func (c *blockingDrainUpstreamConn) writtenBytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.writes.Bytes()...)
}

func (c *blockingDrainUpstreamConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (*blockingDrainUpstreamConn) LocalAddr() net.Addr              { return observerCopyAddr("local") }
func (*blockingDrainUpstreamConn) RemoteAddr() net.Addr             { return observerCopyAddr("remote") }
func (*blockingDrainUpstreamConn) SetDeadline(time.Time) error      { return nil }
func (*blockingDrainUpstreamConn) SetReadDeadline(time.Time) error  { return nil }
func (*blockingDrainUpstreamConn) SetWriteDeadline(time.Time) error { return nil }

func drainConnClosed(closed <-chan struct{}) bool {
	select {
	case <-closed:
		return true
	default:
		return false
	}
}

var _ net.Conn = (*blockingDrainUpstreamConn)(nil)
