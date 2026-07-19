package dbproxy

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestRedisRelayRejectsPostAuthenticationStateCommandsBeforeUpstream(t *testing.T) {
	const password = "never-forward-reauth-secret"
	tests := []struct {
		name    string
		command []byte
	}{
		{name: "AUTH", command: redisObserverTestCommand("AUTH", "other-user", password)},
		{name: "ACL", command: redisObserverTestCommand("ACL", "SETUSER", "other-user", ">"+password)},
		{name: "MIGRATE", command: redisObserverTestCommand("MIGRATE", "127.0.0.1", "6379", "key", "0", "5000", "AUTH", "other-user", password)},
		{name: "RESET", command: redisObserverTestCommand("RESET")},
		{name: "MONITOR", command: redisObserverTestCommand("MONITOR")},
		{name: "SYNC", command: redisObserverTestCommand("SYNC")},
		{name: "PSYNC", command: redisObserverTestCommand("PSYNC", "?", "-1")},
		{name: "CONFIG", command: redisObserverTestCommand("CONFIG", "SET", "requirepass", password)},
		{name: "MODULE", command: redisObserverTestCommand("MODULE", "LOAD", "/tmp/untrusted.so")},
		{name: "SHUTDOWN", command: redisObserverTestCommand("SHUTDOWN", "NOSAVE")},
		{name: "REPLICAOF", command: redisObserverTestCommand("REPLICAOF", "attacker.example", "6379")},
		{name: "SLAVEOF", command: redisObserverTestCommand("SLAVEOF", "attacker.example", "6379")},
		{name: "CLUSTER", command: redisObserverTestCommand("CLUSTER", "MEET", "attacker.example", "6379")},
		{name: "FAILOVER", command: redisObserverTestCommand("FAILOVER", "TO", "attacker.example", "6379")},
		{name: "FUNCTION", command: redisObserverTestCommand("FUNCTION", "LOAD", "replace", password)},
		{name: "FLUSHALL", command: redisObserverTestCommand("FLUSHALL", "ASYNC")},
		{name: "FLUSHDB", command: redisObserverTestCommand("FLUSHDB", "ASYNC")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			split := len(tt.command) / 2
			client := newChunkedObserverCopyConn(tt.command[:split], tt.command[split:])
			upstream := newObserverCopyConn(nil)
			sink := &captureSink{}
			observer := &redisObserver{sink: sink}

			copyClientToUpstream(client, upstream, observer)

			if got := upstream.writes.Len(); got != 0 {
				t.Fatalf("upstream received %d bytes for rejected %s command: %q", got, tt.name, upstream.writes.Bytes())
			}
			if got := client.writes.String(); !strings.HasPrefix(got, "-ERR ") {
				t.Fatalf("client response = %q, want Redis protocol error", got)
			}
			if bytes.Contains(client.writes.Bytes(), []byte(password)) {
				t.Fatal("protocol error exposed reauthentication password")
			}
			if !client.closed || !upstream.closed {
				t.Fatalf("connections closed = client:%t upstream:%t, want both true", client.closed, upstream.closed)
			}
			if len(sink.queries) != 0 {
				t.Fatalf("audited queries = %v, want none", sink.queries)
			}
		})
	}
}

func TestRedisRelayRejectsInvalidCommandNamesBeforeUpstream(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{name: "empty", command: ""},
		{name: "space", command: "SET "},
		{name: "tab", command: "SE\tT"},
		{name: "line feed", command: "SE\nT"},
		{name: "carriage return", command: "SE\rT"},
		{name: "NUL", command: "SE\x00T"},
		{name: "unicode whitespace", command: "SE\u00a0T"},
		{name: "unicode format control", command: "SE\u200bT"},
		{name: "unicode secret command", command: "秘密命令"},
		{name: "unknown ASCII secret command", command: "PASSWORDSECRET"},
		{name: "invalid UTF-8", command: string([]byte{'S', 'E', 0xff, 'T'})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newChunkedObserverCopyConn(redisObserverTestCommand(tt.command, "key", "secret"))
			upstream := newObserverCopyConn(nil)
			sink := &captureSink{}

			copyClientToUpstream(client, upstream, &redisObserver{sink: sink})

			if got := upstream.writes.Len(); got != 0 {
				t.Fatalf("upstream received %d bytes for invalid command name %q", got, tt.command)
			}
			if got := client.writes.String(); !strings.HasPrefix(got, "-ERR ") {
				t.Fatalf("client response = %q, want Redis protocol error", got)
			}
			if len(sink.queries) != 0 {
				t.Fatalf("audited queries = %v, want none", sink.queries)
			}
			if !client.closed || !upstream.closed {
				t.Fatalf("connections closed = client:%t upstream:%t, want both true", client.closed, upstream.closed)
			}
		})
	}
}

func TestRedisObserverCompletesLegalPipelinePrefixBeforeTerminalRejection(t *testing.T) {
	legal := redisObserverTestCommand("SET", "key", "value")
	rejected := redisObserverTestCommand("AUTH", "replacement-user", "replacement-password")
	sink := &captureSink{}
	observer := &redisObserver{sink: sink}

	forward, decision := observer.ObserveClientRelayBytes(append(append([]byte(nil), legal...), rejected...))
	if decision != nil {
		t.Fatalf("client decision = %#v, want rejection deferred until legal response completes", decision)
	}
	if !bytes.Equal(forward, legal) {
		t.Fatalf("forwarded bytes = %q, want legal prefix %q", forward, legal)
	}
	if len(sink.queries) != 1 || len(sink.finished) != 0 {
		t.Fatalf("audit before response = queries:%v finished:%v, want one started query", sink.queries, sink.finished)
	}

	decision = observer.ObserveServerBytes([]byte("+OK\r\n"))
	if decision == nil || decision.Allowed {
		t.Fatalf("server decision = %#v, want terminal rejection after legal response", decision)
	}
	if len(sink.finished) != 1 || sink.finished[0].Status != queryStatusSuccess {
		t.Fatalf("audit after response = %#v, want completed successful prefix", sink.finished)
	}
}

func TestRedisObserverCompletesLegalPrefixBeforeLaterPolicyDenial(t *testing.T) {
	legal := redisObserverTestCommand("SET", "key", "value")
	denied := redisObserverTestCommand("GET", "protected")
	sink := &denySecondRedisQuerySink{}
	observer := &redisObserver{sink: sink}

	forward, decision := observer.ObserveClientRelayBytes(append(append([]byte(nil), legal...), denied...))
	if decision != nil {
		t.Fatalf("client decision = %#v, want denial deferred until legal response", decision)
	}
	if !bytes.Equal(forward, legal) {
		t.Fatalf("forwarded bytes = %q, want only legal prefix %q", forward, legal)
	}
	decision = observer.ObserveServerBytes([]byte("+OK\r\n"))
	if decision == nil || decision.Allowed {
		t.Fatalf("server decision = %#v, want deferred policy denial", decision)
	}
	if len(sink.finished) != 2 {
		t.Fatalf("finished records = %#v, want denied query and completed legal prefix", sink.finished)
	}
}

func TestRedisRelayForwardsLegalPipelinePrefixResponseBeforeTerminalRejection(t *testing.T) {
	legal := redisObserverTestCommand("SET", "key", "value")
	rejected := redisObserverTestCommand("AUTH", "replacement-user", "replacement-password")
	clientGateway, clientPeer := net.Pipe()
	upstreamGateway, upstreamPeer := net.Pipe()
	defer clientPeer.Close()
	defer upstreamPeer.Close()
	sink := &captureSink{}
	observer := &redisObserver{sink: sink}
	relayDone := make(chan struct{})
	go func() {
		relayGatewayConnection(clientGateway, upstreamGateway, observer)
		close(relayDone)
	}()

	writeDone := make(chan error, 1)
	go func() {
		_, err := clientPeer.Write(append(append([]byte(nil), legal...), rejected...))
		writeDone <- err
	}()
	if err := upstreamPeer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set upstream deadline: %v", err)
	}
	gotUpstream := make([]byte, len(legal))
	if _, err := io.ReadFull(upstreamPeer, gotUpstream); err != nil {
		t.Fatalf("read legal upstream prefix: %v", err)
	}
	if !bytes.Equal(gotUpstream, legal) {
		t.Fatalf("upstream bytes = %q, want legal prefix %q", gotUpstream, legal)
	}
	if _, err := upstreamPeer.Write([]byte("+OK\r\n")); err != nil {
		t.Fatalf("write legal upstream response: %v", err)
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("write client pipeline: %v", err)
	}

	if err := clientPeer.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("set client deadline: %v", err)
	}
	clientResponse, err := io.ReadAll(clientPeer)
	if err != nil {
		t.Fatalf("read client response: %v", err)
	}
	wantPrefix := []byte("+OK\r\n-ERR ")
	if !bytes.HasPrefix(clientResponse, wantPrefix) {
		t.Fatalf("client response = %q, want legal response followed by terminal error", clientResponse)
	}
	select {
	case <-relayDone:
	case <-time.After(time.Second):
		t.Fatal("relay did not terminate after deferred rejection")
	}
	if len(sink.finished) != 1 || sink.finished[0].Status != queryStatusSuccess {
		t.Fatalf("legal prefix audit finish = %#v, want one success", sink.finished)
	}
}

func TestHandleGatewayConnKeepsAuditIdentityWhenRedisReauthenticationIsRejected(t *testing.T) {
	tests := []struct {
		name    string
		command []byte
	}{
		{name: "AUTH", command: redisObserverTestCommand("AUTH", "replacement-user", "replacement-password")},
		{name: "HELLO AUTH", command: redisObserverTestCommand("HELLO", "3", "AUTH", "replacement-user", "replacement-password")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			audit := &identityCaptureAudit{}
			client := newChunkedObserverCopyConn(tt.command)
			upstream := newBlockingRedisUpstreamConn()
			gateway := &Gateway{
				replayDir: t.TempDir(),
				logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
				audit:     audit,
			}
			connection := &gatewayConn{
				protocol:      "redis",
				accountID:     "original-account",
				instanceID:    "original-instance",
				accountName:   "original-account-name",
				accountUser:   "original-upstream-user",
				instanceName:  "original-instance-name",
				userID:        "original-user",
				userSessionID: "original-user-session",
				upstream:      upstream,
				upstreamAddr:  "127.0.0.1:6379",
			}

			gateway.handleGatewayConn(context.Background(), client, connection)

			session, queryCount, endCount := audit.snapshot()
			if session == nil {
				t.Fatal("audit session was not created")
			}
			if session.UserID != "original-user" ||
				session.UserSessionID != "original-user-session" ||
				session.AccountName != "original-upstream-user" ||
				session.TargetName != "original-instance-name" {
				t.Fatalf("audit identity changed after rejected reauthentication: %#v", session)
			}
			if queryCount != 0 {
				t.Fatalf("audit query count = %d, want 0", queryCount)
			}
			if endCount != 1 {
				t.Fatalf("ended audit sessions = %d, want 1", endCount)
			}
			if got := upstream.writtenBytes(); len(got) != 0 {
				t.Fatalf("reauthentication reached upstream: %q", got)
			}
			if got := client.writes.String(); !strings.HasPrefix(got, "-ERR ") {
				t.Fatalf("client response = %q, want protocol error", got)
			}
			if connection.userID != "original-user" ||
				connection.accountID != "original-account" ||
				connection.accountUser != "original-upstream-user" {
				t.Fatalf("gateway identity changed after rejected reauthentication: %#v", connection)
			}
		})
	}
}

type identityCaptureAudit struct {
	mu         sync.Mutex
	session    *model.AuditSession
	queryCount int
	endCount   int
}

func (a *identityCaptureAudit) CreateAuditSession(_ context.Context, session *model.AuditSession) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	copied := *session
	a.session = &copied
	return nil
}

func (a *identityCaptureAudit) EndAuditSession(context.Context, string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.endCount++
	return nil
}

func (a *identityCaptureAudit) CreateAuditDBQuery(context.Context, *model.AuditDBQuery) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.queryCount++
	return nil
}

func (a *identityCaptureAudit) snapshot() (*model.AuditSession, int, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var session *model.AuditSession
	if a.session != nil {
		copied := *a.session
		session = &copied
	}
	return session, a.queryCount, a.endCount
}

type blockingRedisUpstreamConn struct {
	closed    chan struct{}
	closeOnce sync.Once
	writeMu   sync.Mutex
	writes    bytes.Buffer
}

func newBlockingRedisUpstreamConn() *blockingRedisUpstreamConn {
	return &blockingRedisUpstreamConn{closed: make(chan struct{})}
}

func (c *blockingRedisUpstreamConn) Read([]byte) (int, error) {
	<-c.closed
	return 0, net.ErrClosed
}

func (c *blockingRedisUpstreamConn) Write(buffer []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writes.Write(buffer)
}

func (c *blockingRedisUpstreamConn) writtenBytes() []byte {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return append([]byte(nil), c.writes.Bytes()...)
}

func (c *blockingRedisUpstreamConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *blockingRedisUpstreamConn) LocalAddr() net.Addr              { return observerCopyAddr("local") }
func (c *blockingRedisUpstreamConn) RemoteAddr() net.Addr             { return observerCopyAddr("remote") }
func (c *blockingRedisUpstreamConn) SetDeadline(time.Time) error      { return nil }
func (c *blockingRedisUpstreamConn) SetReadDeadline(time.Time) error  { return nil }
func (c *blockingRedisUpstreamConn) SetWriteDeadline(time.Time) error { return nil }

type chunkedObserverCopyConn struct {
	chunks [][]byte
	writes bytes.Buffer
	closed bool
}

func newChunkedObserverCopyConn(chunks ...[]byte) *chunkedObserverCopyConn {
	copied := make([][]byte, len(chunks))
	for index, chunk := range chunks {
		copied[index] = append([]byte(nil), chunk...)
	}
	return &chunkedObserverCopyConn{chunks: copied}
}

func (c *chunkedObserverCopyConn) Read(buffer []byte) (int, error) {
	if len(c.chunks) == 0 {
		return 0, io.EOF
	}
	chunk := c.chunks[0]
	c.chunks = c.chunks[1:]
	return copy(buffer, chunk), nil
}

func (c *chunkedObserverCopyConn) Write(buffer []byte) (int, error) {
	return c.writes.Write(buffer)
}

func (c *chunkedObserverCopyConn) Close() error                     { c.closed = true; return nil }
func (c *chunkedObserverCopyConn) LocalAddr() net.Addr              { return observerCopyAddr("local") }
func (c *chunkedObserverCopyConn) RemoteAddr() net.Addr             { return observerCopyAddr("remote") }
func (c *chunkedObserverCopyConn) SetDeadline(time.Time) error      { return nil }
func (c *chunkedObserverCopyConn) SetReadDeadline(time.Time) error  { return nil }
func (c *chunkedObserverCopyConn) SetWriteDeadline(time.Time) error { return nil }

var _ net.Conn = (*chunkedObserverCopyConn)(nil)
var _ auditWriter = (*identityCaptureAudit)(nil)
var _ net.Conn = (*blockingRedisUpstreamConn)(nil)

type denySecondRedisQuerySink struct {
	started  int
	finished []queryFinish
}

func (s *denySecondRedisQuerySink) StartQuery(sql string, detail map[string]any) (queryRecord, queryDecision) {
	s.started++
	record := queryRecord{seq: int64(s.started), sql: sql, detail: detail}
	if s.started == 2 {
		decision := queryDecision{
			Allowed:      false,
			Status:       queryStatusPolicyDenied,
			ErrorCode:    "POLICY_DENIED",
			ErrorMessage: "denied by test policy",
		}
		s.finished = append(s.finished, queryFinish{
			Status:       decision.Status,
			ErrorCode:    decision.ErrorCode,
			ErrorMessage: decision.ErrorMessage,
		})
		return record, decision
	}
	return record, allowQuery()
}

func (s *denySecondRedisQuerySink) FinishQuery(_ queryRecord, finish queryFinish) {
	s.finished = append(s.finished, finish)
}
