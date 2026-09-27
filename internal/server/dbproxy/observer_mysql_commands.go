package dbproxy

func (o *mysqlObserver) enqueueMySQLResponse(record queryRecord, recorded bool, command byte) {
	o.pending = append(o.pending, record)
	o.pendingRecorded = append(o.pendingRecorded, recorded)
	o.pendingFailed = append(o.pendingFailed, false)
	o.pendingCommands = append(o.pendingCommands, command)
	o.pendingPrepared = append(o.pendingPrepared, mysqlPreparedStatement{})
}

func (o *mysqlObserver) mysqlPendingIsRecorded(index int) bool {
	if index < len(o.pendingRecorded) {
		return o.pendingRecorded[index]
	}
	return index < len(o.pending) && o.pending[index].seq != 0
}

func (o *mysqlObserver) mysqlPendingFinishFailed(index int) bool {
	return index < len(o.pendingFailed) && o.pendingFailed[index]
}

func (o *mysqlObserver) markMySQLPendingFinishFailed(index int) {
	for len(o.pendingFailed) < len(o.pending) {
		o.pendingFailed = append(o.pendingFailed, false)
	}
	if index < len(o.pendingFailed) {
		o.pendingFailed[index] = true
	}
}

func validMySQLClientCommand(command byte, payload []byte) bool {
	switch command {
	case 0x01, 0x0e: // COM_QUIT, COM_PING
		return len(payload) == 1
	case 0x02: // COM_INIT_DB
		return len(payload) > 1
	case 0x03: // COM_QUERY
		return len(payload) >= 1
	case 0x16: // COM_STMT_PREPARE
		return len(payload) > 1
	case 0x17: // COM_STMT_EXECUTE
		return len(payload) >= 10
	case 0x18: // COM_STMT_SEND_LONG_DATA
		return len(payload) >= 7
	case 0x19, 0x1a: // COM_STMT_CLOSE, COM_STMT_RESET
		return len(payload) == 5
	default:
		return false
	}
}

func mysqlCommandExpectsResponse(command byte) bool {
	switch command {
	case 0x02, 0x03, 0x0e, 0x16, 0x17, 0x1a:
		return true
	default:
		return false
	}
}

func (o *mysqlObserver) acceptMySQLServerSequence(sequence byte) bool {
	if len(o.pending) == 0 {
		return false
	}
	expected := byte(1)
	if o.serverSeqActive {
		expected = o.expectedServerSeq
	}
	if sequence != expected {
		return false
	}
	o.serverSeqActive = true
	o.expectedServerSeq = sequence + 1
	return true
}

func (o *mysqlObserver) resetMySQLServerSequence() {
	o.serverSeqActive = false
	o.expectedServerSeq = 0
}
