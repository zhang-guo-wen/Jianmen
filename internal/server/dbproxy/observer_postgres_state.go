package dbproxy

type postgresQueryEntry struct {
	record       queryRecord
	finish       queryFinish
	completed    bool
	finishFailed bool
	rows         int64
	hasRows      bool
	commandTags  []string
}

type postgresQueryCycle struct {
	simple  bool
	entries []*postgresQueryEntry
}

type postgresPreparedStatement struct {
	audit databaseSQLAudit
}

type postgresPortal struct {
	statement string
	prepared  *postgresPreparedStatement
}

func (o *postgresObserver) observePostgresClientMessage(typ byte, payload []byte) *queryDecision {
	if o.sink == nil {
		return nil
	}
	if decision := o.ensurePostgresOperationCapacity(typ); decision != nil {
		return decision
	}
	switch typ {
	case 'Q':
		sql, _ := splitCString(payload)
		audit := prepareDatabaseSQLAudit(
			sql,
			o.maxClientMessageBytes,
		)
		return o.startPostgresSimpleQuery(audit)
	case 'P':
		name, rest := splitCString(payload)
		sql, _ := splitCString(rest)
		if name != "" {
			if _, exists := o.projectedPostgresPreparedStatement(name); exists {
				return o.fail(
					observerErrorProtocol,
					"PostgreSQL named prepared statement already exists",
				)
			}
		}
		prepared := &postgresPreparedStatement{
			audit: prepareDatabaseSQLAudit(sql, o.maxClientMessageBytes),
		}
		operation := postgresFrontendOperation{
			messageType: 'P',
			name:        name,
			prepared:    prepared,
		}
		if !o.canQueuePostgresStateOperation(operation) {
			return o.fail(
				observerErrorPendingLimit,
				"PostgreSQL prepared statement audit state exceeds the configured limit",
			)
		}
		o.enqueuePostgresOperation(operation)
	case 'B':
		portal, rest := splitCString(payload)
		statement, _ := splitCString(rest)
		if portal != "" {
			if _, exists := o.projectedPostgresPortal(portal); exists {
				return o.fail(
					observerErrorProtocol,
					"PostgreSQL named portal already exists",
				)
			}
		}
		prepared, exists := o.projectedPostgresPreparedStatement(statement)
		if !exists {
			return o.fail(observerErrorProtocol, "PostgreSQL Bind references an unknown statement")
		}
		binding := postgresPortal{
			statement: statement,
			prepared:  prepared,
		}
		operation := postgresFrontendOperation{
			messageType: 'B',
			name:        portal,
			portal:      binding,
		}
		if !o.canQueuePostgresStateOperation(operation) {
			return o.fail(
				observerErrorPendingLimit,
				"PostgreSQL portal audit state exceeds the configured limit",
			)
		}
		o.enqueuePostgresOperation(operation)
	case 'E':
		portal, _ := splitCString(payload)
		return o.startPostgresPortalExecution(portal)
	case 'S':
		if o.openCycle != nil {
			o.cycles = append(o.cycles, o.openCycle)
			o.openCycle = nil
		}
		o.enqueuePostgresOperation(postgresFrontendOperation{messageType: 'S'})
	case 'C':
		name, _ := splitCString(payload[1:])
		operation := postgresFrontendOperation{
			messageType: 'C',
			target:      payload[0],
			name:        name,
		}
		if !o.canQueuePostgresStateOperation(operation) {
			return o.fail(
				observerErrorPendingLimit,
				"PostgreSQL prepared-state audit data exceeds the configured limit",
			)
		}
		o.enqueuePostgresOperation(operation)
	case 'D':
		o.enqueuePostgresOperation(postgresFrontendOperation{messageType: 'D'})
	}
	return nil
}

func (o *postgresObserver) startPostgresSimpleQuery(audit databaseSQLAudit) *queryDecision {
	if len(o.pending) >= maxObserverPendingQueries {
		return o.fail(observerErrorPendingLimit, "too many in-flight PostgreSQL commands")
	}
	if !observerPendingAuditWithinLimit(o.pending, audit, o.maxClientMessageBytes) {
		return o.fail(observerErrorPendingLimit, "pending PostgreSQL audit text exceeds the configured limit")
	}
	record, decision, ok := startPreparedObservedSQLQuery(o.sink, audit, map[string]any{
		"protocol": "postgres",
		"message":  "Query",
	})
	if !ok {
		return auditSinkFailureDecision()
	}
	if !decision.Allowed {
		return &decision
	}
	entry := &postgresQueryEntry{record: record}
	o.pending = append(o.pending, record)
	o.cycles = append(o.cycles, &postgresQueryCycle{simple: true, entries: []*postgresQueryEntry{entry}})
	o.enqueuePostgresOperation(postgresFrontendOperation{messageType: 'Q'})
	return nil
}

func (o *postgresObserver) startPostgresPortalExecution(portal string) *queryDecision {
	binding, exists := o.projectedPostgresPortal(portal)
	if !exists {
		return o.fail(observerErrorProtocol, "PostgreSQL Execute references an unknown portal")
	}
	if len(o.pending) >= maxObserverPendingQueries {
		return o.fail(observerErrorPendingLimit, "too many in-flight PostgreSQL commands")
	}
	if binding.prepared == nil {
		return o.fail(observerErrorProtocol, "PostgreSQL portal has no prepared statement")
	}
	if !observerPendingAuditWithinLimit(o.pending, binding.prepared.audit, o.maxClientMessageBytes) {
		return o.fail(observerErrorPendingLimit, "pending PostgreSQL audit text exceeds the configured limit")
	}
	record, decision, ok := startPreparedObservedSQLQuery(o.sink, binding.prepared.audit, map[string]any{
		"protocol": "postgres",
		"message":  "Execute",
	})
	if !ok {
		return auditSinkFailureDecision()
	}
	if !decision.Allowed {
		return &decision
	}
	if o.openCycle == nil {
		o.openCycle = &postgresQueryCycle{}
	}
	o.pending = append(o.pending, record)
	o.openCycle.entries = append(o.openCycle.entries, &postgresQueryEntry{record: record})
	o.enqueuePostgresOperation(postgresFrontendOperation{messageType: 'E'})
	return nil
}

func (o *postgresObserver) observePostgresServerMessage(typ byte, payload []byte) {
	if decision := o.observePostgresOperationResponse(typ, payload); decision != nil {
		o.failDecision(decision)
		return
	}
	cycle := o.activePostgresCycle()
	if cycle == nil || len(cycle.entries) == 0 || o.sink == nil {
		return
	}
	switch typ {
	case 'C':
		o.completePostgresCommand(cycle, trimCString(payload))
	case 's':
		o.completePostgresPortalSuspended(cycle)
	case 'I':
		o.completePostgresCommand(cycle, "EMPTY")
	case 'E':
		code, _ := parsePostgresError(payload)
		o.completePostgresError(cycle, sanitizedPostgresErrorCode(code))
	case 'Z':
		o.finishPostgresCycle(cycle)
	}
}

func (o *postgresObserver) completePostgresPortalSuspended(cycle *postgresQueryCycle) {
	if cycle.simple {
		return
	}
	entry := cycle.nextIncomplete()
	if entry == nil {
		return
	}
	entry.finish.Status = queryStatusSuccess
	entry.finish.Detail = mergeDetails(entry.finish.Detail, map[string]any{"portal_suspended": true})
	entry.completed = true
}

func (o *postgresObserver) completePostgresCommand(cycle *postgresQueryCycle, tag string) {
	entry := cycle.nextIncomplete()
	if entry == nil && cycle.simple {
		entry = cycle.entries[0]
	}
	if entry == nil {
		return
	}
	entry.finish.Status = queryStatusSuccess
	entry.commandTags = append(entry.commandTags, tag)
	if rows, ok := parsePostgresRowsFromCommandTag(tag); ok {
		if !entry.hasRows || rows <= maxInt64-entry.rows {
			entry.rows += rows
			entry.hasRows = true
		}
	}
	if !cycle.simple {
		entry.completed = true
	}
}

func (o *postgresObserver) completePostgresError(cycle *postgresQueryCycle, code string) {
	entry := cycle.nextIncomplete()
	if entry == nil && cycle.simple {
		entry = cycle.entries[0]
	}
	if entry == nil {
		return
	}
	entry.finish = queryFinish{
		Status:       queryStatusError,
		ErrorCode:    code,
		ErrorMessage: "postgres upstream error",
	}
	entry.completed = true
}

func (o *postgresObserver) finishPostgresCycle(cycle *postgresQueryCycle) {
	for _, entry := range cycle.entries {
		if entry.finish.Status == "" {
			entry.finish.Status = queryStatusUnknown
		}
		if entry.hasRows {
			rows := entry.rows
			entry.finish.RowsAffected = &rows
		}
		if len(entry.commandTags) > 0 {
			entry.finish.Detail = map[string]any{"command_tags": append([]string(nil), entry.commandTags...)}
		}
		if !finishObservedQuery(o.sink, entry.record, entry.finish) {
			entry.finishFailed = true
			o.failDecision(auditSinkFailureDecision())
			return
		}
		o.removePostgresPending(entry.record)
	}
	if len(o.cycles) > 0 && o.cycles[0] == cycle {
		o.cycles = o.cycles[1:]
	} else if o.openCycle == cycle {
		o.openCycle = nil
	}
}

func (o *postgresObserver) removePostgresPending(record queryRecord) {
	for index := range o.pending {
		if o.pending[index].seq != record.seq {
			continue
		}
		o.pending = append(o.pending[:index], o.pending[index+1:]...)
		return
	}
}

func (o *postgresObserver) postgresPendingFinishFailed(record queryRecord) bool {
	for _, cycle := range o.cycles {
		for _, entry := range cycle.entries {
			if entry.record.seq == record.seq && entry.finishFailed {
				return true
			}
		}
	}
	if o.openCycle != nil {
		for _, entry := range o.openCycle.entries {
			if entry.record.seq == record.seq && entry.finishFailed {
				return true
			}
		}
	}
	return false
}

func (o *postgresObserver) activePostgresCycle() *postgresQueryCycle {
	if len(o.cycles) > 0 {
		return o.cycles[0]
	}
	return o.openCycle
}

func (c *postgresQueryCycle) nextIncomplete() *postgresQueryEntry {
	for _, entry := range c.entries {
		if !entry.completed {
			return entry
		}
	}
	return nil
}

func (o *postgresObserver) ensurePostgresMaps() {
	if o.preparedStatements == nil {
		o.preparedStatements = make(map[string]*postgresPreparedStatement)
	}
	if o.portals == nil {
		o.portals = make(map[string]postgresPortal)
	}
}

func sanitizedPostgresErrorCode(code string) string {
	if len(code) != 5 {
		return "PG_ERROR"
	}
	for _, value := range []byte(code) {
		if (value < '0' || value > '9') && (value < 'A' || value > 'Z') {
			return "PG_ERROR"
		}
	}
	return code
}

const maxInt64 = int64(^uint64(0) >> 1)
