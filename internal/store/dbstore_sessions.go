package store

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"jianmen/internal/model"
	"jianmen/internal/storage"
	"jianmen/internal/util"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var sqliteUserSessionCreationMu sync.Mutex

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
	return s.CreateUserSessionWithContext(context.Background(), sess)
}

func (s *DBStore) FindActiveHostAccount(ctx context.Context, id string) (model.HostAccount, bool, error) {
	var account model.HostAccount
	err := s.db.WithContext(ctx).Where("id = ? AND status = ?", id, "active").First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.HostAccount{}, false, nil
	}
	if err != nil {
		return model.HostAccount{}, false, err
	}
	return account, true, nil
}

func (s *DBStore) FindActiveHost(ctx context.Context, id string) (model.Host, bool, error) {
	var host model.Host
	err := s.db.WithContext(ctx).Where("id = ? AND status = ?", id, "active").First(&host).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Host{}, false, nil
	}
	if err != nil {
		return model.Host{}, false, err
	}
	return host, true, nil
}

func (s *DBStore) FindActiveDatabaseAccount(ctx context.Context, id string) (model.DatabaseAccount, bool, error) {
	var account model.DatabaseAccount
	err := s.db.WithContext(ctx).Preload("Instance").Where("id = ? AND status = ?", id, "active").First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.DatabaseAccount{}, false, nil
	}
	if err != nil {
		return model.DatabaseAccount{}, false, err
	}
	return account, true, nil
}

func (s *DBStore) FindActivePermanentUserSession(ctx context.Context, userID string) (model.UserSession, bool, error) {
	var session model.UserSession
	err := s.db.WithContext(ctx).Where("user_id = ? AND type = ? AND status = ?", userID, "permanent", "active").First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.UserSession{}, false, nil
	}
	if err != nil {
		return model.UserSession{}, false, err
	}
	return session, true, nil
}

func (s *DBStore) CreateUserSessionWithContext(ctx context.Context, sess model.UserSession) (*model.UserSession, error) {
	sess.UserID = strings.TrimSpace(sess.UserID)
	if sess.UserID == "" {
		return nil, fmt.Errorf("user_id is required")
	}
	if err := s.ensureUserSessionSequenceFloor(ctx); err != nil {
		return nil, err
	}
	seq, err := storage.NextSequenceValue(s.db.WithContext(ctx), storage.SequenceUserSession, storage.MaxCompactSessionSeq)
	if err != nil {
		return nil, err
	}
	sess.SessionSeq = seq
	sess.SessionID = util.EncodeBase62Padded(uint64(sess.SessionSeq), 5)
	if err := s.db.WithContext(ctx).Create(&sess).Error; err != nil {
		return nil, err
	}
	return &sess, nil
}

// GetOrCreateActivePermanentUserSession serializes permanent session creation
// with a row lock on the authenticated user. MySQL and PostgreSQL enforce that
// lock across processes; SQLite uses only the test-local process mutex because
// it does not implement SELECT FOR UPDATE.
func (s *DBStore) GetOrCreateActivePermanentUserSession(ctx context.Context, userID string) (model.UserSession, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return model.UserSession{}, fmt.Errorf("user_id is required")
	}
	if s.db.Dialector.Name() == "sqlite" {
		sqliteUserSessionCreationMu.Lock()
		defer sqliteUserSessionCreationMu.Unlock()
	}
	var session model.UserSession
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", userID).Error; err != nil {
			return err
		}
		err := tx.Where("user_id = ? AND type = ? AND status = ?", userID, "permanent", "active").First(&session).Error
		if err == nil {
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := s.ensureUserSessionSequenceFloorTx(tx); err != nil {
			return err
		}
		seq, err := storage.NextSequenceValueInTransaction(tx, storage.SequenceUserSession, storage.MaxCompactSessionSeq)
		if err != nil {
			return err
		}
		session = model.UserSession{UserID: userID, Type: "permanent", Status: "active", SessionSeq: seq, SessionID: util.EncodeBase62Padded(uint64(seq), 5)}
		return tx.Create(&session).Error
	})
	if err != nil {
		return model.UserSession{}, err
	}
	return session, nil
}

// FindUserSessionBySessionID 通过短 session_id（如 "00001"）查找用户会话。
func (s *DBStore) FindUserSessionBySessionID(ctx context.Context, sessionID string) (model.UserSession, error) {
	var session model.UserSession
	if err := s.db.WithContext(ctx).Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		return model.UserSession{}, fmt.Errorf("find user session %q: %w", sessionID, err)
	}
	return session, nil
}

// FindAITokenSessionID 查找用户的 AI 令牌关联的临时会话短 ID，用于 SSH 认证。
func (s *DBStore) FindAITokenSessionID(ctx context.Context, userID string) string {
	type result struct {
		SessionID string
	}
	var r result
	// 查找最新的活跃 AI 令牌对应的临时账号会话
	if err := s.db.WithContext(ctx).
		Table("ai_access_tokens").
		Select("ta.session_id").
		Joins("JOIN temporary_accounts ta ON ta.id = ai_access_tokens.temporary_account_id").
		Where("ai_access_tokens.user_id = ? AND ai_access_tokens.revoked_at IS NULL", userID).
		Where("ta.status = ?", "active").
		Order("ai_access_tokens.created_at DESC").
		Limit(1).
		Scan(&r).Error; err != nil || r.SessionID == "" {
		slog.Default().Info("FindAITokenSessionID", "user_id", userID, "session_id", r.SessionID, "err", err)
		return ""
	}
	slog.Default().Info("FindAITokenSessionID found", "user_id", userID, "session_id", r.SessionID)
	return r.SessionID
}

func (s *DBStore) ensureUserSessionSequenceFloor(ctx context.Context) error {
	var maxSeq int
	if err := s.db.WithContext(ctx).Model(&model.UserSession{}).
		Select("COALESCE(MAX(session_seq), 0)").
		Scan(&maxSeq).Error; err != nil {
		return fmt.Errorf("user session sequence floor: %w", err)
	}
	return storage.EnsureSequenceNextValue(s.db.WithContext(ctx), storage.SequenceUserSession, maxSeq+1)
}

func (s *DBStore) ensureUserSessionSequenceFloorTx(tx *gorm.DB) error {
	var maxSeq int
	if err := tx.Model(&model.UserSession{}).Select("COALESCE(MAX(session_seq), 0)").Scan(&maxSeq).Error; err != nil {
		return fmt.Errorf("user session sequence floor: %w", err)
	}
	return storage.EnsureSequenceNextValueInTransaction(tx, storage.SequenceUserSession, maxSeq+1)
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
