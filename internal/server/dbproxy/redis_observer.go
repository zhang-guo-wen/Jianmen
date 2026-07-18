package dbproxy

import (
	"strings"
)

type redisObserver struct {
	sink         querySink
	buf          []byte
	serverBuf    []byte
	serverStream *redisResponseStream
	slots        []redisResponseSlot
	fatal        *queryDecision
	deferred     *queryDecision
}

type redisResponseSlot struct {
	record       queryRecord
	recorded     bool
	finishFailed bool
}

func (o *redisObserver) ObserveClientBytes(data []byte) *queryDecision {
	_, decision := o.observeClientBytes(data)
	return decision
}

func (o *redisObserver) ObserveClientRelayBytes(data []byte) ([]byte, *queryDecision) {
	return o.observeClientBytes(data)
}

func (o *redisObserver) observeClientBytes(data []byte) ([]byte, *queryDecision) {
	if o.fatal != nil {
		return nil, o.fatal
	}
	if o.deferred != nil {
		return nil, nil
	}
	if len(data) == 0 {
		return nil, nil
	}
	var forward []byte
parseCommands:
	for {
		if len(o.buf) == 0 {
			if len(data) == 0 {
				return forward, nil
			}
			if !appendObserverBufferChunk(&o.buf, &data, maxRedisObserverBufferBytes) {
				return forward, o.fail(observerErrorBufferLimit, "Redis observer frame exceeds the audit limit")
			}
			continue
		}
		if o.buf[0] != '*' {
			return o.rejectClientBytes(forward, newObserverFatalDecision(observerErrorProtocol, "malformed Redis command"))
		}
		crlfIdx := indexCRLF(o.buf)
		if crlfIdx < 0 {
			if len(data) == 0 {
				return forward, nil
			}
			if !appendObserverBufferChunk(&o.buf, &data, maxRedisObserverBufferBytes) {
				return o.rejectClientBytes(forward, newObserverFatalDecision(observerErrorBufferLimit, "Redis observer frame exceeds the audit limit"))
			}
			continue
		}
		countValue, ok := parseCanonicalRESPUnsigned(o.buf[1:crlfIdx], 256)
		if !ok || countValue <= 0 {
			return o.rejectClientBytes(forward, newObserverFatalDecision(observerErrorProtocol, "malformed Redis command"))
		}
		count := int(countValue)
		pos := crlfIdx + 2
		args := make([]string, 0, count)
		for i := 0; i < count; i++ {
			if pos >= len(o.buf) {
				if len(data) == 0 {
					return forward, nil
				}
				if !appendObserverBufferChunk(&o.buf, &data, maxRedisObserverBufferBytes) {
					return o.rejectClientBytes(forward, newObserverFatalDecision(observerErrorBufferLimit, "Redis observer frame exceeds the audit limit"))
				}
				continue parseCommands
			}
			if o.buf[pos] != '$' {
				return o.rejectClientBytes(forward, newObserverFatalDecision(observerErrorProtocol, "malformed Redis command"))
			}
			crlf := indexCRLF(o.buf[pos:])
			if crlf < 0 {
				if len(data) == 0 {
					return forward, nil
				}
				if !appendObserverBufferChunk(&o.buf, &data, maxRedisObserverBufferBytes) {
					return o.rejectClientBytes(forward, newObserverFatalDecision(observerErrorBufferLimit, "Redis observer frame exceeds the audit limit"))
				}
				continue parseCommands
			}
			argLenValue, ok := parseCanonicalRESPNumber(o.buf[pos+1 : pos+crlf])
			if !ok {
				return o.rejectClientBytes(forward, newObserverFatalDecision(observerErrorProtocol, "malformed Redis command"))
			}
			if argLenValue > maxRedisObserverBufferBytes {
				return o.rejectClientBytes(forward, newObserverFatalDecision(observerErrorBufferLimit, "Redis observer frame exceeds the audit limit"))
			}
			argLen := int(argLenValue)
			pos += crlf + 2
			dataEnd := pos + argLen + 2
			if dataEnd > len(o.buf) {
				if len(data) == 0 {
					return forward, nil
				}
				if !appendObserverBufferChunk(&o.buf, &data, maxRedisObserverBufferBytes) {
					return o.rejectClientBytes(forward, newObserverFatalDecision(observerErrorBufferLimit, "Redis observer frame exceeds the audit limit"))
				}
				continue parseCommands
			}
			if o.buf[dataEnd-2] != '\r' || o.buf[dataEnd-1] != '\n' {
				return o.rejectClientBytes(forward, newObserverFatalDecision(observerErrorProtocol, "malformed Redis command"))
			}
			args = append(args, string(o.buf[pos:pos+argLen]))
			pos = dataEnd
		}

		if len(args) == 0 || !validRedisCommandName(args[0]) {
			return o.rejectClientBytes(forward, newObserverFatalDecision(observerErrorProtocol, "malformed Redis command"))
		}
		cmd := strings.ToUpper(args[0])
		auditCommand, auditable := redisAuditCommandChecked(args)
		if !isAllowedRedisPostAuthCommand(cmd) || !auditable {
			return o.rejectClientBytes(forward, newObserverFatalDecision(observerErrorProtocol, "Redis command is not supported after gateway authentication"))
		}
		if len(o.slots) >= maxObserverPendingQueries {
			return o.rejectClientBytes(forward, newObserverFatalDecision(observerErrorPendingLimit, "too many in-flight Redis commands"))
		}
		slot := redisResponseSlot{}
		if shouldRecordRedisCommand(cmd) && o.sink != nil {
			record, decision, ok := startObservedQuery(o.sink, auditCommand, map[string]any{
				"protocol": "redis",
				"command":  cmd,
			})
			if !ok {
				return o.rejectClientBytes(forward, auditSinkFailureDecision())
			}
			if !decision.Allowed {
				return o.rejectClientBytes(forward, &decision)
			}
			slot.record = record
			slot.recorded = true
		}
		o.slots = append(o.slots, slot)
		forward = append(forward, o.buf[:pos]...)
		o.buf = o.buf[pos:]
	}
}

func (o *redisObserver) rejectClientBytes(forward []byte, decision *queryDecision) ([]byte, *queryDecision) {
	o.buf = nil
	if len(o.slots) > 0 &&
		decision.ErrorCode != observerErrorPendingLimit &&
		decision.ErrorCode != observerErrorBufferLimit &&
		decision.ErrorCode != observerErrorAuditFailure {
		o.deferred = decision
		return forward, nil
	}
	if decision.ErrorCode == observerErrorAuditFailure {
		return forward, o.failDecision(decision)
	}
	return forward, o.failDecision(decision)
}

func (o *redisObserver) ObserveServerBytes(data []byte) *queryDecision {
	_, decision := o.observeServerRelayBytes(data)
	return decision
}

func (o *redisObserver) ObserveServerRelayBytes(data []byte) ([]byte, *queryDecision) {
	return o.observeServerRelayBytes(data)
}

func (o *redisObserver) observeServerRelayBytes(data []byte) ([]byte, *queryDecision) {
	if o.fatal != nil {
		return nil, o.fatal
	}
	if len(data) == 0 {
		return nil, nil
	}
	var forward []byte
	for {
		if o.serverStream != nil {
			chunk, consumed, complete, decision := o.consumeRedisResponseStream(data)
			forward = append(forward, chunk...)
			data = data[consumed:]
			if decision != nil {
				return forward, o.failDecision(decision)
			}
			if complete {
				if decision := o.completeRedisResponseStream(); decision != nil {
					return forward, decision
				}
				continue
			}
			if len(data) == 0 {
				return forward, nil
			}
			continue
		}
		if len(o.serverBuf) == 0 {
			if len(data) == 0 {
				return forward, nil
			}
			if !appendObserverBufferChunk(&o.serverBuf, &data, maxRedisObserverBufferBytes) {
				return forward, o.fail(observerErrorBufferLimit, "Redis observer response exceeds the audit limit")
			}
			continue
		}
		frameLen, status := redisRESPFrameLength(o.serverBuf)
		switch status {
		case redisRESPIncomplete:
			if len(data) == 0 {
				return forward, nil
			}
			if !appendObserverBufferChunk(&o.serverBuf, &data, maxRedisObserverBufferBytes) {
				chunk, decision := o.promoteRedisResponseStream()
				forward = append(forward, chunk...)
				if decision != nil {
					return forward, decision
				}
			}
			continue
		case redisRESPLimitExceeded:
			chunk, decision := o.promoteRedisResponseStream()
			forward = append(forward, chunk...)
			if decision != nil {
				return forward, decision
			}
			continue
		case redisRESPMalformed:
			return forward, o.fail(observerErrorProtocol, "malformed Redis response")
		}
		if len(o.slots) == 0 {
			return forward, o.fail(observerErrorProtocol, "unexpected Redis response without a matching command")
		}
		frame := o.serverBuf[:frameLen]
		o.consumeOrdinaryResponse(frame)
		if o.fatal != nil {
			return forward, o.fatal
		}
		forward = append(forward, frame...)
		o.serverBuf = o.serverBuf[frameLen:]
		if len(o.slots) == 0 && o.deferred != nil {
			decision := o.deferred
			o.deferred = nil
			return forward, o.failDecision(decision)
		}
	}
}

func (o *redisObserver) consumeOrdinaryResponse(frame []byte) {
	if len(o.slots) == 0 {
		return
	}
	slot := &o.slots[0]
	if !o.finishSlot(slot, frame) {
		slot.finishFailed = true
		o.failDecision(auditSinkFailureDecision())
		return
	}
	o.slots = o.slots[1:]
}

func (o *redisObserver) finishSlot(slot *redisResponseSlot, frame []byte) bool {
	return o.finishSlotWithResult(slot, redisResponseFinish(frame))
}

func (o *redisObserver) finishSlotWithResult(slot *redisResponseSlot, finish queryFinish) bool {
	if slot.recorded && o.sink != nil {
		return finishObservedQuery(o.sink, slot.record, finish)
	}
	return true
}

func (o *redisObserver) fail(code, message string) *queryDecision {
	return o.failDecision(newObserverFatalDecision(code, message))
}

func (o *redisObserver) Abort(code string) {
	decision := newObserverRelayDecision()
	if code != "" {
		decision.ErrorCode = code
	}
	o.failDecision(decision)
}

func (o *redisObserver) HasPending() bool {
	return len(o.slots) > 0
}

func (o *redisObserver) failDecision(decision *queryDecision) *queryDecision {
	if o.fatal == nil {
		o.fatal = decision
	}
	auditFailed := false
	for _, slot := range o.slots {
		if slot.recorded && !slot.finishFailed && o.sink != nil {
			if !finishObservedQuery(o.sink, slot.record, queryFinish{
				Status:       queryStatusError,
				ErrorCode:    o.fatal.ErrorCode,
				ErrorMessage: "redis observer terminated the command",
			}) {
				auditFailed = true
			}
		}
	}
	if auditFailed {
		o.fatal = auditSinkFailureDecision()
	}
	o.buf = nil
	o.serverBuf = nil
	o.serverStream = nil
	o.slots = nil
	o.deferred = nil
	return o.fatal
}

func (o *redisObserver) ErrorResponse(_ queryDecision) []byte {
	return []byte("-ERR database proxy rejected command\r\n")
}

func shouldRecordRedisCommand(cmd string) bool {
	switch cmd {
	case "AUTH", "PING", "QUIT", "ECHO", "SELECT", "HELLO", "COMMAND":
		return false
	default:
		return true
	}
}

func indexCRLF(b []byte) int {
	for i := 0; i < len(b)-1; i++ {
		if b[i] == '\r' && b[i+1] == '\n' {
			return i
		}
	}
	return -1
}
