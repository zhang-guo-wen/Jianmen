package dbproxy

import (
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestRelayFinalizesPendingAuditOnUpstreamEOF(t *testing.T) {
	sink := &captureSink{}
	observer := newQueryObserver("mysql", sink)
	observer.ObserveClientBytes(buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...)))
	copyUpstreamToClient(newObserverCopyConn(nil), newObserverCopyConn(nil), observer)
	if len(sink.finished) != 1 || sink.finished[0].Status != queryStatusError {
		t.Fatalf("finished = %#v, want terminal error after EOF", sink.finished)
	}
}

func TestRelayFinalizesPendingAuditOnWriteFailure(t *testing.T) {
	sink := &captureSink{}
	observer := newQueryObserver("mysql", sink)
	client := newObserverCopyConn(buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...)))
	upstream := &failingRelayConn{writeErr: io.ErrClosedPipe}
	copyClientToUpstream(client, upstream, observer)
	if len(sink.finished) != 1 || sink.finished[0].Status != queryStatusError {
		t.Fatalf("finished = %#v, want terminal error after write failure", sink.finished)
	}
	if !client.closed || !upstream.closed.Load() {
		t.Fatal("write failure did not close both relay connections")
	}
}

func TestRelayPanicIsTerminalAndAbortsObserver(t *testing.T) {
	observer := &panicLifecycleObserver{}
	client := newObserverCopyConn([]byte("trigger"))
	upstream := newObserverCopyConn(nil)
	copyClientToUpstream(client, upstream, observer)
	if observer.aborts.Load() != 1 {
		t.Fatalf("abort calls = %d, want 1", observer.aborts.Load())
	}
	if !client.closed || !upstream.closed {
		t.Fatal("relay panic did not close both connections")
	}
}

type panicLifecycleObserver struct {
	aborts atomic.Int32
}

func (*panicLifecycleObserver) ObserveClientBytes([]byte) *queryDecision {
	panic("relay observer panic")
}
func (*panicLifecycleObserver) ObserveServerBytes([]byte) *queryDecision { return nil }
func (*panicLifecycleObserver) ErrorResponse(queryDecision) []byte {
	return []byte("-ERR database proxy relay terminated\r\n")
}
func (o *panicLifecycleObserver) Abort(string) { o.aborts.Add(1) }
func (*panicLifecycleObserver) HasPending() bool {
	return true
}

type failingRelayConn struct {
	writeErr error
	closed   atomic.Bool
}

func (*failingRelayConn) Read([]byte) (int, error)    { return 0, io.EOF }
func (c *failingRelayConn) Write([]byte) (int, error) { return 0, c.writeErr }
func (c *failingRelayConn) Close() error {
	c.closed.Store(true)
	return nil
}
func (*failingRelayConn) LocalAddr() net.Addr              { return observerCopyAddr("local") }
func (*failingRelayConn) RemoteAddr() net.Addr             { return observerCopyAddr("remote") }
func (*failingRelayConn) SetDeadline(time.Time) error      { return nil }
func (*failingRelayConn) SetReadDeadline(time.Time) error  { return nil }
func (*failingRelayConn) SetWriteDeadline(time.Time) error { return nil }

var _ queryObserver = (*panicLifecycleObserver)(nil)
var _ net.Conn = (*failingRelayConn)(nil)
