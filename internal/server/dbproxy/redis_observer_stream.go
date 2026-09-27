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
	bulkLength        int64
	bulkType          byte
	verbatimPrefix    [4]byte
	bulkTerminator    [2]byte
	bulkTerminatorLen int
	rootStarted       bool
	rootType          byte
	unsolicited       bool
	pubSubCapture     *redisStreamPubSubCapture
	finish            queryFinish
}

func newRedisResponseStream(primaryType byte) *redisResponseStream {
	stream := &redisResponseStream{
		stack:    []int64{1},
		rootType: primaryType,
		finish:   queryFinish{Status: queryStatusSuccess},
	}
	if primaryType == '-' || primaryType == '!' {
		stream.finish = queryFinish{
			Status:       queryStatusError,
			ErrorCode:    "REDIS_ERROR",
			ErrorMessage: "redis upstream error",
		}
	}
	return stream
}

func (o *redisObserver) promoteRedisResponseStream() ([]byte, *queryDecision) {
	primaryType := redisRESPPrimaryPrefixType(o.serverBuf)
	if primaryType == 0 {
		return nil, o.fail(observerErrorBufferLimit, "Redis response attributes exceed the protocol limit")
	}
	unsolicited := primaryType == '>' ||
		(o.protocolVersion != 3 &&
			o.subscriptions.active() &&
			primaryType == '*' &&
			isRedisRESP2PubSubMessagePrefix(o.serverBuf))
	potentialPubSubAck := len(o.slots) > 0 &&
		isRedisPubSubStateCommand(o.slots[0].command) &&
		(primaryType == '*' || primaryType == '>')
	if potentialPubSubAck && primaryType == '>' {
		unsolicited = false
	}
	if len(o.slots) == 0 && !unsolicited {
		return nil, o.fail(observerErrorProtocol, "unexpected Redis response without a matching command")
	}
	buffered := o.serverBuf
	o.serverBuf = nil
	o.serverStream = newRedisResponseStream(primaryType)
	o.serverStream.unsolicited = unsolicited
	if potentialPubSubAck && !unsolicited {
		o.serverStream.pubSubCapture = newRedisStreamPubSubCapture(
			primaryType,
			o.maxClientMessageBytes,
			&o.slots[0],
		)
	}
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
				if stream.rootType == line[0] && (line[0] == '-' || line[0] == '!') {
					stream.finish = redisResponseFinish(line)
				}
			}
			lineComplete, lineDecision := stream.consumeHeader(line)
			if lineDecision != nil {
				return forward, consumed, false, lineDecision
			}
			if captureDecision := stream.pubSubCapture.consumeHeader(line); captureDecision != nil {
				return forward, consumed, false, captureDecision
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
			if stream.bulkType == '=' {
				copied := stream.bulkLength - stream.bulkRemaining
				for index := consumed; index < end && copied < int64(len(stream.verbatimPrefix)); index++ {
					stream.verbatimPrefix[copied] = data[index]
					copied++
				}
			}
			if captureDecision := stream.pubSubCapture.consumeBulk(data[consumed:end]); captureDecision != nil {
				return forward, consumed, false, captureDecision
			}
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
			if stream.bulkType == '=' && stream.verbatimPrefix[3] != ':' {
				return forward, consumed, false, newObserverFatalDecision(observerErrorProtocol, "malformed Redis response")
			}
			stream.pubSubCapture.finishBulk()
			forward = append(forward, stream.bulkTerminator[:]...)
			stream.bulkTerminator = [2]byte{}
			stream.bulkTerminatorLen = 0
			stream.bulkLength = 0
			stream.bulkType = 0
			stream.verbatimPrefix = [4]byte{}
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
	case '_':
		if len(value) != 0 {
			return false, newObserverFatalDecision(observerErrorProtocol, "malformed Redis response")
		}
		return s.valueComplete(), nil
	case '#':
		if len(value) != 1 || (value[0] != 't' && value[0] != 'f') {
			return false, newObserverFatalDecision(observerErrorProtocol, "malformed Redis response")
		}
		return s.valueComplete(), nil
	case ',':
		if !validRedisRESPDouble(value) {
			return false, newObserverFatalDecision(observerErrorProtocol, "malformed Redis response")
		}
		return s.valueComplete(), nil
	case '(':
		if !validRedisRESPBigNumber(value) {
			return false, newObserverFatalDecision(observerErrorProtocol, "malformed Redis response")
		}
		return s.valueComplete(), nil
	case '$', '!', '=':
		var length int64
		var ok bool
		if line[0] == '$' {
			length, ok = parseCanonicalRESPNullable(value, maxRedisProtocolBulkBytes)
		} else {
			length, ok = parseCanonicalRESPUnsigned(value, maxRedisProtocolBulkBytes)
		}
		if !ok {
			return false, newObserverFatalDecision(observerErrorProtocol, "malformed Redis response")
		}
		if line[0] == '=' && length < 4 {
			return false, newObserverFatalDecision(observerErrorProtocol, "malformed Redis response")
		}
		if length == -1 {
			return s.valueComplete(), nil
		}
		s.bulkRemaining = length
		s.bulkLength = length
		s.bulkType = line[0]
		s.mode = redisResponseStreamBulk
		if length == 0 {
			s.mode = redisResponseStreamBulkTerminator
		}
		return false, nil
	case '*', '~', '>', '%', '|':
		count, ok := parseCanonicalRESPNullableNumber(value)
		if !ok {
			return false, newObserverFatalDecision(observerErrorProtocol, "malformed Redis response")
		}
		if count == -1 {
			if line[0] != '*' {
				return false, newObserverFatalDecision(observerErrorProtocol, "malformed Redis response")
			}
			return s.valueComplete(), nil
		}
		elementCount, ok := redisRESPAggregateElementCount(line[0], count)
		if !ok || elementCount > maxRedisObserverArrayElements {
			return false, newObserverFatalDecision(observerErrorBufferLimit, "Redis response aggregate exceeds the protocol limit")
		}
		if elementCount == 0 {
			return s.valueComplete(), nil
		}
		if len(s.stack) > maxRedisObserverNestingDepth {
			return false, newObserverFatalDecision(observerErrorBufferLimit, "Redis response nesting exceeds the protocol limit")
		}
		s.stack = append(s.stack, elementCount)
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
	if stream == nil {
		return o.fail(observerErrorProtocol, "unexpected Redis response without a matching command")
	}
	if event, ok := stream.pubSubCapture.capturedEvent(); ok {
		handled, decision := o.consumeRedisPubSubEvent(event)
		if decision != nil {
			return decision
		}
		if handled {
			if len(o.slots) == 0 && o.deferred != nil {
				deferred := o.deferred
				o.deferred = nil
				return o.failDecision(deferred)
			}
			return nil
		}
	}
	if stream.pubSubCapture != nil {
		if stream.rootType == '>' {
			o.protocolVersion = 3
			return nil
		}
		return o.fail(observerErrorProtocol, "malformed streamed Redis Pub/Sub acknowledgement")
	}
	if stream.unsolicited {
		if stream.rootType == '>' {
			o.protocolVersion = 3
		}
		return nil
	}
	if len(o.slots) == 0 {
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
