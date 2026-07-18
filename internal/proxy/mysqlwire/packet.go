package mysqlwire

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

type Packet struct {
	Sequence byte
	Payload  []byte
}

const MaxPacketPayloadBytes = 0xFFFFFF

type PacketWriteError struct {
	written int
	cause   error
}

func (e *PacketWriteError) Error() string { return "write mysql packet" }
func (e *PacketWriteError) Unwrap() error { return e.cause }

func WrittenBytes(err error) int {
	var writeError *PacketWriteError
	if errors.As(err, &writeError) {
		return writeError.written
	}
	return 0
}

func EncodePacket(sequence byte, payload []byte) ([]byte, error) {
	if len(payload) > MaxPacketPayloadBytes {
		return nil, errors.New("mysql packet payload exceeds protocol limit")
	}
	packet := make([]byte, 4+len(payload))
	packet[0] = byte(len(payload))
	packet[1] = byte(len(payload) >> 8)
	packet[2] = byte(len(payload) >> 16)
	packet[3] = sequence
	copy(packet[4:], payload)
	return packet, nil
}

func ReadPacket(ctx context.Context, conn net.Conn, maxPayloadBytes int) (Packet, error) {
	if ctx == nil {
		return Packet{}, errors.New("read mysql packet: nil context")
	}
	if conn == nil {
		return Packet{}, errors.New("read mysql packet: nil connection")
	}
	if maxPayloadBytes <= 0 {
		return Packet{}, errors.New("read mysql packet: invalid size limit")
	}
	stopCancellation, err := applyReadDeadline(ctx, conn)
	if err != nil {
		return Packet{}, err
	}
	defer stopCancellation()
	var header [4]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return Packet{}, errors.New("read mysql packet header")
	}
	payloadLength := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	if payloadLength > maxPayloadBytes {
		return Packet{}, fmt.Errorf("mysql packet exceeds %d byte limit", maxPayloadBytes)
	}
	payload := make([]byte, payloadLength)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return Packet{}, errors.New("read mysql packet payload")
	}
	return Packet{Sequence: header[3], Payload: payload}, nil
}

func WritePacket(ctx context.Context, conn net.Conn, sequence byte, payload []byte) error {
	if ctx == nil {
		return errors.New("write mysql packet: nil context")
	}
	if conn == nil {
		return errors.New("write mysql packet: nil connection")
	}
	if len(payload) > MaxPacketPayloadBytes {
		return errors.New("mysql packet payload exceeds protocol limit")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetWriteDeadline(deadline); err != nil {
			return &PacketWriteError{cause: errors.New("set mysql write deadline")}
		}
	}
	stopCancellation := context.AfterFunc(ctx, func() {
		_ = conn.SetWriteDeadline(time.Now())
	})
	defer stopCancellation()
	packet, err := EncodePacket(sequence, payload)
	if err != nil {
		return err
	}
	return writePacketBytes(ctx, conn, packet, func(written int, cause error) error {
		return &PacketWriteError{written: written, cause: cause}
	})
}

// WriteRawPacket writes an already encoded MySQL packet while allowing context
// cancellation to interrupt a blocked network write.
func WriteRawPacket(ctx context.Context, conn net.Conn, packet []byte) error {
	if ctx == nil {
		return errors.New("write mysql raw packet: nil context")
	}
	if conn == nil {
		return errors.New("write mysql raw packet: nil connection")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetWriteDeadline(deadline); err != nil {
			return errors.New("set mysql raw write deadline")
		}
	}
	stopCancellation := context.AfterFunc(ctx, func() {
		_ = conn.SetWriteDeadline(time.Now())
	})
	defer stopCancellation()
	return writePacketBytes(ctx, conn, packet, func(_ int, cause error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return errors.New("write mysql raw packet")
	})
}

func writePacketBytes(
	ctx context.Context,
	conn net.Conn,
	packet []byte,
	wrap func(int, error) error,
) error {
	writtenTotal := 0
	for len(packet) > 0 {
		written, err := conn.Write(packet)
		writtenTotal += written
		if err != nil {
			return wrap(writtenTotal, err)
		}
		if written == 0 {
			return wrap(writtenTotal, io.ErrNoProgress)
		}
		packet = packet[written:]
	}
	return nil
}

func applyReadDeadline(ctx context.Context, conn net.Conn) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetReadDeadline(deadline); err != nil {
			return nil, errors.New("set mysql read deadline")
		}
	}
	stop := context.AfterFunc(ctx, func() {
		_ = conn.SetReadDeadline(time.Now())
	})
	return func() { stop() }, nil
}

func ClearDeadline(conn net.Conn) error {
	if conn == nil {
		return errors.New("clear mysql deadline: nil connection")
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return errors.New("clear mysql deadline")
	}
	return nil
}
