package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

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

// -- audit sessions --

func (s *DBStore) CreateAuditSession(session *model.AuditSession) error {
	s.prepareAuditSessionLease(session)
	if err := s.db.Create(session).Error; err != nil {
		return err
	}
	s.trackAuditSessionLease(session)
	return nil
}

func (s *DBStore) EndAuditSession(id string) error {
	s.untrackAuditSessionLease(id)
	now := time.Now().UTC()
	return s.db.Model(&model.AuditSession{}).
		Where("id = ? AND state = ?", id, "started").
		Updates(map[string]any{
			"state": "ended", "ended_at": now,
			"outcome": gorm.Expr(
				"CASE WHEN outcome IS NULL OR outcome = '' OR outcome IN (?, ?) THEN ? ELSE outcome END",
				model.AuditOutcomeConnecting, model.AuditOutcomeActive, model.AuditOutcomeSucceeded,
			),
		}).Error
}

func (s *DBStore) UpdateAuditProtocol(id string, protocol string) error {
	return s.db.Model(&model.AuditSession{}).
		Where("id = ?", id).
		Updates(map[string]any{"protocol": "ssh", "protocol_subtype": protocol}).Error
}

func (s *DBStore) GetAuditSession(id string) (*model.AuditSession, error) {
	var session model.AuditSession
	if err := s.db.Where("id = ?", id).First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("audit session %q: %w", id, err)
		}
		return nil, err
	}
	return &session, nil
}

func (s *DBStore) ListAuditSessions(params AuditListParams) ([]AuditSessionView, int64, error) {
	q := s.db.Model(&model.AuditSession{})
	if params.Protocol != "" {
		protos := splitCSV(params.Protocol)
		q = q.Where("protocol IN ?", protos)
	}
	if params.Search != "" {
		like := "%" + strings.ToLower(params.Search) + "%"
		q = q.Where(
			`LOWER(username) LIKE ?
				OR LOWER(target_name) LIKE ?
				OR LOWER(target_address) LIKE ?
				OR LOWER(account_name) LIKE ?
				OR LOWER(account_username) LIKE ?
				OR EXISTS (
					SELECT 1 FROM audit_ssh_commands
					WHERE audit_ssh_commands.audit_session_id = audit_sessions.id
						AND LOWER(audit_ssh_commands.command) LIKE ?
				)
				OR EXISTS (
					SELECT 1 FROM audit_db_queries
					WHERE audit_db_queries.audit_session_id = audit_sessions.id
						AND LOWER(audit_db_queries.sql_text) LIKE ?
				)`,
			like, like, like, like, like, like, like,
		)
	}
	if params.Date != "" {
		date, err := time.Parse("2006-01-02", params.Date)
		if err == nil {
			nextDate := date.Add(24 * time.Hour)
			q = q.Where("started_at >= ? AND started_at < ?", date, nextDate)
		}
	}
	if userID := strings.TrimSpace(params.UserID); userID != "" {
		q = q.Where("user_id = ?", userID)
	}
	if accountID := strings.TrimSpace(params.AccountID); accountID != "" {
		q = q.Where("account_id = ?", accountID)
	}
	if outcome := strings.TrimSpace(params.Outcome); outcome != "" {
		q = q.Where("outcome = ?", outcome)
	}
	if status := strings.TrimSpace(params.RecordingStatus); status != "" {
		q = q.Where("recording_status = ?", status)
	}
	if params.StartedFrom != nil {
		q = q.Where("started_at >= ?", params.StartedFrom.UTC())
	}
	if params.StartedTo != nil {
		q = q.Where("started_at < ?", params.StartedTo.UTC())
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if params.Size <= 0 {
		params.Size = 20
	}
	if params.Page <= 0 {
		params.Page = 1
	}
	var sessions []model.AuditSession
	if err := q.Order("started_at DESC").Offset((params.Page - 1) * params.Size).Limit(params.Size).Find(&sessions).Error; err != nil {
		return nil, 0, err
	}
	logCounts, err := s.auditLogCounts(sessions)
	if err != nil {
		return nil, 0, err
	}
	views := make([]AuditSessionView, len(sessions))
	for i, sess := range sessions {
		views[i] = AuditSessionView{
			ID:              sess.ID,
			UserID:          sess.UserID,
			Username:        sess.Username,
			Protocol:        sess.Protocol,
			ProtocolSubtype: sess.ProtocolSubtype,
			ResourceType:    sess.ResourceType,
			ResourceID:      sess.ResourceID,
			HostID:          sess.HostID,
			AccountID:       sess.AccountID,
			TargetName:      sess.TargetName,
			TargetAddress:   sess.TargetAddress,
			AccountName:     sess.AccountName,
			AccountUsername: sess.AccountUsername,
			ClientIP:        sess.ClientIP,
			StartedAt:       sess.StartedAt.Format(time.RFC3339Nano),
			State:           sess.State,
			Outcome:         sess.Outcome,
			FailureCode:     sess.FailureCode,
			FailureMessage:  sess.FailureMessage,
			RecordingStatus: sess.RecordingStatus,
			HasReplay:       sess.ReplayDir != "" || sess.RecordingStatus == model.RecordingStatusReady,
			LogCount:        logCounts[sess.ID],
		}
		if sess.EndedAt != nil {
			views[i].EndedAt = sess.EndedAt.Format(time.RFC3339Nano)
		}
	}
	return views, total, nil
}

func (s *DBStore) FinishAuditSession(
	ctx context.Context,
	id string,
	outcome string,
	failureCode string,
	failureMessage string,
	recordingStatus string,
	endedAt time.Time,
) error {
	s.untrackAuditSessionLease(id)
	outcome = strings.TrimSpace(outcome)
	result := s.db.WithContext(ctx).Model(&model.AuditSession{}).
		Where("id = ?", strings.TrimSpace(id)).
		Where(
			"((state = ? AND lease_owner = ?) OR (state = ? AND outcome = ?))",
			"started",
			s.auditLeaseOwner,
			"ended",
			outcome,
		).
		Updates(map[string]any{
			"state": "ended", "ended_at": endedAt.UTC(), "outcome": outcome,
			"failure_code": strings.TrimSpace(failureCode), "failure_message": strings.TrimSpace(failureMessage),
			"recording_status": strings.TrimSpace(recordingStatus),
		})
	if result.Error != nil {
		return fmt.Errorf("finish audit session: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("finish audit session %q: %w", id, gorm.ErrRecordNotFound)
	}
	return nil
}

func (s *DBStore) CreateAuditArtifact(ctx context.Context, artifact *model.AuditArtifact) error {
	if artifact == nil {
		return errors.New("audit artifact is required")
	}
	if err := s.db.WithContext(ctx).Create(artifact).Error; err != nil {
		return fmt.Errorf("create audit artifact: %w", err)
	}
	return nil
}

func (s *DBStore) UpdateAuditArtifact(ctx context.Context, artifact *model.AuditArtifact) error {
	if artifact == nil || strings.TrimSpace(artifact.ID) == "" {
		return errors.New("audit artifact id is required")
	}
	if err := s.db.WithContext(ctx).Save(artifact).Error; err != nil {
		return fmt.Errorf("update audit artifact: %w", err)
	}
	return nil
}

func (s *DBStore) AuditArtifactBySession(
	ctx context.Context,
	sessionID string,
	kind string,
) (model.AuditArtifact, error) {
	var artifact model.AuditArtifact
	err := s.db.WithContext(ctx).
		Where("audit_session_id = ? AND kind = ?", strings.TrimSpace(sessionID), strings.TrimSpace(kind)).
		First(&artifact).Error
	if err != nil {
		return model.AuditArtifact{}, fmt.Errorf("get audit artifact: %w", err)
	}
	return artifact, nil
}

func (s *DBStore) CreateAuditRDPChannelEvent(ctx context.Context, event *model.AuditRDPChannelEvent) error {
	if event == nil {
		return errors.New("RDP channel event is required")
	}
	if err := s.db.WithContext(ctx).Create(event).Error; err != nil {
		return fmt.Errorf("create RDP channel event: %w", err)
	}
	return nil
}

type auditLogCountRow struct {
	AuditSessionID string `gorm:"column:audit_session_id"`
	Count          int64  `gorm:"column:count"`
}

func (s *DBStore) auditLogCounts(sessions []model.AuditSession) (map[string]int64, error) {
	counts := make(map[string]int64, len(sessions))
	idsByKind := map[string][]string{
		"ssh":  {},
		"sftp": {},
		"db":   {},
	}
	for _, session := range sessions {
		kind := auditLogKind(session)
		idsByKind[kind] = append(idsByKind[kind], session.ID)
	}

	queries := []struct {
		kind  string
		model any
	}{
		{kind: "ssh", model: &model.AuditSSHCommand{}},
		{kind: "sftp", model: &model.AuditSFTPEvent{}},
		{kind: "db", model: &model.AuditDBQuery{}},
	}
	for _, query := range queries {
		ids := idsByKind[query.kind]
		if len(ids) == 0 {
			continue
		}
		var rows []auditLogCountRow
		if err := s.db.Model(query.model).
			Select("audit_session_id, COUNT(*) AS count").
			Where("audit_session_id IN ?", ids).
			Group("audit_session_id").
			Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("count %s audit logs: %w", query.kind, err)
		}
		for _, row := range rows {
			counts[row.AuditSessionID] = row.Count
		}
	}
	return counts, nil
}

func auditLogKind(session model.AuditSession) string {
	if strings.EqualFold(session.ProtocolSubtype, "sftp") || strings.EqualFold(session.Protocol, "sftp") {
		return "sftp"
	}
	switch strings.ToLower(session.Protocol) {
	case "mysql", "postgres", "postgresql", "redis", "db", "database":
		return "db"
	default:
		return "ssh"
	}
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// -- audit SSH commands --

func (s *DBStore) CreateAuditSSHCommand(cmd *model.AuditSSHCommand) error {
	return s.db.Create(cmd).Error
}

func (s *DBStore) ListAuditSSHCommands(sessionID string, opts PageOpts) ([]model.AuditSSHCommand, int64, error) {
	q := s.db.Model(&model.AuditSSHCommand{}).Where("audit_session_id = ?", sessionID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var cmds []model.AuditSSHCommand
	if opts.Limit <= 0 {
		opts.Limit = 500
	}
	if err := q.Order("timestamp ASC").Offset(opts.Offset).Limit(opts.Limit).Find(&cmds).Error; err != nil {
		return nil, 0, err
	}
	return cmds, total, nil
}

// -- audit SFTP events --

func (s *DBStore) CreateAuditSFTPEvent(event *model.AuditSFTPEvent) error {
	return s.db.Create(event).Error
}

func (s *DBStore) ListAuditSFTPEvents(sessionID string, opts PageOpts) ([]model.AuditSFTPEvent, int64, error) {
	q := s.db.Model(&model.AuditSFTPEvent{}).Where("audit_session_id = ?", sessionID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var events []model.AuditSFTPEvent
	if opts.Limit <= 0 {
		opts.Limit = 1000
	}
	if err := q.Order("timestamp ASC").Offset(opts.Offset).Limit(opts.Limit).Find(&events).Error; err != nil {
		return nil, 0, err
	}
	return events, total, nil
}

// -- audit DB queries --

func (s *DBStore) CreateAuditDBQuery(query *model.AuditDBQuery) error {
	return s.db.Create(query).Error
}

func (s *DBStore) ListAuditDBQueries(sessionID string, opts PageOpts) ([]model.AuditDBQuery, int64, error) {
	q := s.db.Model(&model.AuditDBQuery{}).Where("audit_session_id = ?", sessionID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var queries []model.AuditDBQuery
	if opts.Limit <= 0 {
		opts.Limit = 1000
	}
	if err := q.Order("timestamp ASC").Offset(opts.Offset).Limit(opts.Limit).Find(&queries).Error; err != nil {
		return nil, 0, err
	}
	return queries, total, nil
}

func (s *DBStore) ListAuditDBQueryEvents(sessionID string) ([]model.AuditDBQuery, error) {
	var queries []model.AuditDBQuery
	if err := s.db.Where("audit_session_id = ?", sessionID).Order("timestamp ASC").Find(&queries).Error; err != nil {
		return nil, err
	}
	return queries, nil
}

// -- management and login audit logs --

func (s *DBStore) CreateAuditEvent(event *model.AuditEvent) error {
	return s.db.Create(event).Error
}

func (s *DBStore) ListAuditEvents(params AuditEventListParams) ([]model.AuditEvent, int64, error) {
	q := s.db.Model(&model.AuditEvent{})
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

func (s *DBStore) CreateLoginAuditLog(log *model.LoginAuditLog) error {
	return s.db.Create(log).Error
}

func (s *DBStore) ListLoginAuditLogs(params LoginAuditListParams) ([]model.LoginAuditLog, int64, error) {
	q := s.db.Model(&model.LoginAuditLog{})
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

// -- user session lookup --

func (s *DBStore) FindUserSessionByCompactUsername(username string) (*model.UserSession, error) {
	login, err := parseLoginName(username)
	if err != nil {
		return nil, fmt.Errorf("parse compact username %q: %w", username, err)
	}
	var sess model.UserSession
	if err := s.db.Where("session_id = ? AND status = ?", login.SessionID, "active").First(&sess).Error; err != nil {
		return nil, fmt.Errorf("lookup user session by session_id %q: %w", login.SessionID, err)
	}
	return &sess, nil
}
