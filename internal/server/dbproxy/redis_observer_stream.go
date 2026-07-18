package dbproxy

import (
	"strconv"
)

const maxRedisProtocolBulkBytes = 512 * 1024 * 1024

type redisResponseStreamMode uint8

const (
	redisResponseStreamHeader redisResponseStreamMode = iota
	redisResponseStreamBulk
	redisResponseStreamBulkTerminator
)

type redisResponseStream struct {
	mode              redisResponseStreamMode
	stack             []int64
	line              []byte
	bulkRemaining     int64
	bulkTerminator    [2]byte
	bulkTerminatorLen int
	rootStarted       bool
	finish            queryFinish
}

func newRedisResponseStream() *redisResponseStream {
	return &redisResponseStream{
		stack:  []int64{1},
		finish: queryFinish{Status: queryStatusSuccess},
	}
}

func (o *redisObserver) promoteRedisResponseStream() ([]byte, *queryDecision) {
	if len(o.slots) == 0 {
		return nil, o.fail(observerErrorProtocol, "unexpected Redis response without a matching command")
	}
	buffered := o.serverBuf
	o.serverBuf = nil
	o.serverStream = newRedisResponseStream()
	forward, consumed, complete, decision := o.consumeRedisResponseStream(buffered)
	if consumed < len(buffered) {
		o.serverBuf = append(o.serverBuf, buffered[consumed:]...)
	}
	if decision != nil {
		return forward, o.failDecision(decision)
	}
	if complete {
		return forward, o.completeRedisResponseStream()
	}
	return forward, nil
}

func (o *redisObserver) consumeRedisResponseStream(data []byte) (
	forward []byte,
	consumed int,
	complete bool,
	decision *queryDecision,
) {
	stream := o.serverStream
	if stream == nil {
		return nil, 0, false, nil
	}

	for consumed < len(data) {
		switch stream.mode {
		case redisResponseStreamHeader:
			value := data[consumed]
			consumed++
			if len(stream.line) > 0 && stream.line[len(stream.line)-1] == '\r' && value != '\n' {
				return forward, consumed, false, newObserverFatalDecision(observerErrorProtocol, "malformed Redis response")
			}
			stream.line = append(stream.line, value)
			if len(stream.line) > maxRedisObserverLineBytes+2 {
				return forward, consumed, false, newObserverFatalDecision(observerErrorBufferLimit, "Redis response line exceeds the protocol limit")
			}
			if value != '\n' {
				continue
			}
			if len(stream.line) < 3 || stream.line[len(stream.line)-2] != '\r' {
				return forward, consumed, false, newObserverFatalDecision(observerErrorProtocol, "malformed Redis response")
			}
			line := stream.line
			stream.line = nil
			if !stream.rootStarted {
				stream.rootStarted = true
				if line[0] == '-' {
					stream.finish = redisResponseFinish(line)
				}
			}
			lineComplete, lineDecision := stream.consumeHeader(line)
			if lineDecision != nil {
				return forward, consumed, false, lineDecision
			}
			forward = append(forward, line...)
			if lineComplete {
				return forward, consumed, true, nil
			}
		case redisResponseStreamBulk:
			length := int64(len(data) - consumed)
			if length > stream.bulkRemaining {
				length = stream.bulkRemaining
			}
			end := consumed + int(length)
			forward = append(forward, data[consumed:end]...)
			consumed = end
			stream.bulkRemaining -= length
			if stream.bulkRemaining == 0 {
				stream.mode = redisResponseStreamBulkTerminator
			}
		case redisResponseStreamBulkTerminator:
			for consumed < len(data) && stream.bulkTerminatorLen < len(stream.bulkTerminator) {
				stream.bulkTerminator[stream.bulkTerminatorLen] = data[consumed]
				stream.bulkTerminatorLen++
				consumed++
			}
			if stream.bulkTerminatorLen < len(stream.bulkTerminator) {
				continue
			}
			if stream.bulkTerminator != [2]byte{'\r', '\n'} {
				return forward, consumed, false, newObserverFatalDecision(observerErrorProtocol, "malformed Redis response")
			}
			forward = append(forward, stream.bulkTerminator[:]...)
			stream.bulkTerminator = [2]byte{}
			stream.bulkTerminatorLen = 0
			stream.mode = redisResponseStreamHeader
			if stream.valueComplete() {
				return forward, consumed, true, nil
			}
		}
	}
	return forward, consumed, false, nil
}

func (s *redisResponseStream) consumeHeader(line []byte) (bool, *queryDecision) {
	if len(line) < 3 {
		return false, newObserverFatalDecision(observerErrorProtocol, "malformed Redis response")
	}
	value := line[1 : len(line)-2]
	switch line[0] {
	case '+', '-':
		return s.valueComplete(), nil
	case ':':
		if _, err := strconv.ParseInt(string(value), 10, 64); err != nil {
			return false, newObserverFatalDecision(observerErrorProtocol, "malformed Redis response")
		}
		return s.valueComplete(), nil
	case '$':
		length, ok := parseCanonicalRESPNullable(value, maxRedisProtocolBulkBytes)
		if !ok {
			return false, newObserverFatalDecision(observerErrorProtocol, "malformed Redis response")
		}
		if length == -1 {
			return s.valueComplete(), nil
		}
		s.bulkRemaining = length
		s.mode = redisResponseStreamBulk
		if length == 0 {
			s.mode = redisResponseStreamBulkTerminator
		}
		return false, nil
	case '*':
		count, ok := parseCanonicalRESPNullableNumber(value)
		if !ok {
			return false, newObserverFatalDecision(observerErrorProtocol, "malformed Redis response")
		}
		if count <= 0 {
			return s.valueComplete(), nil
		}
		if len(s.stack) > maxRedisObserverNestingDepth {
			return false, newObserverFatalDecision(observerErrorBufferLimit, "Redis response nesting exceeds the protocol limit")
		}
		s.stack = append(s.stack, count)
		return false, nil
	default:
		return false, newObserverFatalDecision(observerErrorProtocol, "malformed Redis response")
	}
}

func (s *redisResponseStream) valueComplete() bool {
	for len(s.stack) > 0 {
		index := len(s.stack) - 1
		s.stack[index]--
		if s.stack[index] > 0 {
			return false
		}
		s.stack = s.stack[:index]
	}
	return true
}

func (o *redisObserver) completeRedisResponseStream() *queryDecision {
	stream := o.serverStream
	o.serverStream = nil
	if stream == nil || len(o.slots) == 0 {
		return o.fail(observerErrorProtocol, "unexpected Redis response without a matching command")
	}
	slot := &o.slots[0]
	if !o.finishSlotWithResult(slot, stream.finish) {
		slot.finishFailed = true
		return o.failDecision(auditSinkFailureDecision())
	}
	o.slots = o.slots[1:]
	if len(o.slots) == 0 && o.deferred != nil {
		decision := o.deferred
		o.deferred = nil
		return o.failDecision(decision)
	}
	return nil
}
