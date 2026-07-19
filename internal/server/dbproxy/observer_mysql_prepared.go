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
	audit     databaseSQLAudit
	queryKind string
}

func newMySQLPreparedStatement(sql string, limit int) mysqlPreparedStatement {
	audit := prepareDatabaseSQLAudit(sql, limit)
	return mysqlPreparedStatement{
		audit:     audit,
		queryKind: classifyQueryKind(audit.text),
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
	if len(o.prepared) >= maxObserverPreparedObjects {
		return false
	}
	limit := normalizeMaxClientMessageBytes(o.maxClientMessageBytes)
	used := 0
	for _, prepared := range o.prepared {
		if len(prepared.audit.text) > limit-used {
			return false
		}
		used += len(prepared.audit.text)
	}
	if len(statement.audit.text) > limit-used {
		return false
	}
	if o.prepared == nil {
		o.prepared = make(map[uint32]mysqlPreparedStatement)
	}
	o.prepared[statementID] = statement
	return true
}
