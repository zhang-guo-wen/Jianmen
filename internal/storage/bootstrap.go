package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/util"
)

var ErrNoActiveSuperAdmin = errors.New("metadata contains users but no active super administrator")

func BootstrapMetadata(db *gorm.DB, cfg *config.Config) error {
	if db == nil {
		return nil
	}
	if cfg == nil {
		return fmt.Errorf("bootstrap metadata: nil config")
	}
	if err := ReconcileMetadata(db); err != nil {
		return fmt.Errorf("reconcile metadata: %w", err)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		activeSuperAdmins, err := countActiveSuperAdmins(tx, time.Now().UTC())
		if err != nil {
			return fmt.Errorf("count active super administrators: %w", err)
		}
		if err := bootstrapConfigUsers(tx, cfg.Users, activeSuperAdmins == 0); err != nil {
			return err
		}
		if err := requireActiveSuperAdminForExistingUsers(tx, time.Now().UTC()); err != nil {
			return err
		}
		if err := repairUserSessions(tx); err != nil {
			return fmt.Errorf("repair user sessions: %w", err)
		}
		return nil
	})
}

func bootstrapConfigUsers(db *gorm.DB, users []config.User, allowSuperAdminSeed bool) error {
	for _, cfgUser := range users {
		userID := configUserID(cfgUser)
		username := strings.TrimSpace(cfgUser.Username)
		if userID == "" || username == "" {
			continue
		}

		user := model.User{
			ID:           userID,
			Username:     username,
			Status:       "active",
			IsSuperAdmin: allowSuperAdminSeed && cfgUser.SuperAdmin,
		}
		if pw := strings.TrimSpace(cfgUser.Password); pw != "" {
			hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
			if err != nil {
				return fmt.Errorf("hash password for %s: %w", username, err)
			}
			user.PasswordHash = string(hash)
			user.MySQLNativeHash = util.MySQLNativePasswordHash(pw)
		}
		if token := strings.TrimSpace(cfgUser.ApiToken); token != "" {
			hash := sha256.Sum256([]byte(token))
			user.TokenHash = hex.EncodeToString(hash[:])
		}

		updates := map[string]any{
			"username":           user.Username,
			"status":             user.Status,
			"password_hash":      user.PasswordHash,
			"my_sql_native_hash": user.MySQLNativeHash,
			"token_hash":         user.TokenHash,
			"active_marker":      model.ActiveMarkerValue,
			"updated_at":         time.Now().UTC(),
		}
		if allowSuperAdminSeed && cfgUser.SuperAdmin {
			updates["is_super_admin"] = true
		}
		if err := db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.Assignments(updates),
		}).Create(&user).Error; err != nil {
			return fmt.Errorf("bootstrap metadata user %q: %w", userID, err)
		}

		var userSessionCount int64
		if err := activeUserSessionsQuery(db).
			Where("user_id = ? AND type = ?", userID, "permanent").
			Count(&userSessionCount).Error; err != nil {
			return fmt.Errorf("count permanent sessions for %s: %w", userID, err)
		}
		if userSessionCount > 0 {
			continue
		}

		if err := ensureUserSessionSequenceFloor(db); err != nil {
			return err
		}
		seq, err := NextSequenceValue(db, SequenceUserSession, MaxCompactSessionSeq)
		if err != nil {
			return fmt.Errorf("allocate permanent session for %s: %w", userID, err)
		}
		permSession := model.UserSession{
			UserID:     userID,
			Type:       "permanent",
			Status:     "active",
			SessionSeq: seq,
			SessionID:  util.EncodeBase62Padded(uint64(seq), 5),
		}
		if err := db.Create(&permSession).Error; err != nil {
			return fmt.Errorf("create permanent session for %s: %w", userID, err)
		}
	}
	return nil
}

func requireActiveSuperAdminForExistingUsers(db *gorm.DB, now time.Time) error {
	var userCount int64
	if err := db.Model(&model.User{}).Count(&userCount).Error; err != nil {
		return fmt.Errorf("count metadata users: %w", err)
	}
	if userCount == 0 {
		return nil
	}
	activeSuperAdmins, err := countActiveSuperAdmins(db, now)
	if err != nil {
		return fmt.Errorf("count active super administrators: %w", err)
	}
	if activeSuperAdmins == 0 {
		return fmt.Errorf(
			"%w; set super_admin=true for one config user to seed the database, or restore users.is_super_admin",
			ErrNoActiveSuperAdmin,
		)
	}
	return nil
}

func countActiveSuperAdmins(db *gorm.DB, now time.Time) (int64, error) {
	var count int64
	err := db.Model(&model.User{}).
		Where(
			"active_marker = ? AND is_super_admin = ? AND status = ? AND (expires_at IS NULL OR expires_at > ?)",
			model.ActiveMarkerValue,
			true,
			"active",
			now,
		).
		Count(&count).Error
	return count, err
}

func repairUserSessions(db *gorm.DB) error {
	var sessions []model.UserSession
	if err := activeUserSessionsQuery(db).
		Order("user_id ASC, session_seq ASC, id ASC").
		Find(&sessions).Error; err != nil {
		return err
	}

	maxSeq := 0
	usedSeqs := make(map[int]struct{}, len(sessions))
	usedSessID := make(map[string]struct{}, len(sessions))
	for _, sess := range sessions {
		if sess.SessionSeq > maxSeq {
			maxSeq = sess.SessionSeq
		}
	}
	for _, sess := range sessions {
		sessionID := ""
		if sess.SessionSeq > 0 {
			sessionID = util.EncodeBase62Padded(uint64(sess.SessionSeq), 5)
		}
		_, duplicateSeq := usedSeqs[sess.SessionSeq]
		_, duplicateID := usedSessID[sess.SessionID]
		needsRepair := sess.SessionSeq <= 0 ||
			sess.SessionID == "" ||
			sess.SessionID != sessionID ||
			duplicateSeq ||
			duplicateID

		if needsRepair {
			for {
				if maxSeq >= MaxCompactSessionSeq {
					return fmt.Errorf("user session sequence exhausted at %d", MaxCompactSessionSeq)
				}
				maxSeq++
				sessionID = util.EncodeBase62Padded(uint64(maxSeq), 5)
				if _, ok := usedSeqs[maxSeq]; ok {
					continue
				}
				if _, ok := usedSessID[sessionID]; ok {
					continue
				}
				break
			}
			if err := db.Model(&sess).Updates(map[string]interface{}{
				"session_seq": maxSeq,
				"session_id":  sessionID,
			}).Error; err != nil {
				return err
			}
			sess.SessionSeq = maxSeq
			sess.SessionID = sessionID
		}
		usedSeqs[sess.SessionSeq] = struct{}{}
		usedSessID[sess.SessionID] = struct{}{}
	}
	if maxSeq > MaxCompactSessionSeq {
		return fmt.Errorf("user session sequence exceeds compact limit %d", MaxCompactSessionSeq)
	}
	if err := EnsureSequenceNextValue(db, SequenceUserSession, maxSeq+1); err != nil {
		return err
	}
	return nil
}

func ensureUserSessionSequenceFloor(db *gorm.DB) error {
	var maxSeq int
	if err := activeUserSessionsQuery(db).
		Select("COALESCE(MAX(session_seq), 0)").
		Scan(&maxSeq).Error; err != nil {
		return fmt.Errorf("user session sequence floor: %w", err)
	}
	return EnsureSequenceNextValue(db, SequenceUserSession, maxSeq+1)
}

// activeUserSessionsQuery keeps bootstrap and pre-index repair compatible with
// legacy user_sessions tables while making the final active-marker schema
// ignore historical rows.
func activeUserSessionsQuery(db *gorm.DB) *gorm.DB {
	query := db.Model(&model.UserSession{})
	if db.Migrator().HasColumn(&model.UserSession{}, "active_marker") {
		query = query.Where("active_marker = ?", model.ActiveMarkerValue)
	}
	return query
}

func configUserID(user config.User) string {
	if id := strings.TrimSpace(user.ID); id != "" {
		return id
	}
	return strings.TrimSpace(user.Username)
}
