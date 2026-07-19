package mysqlwire

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"
)

func FuzzMySQLPacketFrames(f *testing.F) {
	valid, err := EncodePacket(0, append([]byte{0x03}, []byte("select 1")...))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid, uint16(1024))
	f.Add([]byte{1, 0, 0}, uint16(1024))
	f.Add([]byte{5, 0, 0, 1, 'x'}, uint16(1024))
	f.Add([]byte{0xff, 0xff, 0xff, 0}, uint16(1024))

	f.Fuzz(func(t *testing.T, frame []byte, packetLimit uint16) {
		limit := int(packetLimit)
		if limit == 0 {
			limit = 1
		}
		connection := &mysqlFuzzConn{reader: bytes.NewReader(frame)}
		packet, readErr := ReadPacket(context.Background(), connection, limit)
		if readErr != nil {
			return
		}
		if len(packet.Payload) > limit {
			t.Fatalf("accepted %d-byte payload with %d-byte limit", len(packet.Payload), limit)
		}
		encoded, encodeErr := EncodePacket(packet.Sequence, packet.Payload)
		if encodeErr != nil {
			t.Fatalf("re-encode accepted packet: %v", encodeErr)
		}
		if len(frame) < len(encoded) || !bytes.Equal(frame[:len(encoded)], encoded) {
			t.Fatalf("accepted packet changed during round trip: input=%x encoded=%x", frame, encoded)
		}
	})
}

type mysqlFuzzConn struct {
	reader *bytes.Reader
}

func (c *mysqlFuzzConn) Read(payload []byte) (int, error) {
	return c.reader.Read(payload)
}

func (c *mysqlFuzzConn) Write(payload []byte) (int, error) {
	return len(payload), nil
}

func (c *mysqlFuzzConn) Close() error                     { return nil }
func (c *mysqlFuzzConn) LocalAddr() net.Addr              { return nil }
func (c *mysqlFuzzConn) RemoteAddr() net.Addr             { return nil }
func (c *mysqlFuzzConn) SetDeadline(time.Time) error      { return nil }
func (c *mysqlFuzzConn) SetReadDeadline(time.Time) error  { return nil }
func (c *mysqlFuzzConn) SetWriteDeadline(time.Time) error { return nil }
