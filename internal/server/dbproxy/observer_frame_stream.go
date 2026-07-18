package dbproxy

const maxMySQLObservedPacketPrefixBytes = 64

type mysqlServerPacketStream struct {
	remaining int
	sequence  byte
	prefix    []byte
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
	if remainingPrefix := maxMySQLObservedPacketPrefixBytes - len(stream.prefix); remainingPrefix > 0 {
		prefixLength := len(chunk)
		if prefixLength > remainingPrefix {
			prefixLength = remainingPrefix
		}
		stream.prefix = append(stream.prefix, chunk[:prefixLength]...)
	}
	stream.remaining -= consume
	if stream.remaining > 0 {
		return chunk, consume, nil
	}

	o.serverStream = nil
	if decision := o.handleServerPacket(stream.prefix); decision != nil {
		return chunk, consume, decision
	}
	if o.fatal != nil {
		return chunk, consume, o.fatal
	}
	o.nextErrorSeq = stream.sequence + 1
	o.hasErrorSeq = true
	return chunk, consume, nil
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
