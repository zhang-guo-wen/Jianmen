package dbproxy

import "log/slog"

func startObservedQuery(sink querySink, sql string, detail map[string]any) (
	record queryRecord,
	decision queryDecision,
	ok bool,
) {
	if sink == nil {
		return queryRecord{}, allowQuery(), true
	}
	defer func() {
		if recover() != nil {
			slog.Error("database proxy audit sink failed during query start")
			record = queryRecord{}
			decision = queryDecision{}
			ok = false
		}
	}()
	record, decision = sink.StartQuery(sql, detail)
	return record, decision, true
}

func startPreparedObservedSQLQuery(
	sink querySink,
	audit databaseSQLAudit,
	detail map[string]any,
) (queryRecord, queryDecision, bool) {
	return startObservedQuery(sink, audit.text, audit.withDetail(detail))
}

func finishObservedQuery(sink querySink, record queryRecord, finish queryFinish) (ok bool) {
	if sink == nil {
		return true
	}
	defer func() {
		if recover() != nil {
			slog.Error("database proxy audit sink failed during query finish")
			ok = false
		}
	}()
	sink.FinishQuery(record, finish)
	return true
}

func auditSinkFailureDecision() *queryDecision {
	return &queryDecision{
		Allowed:      false,
		Status:       queryStatusError,
		ErrorCode:    observerErrorAuditFailure,
		ErrorMessage: "database proxy audit persistence failed",
	}
}
