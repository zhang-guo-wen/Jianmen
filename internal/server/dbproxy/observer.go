package dbproxy

import (
	"encoding/binary"
)

type mysqlObserver struct {
	sink              querySink
	clientBuf         []byte
	serverBuf         []byte
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
			return forward, o.fail(observerErrorBufferLimit, "MySQL observer frame exceeds the audit limit")
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

type postgresObserver struct {
	sink               querySink
	clientBuf          []byte
	serverBuf          []byte
	startupDone        bool
	pending            []queryRecord
	preparedStatements map[string]string
	portals            map[string]postgresPortal
	cycles             []*postgresQueryCycle
	openCycle          *postgresQueryCycle
	fatal              *queryDecision
}

func (o *postgresObserver) ObserveClientBytes(data []byte) *queryDecision {
	_, decision := o.observeClientRelayBytes(data)
	return decision
}

func (o *postgresObserver) ObserveClientRelayBytes(data []byte) ([]byte, *queryDecision) {
	return o.observeClientRelayBytes(data)
}

func (o *postgresObserver) observeClientRelayBytes(data []byte) ([]byte, *queryDecision) {
	if o.fatal != nil {
		return nil, o.fatal
	}
	if len(data) == 0 {
		return nil, nil
	}
	var forward []byte
	for {
		if !o.startupDone {
			frame, complete := o.nextPostgresStartupFrame()
			if o.fatal != nil {
				return forward, o.fatal
			}
			if !complete {
				if len(data) == 0 {
					return forward, nil
				}
				if !appendObserverBufferChunk(&o.clientBuf, &data, maxPostgresObserverBufferBytes) {
					return forward, o.fail(observerErrorBufferLimit, "PostgreSQL observer frame exceeds the audit limit")
				}
				continue
			}
			forward = append(forward, frame...)
			continue
		}
		if len(o.clientBuf) < 5 {
			if len(data) == 0 {
				return forward, nil
			}
			if !appendObserverBufferChunk(&o.clientBuf, &data, maxPostgresObserverBufferBytes) {
				return forward, o.fail(observerErrorBufferLimit, "PostgreSQL observer frame exceeds the audit limit")
			}
			continue
		}
		typ := o.clientBuf[0]
		msgLen := int(binary.BigEndian.Uint32(o.clientBuf[1:5]))
		if msgLen < 4 || msgLen > 128*1024*1024 {
			return forward, o.fail(observerErrorProtocol, "malformed PostgreSQL message")
		}
		total := 1 + msgLen
		if total > maxPostgresObserverBufferBytes {
			return forward, o.fail(observerErrorBufferLimit, "PostgreSQL observer frame exceeds the audit limit")
		}
		if len(o.clientBuf) < total {
			if len(data) == 0 {
				return forward, nil
			}
			if !appendObserverBufferChunk(&o.clientBuf, &data, maxPostgresObserverBufferBytes) {
				return forward, o.fail(observerErrorBufferLimit, "PostgreSQL observer frame exceeds the audit limit")
			}
			continue
		}
		frame := o.clientBuf[:total]
		payload := o.clientBuf[5:total]
		if !validPostgresFrontendMessage(typ, payload) {
			return forward, o.fail(observerErrorProtocol, "malformed or unsupported PostgreSQL frontend message")
		}
		if decision := o.handleClientMessage(typ, payload); decision != nil && !decision.Allowed {
			return forward, o.failDecision(decision)
		}
		if o.fatal != nil {
			return forward, o.fatal
		}
		forward = append(forward, frame...)
		o.clientBuf = o.clientBuf[total:]
	}
}

func (o *postgresObserver) ObserveServerBytes(data []byte) *queryDecision {
	_, decision := o.observeServerRelayBytes(data)
	return decision
}

func (o *postgresObserver) ObserveServerRelayBytes(data []byte) ([]byte, *queryDecision) {
	return o.observeServerRelayBytes(data)
}

func (o *postgresObserver) observeServerRelayBytes(data []byte) ([]byte, *queryDecision) {
	if o.fatal != nil {
		return nil, o.fatal
	}
	if len(data) == 0 {
		return nil, nil
	}
	var forward []byte
	for {
		if len(o.serverBuf) < 5 {
			if len(data) == 0 {
				return forward, nil
			}
			if !appendObserverBufferChunk(&o.serverBuf, &data, maxPostgresObserverBufferBytes) {
				return forward, o.fail(observerErrorBufferLimit, "PostgreSQL observer frame exceeds the audit limit")
			}
			continue
		}
		typ := o.serverBuf[0]
		msgLen := int(binary.BigEndian.Uint32(o.serverBuf[1:5]))
		if msgLen < 4 || msgLen > 128*1024*1024 {
			return forward, o.fail(observerErrorProtocol, "malformed PostgreSQL message")
		}
		total := 1 + msgLen
		if total > maxPostgresObserverBufferBytes {
			return forward, o.fail(observerErrorBufferLimit, "PostgreSQL observer frame exceeds the audit limit")
		}
		if len(o.serverBuf) < total {
			if len(data) == 0 {
				return forward, nil
			}
			if !appendObserverBufferChunk(&o.serverBuf, &data, maxPostgresObserverBufferBytes) {
				return forward, o.fail(observerErrorBufferLimit, "PostgreSQL observer frame exceeds the audit limit")
			}
			continue
		}
		frame := o.serverBuf[:total]
		payload := o.serverBuf[5:total]
		if !validPostgresBackendMessage(typ, payload) {
			return forward, o.fail(observerErrorProtocol, "malformed or unsupported PostgreSQL backend message")
		}
		o.handleServerMessage(typ, payload)
		if o.fatal != nil {
			return forward, o.fatal
		}
		forward = append(forward, frame...)
		o.serverBuf = o.serverBuf[total:]
	}
}

func (o *postgresObserver) fail(code, message string) *queryDecision {
	return o.failDecision(newObserverFatalDecision(code, message))
}

func (o *postgresObserver) Abort(code string) {
	decision := newObserverRelayDecision()
	if code != "" {
		decision.ErrorCode = code
	}
	o.failDecision(decision)
}

func (o *postgresObserver) HasPending() bool {
	return len(o.pending) > 0
}

func (o *postgresObserver) failDecision(decision *queryDecision) *queryDecision {
	if o.fatal == nil {
		o.fatal = decision
	}
	auditFailed := false
	if o.sink != nil {
		for _, record := range o.pending {
			if o.postgresPendingFinishFailed(record) {
				continue
			}
			if !finishObservedQuery(o.sink, record, queryFinish{
				Status:       queryStatusError,
				ErrorCode:    o.fatal.ErrorCode,
				ErrorMessage: "postgres observer terminated the command",
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
	o.pending = nil
	o.preparedStatements = nil
	o.portals = nil
	o.cycles = nil
	o.openCycle = nil
	return o.fatal
}

func (o *postgresObserver) ErrorResponse(_ queryDecision) []byte {
	const message = "database proxy rejected query"
	payload := []byte{'S'}
	payload = append(payload, []byte("ERROR")...)
	payload = append(payload, 0, 'C')
	payload = append(payload, []byte("42501")...)
	payload = append(payload, 0, 'M')
	payload = append(payload, []byte(message)...)
	payload = append(payload, 0, 0)
	return append(postgresMessageWithType('E', payload), postgresReadyForQuery()...)
}

func (o *postgresObserver) consumeStartup() bool {
	_, complete := o.nextPostgresStartupFrame()
	return complete
}

func (o *postgresObserver) nextPostgresStartupFrame() ([]byte, bool) {
	if len(o.clientBuf) < 4 {
		return nil, false
	}
	msgLen := int(binary.BigEndian.Uint32(o.clientBuf[:4]))
	if msgLen < 8 || msgLen > 128*1024*1024 {
		o.fail(observerErrorProtocol, "malformed PostgreSQL startup message")
		return nil, false
	}
	if msgLen > maxPostgresObserverBufferBytes {
		o.fail(observerErrorBufferLimit, "PostgreSQL observer frame exceeds the audit limit")
		return nil, false
	}
	if len(o.clientBuf) < msgLen {
		return nil, false
	}
	frame := append([]byte(nil), o.clientBuf[:msgLen]...)
	code := binary.BigEndian.Uint32(o.clientBuf[4:8])
	o.clientBuf = o.clientBuf[msgLen:]
	switch code {
	case 80877103, 80877104:
		return frame, true
	default:
		o.startupDone = true
		return frame, true
	}
}

func (o *postgresObserver) handleClientMessage(typ byte, payload []byte) *queryDecision {
	return o.observePostgresClientMessage(typ, payload)
}

func (o *postgresObserver) handleServerMessage(typ byte, payload []byte) {
	o.observePostgresServerMessage(typ, payload)
}
