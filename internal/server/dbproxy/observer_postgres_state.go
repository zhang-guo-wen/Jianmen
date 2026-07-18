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

type postgresPortal struct {
	statement string
	sql       string
}

func (o *postgresObserver) observePostgresClientMessage(typ byte, payload []byte) *queryDecision {
	if o.sink == nil {
		return nil
	}
	switch typ {
	case 'Q':
		sql := redactDatabaseSQL(trimCString(payload))
		return o.startPostgresSimpleQuery(sql)
	case 'P':
		name, rest := splitCString(payload)
		sql, _ := splitCString(rest)
		o.ensurePostgresMaps()
		o.preparedStatements[name] = redactDatabaseSQL(sql)
	case 'B':
		portal, rest := splitCString(payload)
		statement, _ := splitCString(rest)
		o.ensurePostgresMaps()
		if _, exists := o.preparedStatements[statement]; !exists {
			return o.fail(observerErrorProtocol, "PostgreSQL Bind references an unknown statement")
		}
		o.portals[portal] = postgresPortal{
			statement: statement,
			sql:       o.preparedStatements[statement],
		}
	case 'E':
		portal, _ := splitCString(payload)
		return o.startPostgresPortalExecution(portal)
	case 'S':
		if o.openCycle != nil {
			o.cycles = append(o.cycles, o.openCycle)
			o.openCycle = nil
		}
	case 'C':
		o.closePostgresPreparedObject(payload)
	}
	return nil
}

func (o *postgresObserver) startPostgresSimpleQuery(sql string) *queryDecision {
	if len(o.pending) >= maxObserverPendingQueries {
		return o.fail(observerErrorPendingLimit, "too many in-flight PostgreSQL commands")
	}
	record, decision, ok := startObservedQuery(o.sink, sql, map[string]any{
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
	return nil
}

func (o *postgresObserver) startPostgresPortalExecution(portal string) *queryDecision {
	o.ensurePostgresMaps()
	binding, exists := o.portals[portal]
	if !exists {
		return o.fail(observerErrorProtocol, "PostgreSQL Execute references an unknown portal")
	}
	if len(o.pending) >= maxObserverPendingQueries {
		return o.fail(observerErrorPendingLimit, "too many in-flight PostgreSQL commands")
	}
	record, decision, ok := startObservedQuery(o.sink, binding.sql, map[string]any{
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
	return nil
}

func (o *postgresObserver) observePostgresServerMessage(typ byte, payload []byte) {
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
		o.preparedStatements = make(map[string]string)
	}
	if o.portals == nil {
		o.portals = make(map[string]postgresPortal)
	}
}

func (o *postgresObserver) closePostgresPreparedObject(payload []byte) {
	if len(payload) < 2 {
		return
	}
	name := trimCString(payload[1:])
	switch payload[0] {
	case 'S':
		delete(o.preparedStatements, name)
	case 'P':
		delete(o.portals, name)
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
