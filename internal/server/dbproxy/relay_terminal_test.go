package dbproxy

import (
	"bytes"
	"crypto/tls"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestRelayObserverErrorRemainsLastAfterObservedUpstreamWriteWaitsForLock(t *testing.T) {
	const repetitions = 128
	errorBytes := []byte("-ERR terminal observer failure\r\n")
	responseBytes := []byte("+SHOULD-NOT-FOLLOW\r\n")

	for iteration := 0; iteration < repetitions; iteration++ {
		client := newTerminalWriteOrderConn()
		upstream := newObserverCopyConn(responseBytes)
		observer := newResponseObservedObserver(errorBytes)
		relay := newRelayCoordinator(client, upstream)

		errorDone := make(chan struct{})
		go func() {
			defer close(errorDone)
			relay.writeObserverError(observer, queryDecision{
				Allowed:      false,
				ErrorMessage: "terminal observer failure",
			})
		}()

		select {
		case <-client.errorWriteStarted:
		case <-time.After(time.Second):
			t.Fatalf("iteration %d: observer error write did not acquire the client writer", iteration)
		}

		upstreamDone := make(chan struct{})
		go func() {
			defer close(upstreamDone)
			_ = relay.copyUpstreamToClient(observer)
		}()

		select {
		case <-observer.responseObserved:
		case <-time.After(time.Second):
			t.Fatalf("iteration %d: upstream response was not observed", iteration)
		}
		// The observer callback returns immediately before writeClient takes the
		// shared lock. Keep the error write locked briefly so the normal writer
		// is queued behind it.
		time.Sleep(time.Millisecond)
		close(client.releaseErrorWrite)

		select {
		case <-errorDone:
		case <-time.After(time.Second):
			t.Fatalf("iteration %d: observer error write did not finish", iteration)
		}
		select {
		case <-upstreamDone:
		case <-time.After(time.Second):
			t.Fatalf("iteration %d: upstream relay did not finish", iteration)
		}
		relay.close()

		if got := client.writtenBytes(); !bytes.Equal(got, errorBytes) {
			t.Fatalf("iteration %d: client bytes = %q, want terminal observer error only", iteration, got)
		}
	}
}

func TestRelayObserverErrorIsTerminalThroughTLSWrappedClient(t *testing.T) {
	certFile, keyFile := writeListenerCertificate(t)
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("load listener certificate: %v", err)
	}

	serverRaw, peerRaw := net.Pipe()
	serverTLS := tls.Server(serverRaw, &tls.Config{Certificates: []tls.Certificate{certificate}})
	peerTLS := tls.Client(peerRaw, &tls.Config{InsecureSkipVerify: true}) // test certificate is self-signed
	serverHandshake := make(chan error, 1)
	go func() {
		serverHandshake <- serverTLS.Handshake()
	}()
	if err := peerTLS.Handshake(); err != nil {
		t.Fatalf("TLS client handshake: %v", err)
	}
	if err := <-serverHandshake; err != nil {
		t.Fatalf("TLS server handshake: %v", err)
	}

	readDone := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(peerTLS)
		readDone <- data
	}()

	upstream := newObserverCopyConn(nil)
	relay := newRelayCoordinator(serverTLS, upstream)
	observer := &redisObserver{}
	decision := queryDecision{Allowed: false, ErrorMessage: "terminal TLS observer failure"}
	relay.writeObserverError(observer, decision)
	_ = relay.writeClient([]byte("+LATE-UPSTREAM-RESPONSE\r\n"))
	relay.close()

	var got []byte
	select {
	case got = <-readDone:
	case <-time.After(time.Second):
		t.Fatal("TLS peer did not observe terminal connection close")
	}
	want := observer.ErrorResponse(decision)
	if !bytes.Equal(got, want) {
		t.Fatalf("TLS client bytes = %q, want terminal observer error %q", got, want)
	}
}

type responseObservedObserver struct {
	responseObserved chan struct{}
	observeOnce      sync.Once
	errorBytes       []byte
}

func newResponseObservedObserver(errorBytes []byte) *responseObservedObserver {
	return &responseObservedObserver{
		responseObserved: make(chan struct{}),
		errorBytes:       append([]byte(nil), errorBytes...),
	}
}

func (o *responseObservedObserver) ObserveClientBytes([]byte) *queryDecision {
	return nil
}

func (o *responseObservedObserver) ObserveServerBytes([]byte) *queryDecision {
	o.observeOnce.Do(func() { close(o.responseObserved) })
	return nil
}

func (o *responseObservedObserver) ErrorResponse(queryDecision) []byte {
	return append([]byte(nil), o.errorBytes...)
}

type terminalWriteOrderConn struct {
	errorWriteStarted chan struct{}
	releaseErrorWrite chan struct{}
	closed            chan struct{}
	closeOnce         sync.Once
	writeOnce         sync.Once
	writeMu           sync.Mutex
	writes            bytes.Buffer
}

func newTerminalWriteOrderConn() *terminalWriteOrderConn {
	return &terminalWriteOrderConn{
		errorWriteStarted: make(chan struct{}),
		releaseErrorWrite: make(chan struct{}),
		closed:            make(chan struct{}),
	}
}

func (c *terminalWriteOrderConn) Read([]byte) (int, error) {
	<-c.closed
	return 0, net.ErrClosed
}

func (c *terminalWriteOrderConn) Write(buffer []byte) (int, error) {
	c.writeOnce.Do(func() {
		close(c.errorWriteStarted)
		<-c.releaseErrorWrite
	})
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writes.Write(buffer)
}

func (c *terminalWriteOrderConn) writtenBytes() []byte {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return append([]byte(nil), c.writes.Bytes()...)
}

func (c *terminalWriteOrderConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *terminalWriteOrderConn) LocalAddr() net.Addr              { return observerCopyAddr("local") }
func (c *terminalWriteOrderConn) RemoteAddr() net.Addr             { return observerCopyAddr("remote") }
func (c *terminalWriteOrderConn) SetDeadline(time.Time) error      { return nil }
func (c *terminalWriteOrderConn) SetReadDeadline(time.Time) error  { return nil }
func (c *terminalWriteOrderConn) SetWriteDeadline(time.Time) error { return nil }

var _ queryObserver = (*responseObservedObserver)(nil)
var _ net.Conn = (*terminalWriteOrderConn)(nil)
