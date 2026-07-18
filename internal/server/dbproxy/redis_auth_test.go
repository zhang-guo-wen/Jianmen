package dbproxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

type scriptedRedisAuthConn struct {
	response         *bytes.Reader
	writes           bytes.Buffer
	deadlines        []time.Time
	setDeadlineErr   error
	clearDeadlineErr error
}

func newScriptedRedisAuthConn(response string) *scriptedRedisAuthConn {
	return &scriptedRedisAuthConn{response: bytes.NewReader([]byte(response))}
}

func (c *scriptedRedisAuthConn) Read(buffer []byte) (int, error) {
	return c.response.Read(buffer)
}

func (c *scriptedRedisAuthConn) Write(buffer []byte) (int, error) {
	return c.writes.Write(buffer)
}

func (c *scriptedRedisAuthConn) Close() error                     { return nil }
func (c *scriptedRedisAuthConn) LocalAddr() net.Addr              { return nil }
func (c *scriptedRedisAuthConn) RemoteAddr() net.Addr             { return nil }
func (c *scriptedRedisAuthConn) SetReadDeadline(time.Time) error  { return nil }
func (c *scriptedRedisAuthConn) SetWriteDeadline(time.Time) error { return nil }

func (c *scriptedRedisAuthConn) SetDeadline(deadline time.Time) error {
	c.deadlines = append(c.deadlines, deadline)
	if deadline.IsZero() {
		return c.clearDeadlineErr
	}
	return c.setDeadlineErr
}

func TestRedisUpstreamAuthenticationUsesProvidedFiveSecondDeadline(t *testing.T) {
	now := time.Now()
	deadline := redisUpstreamAuthenticationDeadline(context.Background(), now)
	if deadline.After(now.Add(5 * time.Second)) {
		t.Fatalf("authentication deadline = %v, want no later than five seconds", deadline)
	}

	conn := newScriptedRedisAuthConn("+OK\r\n")
	if err := authenticateUpstreamRedis(conn, "app", "secret", deadline); err != nil {
		t.Fatalf("authenticateUpstreamRedis() error = %v", err)
	}
	if len(conn.deadlines) != 2 {
		t.Fatalf("SetDeadline calls = %d, want set and clear", len(conn.deadlines))
	}
	if !conn.deadlines[0].Equal(deadline) {
		t.Fatalf("authentication deadline = %v, want preserved absolute deadline %v", conn.deadlines[0], deadline)
	}
	if !conn.deadlines[1].IsZero() {
		t.Fatalf("final deadline = %v, want cleared deadline", conn.deadlines[1])
	}
}

func TestRedisUpstreamAuthenticationHonorsEarlierContextDeadline(t *testing.T) {
	now := time.Now()
	contextDeadline := now.Add(time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), contextDeadline)
	defer cancel()

	if got := redisUpstreamAuthenticationDeadline(ctx, now); !got.Equal(contextDeadline) {
		t.Fatalf("authentication deadline = %v, want context deadline %v", got, contextDeadline)
	}
}

func TestRedisUpstreamAuthenticationHandlesDeadlineErrors(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		deadlineErr := errors.New("set deadline failed")
		conn := newScriptedRedisAuthConn("+OK\r\n")
		conn.setDeadlineErr = deadlineErr

		err := authenticateUpstreamRedis(conn, "app", "secret", time.Now().Add(time.Second))
		if !errors.Is(err, deadlineErr) {
			t.Fatalf("authenticateUpstreamRedis() error = %v, want set deadline error", err)
		}
		if conn.writes.Len() != 0 {
			t.Fatal("authentication wrote credentials after deadline setup failed")
		}
	})

	t.Run("clear", func(t *testing.T) {
		deadlineErr := errors.New("clear deadline failed")
		conn := newScriptedRedisAuthConn("+OK\r\n")
		conn.clearDeadlineErr = deadlineErr

		err := authenticateUpstreamRedis(conn, "app", "secret", time.Now().Add(time.Second))
		if !errors.Is(err, deadlineErr) {
			t.Fatalf("authenticateUpstreamRedis() error = %v, want clear deadline error", err)
		}
	})
}

func TestRedisUpstreamAuthenticationErrorDoesNotExposeCredentials(t *testing.T) {
	const password = "do-not-log-this-password"
	conn := newScriptedRedisAuthConn("-ERR echoed " + password + "\r\n")

	err := authenticateUpstreamRedis(conn, "app", password, time.Now().Add(time.Second))
	if err == nil {
		t.Fatal("expected upstream authentication rejection")
	}
	if strings.Contains(err.Error(), password) {
		t.Fatalf("authentication error exposed password: %v", err)
	}
	if !errors.Is(err, errRedisUpstreamAuthenticationDenied) {
		t.Fatalf("authentication error = %v, want sanitized denial", err)
	}
}

func TestRedisUpstreamAuthenticationRejectsNonCRLFAndOversizedLines(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{name: "LF only", response: "+OK\n"},
		{name: "bare CR", response: "+OK\rX\r\n"},
		{name: "line limit", response: "+" + strings.Repeat("x", maxRESPAuthLineBytes) + "\r\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := newScriptedRedisAuthConn(tt.response)
			err := authenticateUpstreamRedis(conn, "app", "secret", time.Now().Add(time.Second))
			if err == nil {
				t.Fatalf("authenticateUpstreamRedis accepted malformed response %q", tt.response)
			}
		})
	}
}

var _ net.Conn = (*scriptedRedisAuthConn)(nil)
var _ io.Reader = (*scriptedRedisAuthConn)(nil)
