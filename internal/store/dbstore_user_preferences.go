package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"jianmen/internal/model"
)

func (s *DBStore) UserPreference(ctx context.Context, userID string) (model.UserPreference, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return model.UserPreference{}, errors.New("user id is required")
	}
	var preference model.UserPreference
	err := s.db.WithContext(ctx).First(&preference, "user_id = ?", userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return defaultUserPreference(userID), nil
	}
	if err != nil {
		return model.UserPreference{}, fmt.Errorf("get user preference: %w", err)
	}
	return normalizeStoredUserPreference(preference), nil
}

func (s *DBStore) SaveUserPreference(ctx context.Context, preference model.UserPreference) (model.UserPreference, error) {
	preference = normalizeStoredUserPreference(preference)
	if preference.UserID == "" {
		return model.UserPreference{}, errors.New("user id is required")
	}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"theme", "ssh_client", "ssh_client_path", "ssh_client_platform",
				"db_client", "db_client_platform", "db_client_path", "db_client_ca_file_path",
				"terminal_font_family", "terminal_font_size", "updated_at",
		}),
	}).Create(&preference).Error
	if err != nil {
		return model.UserPreference{}, fmt.Errorf("save user preference: %w", err)
	}
	return preference, nil
}

func defaultUserPreference(userID string) model.UserPreference {
	return model.UserPreference{
		UserID:             userID,
		Theme:              "light",
		TerminalFontFamily: "Cascadia Mono, Consolas, monospace",
		TerminalFontSize:   14,
	}
}

func normalizeStoredUserPreference(preference model.UserPreference) model.UserPreference {
	preference.UserID = strings.TrimSpace(preference.UserID)
	preference.Theme = strings.TrimSpace(preference.Theme)
	preference.SSHClient = strings.TrimSpace(preference.SSHClient)
	preference.SSHClientPath = strings.TrimSpace(preference.SSHClientPath)
	preference.SSHClientPlatform = strings.TrimSpace(preference.SSHClientPlatform)
	preference.DBClient = strings.TrimSpace(preference.DBClient)
	preference.DBClientPlatform = strings.TrimSpace(preference.DBClientPlatform)
	preference.DBClientPath = strings.TrimSpace(preference.DBClientPath)
	preference.DBClientCAFilePath = strings.TrimSpace(preference.DBClientCAFilePath)
	preference.TerminalFontFamily = strings.TrimSpace(preference.TerminalFontFamily)
	if preference.Theme == "" {
		preference.Theme = "light"
	}
	if preference.TerminalFontFamily == "" {
		preference.TerminalFontFamily = "Cascadia Mono, Consolas, monospace"
	}
	if preference.TerminalFontSize == 0 {
		preference.TerminalFontSize = 14
	}
	if preference.SSHClientPlatform == "" {
		preference.SSHClientPlatform = "windows"
	}
	if preference.DBClientPlatform == "" {
		preference.DBClientPlatform = "windows"
	}
	return preference
}
