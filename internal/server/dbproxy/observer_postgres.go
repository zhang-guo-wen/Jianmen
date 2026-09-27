package dbproxy

import (
	"encoding/binary"

	"jianmen/internal/config"
)

type postgresObserver struct {
	sink                  querySink
	maxClientMessageBytes int
	clientBuf             []byte
	serverBuf             []byte
	clientStream          *postgresClientFrameStream
	serverStream          *postgresFrameStream
	memoryBudget          *observerMemoryBudget
	startupDone           bool
	pending               []queryRecord
	preparedStatements    map[string]*postgresPreparedStatement
	portals               map[string]postgresPortal
	operations            []postgresFrontendOperation
	cycles                []*postgresQueryCycle
	openCycle             *postgresQueryCycle
	fatal                 *queryDecision
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
		if o.clientStream != nil {
			chunk, consumed, decision := o.consumePostgresClientFrameStream(data)
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
			headerBufferLimit := normalizeMaxClientMessageBytes(o.maxClientMessageBytes)
			if headerBufferLimit > maxPostgresObserverBufferBytes {
				headerBufferLimit = maxPostgresObserverBufferBytes
			}
			if !appendObserverBufferChunk(&o.clientBuf, &data, headerBufferLimit) {
				return forward, o.fail(observerErrorBufferLimit, "PostgreSQL observer frame exceeds the audit limit")
			}
			continue
		}
		typ := o.clientBuf[0]
		msgLen := int(binary.BigEndian.Uint32(o.clientBuf[1:5]))
		if msgLen < 4 || msgLen > config.MaxDatabaseGatewayMaxClientMessageBytes {
			return forward, o.fail(observerErrorProtocol, "malformed PostgreSQL message")
		}
		total := 1 + msgLen
		maxClientMessageBytes := normalizeMaxClientMessageBytes(o.maxClientMessageBytes)
		if typ == 'd' && total > maxPostgresObserverBufferBytes {
			o.clientStream = newPostgresClientFrameStream(
				typ,
				total-5,
				true,
				nil,
				o.memoryBudget,
				o.sink,
			)
			if o.clientStream.forward {
				forward = append(forward, o.clientBuf[:5]...)
			}
			bufferedPayload := o.clientBuf[5:]
			o.clientBuf = nil
			chunk, _, streamDecision := o.consumePostgresClientFrameStream(bufferedPayload)
			forward = append(forward, chunk...)
			if streamDecision != nil {
				return forward, streamDecision
			}
			continue
		}
		if total > maxClientMessageBytes {
			decision := newObserverFatalDecision(
				observerErrorClientMessageLimit,
				"PostgreSQL client message exceeds the configured limit",
			)
			o.clientStream = newPostgresClientFrameStream(
				typ,
				total-5,
				false,
				decision,
				o.memoryBudget,
				o.sink,
			)
			bufferedPayload := o.clientBuf[5:]
			o.clientBuf = nil
			_, _, streamDecision := o.consumePostgresClientFrameStream(bufferedPayload)
			if streamDecision != nil {
				return forward, streamDecision
			}
			continue
		}
		if total > maxPostgresObserverBufferBytes && canStreamPostgresFrontendFrame(typ) {
			o.clientStream = newPostgresClientFrameStream(
				typ,
				total-5,
				true,
				nil,
				o.memoryBudget,
				o.sink,
			)
			if o.clientStream.forward {
				forward = append(forward, o.clientBuf[:5]...)
			}
			bufferedPayload := o.clientBuf[5:]
			o.clientBuf = nil
			chunk, _, streamDecision := o.consumePostgresClientFrameStream(bufferedPayload)
			forward = append(forward, chunk...)
			if streamDecision != nil {
				return forward, streamDecision
			}
			continue
		}
		if len(o.clientBuf) < total {
			if len(data) == 0 {
				return forward, nil
			}
			if !appendObserverBufferChunk(&o.clientBuf, &data, maxPostgresObserverBufferBytes) {
				return forward, o.fail(observerErrorBufferLimit, "PostgreSQL observer frame exceeds the audit buffer limit")
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
		if o.serverStream != nil {
			chunk, consumed := consumePostgresFrameStream(&o.serverStream, data)
			forward = append(forward, chunk...)
			data = data[consumed:]
			if len(data) == 0 {
				return forward, nil
			}
			continue
		}
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
			if !canStreamPostgresBackendFrame(typ) {
				return forward, o.fail(observerErrorBufferLimit, "PostgreSQL observer frame exceeds the audit limit")
			}
			o.serverStream = &postgresFrameStream{remaining: total - 5}
			forward = append(forward, o.serverBuf[:5]...)
			bufferedPayload := o.serverBuf[5:]
			o.serverBuf = nil
			chunk, _ := consumePostgresFrameStream(&o.serverStream, bufferedPayload)
			forward = append(forward, chunk...)
			continue
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

func (o *postgresObserver) CloseObserver() {
	if o.clientStream != nil {
		o.clientStream.close()
		o.clientStream = nil
	}
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
	if o.clientStream != nil {
		o.clientStream.close()
		o.clientStream = nil
	}
	o.serverStream = nil
	o.pending = nil
	o.preparedStatements = nil
	o.portals = nil
	o.operations = nil
	o.cycles = nil
	o.openCycle = nil
	return o.fatal
}

func (o *postgresObserver) ErrorResponse(decision queryDecision) []byte {
	code, message, hint := postgresProxyErrorFields(decision.ErrorCode)
	payload := []byte{'S'}
	payload = append(payload, []byte("ERROR")...)
	payload = append(payload, 0, 'C')
	payload = append(payload, []byte(code)...)
	payload = append(payload, 0, 'M')
	payload = append(payload, []byte(message)...)
	if hint != "" {
		payload = append(payload, 0, 'H')
		payload = append(payload, []byte(hint)...)
	}
	payload = append(payload, 0, 0)
	return append(postgresMessageWithType('E', payload), postgresReadyForQuery()...)
}

func postgresProxyErrorFields(observerCode string) (code, message, hint string) {
	switch observerCode {
	case observerErrorClientMessageLimit:
		return "54000",
			"database proxy client message exceeds the configured limit",
			"increase the gateway limit, split the statement, or use COPY"
	case observerErrorBufferLimit:
		return "54000",
			"database proxy protocol buffer limit exceeded",
			"retry with a smaller protocol message"
	case observerErrorPendingLimit:
		return "54000",
			"database proxy audit state limit exceeded",
			"reduce pipelining or close unused prepared statements"
	case observerErrorProtocol:
		return "08P01", "database proxy rejected a malformed protocol message", ""
	case observerErrorAuditFailure:
		return "58000", "database proxy audit service is unavailable", ""
	case observerErrorRelay, observerErrorDrainTimeout:
		return "08006", "database proxy relay terminated", ""
	default:
		return "42501", "database proxy rejected query", ""
	}
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
