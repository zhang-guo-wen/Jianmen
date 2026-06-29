package store

import (
	"fmt"
	"strings"

	"jianmen/internal/model"
	"jianmen/internal/storage"
	"jianmen/internal/util"
)

// -- user sessions --

func (s *DBStore) UserSessions(userID string) ([]SessionView, error) {
	var sessions []model.UserSession
	q := s.db.Preload("User").Order("session_seq DESC")
	if userID != "" {
		q = q.Where("user_id = ?", userID)
	}
	if err := q.Find(&sessions).Error; err != nil {
		return nil, err
	}
	views := make([]SessionView, len(sessions))
	for i, sess := range sessions {
		views[i] = s.sessionView(sess)
	}
	return views, nil
}

func (s *DBStore) sessionView(sess model.UserSession) SessionView {
	username := sess.User.Username
	if username == "" && sess.UserID != "" {
		var user model.User
		if s.db.Where("id = ?", sess.UserID).First(&user).Error == nil {
			username = user.Username
		}
	}
	return SessionView{
		ID: sess.ID, UserID: sess.UserID, Username: username,
		SessionSeq: sess.SessionSeq, SessionID: sess.SessionID,
		Type: sess.Type, Status: sess.Status,
		ExpiresAt: sess.ExpiresAt, CreatedBy: sess.CreatedBy,
		CreatedAt: sess.CreatedAt,
	}
}

func (s *DBStore) CreateUserSession(sess model.UserSession) (*model.UserSession, error) {
	sess.UserID = strings.TrimSpace(sess.UserID)
	if sess.UserID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	if err := s.ensureUserSessionSequenceFloor(); err != nil {
		return nil, err
	}
	seq, err := storage.NextSequenceValue(s.db, storage.SequenceUserSession, storage.MaxCompactSessionSeq)
	if err != nil {
		return nil, err
	}
	sess.SessionSeq = seq
	sess.SessionID = util.EncodeBase62Padded(uint64(sess.SessionSeq), 5)
	if err := s.db.Create(&sess).Error; err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *DBStore) ensureUserSessionSequenceFloor() error {
	var maxSeq int
	if err := s.db.Model(&model.UserSession{}).
		Select("COALESCE(MAX(session_seq), 0)").
		Scan(&maxSeq).Error; err != nil {
		return fmt.Errorf("user session sequence floor: %w", err)
	}
	return storage.EnsureSequenceNextValue(s.db, storage.SequenceUserSession, maxSeq+1)
}

func (s *DBStore) DisableUserSession(id string) error {
	return s.db.Model(&model.UserSession{}).Where("id = ?", id).Update("status", "disabled").Error
}

func (s *DBStore) EnableUserSession(id string) error {
	return s.db.Model(&model.UserSession{}).Where("id = ?", id).Update("status", "active").Error
}

func (s *DBStore) UserSessionByID(sessionID string, userID string) (*model.UserSession, error) {
	var sess model.UserSession
	q := s.db.Where("session_id = ?", sessionID)
	if userID != "" {
		q = q.Where("user_id = ?", userID)
	}
	if err := q.First(&sess).Error; err != nil {
		return nil, err
	}
	return &sess, nil
}
