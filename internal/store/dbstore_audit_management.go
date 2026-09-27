package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/model"
)

func auditDateRange(date string) (time.Time, time.Time, bool) {
	if strings.TrimSpace(date) == "" {
		return time.Time{}, time.Time{}, false
	}
	start, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(date), time.UTC)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	return start, start.AddDate(0, 0, 1), true
}

func (s *DBStore) CreateAuditEvent(ctx context.Context, event *model.AuditEvent) error {
	if ctx == nil {
		return errors.New("create audit event: nil context")
	}
	if err := s.db.WithContext(ctx).Create(event).Error; err != nil {
		return fmt.Errorf("create audit event: %w", err)
	}
	return nil
}

// auditEventWithDisplayName 用于接收 ListAuditEvents 的 join 查询结果：
// 外层同名字段优先于内嵌模型，从而把 users.display_name 扫描进来
//（model.AuditEvent.ActorDisplayName 标记 gorm:"-" 不会被 gorm 扫描）。
type auditEventWithDisplayName struct {
	model.AuditEvent
	ActorDisplayName string
}

func (s *DBStore) ListAuditEvents(
	ctx context.Context,
	params AuditEventListParams,
) ([]model.AuditEvent, int64, error) {
	if ctx == nil {
		return nil, 0, errors.New("list audit events: nil context")
	}
	q := s.db.WithContext(ctx).
		Model(&model.AuditEvent{}).
		// 关联用户表取出操作人显示名；active_marker 为 NULL 表示已软删除的用户
		Joins("LEFT JOIN users ON users.id = audit_events.actor_id AND users.active_marker IS NOT NULL").
		Select("audit_events.*, COALESCE(users.display_name, '') AS actor_display_name").
		Where(logicalAuditEventRowsCondition(s.db.Dialector.Name()))
	if params.Search != "" {
		like := "%" + strings.ToLower(strings.TrimSpace(params.Search)) + "%"
		q = q.Where(
			"LOWER(audit_events.actor_username) LIKE ? OR LOWER(audit_events.action) LIKE ? OR LOWER(audit_events.resource_type) LIKE ? OR LOWER(audit_events.resource_name) LIKE ? OR LOWER(audit_events.result) LIKE ? OR LOWER(audit_events.request_id) LIKE ? OR LOWER(audit_events.intent_id) LIKE ? OR LOWER(audit_events.detail) LIKE ? OR LOWER(audit_events.client_ip) LIKE ?",
			like, like, like, like, like, like, like, like, like,
		)
	}
	if params.Action != "" {
		q = q.Where("audit_events.action = ?", params.Action)
	}
	if params.ResourceType != "" {
		q = q.Where("audit_events.resource_type = ?", params.ResourceType)
	}
	if start, end, ok := auditDateRange(params.Date); ok {
		q = q.Where("audit_events.created_at >= ? AND audit_events.created_at < ?", start, end)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count audit events: %w", err)
	}
	page, size := normalizeAuditPage(params.Page, params.Size)
	var rows []auditEventWithDisplayName
	if err := q.Order("audit_events.created_at DESC, audit_events.id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("list audit events: %w", err)
	}
	items := make([]model.AuditEvent, len(rows))
	for i, row := range rows {
		items[i] = row.AuditEvent
		items[i].ActorDisplayName = row.ActorDisplayName
	}
	return items, total, nil
}

func (s *DBStore) CreateLoginAuditLog(ctx context.Context, log *model.LoginAuditLog) error {
	if ctx == nil {
		return errors.New("create login audit log: nil context")
	}
	if err := s.db.WithContext(ctx).Create(log).Error; err != nil {
		return fmt.Errorf("create login audit log: %w", err)
	}
	return nil
}

func (s *DBStore) ListLoginAuditLogs(
	ctx context.Context,
	params LoginAuditListParams,
) ([]model.LoginAuditLog, int64, error) {
	if ctx == nil {
		return nil, 0, errors.New("list login audit logs: nil context")
	}
	q := s.db.WithContext(ctx).
		Model(&model.LoginAuditLog{}).
		Where(logicalLoginAuditRowsCondition(s.db.Dialector.Name()))
	if params.Search != "" {
		like := "%" + strings.ToLower(strings.TrimSpace(params.Search)) + "%"
		q = q.Where(
			"LOWER(username) LIKE ? OR LOWER(client_ip) LIKE ? OR LOWER(result) LIKE ? OR LOWER(request_id) LIKE ? OR LOWER(intent_id) LIKE ? OR LOWER(reason) LIKE ? OR LOWER(user_agent) LIKE ?",
			like, like, like, like, like, like, like,
		)
	}
	if params.Outcome != "" {
		q = q.Where("outcome = ?", params.Outcome)
	}
	if start, end, ok := auditDateRange(params.Date); ok {
		q = q.Where("created_at >= ? AND created_at < ?", start, end)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count login audit logs: %w", err)
	}
	page, size := normalizeAuditPage(params.Page, params.Size)
	var items []model.LoginAuditLog
	if err := q.Order("created_at DESC, id DESC").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list login audit logs: %w", err)
	}
	return items, total, nil
}

// logicalAuditEventRowsCondition keeps the durable result row for a completed
// two-phase operation and keeps the intent only when no linked result exists.
// The legacy JSON predicate preserves the same semantics for pre-structure rows.
//
// 配对 EXISTS 拆成两个独立子查询（EXISTS(A OR B) ≡ EXISTS(A) OR EXISTS(B)），
// 使现代 result 分支能走 (phase, intent_id) 复合索引，避免外层每一行都对
// 整个配对表做全表扫描（数据量大后操作日志查询呈 O(N²)）。
func logicalAuditEventRowsCondition(dialect string) string {
	legacyIntentPattern := sqlAuditConcat(
		dialect,
		`'%"intent_id":"'`,
		"audit_events.id",
		`'"%'`,
	)
	return fmt.Sprintf(`NOT (
		(
			COALESCE(audit_events.phase, '') = 'intent'
			OR (
				COALESCE(audit_events.phase, '') = ''
				AND audit_events.detail LIKE '%%"phase":"intent"%%'
			)
		)
		AND (
			EXISTS (
				SELECT 1
				FROM audit_events AS audit_event_results
				WHERE audit_event_results.phase = 'result'
					AND audit_event_results.intent_id = audit_events.id
			)
			OR EXISTS (
				SELECT 1
				FROM audit_events AS audit_event_results
				WHERE COALESCE(audit_event_results.phase, '') = ''
					AND audit_event_results.detail LIKE '%%"phase":"result"%%'
					AND audit_event_results.detail LIKE %s
			)
		)
	)`, legacyIntentPattern)
}

// logicalLoginAuditRowsCondition applies the same completed-pair collapsing to
// login logs. Legacy rows encoded their linkage at the start of Reason.
//
// 与 logicalAuditEventRowsCondition 相同：拆分配对 EXISTS，让现代 result 分支
// 走 (phase, intent_id) 复合索引，避免每行全表扫描的 O(N²) 执行计划。
func logicalLoginAuditRowsCondition(dialect string) string {
	legacyIntentPattern := sqlAuditConcat(
		dialect,
		"'intent_id='",
		"audit_login_logs.id",
		"'%'",
	)
	return fmt.Sprintf(`NOT (
		(
			COALESCE(audit_login_logs.phase, '') = 'intent'
			OR (
				COALESCE(audit_login_logs.phase, '') = ''
				AND audit_login_logs.outcome = 'pending'
				AND audit_login_logs.reason = 'intent'
			)
		)
		AND (
			EXISTS (
				SELECT 1
				FROM audit_login_logs AS login_audit_results
				WHERE login_audit_results.phase = 'result'
					AND login_audit_results.intent_id = audit_login_logs.id
			)
			OR EXISTS (
				SELECT 1
				FROM audit_login_logs AS login_audit_results
				WHERE COALESCE(login_audit_results.phase, '') = ''
					AND login_audit_results.outcome <> 'pending'
					AND login_audit_results.reason LIKE %s
			)
		)
	)`, legacyIntentPattern)
}

func sqlAuditConcat(dialect string, expressions ...string) string {
	if dialect == "mysql" {
		return "CONCAT(" + strings.Join(expressions, ", ") + ")"
	}
	return strings.Join(expressions, " || ")
}

func normalizeAuditPage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size <= 0 {
		size = 50
	}
	if size > 200 {
		size = 200
	}
	return page, size
}
