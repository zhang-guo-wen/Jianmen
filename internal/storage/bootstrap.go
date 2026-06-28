package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/util"
)

func BootstrapMetadata(db *gorm.DB, cfg *config.Config) error {
	if db == nil {
		return nil
	}
	if cfg == nil {
		return fmt.Errorf("bootstrap metadata: nil config")
	}
	// Config-defined users are only created when they don't exist yet,
	// providing a dev convenience without blocking the setup wizard.
	if err := bootstrapConfigUsers(db, cfg.Users); err != nil {
		return err
	}
	if err := repairUserSessions(db); err != nil {
		return fmt.Errorf("repair user sessions: %w", err)
	}
	return nil
}

func bootstrapConfigUsers(db *gorm.DB, users []config.User) error {
	for _, cfgUser := range users {
		userID := configUserID(cfgUser)
		username := strings.TrimSpace(cfgUser.Username)
		if userID == "" || username == "" {
			continue
		}
		user := model.User{
			ID:       userID,
			Username: username,
			Status:   "active",
		}
		if pw := strings.TrimSpace(cfgUser.Password); pw != "" {
			hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
			if err != nil {
				return fmt.Errorf("hash password for %s: %w", username, err)
			}
			user.PasswordHash = string(hash)
		}
		if token := strings.TrimSpace(cfgUser.ApiToken); token != "" {
			hash := sha256.Sum256([]byte(token))
			user.TokenHash = hex.EncodeToString(hash[:])
		}
		if err := db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"username":      user.Username,
				"status":        user.Status,
				"password_hash": user.PasswordHash,
				"token_hash":    user.TokenHash,
			}),
		}).Create(&user).Error; err != nil {
			return fmt.Errorf("bootstrap metadata user %q: %w", userID, err)
		}

		// Auto-create permanent session for bootstrap user
		var userSessionCount int64
		db.Model(&model.UserSession{}).Where("user_id = ? AND type = ?", userID, "permanent").Count(&userSessionCount)
		if userSessionCount == 0 {
			var maxSeq int
			db.Model(&model.UserSession{}).Where("user_id = ?", userID).
				Select("COALESCE(MAX(session_seq), 0)").Scan(&maxSeq)
			seq := maxSeq + 1
			permSession := model.UserSession{
				UserID:     userID,
				Type:       "permanent",
				Status:     "active",
				SessionSeq: seq,
				SessionID:  util.EncodeBase62Padded(uint64(seq), 5),
			}
			db.Create(&permSession)
		}
	}
	return nil
}

// repairUserSessions 修复已有但session_seq=0或session_id为空的UserSession
func repairUserSessions(db *gorm.DB) error {
	var broken []model.UserSession
	db.Where("session_seq = 0 OR session_id = ''").Find(&broken)
	for _, s := range broken {
		var maxSeq int
		db.Model(&model.UserSession{}).Where("user_id = ?", s.UserID).
			Select("COALESCE(MAX(session_seq), 0)").Scan(&maxSeq)
		seq := maxSeq + 1
		db.Model(&s).Updates(map[string]interface{}{
			"session_seq": seq,
			"session_id":  util.EncodeBase62Padded(uint64(seq), 5),
		})
	}
	return nil
}

func configUserID(user config.User) string {
	if id := strings.TrimSpace(user.ID); id != "" {
		return id
	}
	return strings.TrimSpace(user.Username)
}
