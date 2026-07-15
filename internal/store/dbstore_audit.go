package store

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

// -- audit sessions --

func (s *DBStore) CreateAuditSession(session *model.AuditSession) error {
	return s.db.Create(session).Error
}

func (s *DBStore) EndAuditSession(id string) error {
	now := time.Now().UTC()
	return s.db.Model(&model.AuditSession{}).
		Where("id = ?", id).
		Updates(map[string]any{"state": "ended", "ended_at": now}).Error
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
	views := make([]AuditSessionView, len(sessions))
	for i, sess := range sessions {
		views[i] = AuditSessionView{
			ID:              sess.ID,
			Username:        sess.Username,
			Protocol:        sess.Protocol,
			ProtocolSubtype: sess.ProtocolSubtype,
			TargetName:      sess.TargetName,
			TargetAddress:   sess.TargetAddress,
			AccountName:     sess.AccountName,
			AccountUsername: sess.AccountUsername,
			ClientIP:        sess.ClientIP,
			StartedAt:       sess.StartedAt.Format(time.RFC3339Nano),
			State:           sess.State,
			ReplayDir:       sess.ReplayDir,
		}
		if sess.EndedAt != nil {
			views[i].EndedAt = sess.EndedAt.Format(time.RFC3339Nano)
		}
	}
	return views, total, nil
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
