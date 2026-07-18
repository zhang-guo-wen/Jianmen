package dbproxy

import (
	"encoding/binary"
)

type mysqlObserver struct {
	sink              querySink
	clientBuf         []byte
	serverBuf         []byte
	serverStream      *mysqlServerPacketStream
	pending           []queryRecord
	pendingRecorded   []bool
	pendingFailed     []bool
	pendingCommands   []byte
	pendingPrepared   []mysqlPreparedStatement
	prepared          map[uint32]mysqlPreparedStatement
	response          *mysqlResponseState
	fatal             *queryDecision
	nextErrorSeq      byte
	hasErrorSeq       bool
	expectedServerSeq byte
	serverSeqActive   bool
}

func (o *mysqlObserver) ObserveClientBytes(data []byte) *queryDecision {
	_, decision := o.observeClientRelayBytes(data)
	return decision
}

func (o *mysqlObserver) ObserveClientRelayBytes(data []byte) ([]byte, *queryDecision) {
	return o.observeClientRelayBytes(data)
}

func (o *mysqlObserver) observeClientRelayBytes(data []byte) ([]byte, *queryDecision) {
	if o.fatal != nil {
		return nil, o.fatal
	}
	if len(data) == 0 {
		return nil, nil
	}
	var forward []byte
	for {
		if len(o.clientBuf) < 4 {
			if len(data) == 0 {
				return forward, nil
			}
			if !appendObserverBufferChunk(&o.clientBuf, &data, maxMySQLObserverBufferBytes) {
				return forward, o.fail(observerErrorBufferLimit, "MySQL observer frame exceeds the audit limit")
			}
			continue
		}
		payloadLen := int(o.clientBuf[0]) | int(o.clientBuf[1])<<8 | int(o.clientBuf[2])<<16
		total := 4 + payloadLen
		if payloadLen < 0 || total < 4 {
			return forward, o.fail(observerErrorProtocol, "malformed MySQL packet")
		}
		if total > maxMySQLObserverBufferBytes {
			return forward, o.fail(observerErrorBufferLimit, "MySQL observer frame exceeds the audit limit")
		}
		if len(o.clientBuf) < total {
			if len(data) == 0 {
				return forward, nil
			}
			if !appendObserverBufferChunk(&o.clientBuf, &data, maxMySQLObserverBufferBytes) {
				return forward, o.fail(observerErrorBufferLimit, "MySQL observer frame exceeds the audit limit")
			}
			continue
		}
		packet := o.clientBuf[:total]
		seq := o.clientBuf[3]
		payload := o.clientBuf[4:total]
		o.nextErrorSeq = 1
		o.hasErrorSeq = true
		if seq != 0 {
			return forward, o.fail(observerErrorProtocol, "unexpected MySQL client packet sequence")
		}
		if decision := o.handleClientPacket(seq, payload); decision != nil && !decision.Allowed {
			return forward, o.failDecision(decision)
		}
		if o.fatal != nil {
			return forward, o.fatal
		}
		forward = append(forward, packet...)
		o.clientBuf = o.clientBuf[total:]
	}
}

func (o *mysqlObserver) ObserveServerBytes(data []byte) *queryDecision {
	_, decision := o.observeServerRelayBytes(data)
	return decision
}

func (o *mysqlObserver) ObserveServerRelayBytes(data []byte) ([]byte, *queryDecision) {
	return o.observeServerRelayBytes(data)
}

func (o *mysqlObserver) observeServerRelayBytes(data []byte) ([]byte, *queryDecision) {
	if o.fatal != nil {
		return nil, o.fatal
	}
	if len(data) == 0 {
		return nil, nil
	}
	var forward []byte
	for {
		if o.serverStream != nil {
			chunk, consumed, decision := o.consumeMySQLServerPacketStream(data)
			forward = append(forward, chunk...)
			data = data[consumed:]
			if decision != nil {
				return forward, decision
			}
			if len(data) == 0 {
				return forward, nil
			}
			continue
		}
		if len(o.serverBuf) < 4 {
			if len(data) == 0 {
				return forward, nil
			}
			if !appendObserverBufferChunk(&o.serverBuf, &data, maxMySQLObserverBufferBytes) {
				return forward, o.fail(observerErrorBufferLimit, "MySQL observer frame exceeds the audit limit")
			}
			continue
		}
		payloadLen := int(o.serverBuf[0]) | int(o.serverBuf[1])<<8 | int(o.serverBuf[2])<<16
		total := 4 + payloadLen
		if payloadLen < 0 || total < 4 {
			return forward, o.fail(observerErrorProtocol, "malformed MySQL packet")
		}
		if total > maxMySQLObserverBufferBytes {
			sequence := o.serverBuf[3]
			if !o.acceptMySQLServerSequence(sequence) {
				return forward, o.fail(observerErrorProtocol, "unexpected MySQL server packet sequence")
			}
			o.serverStream = &mysqlServerPacketStream{
				remaining: payloadLen,
				sequence:  sequence,
			}
			forward = append(forward, o.serverBuf[:4]...)
			bufferedPayload := o.serverBuf[4:]
			o.serverBuf = nil
			chunk, _, decision := o.consumeMySQLServerPacketStream(bufferedPayload)
			forward = append(forward, chunk...)
			if decision != nil {
				return forward, decision
			}
			continue
		}
		if len(o.serverBuf) < total {
			if len(data) == 0 {
				return forward, nil
			}
			if !appendObserverBufferChunk(&o.serverBuf, &data, maxMySQLObserverBufferBytes) {
				return forward, o.fail(observerErrorBufferLimit, "MySQL observer frame exceeds the audit limit")
			}
			continue
		}
		packet := o.serverBuf[:total]
		if !o.acceptMySQLServerSequence(packet[3]) {
			return forward, o.fail(observerErrorProtocol, "unexpected MySQL server packet sequence")
		}
		payload := o.serverBuf[4:total]
		if decision := o.handleServerPacket(payload); decision != nil {
			return forward, decision
		}
		if o.fatal != nil {
			return forward, o.fatal
		}
		forward = append(forward, packet...)
		o.nextErrorSeq = packet[3] + 1
		o.hasErrorSeq = true
		o.serverBuf = o.serverBuf[total:]
	}
}

func (o *mysqlObserver) fail(code, message string) *queryDecision {
	return o.failDecision(newObserverFatalDecision(code, message))
}

func (o *mysqlObserver) Abort(code string) {
	decision := newObserverRelayDecision()
	if code != "" {
		decision.ErrorCode = code
	}
	o.failDecision(decision)
}

func (o *mysqlObserver) HasPending() bool {
	return len(o.pending) > 0
}

func (o *mysqlObserver) failDecision(decision *queryDecision) *queryDecision {
	if o.fatal == nil {
		o.fatal = decision
	}
	auditFailed := false
	if o.sink != nil {
		for index, record := range o.pending {
			if !o.mysqlPendingIsRecorded(index) || o.mysqlPendingFinishFailed(index) {
				continue
			}
			if !finishObservedQuery(o.sink, record, queryFinish{
				Status:       queryStatusError,
				ErrorCode:    o.fatal.ErrorCode,
				ErrorMessage: "mysql observer terminated the command",
			}) {
				auditFailed = true
			}
		}
	}
	if auditFailed {
		o.fatal = auditSinkFailureDecision()
	}
	o.clientBuf = nil
	o.serverBuf = nil
	o.serverStream = nil
	o.pending = nil
	o.pendingRecorded = nil
	o.pendingFailed = nil
	o.pendingCommands = nil
	o.pendingPrepared = nil
	o.prepared = nil
	o.response = nil
	o.expectedServerSeq = 0
	o.serverSeqActive = false
	return o.fatal
}

func (o *mysqlObserver) ErrorResponse(_ queryDecision) []byte {
	const message = "database proxy rejected query"
	payload := make([]byte, 0, 9+len(message))
	payload = append(payload, 0xff)
	payload = append(payload, 0x84, 0x04)
	payload = append(payload, '#')
	payload = append(payload, []byte("HY000")...)
	payload = append(payload, []byte(message)...)
	seq := o.nextErrorSeq
	if !o.hasErrorSeq {
		seq = 1
	}
	return mysqlPacketWithSeq(seq, payload)
}

func (o *mysqlObserver) handleClientPacket(seq byte, payload []byte) (decision *queryDecision) {
	defer func() {
		if r := recover(); r != nil {
			decision = o.fail(observerErrorProtocol, "malformed MySQL command")
		}
	}()
	if len(payload) == 0 {
		return newObserverFatalDecision(observerErrorProtocol, "malformed MySQL command")
	}
	o.finishMySQLPrepareWithOmittedTerminator()
	if o.fatal != nil {
		return o.fatal
	}
	cmd := payload[0]
	if !validMySQLClientCommand(cmd, payload) {
		return newObserverFatalDecision(observerErrorProtocol, "MySQL command is not supported after gateway authentication")
	}
	if mysqlCommandExpectsResponse(cmd) {
		if len(o.pending) >= maxObserverPendingQueries {
			return o.fail(observerErrorPendingLimit, "too many in-flight MySQL commands")
		}
	}
	statement, statementDecision := o.resolveMySQLStatementCommand(cmd, payload)
	if statementDecision != nil {
		return statementDecision
	}
	prepared := mysqlPreparedStatement{}
	if cmd == mysqlCommandStmtPrepare {
		prepared = newMySQLPreparedStatement(string(payload[1:]))
	}
	if o.sink == nil {
		if mysqlCommandExpectsResponse(cmd) {
			o.enqueueMySQLResponse(queryRecord{}, false, cmd)
			o.setLastMySQLPendingPrepared(prepared)
		}
		return nil
	}
	switch cmd {
	case 0x03: // COM_QUERY
		record, decision, ok := startObservedQuery(o.sink, redactDatabaseSQL(string(payload[1:])), map[string]any{
			"protocol": "mysql",
			"command":  "COM_QUERY",
			"seq":      seq,
		})
		if !ok {
			return auditSinkFailureDecision()
		}
		if !decision.Allowed {
			return &decision
		}
		o.enqueueMySQLResponse(record, true, cmd)
	case mysqlCommandStmtPrepare:
		record, decision, ok := startObservedQuery(o.sink, prepared.sql, map[string]any{
			"protocol":   "mysql",
			"command":    "COM_STMT_PREPARE",
			"query_kind": prepared.queryKind,
			"seq":        seq,
		})
		if !ok {
			return auditSinkFailureDecision()
		}
		if !decision.Allowed {
			return &decision
		}
		o.enqueueMySQLResponse(record, true, cmd)
		o.setLastMySQLPendingPrepared(prepared)
	case mysqlCommandStmtExecute:
		stmtID := binary.LittleEndian.Uint32(payload[1:5])
		record, decision, ok := startObservedQuery(o.sink, statement.sql, map[string]any{
			"protocol":            "mysql",
			"command":             "COM_STMT_EXECUTE",
			"stmt_id":             stmtID,
			"prepared_query_kind": statement.queryKind,
			"seq":                 seq,
		})
		if !ok {
			return auditSinkFailureDecision()
		}
		if !decision.Allowed {
			return &decision
		}
		o.enqueueMySQLResponse(record, true, cmd)
	default:
		if mysqlCommandExpectsResponse(cmd) {
			o.enqueueMySQLResponse(queryRecord{}, false, cmd)
		}
	}
	return nil
}

func (o *mysqlObserver) handleServerPacket(payload []byte) (decision *queryDecision) {
	defer func() {
		if recover() != nil {
			decision = o.fail(observerErrorProtocol, "malformed MySQL response")
		}
	}()
	if len(payload) == 0 || len(o.pending) == 0 {
		return nil
	}
	o.observeMySQLResponsePacket(payload)
	return o.fatal
}
