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

func (s *DBStore) ListAuditEvents(
	ctx context.Context,
	params AuditEventListParams,
) ([]model.AuditEvent, int64, error) {
	if ctx == nil {
		return nil, 0, errors.New("list audit events: nil context")
	}
	q := s.db.WithContext(ctx).Model(&model.AuditEvent{})
	if params.Search != "" {
		like := "%" + strings.ToLower(strings.TrimSpace(params.Search)) + "%"
		q = q.Where("LOWER(actor_username) LIKE ? OR LOWER(action) LIKE ? OR LOWER(resource_type) LIKE ? OR LOWER(resource_name) LIKE ? OR LOWER(detail) LIKE ? OR LOWER(client_ip) LIKE ?", like, like, like, like, like, like)
	}
	if params.Action != "" {
		q = q.Where("action = ?", params.Action)
	}
	if params.ResourceType != "" {
		q = q.Where("resource_type = ?", params.ResourceType)
	}
	if start, end, ok := auditDateRange(params.Date); ok {
		q = q.Where("created_at >= ? AND created_at < ?", start, end)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count audit events: %w", err)
	}
	page, size := normalizeAuditPage(params.Page, params.Size)
	var items []model.AuditEvent
	if err := q.Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list audit events: %w", err)
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
	q := s.db.WithContext(ctx).Model(&model.LoginAuditLog{})
	if params.Search != "" {
		like := "%" + strings.ToLower(strings.TrimSpace(params.Search)) + "%"
		q = q.Where("LOWER(username) LIKE ? OR LOWER(client_ip) LIKE ? OR LOWER(reason) LIKE ? OR LOWER(user_agent) LIKE ?", like, like, like, like)
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
	if err := q.Order("created_at DESC").Offset((page - 1) * size).Limit(size).Find(&items).Error; err != nil {
		return nil, 0, fmt.Errorf("list login audit logs: %w", err)
	}
	return items, total, nil
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
