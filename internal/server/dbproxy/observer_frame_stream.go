package dbproxy

const (
	maxMySQLObservedPacketPrefixBytes  = 64
	maxMySQLPhysicalPacketPayloadBytes = 0xFFFFFF
)

type mysqlServerLogicalPacket struct {
	prefix []byte
}

type mysqlServerPacketStream struct {
	remaining     int
	sequence      byte
	collectPrefix bool
	finalFragment bool
}

func (o *mysqlObserver) consumeMySQLServerPacketStream(data []byte) ([]byte, int, *queryDecision) {
	stream := o.serverStream
	if stream == nil || len(data) == 0 {
		return nil, 0, nil
	}
	consume := stream.remaining
	if consume > len(data) {
		consume = len(data)
	}
	chunk := data[:consume]
	logical := o.serverLogical
	if stream.collectPrefix && logical != nil {
		remainingPrefix := maxMySQLObservedPacketPrefixBytes - len(logical.prefix)
		prefixLength := len(chunk)
		if prefixLength > remainingPrefix {
			prefixLength = remainingPrefix
		}
		logical.prefix = append(logical.prefix, chunk[:prefixLength]...)
	}
	stream.remaining -= consume
	if stream.remaining > 0 {
		return chunk, consume, nil
	}

	o.serverStream = nil
	if stream.finalFragment {
		if decision := o.finishMySQLServerLogicalPacket(stream.sequence); decision != nil {
			return chunk, consume, decision
		}
		if o.fatal != nil {
			return chunk, consume, o.fatal
		}
	} else {
		o.nextErrorSeq = stream.sequence + 1
		o.hasErrorSeq = true
	}
	return chunk, consume, nil
}

func (o *mysqlObserver) finishMySQLServerLogicalPacket(sequence byte) *queryDecision {
	logical := o.serverLogical
	if logical == nil {
		return o.fail(observerErrorProtocol, "malformed MySQL logical packet")
	}
	o.serverLogical = nil
	if decision := o.handleServerPacket(logical.prefix); decision != nil {
		return decision
	}
	if o.fatal != nil {
		return o.fatal
	}
	o.nextErrorSeq = sequence + 1
	o.hasErrorSeq = true
	return nil
}

type postgresFrameStream struct {
	remaining int
}

func consumePostgresFrameStream(stream **postgresFrameStream, data []byte) ([]byte, int) {
	if *stream == nil || len(data) == 0 {
		return nil, 0
	}
	consume := (*stream).remaining
	if consume > len(data) {
		consume = len(data)
	}
	(*stream).remaining -= consume
	if (*stream).remaining == 0 {
		*stream = nil
	}
	return data[:consume], consume
}

func canStreamPostgresFrontendFrame(messageType byte) bool {
	return messageType == 'd'
}

func canStreamPostgresBackendFrame(messageType byte) bool {
	return messageType == 'D' || messageType == 'd'
}
