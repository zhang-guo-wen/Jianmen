package dbproxy

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

func TestMySQLPreparedStatementLifecycleIsFailClosedAndAuditable(t *testing.T) {
	t.Run("prepare maps id and execute audits redacted sql", func(t *testing.T) {
		sink := &captureSink{}
		observer := &mysqlObserver{sink: sink}
		establishMySQLPreparedStatement(t, observer, 41, "SELECT * FROM users WHERE token = 'prepare-secret'", true)

		execute := mysqlStatementCommand(0x17, 41)
		forward, decision := observer.ObserveClientRelayBytes(execute)
		if decision != nil || !bytes.Equal(forward, execute) {
			t.Fatalf("execute relay = (%x, %#v), want original packet", forward, decision)
		}
		if len(sink.queries) != 2 {
			t.Fatalf("audited queries = %v, want prepare and execute", sink.queries)
		}
		if sink.queries[1] != sink.queries[0] || strings.Contains(sink.queries[1], "prepare-secret") {
			t.Fatalf("execute audit = %q, prepare audit = %q", sink.queries[1], sink.queries[0])
		}
		if got := fmt.Sprint(sink.details[1]["stmt_id"]); got != "41" {
			t.Fatalf("execute stmt_id detail = %q, want 41", got)
		}
		if got := sink.details[1]["command"]; got != "COM_STMT_EXECUTE" {
			t.Fatalf("execute command detail = %#v", got)
		}
		if got := sink.details[1]["prepared_query_kind"]; got != "select" {
			t.Fatalf("prepared query kind = %#v, want select", got)
		}
	})

	t.Run("unknown statement commands never reach upstream", func(t *testing.T) {
		for _, command := range []byte{0x17, 0x18, 0x19, 0x1a} {
			t.Run(fmt.Sprintf("command_%02x", command), func(t *testing.T) {
				observer := &mysqlObserver{sink: &captureSink{}}
				packet := mysqlStatementCommand(command, 999)

				forward, decision := observer.ObserveClientRelayBytes(packet)

				if len(forward) != 0 {
					t.Fatalf("unknown statement command forwarded: %x", forward)
				}
				requireObserverDenied(t, decision)
			})
		}
	})

	t.Run("reset preserves statement and close deletes it", func(t *testing.T) {
		observer := &mysqlObserver{sink: &captureSink{}}
		establishMySQLPreparedStatement(t, observer, 7, "select ?", false)

		reset := mysqlStatementCommand(0x1a, 7)
		forward, decision := observer.ObserveClientRelayBytes(reset)
		if decision != nil || !bytes.Equal(forward, reset) {
			t.Fatalf("reset relay = (%x, %#v)", forward, decision)
		}
		resetOK := buildMySQLPacket(1, []byte{0x00, 0, 0, 0, 0, 0, 0})
		if forward, decision = observer.ObserveServerRelayBytes(resetOK); decision != nil || !bytes.Equal(forward, resetOK) {
			t.Fatalf("reset response relay = (%x, %#v)", forward, decision)
		}

		execute := mysqlStatementCommand(0x17, 7)
		if forward, decision = observer.ObserveClientRelayBytes(execute); decision != nil || !bytes.Equal(forward, execute) {
			t.Fatalf("execute after reset relay = (%x, %#v)", forward, decision)
		}
		executeOK := buildMySQLPacket(1, []byte{0x00, 0, 0, 0, 0, 0, 0})
		if _, decision = observer.ObserveServerRelayBytes(executeOK); decision != nil {
			t.Fatalf("execute response decision = %#v", decision)
		}

		closePacket := mysqlStatementCommand(0x19, 7)
		if forward, decision = observer.ObserveClientRelayBytes(closePacket); decision != nil || !bytes.Equal(forward, closePacket) {
			t.Fatalf("close relay = (%x, %#v)", forward, decision)
		}
		if forward, decision = observer.ObserveClientRelayBytes(execute); len(forward) != 0 || decision == nil {
			t.Fatalf("execute after close relay = (%x, %#v), want fail closed", forward, decision)
		}
	})

	t.Run("failed prepare never creates mapping", func(t *testing.T) {
		observer := &mysqlObserver{sink: &captureSink{}}
		prepare := buildMySQLPacket(0, append([]byte{0x16}, []byte("select ?")...))
		forward, decision := observer.ObserveClientRelayBytes(prepare)
		requireMySQLRelayForward(t, forward, decision, prepare)
		errorPacket := buildMySQLPacket(1, []byte{0xff, 0x28, 0x04})
		forward, decision = observer.ObserveServerRelayBytes(errorPacket)
		requireMySQLRelayForward(t, forward, decision, errorPacket)

		forward, decision = observer.ObserveClientRelayBytes(mysqlStatementCommand(0x17, 55))
		if len(forward) != 0 {
			t.Fatalf("execute after failed prepare forwarded: %x", forward)
		}
		requireObserverDenied(t, decision)
	})

	t.Run("duplicate server statement id is fatal and not forwarded", func(t *testing.T) {
		observer := &mysqlObserver{sink: &captureSink{}}
		establishMySQLPreparedStatement(t, observer, 12, "select 1", false)
		prepare := buildMySQLPacket(0, append([]byte{0x16}, []byte("select 2")...))
		forward, decision := observer.ObserveClientRelayBytes(prepare)
		requireMySQLRelayForward(t, forward, decision, prepare)

		duplicate := buildMySQLPacket(1, mysqlPrepareOKPayload(12))
		forward, decision = observer.ObserveServerRelayBytes(duplicate)
		if len(forward) != 0 {
			t.Fatalf("duplicate statement id response forwarded: %x", forward)
		}
		requireObserverDenied(t, decision)
	})
}

func establishMySQLPreparedStatement(
	t *testing.T,
	observer *mysqlObserver,
	statementID uint32,
	sql string,
	fragmentResponse bool,
) {
	t.Helper()
	prepare := buildMySQLPacket(0, append([]byte{0x16}, []byte(sql)...))
	forward, decision := observer.ObserveClientRelayBytes(prepare)
	requireMySQLRelayForward(t, forward, decision, prepare)
	response := buildMySQLPacket(1, mysqlPrepareOKPayload(statementID))
	if !fragmentResponse {
		forward, decision = observer.ObserveServerRelayBytes(response)
		requireMySQLRelayForward(t, forward, decision, response)
		return
	}
	var forwarded []byte
	for _, value := range response {
		chunk, chunkDecision := observer.ObserveServerRelayBytes([]byte{value})
		if chunkDecision != nil {
			t.Fatalf("fragmented prepare response denied: %#v", chunkDecision)
		}
		forwarded = append(forwarded, chunk...)
	}
	if !bytes.Equal(forwarded, response) {
		t.Fatalf("fragmented prepare response forwarded = %x, want %x", forwarded, response)
	}
}

func mysqlPrepareOKPayload(statementID uint32) []byte {
	payload := make([]byte, 12)
	payload[0] = 0x00
	binary.LittleEndian.PutUint32(payload[1:5], statementID)
	return payload
}

func mysqlStatementCommand(command byte, statementID uint32) []byte {
	length := 5
	switch command {
	case 0x17:
		length = 10
	case 0x18:
		length = 7
	}
	payload := make([]byte, length)
	payload[0] = command
	binary.LittleEndian.PutUint32(payload[1:5], statementID)
	if command == 0x17 {
		payload[5] = 0
		binary.LittleEndian.PutUint32(payload[6:10], 1)
	}
	return buildMySQLPacket(0, payload)
}

func requireMySQLRelayForward(
	t *testing.T,
	forward []byte,
	decision *queryDecision,
	want []byte,
) {
	t.Helper()
	if decision != nil || !bytes.Equal(forward, want) {
		t.Fatalf("relay = (%x, %#v), want (%x, nil)", forward, decision, want)
	}
}
