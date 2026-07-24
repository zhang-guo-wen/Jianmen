package dbproxy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/online"
)

func TestHandleGatewayConnRejectsWhenAuditSessionCreationFails(t *testing.T) {
	client := newAuditGateConn()
	upstream := newAuditGateConn()
	audit := &auditGateWriter{
		createErr:    errors.New("audit store unavailable"),
		createCalled: make(chan struct{}),
	}
	onlineSessions := online.NewRegistry()
	gateway := &Gateway{
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		audit:          audit,
		auditRequired:  true,
		onlineSessions: onlineSessions,
	}
	conn := &gatewayConn{
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
		gateway.handleGatewayConn(context.Background(), client, conn)
		close(done)
	}()

	select {
	case <-audit.createCalled:
	case <-time.After(time.Second):
		t.Fatal("CreateAuditSession was not called")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("connection was relayed after audit session creation failed")
	}

	if got := len(onlineSessions.List()); got != 0 {
		t.Fatalf("online sessions = %d, want 0", got)
	}
	if got := client.written(); string(got) != "-ERR database proxy rejected command\r\n" {
		t.Fatalf("client response = %q, want generic Redis error", got)
	}
	if got := upstream.writeCount(); got != 0 {
		t.Fatalf("upstream writes = %d, want 0", got)
	}
}

func TestFinishProtocolConnectionThreadsListenerContextAndBoundsAuditEnd(t *testing.T) {
	audit := &auditContextWriter{
		createObserved: make(chan auditContextObservation, 1),
		endObserved:    make(chan auditContextObservation, 1),
	}
	limiter := newPendingHandshakeLimiter(1)
	handshakeLease, acquired := limiter.tryAcquire()
	if !acquired {
		t.Fatal("acquire pending handshake lease")
	}
	client, clientPeer := net.Pipe()
	upstream, upstreamPeer := net.Pipe()
	defer clientPeer.Close()
	defer upstreamPeer.Close()

	onlineSessions := online.NewRegistry()
	gateway := &Gateway{
		replayDir:         t.TempDir(),
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		audit:             audit,
		auditRequired:     true,
		onlineSessions:    onlineSessions,
		pendingHandshakes: limiter,
	}
	connection := &gatewayConn{
		protocol:      "redis",
		accountID:     "account-1",
		instanceID:    "instance-1",
		accountName:   "account-name",
		accountUser:   "upstream-user",
		instanceName:  "redis-instance",
		userID:        "user-1",
		userSessionID: "user-session-1",
		upstream:      upstream,
		upstreamAddr:  "127.0.0.1:6379",
	}
	listenerCtx, cancelListener := context.WithCancel(
		context.WithValue(context.Background(), auditContextKey{}, "listener-value"),
	)
	defer cancelListener()

	done := make(chan struct{})
	go func() {
		defer close(done)
		gateway.finishProtocolConnection(
			listenerCtx,
			client,
			connection,
			databaseProtocolRedis,
			handshakeLease,
		)
	}()

	create := waitAuditContextObservation(t, audit.createObserved, "CreateAuditSession")
	if create.err != nil || create.value != "listener-value" {
		t.Fatalf("CreateAuditSession context = %#v, want active listener context", create)
	}
	var onlineItems []online.Session
	onlineDeadline := time.Now().Add(time.Second)
	for time.Now().Before(onlineDeadline) {
		onlineItems = onlineSessions.List()
		if len(onlineItems) == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(onlineItems) != 1 || onlineItems[0].UserSessionID != "user-session-1" {
		t.Fatalf("online sessions = %#v, want linked user session", onlineItems)
	}

	cancelListener()
	_ = clientPeer.Close()
	_ = upstreamPeer.Close()

	end := waitAuditContextObservation(t, audit.endObserved, "EndAuditSession")
	if end.err != nil {
		t.Fatalf("EndAuditSession context error = %v, want detached cancellation", end.err)
	}
	if end.value != "listener-value" {
		t.Fatalf("EndAuditSession context value = %#v, want listener-value", end.value)
	}
	if !end.hasDeadline {
		t.Fatal("EndAuditSession context has no deadline")
	}
	remaining := time.Until(end.deadline)
	if remaining <= 0 || remaining > auditSessionEndTimeout {
		t.Fatalf("EndAuditSession deadline remaining = %v, want (0, %v]", remaining, auditSessionEndTimeout)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("gateway connection did not finish")
	}
}

func TestDatabaseGatewayClientIPUsesRemoteAddressHost(t *testing.T) {
	server, peer := net.Pipe()
	defer server.Close()
	defer peer.Close()

	connection := &remoteAddrConn{Conn: server, remote: "203.0.113.27:43210"}
	if got := databaseGatewayClientIP(connection); got != "203.0.113.27" {
		t.Fatalf("databaseGatewayClientIP() = %q, want 203.0.113.27", got)
	}
	if got := databaseGatewayClientIP(newAuditGateConn()); got != "" {
		t.Fatalf("databaseGatewayClientIP() for non-IP address = %q, want empty", got)
	}
}

type auditGateWriter struct {
	createErr    error
	createCalled chan struct{}
}

func (w *auditGateWriter) CreateAuditSession(context.Context, *model.AuditSession) error {
	close(w.createCalled)
	return w.createErr
}
func (*auditGateWriter) EndAuditSession(context.Context, string) error { return nil }
func (*auditGateWriter) CreateAuditDBQuery(context.Context, *model.AuditDBQuery) error {
	return nil
}

type auditContextKey struct{}

type auditContextObservation struct {
	value       any
	err         error
	deadline    time.Time
	hasDeadline bool
}

type auditContextWriter struct {
	createObserved chan auditContextObservation
	endObserved    chan auditContextObservation
}

func (w *auditContextWriter) CreateAuditSession(ctx context.Context, _ *model.AuditSession) error {
	w.createObserved <- observeAuditContext(ctx)
	return nil
}

func (w *auditContextWriter) EndAuditSession(ctx context.Context, _ string) error {
	w.endObserved <- observeAuditContext(ctx)
	return nil
}

func (*auditContextWriter) CreateAuditDBQuery(context.Context, *model.AuditDBQuery) error {
	return nil
}

func observeAuditContext(ctx context.Context) auditContextObservation {
	deadline, hasDeadline := ctx.Deadline()
	return auditContextObservation{
		value:       ctx.Value(auditContextKey{}),
		err:         ctx.Err(),
		deadline:    deadline,
		hasDeadline: hasDeadline,
	}
}

func waitAuditContextObservation(
	t *testing.T,
	observed <-chan auditContextObservation,
	operation string,
) auditContextObservation {
	t.Helper()
	select {
	case observation := <-observed:
		return observation
	case <-time.After(time.Second):
		t.Fatalf("%s was not called", operation)
		return auditContextObservation{}
	}
}

type auditGateConn struct {
	mu     sync.Mutex
	closed chan struct{}
	writes []byte
	count  int
}

func newAuditGateConn() *auditGateConn            { return &auditGateConn{closed: make(chan struct{})} }
func (c *auditGateConn) Read([]byte) (int, error) { <-c.closed; return 0, net.ErrClosed }
func (c *auditGateConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count++
	c.writes = append(c.writes, p...)
	return len(p), nil
}
func (c *auditGateConn) Close() error {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
	return nil
}
func (c *auditGateConn) LocalAddr() net.Addr              { return auditGateAddr("local") }
func (c *auditGateConn) RemoteAddr() net.Addr             { return auditGateAddr("remote") }
func (c *auditGateConn) SetDeadline(time.Time) error      { return nil }
func (c *auditGateConn) SetReadDeadline(time.Time) error  { return nil }
func (c *auditGateConn) SetWriteDeadline(time.Time) error { return nil }
func (c *auditGateConn) written() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.writes...)
}
func (c *auditGateConn) writeCount() int { c.mu.Lock(); defer c.mu.Unlock(); return c.count }

type auditGateAddr string

func (a auditGateAddr) Network() string { return "audit-gate" }
func (a auditGateAddr) String() string  { return string(a) }

var _ auditWriter = (*auditGateWriter)(nil)
var _ auditWriter = (*auditContextWriter)(nil)
var _ net.Conn = (*auditGateConn)(nil)
