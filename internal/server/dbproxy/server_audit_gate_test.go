package dbproxy

import (
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
		gateway.handleGatewayConn(client, conn)
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

type auditGateWriter struct {
	createErr    error
	createCalled chan struct{}
}

func (w *auditGateWriter) CreateAuditSession(*model.AuditSession) error {
	close(w.createCalled)
	return w.createErr
}
func (*auditGateWriter) EndAuditSession(string) error                 { return nil }
func (*auditGateWriter) CreateAuditDBQuery(*model.AuditDBQuery) error { return nil }

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
var _ net.Conn = (*auditGateConn)(nil)
