package dbproxy

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestCopyClientToUpstreamStopsOnObserverFatal(t *testing.T) {
	mysqlHeader := []byte{0xff, 0xff, 0xff, 0}
	postgresHeader := make([]byte, 5)
	postgresHeader[0] = 'Q'
	binary.BigEndian.PutUint32(postgresHeader[1:], 128*1024*1024)

	tests := []struct {
		name     string
		observer queryObserver
		input    []byte
		want     []byte
	}{
		{
			name:     "MySQL",
			observer: &mysqlObserver{sink: &captureSink{}},
			input:    mysqlHeader,
			want: (&mysqlObserver{}).ErrorResponse(queryDecision{
				ErrorMessage: "MySQL observer frame exceeds the audit limit",
			}),
		},
		{
			name:     "PostgreSQL",
			observer: &postgresObserver{sink: &captureSink{}, startupDone: true},
			input:    postgresHeader,
			want: (&postgresObserver{}).ErrorResponse(queryDecision{
				ErrorMessage: "PostgreSQL observer frame exceeds the audit limit",
			}),
		},
		{
			name:     "Redis",
			observer: &redisObserver{sink: &captureSink{}},
			input:    []byte("PING\r\n"),
			want:     []byte("-ERR database proxy rejected command\r\n"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newObserverCopyConn(tt.input)
			upstream := newObserverCopyConn(nil)

			copyClientToUpstream(client, upstream, tt.observer)

			if !client.closed || !upstream.closed {
				t.Fatalf("connections closed = client:%t upstream:%t, want both true", client.closed, upstream.closed)
			}
			if got := upstream.writes.Len(); got != 0 {
				t.Fatalf("forwarded %d bytes after observer fatal, want 0", got)
			}
			if got := client.writes.Bytes(); !bytes.Equal(got, tt.want) {
				t.Fatalf("observer error response = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCopyUpstreamToClientStopsOnObserverFatal(t *testing.T) {
	mysqlHeader := []byte{0xff, 0xff, 0xff, 0}
	postgresHeader := make([]byte, 5)
	postgresHeader[0] = 'D'
	binary.BigEndian.PutUint32(postgresHeader[1:], 128*1024*1024)

	tests := []struct {
		name     string
		observer queryObserver
		input    []byte
		want     []byte
	}{
		{
			name:     "MySQL",
			observer: &mysqlObserver{sink: &captureSink{}, pending: []queryRecord{{seq: 1}}},
			input:    mysqlHeader,
			want: (&mysqlObserver{}).ErrorResponse(queryDecision{
				ErrorMessage: "MySQL observer frame exceeds the audit limit",
			}),
		},
		{
			name:     "PostgreSQL",
			observer: &postgresObserver{sink: &captureSink{}, startupDone: true, pending: []queryRecord{{seq: 1}}},
			input:    postgresHeader,
			want: append(
				append([]byte(nil), postgresHeader...),
				(&postgresObserver{}).ErrorResponse(queryDecision{
					ErrorMessage: "PostgreSQL relay ended during a streamed frame",
				})...,
			),
		},
		{
			name: "Redis",
			observer: &redisObserver{sink: &captureSink{}, slots: []redisResponseSlot{{
				record: queryRecord{seq: 1}, recorded: true,
			}}},
			input: []byte("$536870912\r\n"),
			want:  []byte("$536870912\r\n-ERR database proxy rejected command\r\n"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newObserverCopyConn(nil)
			upstream := newObserverCopyConn(tt.input)

			copyUpstreamToClient(client, upstream, tt.observer)

			if !client.closed || !upstream.closed {
				t.Fatalf("connections closed = client:%t upstream:%t, want both true", client.closed, upstream.closed)
			}
			if got := client.writes.Bytes(); !bytes.Equal(got, tt.want) {
				t.Fatalf("observer error response = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSerializedObserverNeverForwardsDeniedSQL(t *testing.T) {
	tests := []struct {
		name     string
		protocol string
		input    []byte
	}{
		{
			name:     "MySQL",
			protocol: "mysql",
			input:    buildMySQLPacket(0, append([]byte{0x03}, []byte("delete from protected")...)),
		},
		{
			name:     "PostgreSQL",
			protocol: "postgres",
			input:    postgresMessage('Q', []byte("delete from protected\x00")),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newObserverCopyConn(tt.input)
			upstream := newObserverCopyConn(nil)
			observer := newQueryObserver(tt.protocol, &captureSink{deny: true})

			copyClientToUpstream(client, upstream, observer)

			if got := upstream.writes.Len(); got != 0 {
				t.Fatalf("denied SQL forwarded %d bytes: %q", got, upstream.writes.Bytes())
			}
		})
	}
}

type observerCopyConn struct {
	input  *bytes.Reader
	writes bytes.Buffer
	closed bool
}

func newObserverCopyConn(input []byte) *observerCopyConn {
	return &observerCopyConn{input: bytes.NewReader(input)}
}

func (c *observerCopyConn) Read(buffer []byte) (int, error) {
	if c.input == nil {
		return 0, io.EOF
	}
	return c.input.Read(buffer)
}

func (c *observerCopyConn) Write(buffer []byte) (int, error) {
	return c.writes.Write(buffer)
}

func (c *observerCopyConn) Close() error                     { c.closed = true; return nil }
func (c *observerCopyConn) LocalAddr() net.Addr              { return observerCopyAddr("local") }
func (c *observerCopyConn) RemoteAddr() net.Addr             { return observerCopyAddr("remote") }
func (c *observerCopyConn) SetDeadline(time.Time) error      { return nil }
func (c *observerCopyConn) SetReadDeadline(time.Time) error  { return nil }
func (c *observerCopyConn) SetWriteDeadline(time.Time) error { return nil }

type observerCopyAddr string

func (a observerCopyAddr) Network() string { return string(a) }
func (a observerCopyAddr) String() string  { return string(a) }

var _ net.Conn = (*observerCopyConn)(nil)
