package dbproxy

import (
	"encoding/binary"
	"testing"
)

type captureSink struct {
	queries []string
	details []map[string]any
}

func (s *captureSink) RecordQuery(sql string, detail map[string]any) {
	s.queries = append(s.queries, sql)
	s.details = append(s.details, detail)
}

func TestMySQLObserverCapturesQueryAcrossChunks(t *testing.T) {
	sink := &captureSink{}
	observer := &mysqlObserver{sink: sink}
	packet := mysqlPacket(0, append([]byte{0x03}, []byte("select 1")...))

	observer.ObserveClientBytes(packet[:3])
	observer.ObserveClientBytes(packet[3:])

	if len(sink.queries) != 1 {
		t.Fatalf("expected 1 query, got %d", len(sink.queries))
	}
	if sink.queries[0] != "select 1" {
		t.Fatalf("unexpected query %q", sink.queries[0])
	}
	if sink.details[0]["command"] != "COM_QUERY" {
		t.Fatalf("unexpected command detail %#v", sink.details[0])
	}
}

func TestPostgresObserverCapturesSimpleQuery(t *testing.T) {
	sink := &captureSink{}
	observer := &postgresObserver{sink: sink}

	startup := make([]byte, 8)
	binary.BigEndian.PutUint32(startup[0:4], 8)
	binary.BigEndian.PutUint32(startup[4:8], 196608)
	query := postgresMessage('Q', append([]byte("select now()"), 0))

	observer.ObserveClientBytes(append(startup, query...))

	if len(sink.queries) != 1 {
		t.Fatalf("expected 1 query, got %d", len(sink.queries))
	}
	if sink.queries[0] != "select now()" {
		t.Fatalf("unexpected query %q", sink.queries[0])
	}
	if sink.details[0]["message"] != "Query" {
		t.Fatalf("unexpected message detail %#v", sink.details[0])
	}
}

func mysqlPacket(seq byte, payload []byte) []byte {
	packet := make([]byte, 4+len(payload))
	packet[0] = byte(len(payload))
	packet[1] = byte(len(payload) >> 8)
	packet[2] = byte(len(payload) >> 16)
	packet[3] = seq
	copy(packet[4:], payload)
	return packet
}

func postgresMessage(typ byte, payload []byte) []byte {
	msg := make([]byte, 1+4+len(payload))
	msg[0] = typ
	binary.BigEndian.PutUint32(msg[1:5], uint32(4+len(payload)))
	copy(msg[5:], payload)
	return msg
}
