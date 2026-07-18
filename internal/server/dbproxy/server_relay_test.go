package dbproxy

import (
	"bytes"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestHandleGatewayConnSerializesObserverErrorWithServerResponse(t *testing.T) {
	client := newSerializedRelayClientConn()
	upstream := newSerializedRelayUpstreamConn()
	defer client.Close()
	defer upstream.Close()
	gateway := &Gateway{
		replayDir: t.TempDir(),
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	connection := &gatewayConn{
		protocol:     "redis",
		accountID:    "account-1",
		instanceID:   "instance-1",
		accountName:  "account-name",
		accountUser:  "upstream-user",
		instanceName: "redis-instance",
		userID:       "user-1",
		upstream:     upstream,
		upstreamAddr: "127.0.0.1:6379",
	}
	done := make(chan struct{})
	go func() {
		gateway.handleGatewayConn(client, connection)
		close(done)
	}()

	select {
	case <-client.normalWriteStarted:
	case <-time.After(time.Second):
		t.Fatal("upstream response write did not start")
	}
	select {
	case <-client.secondWriteEntered:
		t.Fatal("observer error write entered net.Conn.Write while upstream response write was active")
	case <-time.After(50 * time.Millisecond):
	}

	close(client.releaseNormalWrite)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("relay did not stop after observer fatal")
	}

	if client.concurrentWrite.Load() {
		t.Fatal("client connection received concurrent writes")
	}
	want := []byte("+PONG\r\n-ERR database proxy rejected command\r\n")
	if got := client.writtenBytes(); !bytes.Equal(got, want) {
		t.Fatalf("serialized client bytes = %q, want %q", got, want)
	}
	if got := upstream.writtenBytes(); !bytes.Equal(got, redisObserverTestCommand("PING")) {
		t.Fatalf("upstream bytes = %q, want only the valid PING command", got)
	}
}

func TestHandleGatewayConnClosesWhenObserverErrorWriterIsBlocked(t *testing.T) {
	client := newSerializedRelayClientConn()
	upstream := newSerializedRelayUpstreamConn()
	defer client.Close()
	defer upstream.Close()
	gateway := &Gateway{
		replayDir: t.TempDir(),
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	connection := &gatewayConn{
		protocol:     "redis",
		accountID:    "account-1",
		instanceID:   "instance-1",
		accountName:  "account-name",
		accountUser:  "upstream-user",
		instanceName: "redis-instance",
		userID:       "user-1",
		upstream:     upstream,
		upstreamAddr: "127.0.0.1:6379",
	}

	done := make(chan struct{})
	go func() {
		gateway.handleGatewayConn(client, connection)
		close(done)
	}()

	select {
	case <-client.normalWriteStarted:
	case <-time.After(time.Second):
		t.Fatal("upstream response write did not start")
	}
	startedWaiting := time.Now()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("blocked client writer prevented coordinated relay shutdown")
	}
	elapsed := time.Since(startedWaiting)
	if elapsed < observerErrorWriteTimeout/2 {
		t.Fatalf("relay stopped after %v, want observer error writer to honor the approximate %v lock budget", elapsed, observerErrorWriteTimeout)
	}
	if elapsed > observerErrorWriteTimeout+500*time.Millisecond {
		t.Fatalf("relay stopped after %v, want shutdown near the %v observer error write limit", elapsed, observerErrorWriteTimeout)
	}
	if !client.isClosed() || !upstream.isClosed() {
		t.Fatalf("connections closed = client:%t upstream:%t, want both true", client.isClosed(), upstream.isClosed())
	}
	if client.concurrentWrite.Load() {
		t.Fatal("client connection received concurrent writes")
	}
}

func TestHandleGatewayConnWaitsForFinishQueryAndBothRelays(t *testing.T) {
	audit := newBlockingFinishAudit()
	client := newLifecycleClientConn(redisObserverTestCommand("SET", "key", "value"), audit.finishStarted)
	upstream := newLifecycleUpstreamConn()
	defer client.Close()
	defer upstream.Close()
	gateway := &Gateway{
		replayDir: t.TempDir(),
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		audit:     audit,
	}
	connection := &gatewayConn{
		protocol:     "redis",
		accountID:    "account-1",
		instanceID:   "instance-1",
		accountName:  "account-name",
		accountUser:  "upstream-user",
		instanceName: "redis-instance",
		userID:       "user-1",
		upstream:     upstream,
		upstreamAddr: "127.0.0.1:6379",
	}

	done := make(chan struct{})
	go func() {
		gateway.handleGatewayConn(client, connection)
		close(done)
	}()

	select {
	case <-audit.finishStarted:
	case <-time.After(time.Second):
		t.Fatal("FinishQuery did not reach the audit writer")
	}
	select {
	case <-done:
		t.Fatal("handleGatewayConn returned while FinishQuery was blocked")
	default:
	}

	close(audit.releaseFinish)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("handleGatewayConn did not return after FinishQuery completed; write deadlines=%d", client.writeDeadlineCalls.Load())
	}

	select {
	case <-audit.finishReturned:
	default:
		t.Fatal("handleGatewayConn returned before FinishQuery completed")
	}
	if !client.isClosed() || !upstream.isClosed() {
		t.Fatalf("connections closed = client:%t upstream:%t, want both true", client.isClosed(), upstream.isClosed())
	}
	select {
	case <-client.writeExited:
	default:
		t.Fatal("handleGatewayConn returned before the server-to-client relay exited")
	}
}

type serializedRelayClientConn struct {
	readCount          atomic.Int32
	activeWrites       atomic.Int32
	concurrentWrite    atomic.Bool
	normalWriteStarted chan struct{}
	releaseNormalWrite chan struct{}
	secondWriteEntered chan struct{}
	closed             chan struct{}
	closeOnce          sync.Once
	writeMu            sync.Mutex
	writes             bytes.Buffer
}

func newSerializedRelayClientConn() *serializedRelayClientConn {
	return &serializedRelayClientConn{
		normalWriteStarted: make(chan struct{}),
		releaseNormalWrite: make(chan struct{}),
		secondWriteEntered: make(chan struct{}),
		closed:             make(chan struct{}),
	}
}

func (c *serializedRelayClientConn) Read(buffer []byte) (int, error) {
	switch c.readCount.Add(1) {
	case 1:
		return copy(buffer, redisObserverTestCommand("PING")), nil
	case 2:
		<-c.normalWriteStarted
		return copy(buffer, []byte("PING\r\n")), nil
	default:
		<-c.closed
		return 0, net.ErrClosed
	}
}

func (c *serializedRelayClientConn) Write(buffer []byte) (int, error) {
	active := c.activeWrites.Add(1)
	if active > 1 {
		c.concurrentWrite.Store(true)
	}
	defer c.activeWrites.Add(-1)

	if bytes.HasPrefix(buffer, []byte("+PONG")) {
		close(c.normalWriteStarted)
		select {
		case <-c.releaseNormalWrite:
		case <-c.closed:
			return 0, net.ErrClosed
		}
	} else {
		close(c.secondWriteEntered)
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writes.Write(buffer)
}

func (c *serializedRelayClientConn) writtenBytes() []byte {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return append([]byte(nil), c.writes.Bytes()...)
}

func (c *serializedRelayClientConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *serializedRelayClientConn) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

func (c *serializedRelayClientConn) LocalAddr() net.Addr              { return observerCopyAddr("local") }
func (c *serializedRelayClientConn) RemoteAddr() net.Addr             { return observerCopyAddr("remote") }
func (c *serializedRelayClientConn) SetDeadline(time.Time) error      { return nil }
func (c *serializedRelayClientConn) SetReadDeadline(time.Time) error  { return nil }
func (c *serializedRelayClientConn) SetWriteDeadline(time.Time) error { return nil }

type serializedRelayUpstreamConn struct {
	readOnce       atomic.Bool
	commandWritten chan struct{}
	writeOnce      sync.Once
	closed         chan struct{}
	closeOnce      sync.Once
	writeMu        sync.Mutex
	writes         bytes.Buffer
}

func newSerializedRelayUpstreamConn() *serializedRelayUpstreamConn {
	return &serializedRelayUpstreamConn{
		commandWritten: make(chan struct{}),
		closed:         make(chan struct{}),
	}
}

func (c *serializedRelayUpstreamConn) Read(buffer []byte) (int, error) {
	if !c.readOnce.Swap(true) {
		<-c.commandWritten
		return copy(buffer, []byte("+PONG\r\n")), nil
	}
	<-c.closed
	return 0, net.ErrClosed
}

func (c *serializedRelayUpstreamConn) Write(buffer []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.writeOnce.Do(func() { close(c.commandWritten) })
	return c.writes.Write(buffer)
}

func (c *serializedRelayUpstreamConn) writtenBytes() []byte {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return append([]byte(nil), c.writes.Bytes()...)
}

func (c *serializedRelayUpstreamConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *serializedRelayUpstreamConn) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

func (c *serializedRelayUpstreamConn) LocalAddr() net.Addr              { return observerCopyAddr("local") }
func (c *serializedRelayUpstreamConn) RemoteAddr() net.Addr             { return observerCopyAddr("remote") }
func (c *serializedRelayUpstreamConn) SetDeadline(time.Time) error      { return nil }
func (c *serializedRelayUpstreamConn) SetReadDeadline(time.Time) error  { return nil }
func (c *serializedRelayUpstreamConn) SetWriteDeadline(time.Time) error { return nil }

type blockingFinishAudit struct {
	finishStarted  chan struct{}
	releaseFinish  chan struct{}
	finishReturned chan struct{}
	startOnce      sync.Once
	returnOnce     sync.Once
}

func newBlockingFinishAudit() *blockingFinishAudit {
	return &blockingFinishAudit{
		finishStarted:  make(chan struct{}),
		releaseFinish:  make(chan struct{}),
		finishReturned: make(chan struct{}),
	}
}

func (a *blockingFinishAudit) CreateAuditSession(*model.AuditSession) error { return nil }
func (a *blockingFinishAudit) EndAuditSession(string) error                 { return nil }
func (a *blockingFinishAudit) CreateAuditDBQuery(*model.AuditDBQuery) error {
	a.startOnce.Do(func() { close(a.finishStarted) })
	<-a.releaseFinish
	a.returnOnce.Do(func() { close(a.finishReturned) })
	return nil
}

type lifecycleClientConn struct {
	command            []byte
	readOnce           atomic.Bool
	finishStarted      <-chan struct{}
	closed             chan struct{}
	closeOnce          sync.Once
	writeExited        chan struct{}
	writeExitOnce      sync.Once
	writeDeadlineCalls atomic.Int32
}

func newLifecycleClientConn(command []byte, finishStarted <-chan struct{}) *lifecycleClientConn {
	return &lifecycleClientConn{
		command:       append([]byte(nil), command...),
		finishStarted: finishStarted,
		closed:        make(chan struct{}),
		writeExited:   make(chan struct{}),
	}
}

func (c *lifecycleClientConn) Read(buffer []byte) (int, error) {
	if !c.readOnce.Swap(true) {
		return copy(buffer, c.command), nil
	}
	<-c.finishStarted
	return 0, io.EOF
}

func (c *lifecycleClientConn) Write([]byte) (int, error) {
	<-c.closed
	c.writeExitOnce.Do(func() { close(c.writeExited) })
	return 0, net.ErrClosed
}

func (c *lifecycleClientConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *lifecycleClientConn) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

func (c *lifecycleClientConn) LocalAddr() net.Addr             { return observerCopyAddr("local") }
func (c *lifecycleClientConn) RemoteAddr() net.Addr            { return observerCopyAddr("remote") }
func (c *lifecycleClientConn) SetDeadline(time.Time) error     { return nil }
func (c *lifecycleClientConn) SetReadDeadline(time.Time) error { return nil }
func (c *lifecycleClientConn) SetWriteDeadline(deadline time.Time) error {
	c.writeDeadlineCalls.Add(1)
	go func() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		select {
		case <-timer.C:
			_ = c.Close()
		case <-c.closed:
		}
	}()
	return nil
}

type lifecycleUpstreamConn struct {
	commandWritten chan struct{}
	writeOnce      sync.Once
	readOnce       atomic.Bool
	closed         chan struct{}
	closeOnce      sync.Once
}

func newLifecycleUpstreamConn() *lifecycleUpstreamConn {
	return &lifecycleUpstreamConn{
		commandWritten: make(chan struct{}),
		closed:         make(chan struct{}),
	}
}

func (c *lifecycleUpstreamConn) Read(buffer []byte) (int, error) {
	if !c.readOnce.Swap(true) {
		<-c.commandWritten
		return copy(buffer, []byte("+OK\r\n")), nil
	}
	<-c.closed
	return 0, net.ErrClosed
}

func (c *lifecycleUpstreamConn) Write(buffer []byte) (int, error) {
	c.writeOnce.Do(func() { close(c.commandWritten) })
	return len(buffer), nil
}

func (c *lifecycleUpstreamConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *lifecycleUpstreamConn) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

func (c *lifecycleUpstreamConn) LocalAddr() net.Addr              { return observerCopyAddr("local") }
func (c *lifecycleUpstreamConn) RemoteAddr() net.Addr             { return observerCopyAddr("remote") }
func (c *lifecycleUpstreamConn) SetDeadline(time.Time) error      { return nil }
func (c *lifecycleUpstreamConn) SetReadDeadline(time.Time) error  { return nil }
func (c *lifecycleUpstreamConn) SetWriteDeadline(time.Time) error { return nil }

var _ net.Conn = (*serializedRelayClientConn)(nil)
var _ net.Conn = (*serializedRelayUpstreamConn)(nil)
var _ auditWriter = (*blockingFinishAudit)(nil)
var _ net.Conn = (*lifecycleClientConn)(nil)
var _ net.Conn = (*lifecycleUpstreamConn)(nil)
