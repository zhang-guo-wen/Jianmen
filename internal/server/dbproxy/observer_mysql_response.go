package dbproxy

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	mysqlServerMoreResultsExists = 0x0008
	maxMySQLObservedColumns      = 4096
)

type mysqlResponsePhase uint8

const (
	mysqlResponseInitial mysqlResponsePhase = iota
	mysqlResponseColumns
	mysqlResponseColumnTerminator
	mysqlResponseRows
	mysqlResponsePrepareParams
	mysqlResponsePrepareParamTerminator
	mysqlResponsePrepareColumns
	mysqlResponsePrepareColumnTerminator
	mysqlResponseLocalInfile
)

type mysqlResponseState struct {
	command             byte
	phase               mysqlResponsePhase
	remaining           int
	columns             int
	preparedStatementID uint32
	hasPreparedID       bool
}

func (o *mysqlObserver) observeMySQLResponsePacket(payload []byte) {
	if len(payload) == 0 || len(o.pending) == 0 {
		return
	}
	if o.response == nil {
		command := byte(0x03)
		if len(o.pendingCommands) > 0 {
			command = o.pendingCommands[0]
		}
		o.response = &mysqlResponseState{command: command}
	}
	state := o.response
	if payload[0] == 0xff {
		o.finishMySQLPending(mysqlErrorFinish(payload))
		return
	}

	if state.command == 0x16 {
		o.observeMySQLPrepareResponse(state, payload)
		return
	}
	o.observeMySQLCommandResponse(state, payload)
}

func (o *mysqlObserver) observeMySQLCommandResponse(state *mysqlResponseState, payload []byte) {
	switch state.phase {
	case mysqlResponseInitial:
		switch payload[0] {
		case 0x00:
			finish, status := mysqlOKFinish(payload)
			if status&mysqlServerMoreResultsExists != 0 {
				state.phase = mysqlResponseInitial
				return
			}
			o.finishMySQLPending(finish)
		case 0xfb:
			state.phase = mysqlResponseLocalInfile
		default:
			columnCount, _, ok := readLengthEncodedInt(payload)
			if !ok || columnCount == 0 || columnCount > maxMySQLObservedColumns {
				o.fail(observerErrorProtocol, "malformed MySQL result set")
				return
			}
			state.phase = mysqlResponseColumns
			state.remaining = int(columnCount)
		}
	case mysqlResponseColumns:
		state.remaining--
		if state.remaining == 0 {
			state.phase = mysqlResponseColumnTerminator
		}
	case mysqlResponseColumnTerminator:
		if mysqlIsResultTerminator(payload) {
			state.phase = mysqlResponseRows
			return
		}
		state.phase = mysqlResponseRows
	case mysqlResponseRows:
		if !mysqlIsResultTerminator(payload) {
			return
		}
		status := mysqlTerminatorStatus(payload)
		if status&mysqlServerMoreResultsExists != 0 {
			state.phase = mysqlResponseInitial
			return
		}
		o.finishMySQLPending(queryFinish{Status: queryStatusSuccess})
	case mysqlResponseLocalInfile:
		if payload[0] == 0x00 {
			finish, _ := mysqlOKFinish(payload)
			o.finishMySQLPending(finish)
		}
	}
}

func (o *mysqlObserver) observeMySQLPrepareResponse(state *mysqlResponseState, payload []byte) {
	switch state.phase {
	case mysqlResponseInitial:
		if payload[0] != 0x00 || len(payload) < 12 {
			o.fail(observerErrorProtocol, "malformed MySQL prepare response")
			return
		}
		statementID := binary.LittleEndian.Uint32(payload[1:5])
		if _, duplicate := o.prepared[statementID]; duplicate {
			o.fail(observerErrorProtocol, "duplicate MySQL prepared statement identifier")
			return
		}
		state.preparedStatementID = statementID
		state.hasPreparedID = true
		state.columns = int(binary.LittleEndian.Uint16(payload[5:7]))
		state.remaining = int(binary.LittleEndian.Uint16(payload[7:9]))
		switch {
		case state.remaining > 0:
			state.phase = mysqlResponsePrepareParams
		case state.columns > 0:
			state.remaining = state.columns
			state.phase = mysqlResponsePrepareColumns
		default:
			o.finishMySQLPending(queryFinish{Status: queryStatusSuccess})
		}
	case mysqlResponsePrepareParams:
		state.remaining--
		if state.remaining == 0 {
			state.phase = mysqlResponsePrepareParamTerminator
		}
	case mysqlResponsePrepareParamTerminator:
		if mysqlIsResultTerminator(payload) {
			if state.columns == 0 {
				o.finishMySQLPending(queryFinish{Status: queryStatusSuccess})
				return
			}
			state.remaining = state.columns
			state.phase = mysqlResponsePrepareColumns
			return
		}
		if state.columns == 0 {
			o.fail(observerErrorProtocol, "malformed MySQL prepare metadata")
			return
		}
		state.remaining = state.columns - 1
		if state.remaining == 0 {
			state.phase = mysqlResponsePrepareColumnTerminator
		} else {
			state.phase = mysqlResponsePrepareColumns
		}
	case mysqlResponsePrepareColumns:
		state.remaining--
		if state.remaining == 0 {
			state.phase = mysqlResponsePrepareColumnTerminator
		}
	case mysqlResponsePrepareColumnTerminator:
		if !mysqlIsResultTerminator(payload) {
			o.fail(observerErrorProtocol, "malformed MySQL prepare metadata")
			return
		}
		o.finishMySQLPending(queryFinish{Status: queryStatusSuccess})
	}
}

func (o *mysqlObserver) finishMySQLPrepareWithOmittedTerminator() {
	if o.response == nil || o.response.command != 0x16 {
		return
	}
	switch o.response.phase {
	case mysqlResponsePrepareColumnTerminator:
		o.finishMySQLPending(queryFinish{Status: queryStatusSuccess})
	case mysqlResponsePrepareParamTerminator:
		if o.response.columns == 0 {
			o.finishMySQLPending(queryFinish{Status: queryStatusSuccess})
		}
	}
}

func (o *mysqlObserver) finishMySQLPending(finish queryFinish) {
	record := o.pending[0]
	recorded := o.mysqlPendingIsRecorded(0)
	if len(o.pendingCommands) > 0 &&
		o.pendingCommands[0] == mysqlCommandStmtPrepare &&
		finish.Status == queryStatusSuccess {
		if o.response == nil || !o.response.hasPreparedID || len(o.pendingPrepared) == 0 ||
			!o.registerCompletedMySQLPrepare(o.response.preparedStatementID, o.pendingPrepared[0]) {
			o.fail(observerErrorProtocol, "invalid MySQL prepared statement lifecycle")
			return
		}
	}
	if recorded && o.sink != nil && !finishObservedQuery(o.sink, record, finish) {
		o.markMySQLPendingFinishFailed(0)
		o.failDecision(auditSinkFailureDecision())
		return
	}
	o.pending = o.pending[1:]
	if len(o.pendingRecorded) > 0 {
		o.pendingRecorded = o.pendingRecorded[1:]
	}
	if len(o.pendingFailed) > 0 {
		o.pendingFailed = o.pendingFailed[1:]
	}
	if len(o.pendingCommands) > 0 {
		o.pendingCommands = o.pendingCommands[1:]
	}
	if len(o.pendingPrepared) > 0 {
		o.pendingPrepared = o.pendingPrepared[1:]
	}
	o.response = nil
	o.resetMySQLServerSequence()
}

func mysqlErrorFinish(payload []byte) queryFinish {
	finish := queryFinish{Status: queryStatusError}
	if len(payload) >= 3 {
		finish.ErrorCode = fmt.Sprintf("%d", binary.LittleEndian.Uint16(payload[1:3]))
	}
	finish.ErrorMessage = "mysql upstream error"
	return finish
}

func mysqlOKFinish(payload []byte) (queryFinish, uint16) {
	finish := queryFinish{Status: queryStatusSuccess}
	affected, offset, ok := readLengthEncodedInt(payload[1:])
	if !ok {
		return finish, 0
	}
	if affected <= math.MaxInt64 {
		value := int64(affected)
		finish.RowsAffected = &value
	}
	_, insertOffset, ok := readLengthEncodedInt(payload[1+offset:])
	if !ok {
		return finish, 0
	}
	statusOffset := 1 + offset + insertOffset
	if len(payload) < statusOffset+2 {
		return finish, 0
	}
	return finish, binary.LittleEndian.Uint16(payload[statusOffset : statusOffset+2])
}

func mysqlIsResultTerminator(payload []byte) bool {
	return len(payload) >= 5 && len(payload) < 9 && payload[0] == 0xfe
}

func mysqlTerminatorStatus(payload []byte) uint16 {
	if len(payload) >= 5 && payload[0] == 0xfe {
		return binary.LittleEndian.Uint16(payload[3:5])
	}
	return 0
}
