package dbproxy

import (
	"encoding/binary"
	"testing"
)

func TestMySQLObserverWaitsForFinalResultSetTerminator(t *testing.T) {
	sink := &captureSink{}
	observer := &mysqlObserver{sink: sink}
	observer.ObserveClientBytes(buildMySQLPacket(0, append([]byte{0x03}, []byte("select value from items")...)))

	responses := [][]byte{
		buildMySQLPacket(1, []byte{0x01}),
		buildMySQLPacket(2, []byte("column-definition")),
		buildMySQLPacket(3, mysqlTestEOFPacket(0)),
		buildMySQLPacket(4, []byte{0x05, 'v', 'a', 'l', 'u', 'e'}),
	}
	for index, response := range responses {
		if decision := observer.ObserveServerBytes(response); decision != nil {
			t.Fatalf("response %d decision = %#v", index, decision)
		}
		if len(sink.finished) != 0 {
			t.Fatalf("query finished after response %d before final EOF: %#v", index, sink.finished)
		}
	}

	observer.ObserveServerBytes(buildMySQLPacket(5, mysqlTestEOFPacket(0)))
	if len(sink.finished) != 1 || sink.finished[0].Status != queryStatusSuccess {
		t.Fatalf("finished queries = %#v, want one success after final EOF", sink.finished)
	}
}

func TestMySQLObserverKeepsPipelinedResponsesPaired(t *testing.T) {
	sink := &captureSink{}
	observer := &mysqlObserver{sink: sink}
	first := buildMySQLPacket(0, append([]byte{0x03}, []byte("select value from items")...))
	second := buildMySQLPacket(0, append([]byte{0x03}, []byte("delete from missing_table")...))
	observer.ObserveClientBytes(append(first, second...))

	firstResponse := append(buildMySQLPacket(1, []byte{0x01}), buildMySQLPacket(2, []byte("column-definition"))...)
	firstResponse = append(firstResponse, buildMySQLPacket(3, mysqlTestEOFPacket(0))...)
	firstResponse = append(firstResponse, buildMySQLPacket(4, mysqlTestEOFPacket(0))...)
	errorPayload := append([]byte{0xff, 0x72, 0x04, '#'}, []byte("42S02table missing")...)
	observer.ObserveServerBytes(append(firstResponse, buildMySQLPacket(1, errorPayload)...))

	if len(sink.finished) != 2 {
		t.Fatalf("finished queries = %#v, want two paired completions", sink.finished)
	}
	if sink.finished[0].Status != queryStatusSuccess || sink.finished[1].Status != queryStatusError {
		t.Fatalf("pipeline statuses = %#v, want success then error", sink.finished)
	}
}

func TestMySQLObserverSanitizesUpstreamErrorText(t *testing.T) {
	sink := &captureSink{}
	observer := &mysqlObserver{sink: sink}
	observer.ObserveClientBytes(buildMySQLPacket(0, append([]byte{0x03}, []byte("select secret")...)))
	errorPayload := append([]byte{0xff, 0x72, 0x04, '#'}, []byte("42S02server echoed upstream-secret")...)
	observer.ObserveServerBytes(buildMySQLPacket(1, errorPayload))

	if len(sink.finished) != 1 {
		t.Fatalf("finished queries = %#v, want one", sink.finished)
	}
	if sink.finished[0].ErrorCode != "1138" || sink.finished[0].ErrorMessage != "mysql upstream error" {
		t.Fatalf("sanitized MySQL error = %#v", sink.finished[0])
	}
}

func TestMySQLObserverWaitsForPrepareMetadataCompletion(t *testing.T) {
	sink := &captureSink{}
	observer := &mysqlObserver{sink: sink}
	observer.ObserveClientBytes(buildMySQLPacket(0, append([]byte{0x16}, []byte("select ?")...)))

	prepareOK := make([]byte, 12)
	prepareOK[0] = 0x00
	binary.LittleEndian.PutUint32(prepareOK[1:5], 7)
	binary.LittleEndian.PutUint16(prepareOK[5:7], 1)
	binary.LittleEndian.PutUint16(prepareOK[7:9], 1)
	observer.ObserveServerBytes(buildMySQLPacket(1, prepareOK))
	if len(sink.finished) != 0 {
		t.Fatalf("prepare finished at initial OK: %#v", sink.finished)
	}

	for index, payload := range [][]byte{
		[]byte("parameter-definition"),
		mysqlTestEOFPacket(0),
		[]byte("column-definition"),
	} {
		observer.ObserveServerBytes(buildMySQLPacket(byte(index+2), payload))
		if len(sink.finished) != 0 {
			t.Fatalf("prepare finished before final metadata EOF at packet %d", index+2)
		}
	}
	observer.ObserveServerBytes(buildMySQLPacket(5, mysqlTestEOFPacket(0)))
	if len(sink.finished) != 1 || sink.finished[0].Status != queryStatusSuccess {
		t.Fatalf("prepare finish = %#v, want final success", sink.finished)
	}
}

func TestMySQLObserverHandlesPrepareMetadataWhenDeprecatedEOFIsOmitted(t *testing.T) {
	sink := &captureSink{}
	observer := &mysqlObserver{sink: sink}
	observer.ObserveClientBytes(buildMySQLPacket(0, append([]byte{0x16}, []byte("select ?")...)))

	prepareOK := make([]byte, 12)
	prepareOK[0] = 0x00
	binary.LittleEndian.PutUint32(prepareOK[1:5], 7)
	binary.LittleEndian.PutUint16(prepareOK[5:7], 1)
	binary.LittleEndian.PutUint16(prepareOK[7:9], 1)
	observer.ObserveServerBytes(buildMySQLPacket(1, prepareOK))
	observer.ObserveServerBytes(buildMySQLPacket(2, []byte("parameter-definition")))
	observer.ObserveServerBytes(buildMySQLPacket(3, []byte("column-definition")))
	if len(sink.finished) != 0 {
		t.Fatalf("prepare finished before the client observed omitted metadata terminator: %#v", sink.finished)
	}

	observer.ObserveClientBytes(buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...)))
	if len(sink.finished) != 1 || sink.finished[0].Status != queryStatusSuccess {
		t.Fatalf("prepare finish = %#v, want success before next command", sink.finished)
	}
	if len(sink.queries) != 2 {
		t.Fatalf("started queries = %v, want prepare and next query", sink.queries)
	}
}

func mysqlTestEOFPacket(status uint16) []byte {
	payload := []byte{0xfe, 0, 0, 0, 0}
	binary.LittleEndian.PutUint16(payload[3:5], status)
	return payload
}
