package dbproxy

import "encoding/binary"

const (
	mysqlCommandStmtPrepare  = byte(0x16)
	mysqlCommandStmtExecute  = byte(0x17)
	mysqlCommandStmtLongData = byte(0x18)
	mysqlCommandStmtClose    = byte(0x19)
	mysqlCommandStmtReset    = byte(0x1a)
)

type mysqlPreparedStatement struct {
	sql       string
	queryKind string
}

func newMySQLPreparedStatement(sql string) mysqlPreparedStatement {
	redacted := redactDatabaseSQL(sql)
	return mysqlPreparedStatement{
		sql:       redacted,
		queryKind: classifyQueryKind(redacted),
	}
}

func (o *mysqlObserver) resolveMySQLStatementCommand(
	command byte,
	payload []byte,
) (mysqlPreparedStatement, *queryDecision) {
	switch command {
	case mysqlCommandStmtExecute,
		mysqlCommandStmtLongData,
		mysqlCommandStmtClose,
		mysqlCommandStmtReset:
		statementID := binary.LittleEndian.Uint32(payload[1:5])
		statement, exists := o.prepared[statementID]
		if !exists {
			return mysqlPreparedStatement{}, newObserverFatalDecision(
				observerErrorProtocol,
				"MySQL command references an unknown prepared statement",
			)
		}
		if command == mysqlCommandStmtClose {
			delete(o.prepared, statementID)
		}
		return statement, nil
	default:
		return mysqlPreparedStatement{}, nil
	}
}

func (o *mysqlObserver) setLastMySQLPendingPrepared(statement mysqlPreparedStatement) {
	if len(o.pendingPrepared) == 0 {
		return
	}
	o.pendingPrepared[len(o.pendingPrepared)-1] = statement
}

func (o *mysqlObserver) registerCompletedMySQLPrepare(
	statementID uint32,
	statement mysqlPreparedStatement,
) bool {
	if _, exists := o.prepared[statementID]; exists {
		return false
	}
	if o.prepared == nil {
		o.prepared = make(map[uint32]mysqlPreparedStatement)
	}
	o.prepared[statementID] = statement
	return true
}
