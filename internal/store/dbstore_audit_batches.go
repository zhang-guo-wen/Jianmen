package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

var _ service.AuditBatchQueryRepository = (*DBStore)(nil)

func auditBatchStates(includeStarted bool) []string {
	if includeStarted {
		return []string{"ended", "started"}
	}
	return []string{"ended"}
}

func auditBatchIsSSH(protocols string) bool { return protocols == "ssh,sftp" }

func auditBatchScope(db *gorm.DB, filter service.AuditBatchFilter) *gorm.DB {
	q := db.Where("protocol IN ?", splitCSV(filter.Protocol)).Where("state IN ?", auditBatchStates(filter.IncludeStarted))
	if filter.BatchID != "" {
		return q.Where("batch_id = ?", filter.BatchID)
	}
	return q.Where("batch_id IS NOT NULL AND batch_id <> ''")
}

func auditBatchLike(search string) string {
	return "%" + escapeAuditDBQueryLikePattern(strings.ToLower(strings.TrimSpace(search))) + "%"
}

// 列表只在有权读取的同协议会话内匹配正文，命中后汇总仍包含全部可见成员。
func applyAuditBatchSearch(q *gorm.DB, search string, ssh bool, alias string) *gorm.DB {
	like := auditBatchLike(search)
	preds := make([]string, 0, 9)
	args := make([]any, 0, 11)
	for _, column := range []string{"batch_id", "username", "target_name", "target_address", "account_name", "account_username", "client_ip"} {
		preds = append(preds, "LOWER("+alias+"."+column+") LIKE ? ESCAPE '!'")
		args = append(args, like)
	}
	if ssh {
		preds = append(preds, "EXISTS (SELECT 1 FROM audit_ssh_commands AS c WHERE c.audit_session_id = "+alias+".id AND LOWER(c.command) LIKE ? ESCAPE '!')",
			"EXISTS (SELECT 1 FROM audit_sftp_events AS f WHERE f.audit_session_id = "+alias+".id AND (LOWER(f.path) LIKE ? ESCAPE '!' OR LOWER(f.action) LIKE ? ESCAPE '!'))")
		args = append(args, like, like, like)
	} else {
		preds = append(preds, "EXISTS (SELECT 1 FROM audit_db_queries AS d WHERE d.audit_session_id = "+alias+".id AND LOWER(SUBSTR(d.sql_text, 1, ?)) LIKE ? ESCAPE '!')")
		args = append(args, auditDBQueryPreviewMaxCharacters, like)
	}
	return q.Where("("+strings.Join(preds, " OR ")+")", args...)
}

func auditBatchPage(page, size int) (limit, offset int) {
	if size <= 0 {
		size = service.AuditDBQueryDefaultPageSize
	}
	size = min(size, service.AuditDBQueryMaxPageSize)
	page = min(max(1, page), int(^uint(0)>>1)/size)
	return size, (page - 1) * size
}

type auditBatchSummary struct {
	BatchID        string
	SessionCount   int64
	FailureCount   int64
	StartedCount   int64
	SucceededCount int64
	LastEnded      string
}

type batchLogTable struct{ table, kind string }

func auditBatchLogTables(protocols string) []batchLogTable {
	if auditBatchIsSSH(protocols) {
		return []batchLogTable{{"audit_ssh_commands", "command"}, {"audit_sftp_events", "file"}}
	}
	return []batchLogTable{{"audit_db_queries", "query"}}
}

func auditBatchSummariesQuery(q *gorm.DB, limit, offset int) *gorm.DB {
	castType := "TEXT"
	if q.Dialector.Name() == "mysql" {
		castType = "CHAR"
	}
	return q.Select(`batch_id, COUNT(*) AS session_count,
		SUM(CASE WHEN outcome IN ('failed','denied','terminated') THEN 1 ELSE 0 END) AS failure_count,
		SUM(CASE WHEN state = 'started' THEN 1 ELSE 0 END) AS started_count,
		SUM(CASE WHEN state = 'ended' AND outcome = 'succeeded' THEN 1 ELSE 0 END) AS succeeded_count,
		CAST(MAX(ended_at) AS ` + castType + `) AS last_ended`).Group("batch_id").
		Order("MIN(started_at) DESC, batch_id DESC").Limit(limit).Offset(offset)
}

func (s *DBStore) ListAuditBatches(ctx context.Context, filter service.AuditBatchFilter, params service.AuditSessionListParams) ([]service.AuditBatchListItem, int64, error) {
	if ctx == nil {
		return nil, 0, errors.New("list audit batches: nil context")
	}
	db := s.db.WithContext(ctx)
	match := auditBatchScope(db.Table("audit_sessions"), filter)
	if date, err := time.Parse("2006-01-02", params.Date); err == nil {
		match = match.Where("started_at >= ? AND started_at < ?", date, date.AddDate(0, 0, 1))
	}
	if params.Search != "" {
		match = applyAuditBatchSearch(match, params.Search, auditBatchIsSSH(filter.Protocol), "audit_sessions")
	}
	q := auditBatchScope(db.Table("audit_sessions"), filter)
	if params.Search != "" || params.Date != "" {
		q = q.Where("batch_id IN (?)", match.Select("batch_id"))
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Distinct("batch_id").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]service.AuditBatchListItem, 0)
	if total == 0 {
		return items, 0, nil
	}
	limit, offset := auditBatchPage(params.Page, params.Size)
	var summaries []auditBatchSummary
	if err := auditBatchSummariesQuery(q, limit, offset).Scan(&summaries).Error; err != nil {
		return nil, 0, err
	}
	if len(summaries) == 0 {
		return items, total, nil
	}
	batchIDs := make([]string, 0, len(summaries))
	for _, item := range summaries {
		batchIDs = append(batchIDs, item.BatchID)
	}
	// 先分页，再按索引取每批首条，避免给整个历史按批执行窗口排序。
	firstID := auditBatchScope(db.Table("audit_sessions"), filter).
		Where("audit_sessions.batch_id = batches.batch_id").Select("id").Order("started_at ASC, id ASC").Limit(1)
	pageIDs := auditBatchScope(db.Table("audit_sessions"), filter).Where("batch_id IN ?", batchIDs).Select("DISTINCT batch_id")
	var sessions []model.AuditSession
	if err := db.Table("(?) AS batches", pageIDs).Joins("JOIN audit_sessions ON audit_sessions.id = (?)", firstID).
		Select("audit_sessions.*").Scan(&sessions).Error; err != nil {
		return nil, 0, err
	}
	byBatch := make(map[string]model.AuditSession, len(sessions))
	for _, row := range sessions {
		byBatch[row.BatchID] = row
	}
	counts := map[string]int64{}
	for _, entry := range auditBatchLogTables(filter.Protocol) {
		scope := auditBatchScope(db.Table("audit_sessions"), filter).Where("batch_id IN ?", batchIDs).Select("id, batch_id")
		var values []struct {
			BatchID string
			Count   int64
		}
		if err := db.Table(entry.table+" AS logs").Joins("JOIN (?) AS scoped ON scoped.id = logs.audit_session_id", scope).Select("scoped.batch_id, COUNT(*) AS count").Group("scoped.batch_id").Scan(&values).Error; err != nil {
			return nil, 0, err
		}
		for _, value := range values {
			counts[value.BatchID] += value.Count
		}
	}
	for _, summary := range summaries {
		first, ok := byBatch[summary.BatchID]
		if !ok {
			continue
		}
		state, outcome := "ended", ""
		endedAt := summary.LastEnded
		if summary.FailureCount > 0 {
			outcome = model.AuditOutcomeFailed
		}
		if summary.StartedCount > 0 {
			state, endedAt = "started", ""
			if outcome == "" {
				outcome = model.AuditOutcomeActive
			}
		}
		if summary.SucceededCount == summary.SessionCount {
			outcome = model.AuditOutcomeSucceeded
		}
		items = append(items, service.AuditBatchListItem{BatchID: summary.BatchID, SessionCount: summary.SessionCount, FailureCount: summary.FailureCount, AuditSessionListItem: service.AuditSessionListItem{
			ID: summary.BatchID, UserID: first.UserID, Username: first.Username, Protocol: first.Protocol, ProtocolSubtype: first.ProtocolSubtype, ResourceType: first.ResourceType, ResourceID: first.ResourceID, HostID: first.HostID, AccountID: first.AccountID, TargetName: first.TargetName, TargetAddress: first.TargetAddress, AccountName: first.AccountName, AccountUsername: first.AccountUsername, ClientIP: first.ClientIP, StartedAt: first.StartedAt.Format(time.RFC3339Nano), EndedAt: formatAuditBatchDBTime(endedAt), State: state, Outcome: outcome, LogCount: counts[summary.BatchID],
		}})
	}
	return items, total, nil
}

func formatAuditBatchDBTime(s string) string {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999-07", "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05.999999999"} {
		if parsed, err := time.Parse(layout, s); err == nil {
			return parsed.UTC().Format(time.RFC3339Nano)
		}
	}
	return ""
}

func (s *DBStore) ListAuditBatchLogs(ctx context.Context, filter service.AuditBatchFilter, params service.AuditDBQueryPreviewParams) ([]service.AuditBatchLogItem, int64, error) {
	if ctx == nil {
		return nil, 0, errors.New("list audit batch logs: nil context")
	}
	if filter.BatchID == "" {
		return nil, 0, gorm.ErrRecordNotFound
	}
	db := s.db.WithContext(ctx)
	var exists model.AuditSession
	if err := auditBatchScope(db.Model(&model.AuditSession{}), filter).Select("id").Take(&exists).Error; err != nil {
		return nil, 0, err
	}
	limit := params.Limit
	if limit <= 0 {
		limit = service.AuditDBQueryDefaultPageSize
	}
	limit = min(limit, service.AuditDBQueryMaxPageSize)
	offset := max(0, params.Offset)
	if auditBatchIsSSH(filter.Protocol) {
		return listAuditBatchSSHLogs(db, filter, params.Search, limit, offset)
	}
	return listAuditBatchDBLogs(db, filter, params.Search, limit, offset)
}

func auditBatchLogSource(db *gorm.DB, filter service.AuditBatchFilter, table string) *gorm.DB {
	allowed := auditBatchScope(db.Table("audit_sessions"), filter).Select("id, protocol")
	return db.Table(table+" AS logs").Joins("JOIN (?) AS scoped ON scoped.id = logs.audit_session_id", allowed)
}

func listAuditBatchDBLogs(db *gorm.DB, filter service.AuditBatchFilter, search string, limit, offset int) ([]service.AuditBatchLogItem, int64, error) {
	q := auditBatchLogSource(db, filter, "audit_db_queries")
	if search != "" {
		p := auditBatchLike(search)
		q = q.Where("(LOWER(SUBSTR(logs.sql_text, 1, ?)) LIKE ? ESCAPE '!' OR LOWER(logs.audit_session_id) LIKE ? ESCAPE '!')", auditDBQueryPreviewMaxCharacters, p, p)
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	stored := "OCTET_LENGTH(logs.sql_text)"
	if db.Dialector.Name() == "sqlite" {
		stored = "LENGTH(CAST(logs.sql_text AS BLOB))"
	}
	var rows []struct {
		service.AuditDBQueryPreview
		Protocol string
	}
	cols := fmt.Sprintf(`logs.id, logs.audit_session_id, logs.timestamp, scoped.protocol,
  SUBSTR(logs.sql_text, 1, ?) AS sql_text, %s AS sql_stored_bytes, logs.original_sql_bytes, logs.sql_truncated,
  logs.sql_log_bytes, logs.parameter_log_bytes, logs.audit_data_redacted, logs.query_kind, logs.duration_ms,
  logs.status, logs.error_code, logs.error_message, logs.rows_affected, logs.%s`, stored, quoteAuditDBQueryColumn(db, "rows"))
	if err := q.Select(cols, auditDBQueryPreviewMaxCharacters).Order("logs.timestamp DESC, logs.id ASC").Limit(limit).Offset(offset).Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	items := make([]service.AuditBatchLogItem, 0, len(rows))
	for _, row := range rows {
		data := row.AuditDBQueryPreview
		text, _ := service.AuditDBQuerySQLPreview(data)
		items = append(items, service.AuditBatchLogItem{ID: data.ID, AuditSessionID: data.AuditSessionID, Kind: "query", Timestamp: data.Timestamp, Protocol: row.Protocol, Text: text, Status: model.NormalizeAuditDBQueryStatus(data.Status), DurationMs: data.DurationMs, Rows: data.Rows, RowsAffected: data.RowsAffected, ErrorCode: data.ErrorCode, ErrorMessage: data.ErrorMessage, SQLTruncated: data.SQLTruncated || data.SQLStoredBytes > service.AuditDBQuerySQLPreviewByteLimit, HasFullLog: data.SQLLogBytes > 0 || data.ParameterLogBytes > 0})
	}
	return items, total, nil
}

func auditBatchSSHLogQuery(db *gorm.DB, filter service.AuditBatchFilter, search string) *gorm.DB {
	cmd := auditBatchLogSource(db, filter, "audit_ssh_commands").Select("logs.id, logs.audit_session_id, logs.timestamp, 'command' AS kind, logs.command AS text, '' AS action, '' AS status")
	file := auditBatchLogSource(db, filter, "audit_sftp_events").Select("logs.id, logs.audit_session_id, logs.timestamp, 'file' AS kind, logs.path AS text, logs.action, logs.result AS status")
	if search != "" {
		p := auditBatchLike(search)
		cmd = cmd.Where("(LOWER(logs.command) LIKE ? ESCAPE '!' OR LOWER(logs.audit_session_id) LIKE ? ESCAPE '!')", p, p)
		file = file.Where("(LOWER(logs.path) LIKE ? ESCAPE '!' OR LOWER(logs.action) LIKE ? ESCAPE '!' OR LOWER(logs.audit_session_id) LIKE ? ESCAPE '!')", p, p, p)
	}
	return db.Table("(? UNION ALL ?) AS batch_logs", cmd, file)
}

func listAuditBatchSSHLogs(db *gorm.DB, filter service.AuditBatchFilter, search string, limit, offset int) ([]service.AuditBatchLogItem, int64, error) {
	q := auditBatchSSHLogQuery(db, filter, search)
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	items := make([]service.AuditBatchLogItem, 0)
	if err := q.Order("timestamp DESC, kind ASC, id ASC").Limit(limit).Offset(offset).Scan(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
