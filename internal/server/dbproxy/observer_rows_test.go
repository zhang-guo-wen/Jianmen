package dbproxy

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestMySQLObserverRowsAffectedRejectsUint64Overflow(t *testing.T) {
	tests := []struct {
		name string
		rows uint64
		want *int64
	}{
		{name: "MaxInt64", rows: math.MaxInt64, want: int64Pointer(math.MaxInt64)},
		{name: "MaxInt64 plus one", rows: uint64(math.MaxInt64) + 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &captureSink{}
			observer := &mysqlObserver{
				sink:    sink,
				pending: []queryRecord{{seq: 1}},
			}
			payload := make([]byte, 10)
			payload[0] = 0x00
			payload[1] = 0xfe
			binary.LittleEndian.PutUint64(payload[2:], tt.rows)

			if decision := observer.ObserveServerBytes(mysqlPacketWithSeq(1, payload)); decision != nil {
				t.Fatalf("server response denied: %#v", decision)
			}
			if len(sink.finished) != 1 {
				t.Fatalf("finished responses = %d, want 1", len(sink.finished))
			}
			got := sink.finished[0].RowsAffected
			if tt.want == nil {
				if got != nil {
					t.Fatalf("rows affected = %d, want unknown", *got)
				}
				return
			}
			if got == nil || *got != *tt.want {
				t.Fatalf("rows affected = %v, want %d", got, *tt.want)
			}
		})
	}
}

func TestPostgresObserverRowsAffectedRejectsDecimalOverflow(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		want *int64
	}{
		{name: "MaxInt64", tag: "UPDATE 9223372036854775807", want: int64Pointer(math.MaxInt64)},
		{name: "MaxInt64 plus one", tag: "UPDATE 9223372036854775808"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink := &captureSink{}
			observer := &postgresObserver{
				sink:        sink,
				startupDone: true,
			}
			if decision := observer.ObserveClientBytes(postgresMessage('Q', []byte("update items set active = true\x00"))); decision != nil {
				t.Fatalf("client query denied: %#v", decision)
			}
			response := append(
				postgresMessageWithType('C', append([]byte(tt.tag), 0)),
				postgresReadyForQuery()...,
			)

			if decision := observer.ObserveServerBytes(response); decision != nil {
				t.Fatalf("server response denied: %#v", decision)
			}
			if len(sink.finished) != 1 {
				t.Fatalf("finished responses = %d, want 1", len(sink.finished))
			}
			got := sink.finished[0].RowsAffected
			if tt.want == nil {
				if got != nil {
					t.Fatalf("rows affected = %d, want unknown", *got)
				}
				if _, ok := parsePostgresRowsFromCommandTag(tt.tag); ok {
					t.Fatal("overflowing command tag parsed as a valid row count")
				}
				return
			}
			if got == nil || *got != *tt.want {
				t.Fatalf("rows affected = %v, want %d", got, *tt.want)
			}
		})
	}
}

func int64Pointer(value int64) *int64 {
	return &value
}
