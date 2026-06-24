package dbproxy

import (
	"encoding/binary"
	"testing"
	"time"

	"jianmen/internal/config"
)

type captureSink struct {
	queries  []string
	details  []map[string]any
	finished []queryFinish
	policy   *sqlPolicy
}

func (s *captureSink) StartQuery(sql string, detail map[string]any) (queryRecord, queryDecision) {
	s.queries = append(s.queries, sql)
	s.details = append(s.details, detail)
	decision := allowQuery()
	if s.policy != nil {
		decision = s.policy.Evaluate(sql)
	}
	record := queryRecord{
		seq:       int64(len(s.queries)),
		protocol:  "test",
		sql:       sql,
		queryKind: classifyQueryKind(sql),
		detail:    detail,
		startedAt: time.Now(),
	}
	if !decision.Allowed {
		s.finished = append(s.finished, queryFinish{
			Status:       decision.Status,
			ErrorCode:    decision.ErrorCode,
			ErrorMessage: decision.ErrorMessage,
			Detail:       decision.Detail,
		})
	}
	return record, decision
}

func (s *captureSink) FinishQuery(_ queryRecord, finish queryFinish) {
	s.finished = append(s.finished, finish)
}

func TestMySQLObserverCapturesQueryAcrossChunks(t *testing.T) {
	sink := &captureSink{}
	observer := &mysqlObserver{sink: sink}
	packet := mysqlPacket(0, append([]byte{0x03}, []byte("select 1")...))

	if decision := observer.ObserveClientBytes(packet[:3]); decision != nil {
		t.Fatalf("unexpected decision for partial packet: %#v", decision)
	}
	if decision := observer.ObserveClientBytes(packet[3:]); decision != nil {
		t.Fatalf("unexpected decision: %#v", decision)
	}

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

func TestMySQLObserverRejectsReadOnlyPolicyViolation(t *testing.T) {
	sink := &captureSink{policy: newSQLPolicy(config.DatabaseQueryPolicyConfig{ReadOnly: true})}
	observer := &mysqlObserver{sink: sink}
	packet := mysqlPacket(0, append([]byte{0x03}, []byte("delete from users")...))

	decision := observer.ObserveClientBytes(packet)

	if decision == nil || decision.Allowed {
		t.Fatalf("expected deny decision, got %#v", decision)
	}
	if decision.Status != queryStatusPolicyDenied {
		t.Fatalf("unexpected decision status %#v", decision)
	}
	if len(sink.finished) != 1 || sink.finished[0].Status != queryStatusPolicyDenied {
		t.Fatalf("expected policy denied finish event, got %#v", sink.finished)
	}
	if response := observer.ErrorResponse(*decision); len(response) == 0 {
		t.Fatal("expected mysql error response")
	}
}

func TestPostgresObserverCapturesSimpleQuery(t *testing.T) {
	sink := &captureSink{}
	observer := &postgresObserver{sink: sink}

	startup := make([]byte, 8)
	binary.BigEndian.PutUint32(startup[0:4], 8)
	binary.BigEndian.PutUint32(startup[4:8], 196608)
	query := postgresMessage('Q', append([]byte("select now()"), 0))

	if decision := observer.ObserveClientBytes(append(startup, query...)); decision != nil {
		t.Fatalf("unexpected decision: %#v", decision)
	}

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
