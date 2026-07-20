package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"jianmen/internal/model"
)

const maxSystemSettingRevisionPageSize = 100

func (s *DBStore) LoadSystemSetting(
	ctx context.Context,
) (model.SystemSetting, bool, error) {
	if ctx == nil {
		return model.SystemSetting{}, false, errors.New("load system setting: nil context")
	}
	var setting model.SystemSetting
	err := s.db.WithContext(ctx).
		First(&setting, "id = ?", model.SystemSettingSingletonID).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.SystemSetting{}, false, nil
	}
	if err != nil {
		return model.SystemSetting{}, false, fmt.Errorf("load system setting: %w", err)
	}
	return setting, true, nil
}

// InitializeSystemSetting creates the singleton and its first immutable
// revision in one transaction. Existing desired settings are never replaced.
func (s *DBStore) InitializeSystemSetting(
	ctx context.Context,
	setting model.SystemSetting,
	revision model.SystemSettingRevision,
) (model.SystemSetting, bool, error) {
	if ctx == nil {
		return model.SystemSetting{}, false, errors.New("initialize system setting: nil context")
	}
	if err := validateSystemSettingRevisionPayload(revision); err != nil {
		return model.SystemSetting{}, false, err
	}

	setting.ID = model.SystemSettingSingletonID
	setting.Revision = 1
	setting.AppliedRevision = 0
	setting.AppliedAt = nil
	revision.Revision = setting.Revision
	var persisted model.SystemSetting
	created := false

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoNothing: true,
		}).Create(&setting)
		if result.Error != nil {
			return fmt.Errorf("initialize system setting: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			if err := tx.First(
				&persisted,
				"id = ?",
				model.SystemSettingSingletonID,
			).Error; err != nil {
				return fmt.Errorf("load initialized system setting: %w", err)
			}
			return nil
		}
		if err := tx.Create(&revision).Error; err != nil {
			return fmt.Errorf("create initial system setting revision: %w", err)
		}
		persisted = setting
		created = true
		return nil
	})
	if err != nil {
		return model.SystemSetting{}, false, err
	}
	return persisted, created, nil
}

// UpdateSystemSetting atomically advances the desired singleton and appends
// its immutable revision when expectedRevision still matches.
func (s *DBStore) UpdateSystemSetting(
	ctx context.Context,
	expectedRevision int64,
	setting model.SystemSetting,
	revision model.SystemSettingRevision,
) (model.SystemSetting, bool, error) {
	if ctx == nil {
		return model.SystemSetting{}, false, errors.New("update system setting: nil context")
	}
	if expectedRevision < 1 {
		return model.SystemSetting{}, false, errors.New("update system setting: expected revision must be positive")
	}
	if err := validateSystemSettingRevisionPayload(revision); err != nil {
		return model.SystemSetting{}, false, err
	}

	nextRevision := expectedRevision + 1
	changedAt := revision.CreatedAt.UTC()
	if revision.CreatedAt.IsZero() {
		changedAt = time.Now().UTC()
	}
	revision.Revision = nextRevision
	revision.CreatedAt = changedAt
	setting.ID = model.SystemSettingSingletonID
	setting.Revision = nextRevision
	setting.UpdatedAt = changedAt

	var persisted model.SystemSetting
	updated := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.SystemSetting{}).
			Where("id = ? AND revision = ?", model.SystemSettingSingletonID, expectedRevision).
			UpdateColumns(systemSettingUpdateColumns(setting))
		if result.Error != nil {
			return fmt.Errorf("update system setting: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return nil
		}
		if err := tx.Create(&revision).Error; err != nil {
			return fmt.Errorf("create system setting revision %d: %w", nextRevision, err)
		}
		if err := tx.First(&persisted, "id = ?", model.SystemSettingSingletonID).Error; err != nil {
			return fmt.Errorf("reload system setting: %w", err)
		}
		updated = true
		return nil
	})
	if err != nil {
		return model.SystemSetting{}, false, err
	}
	return persisted, updated, nil
}

// MarkSystemSettingApplied records the revision loaded by this process without
// changing the desired-setting update timestamp.
func (s *DBStore) MarkSystemSettingApplied(
	ctx context.Context,
	revision int64,
	appliedAt time.Time,
) (model.SystemSetting, bool, error) {
	if ctx == nil {
		return model.SystemSetting{}, false, errors.New("mark system setting applied: nil context")
	}
	if revision < 1 {
		return model.SystemSetting{}, false, errors.New("mark system setting applied: revision must be positive")
	}
	appliedAt = appliedAt.UTC()
	if appliedAt.IsZero() {
		appliedAt = time.Now().UTC()
	}

	var persisted model.SystemSetting
	marked := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.SystemSetting{}).
			Where("id = ? AND revision = ?", model.SystemSettingSingletonID, revision).
			UpdateColumns(map[string]any{
				"applied_revision": revision,
				"applied_at":       appliedAt,
			})
		if result.Error != nil {
			return fmt.Errorf("mark system setting applied: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return nil
		}
		if err := tx.First(
			&persisted,
			"id = ? AND revision = ?",
			model.SystemSettingSingletonID,
			revision,
		).Error; err != nil {
			return fmt.Errorf("reload applied system setting: %w", err)
		}
		marked = true
		return nil
	})
	if err != nil {
		return model.SystemSetting{}, false, err
	}
	return persisted, marked, nil
}

func (s *DBStore) ListSystemSettingRevisions(
	ctx context.Context,
	limit int,
) ([]model.SystemSettingRevision, error) {
	if ctx == nil {
		return nil, errors.New("list system setting revisions: nil context")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > maxSystemSettingRevisionPageSize {
		limit = maxSystemSettingRevisionPageSize
	}
	var revisions []model.SystemSettingRevision
	if err := s.db.WithContext(ctx).
		Order("revision DESC").
		Limit(limit).
		Find(&revisions).Error; err != nil {
		return nil, fmt.Errorf("list system setting revisions: %w", err)
	}
	return revisions, nil
}

func validateSystemSettingRevisionPayload(revision model.SystemSettingRevision) error {
	if strings.TrimSpace(revision.SnapshotJSON) == "" {
		return errors.New("system setting revision snapshot is required")
	}
	if strings.TrimSpace(revision.ChangedFieldsJSON) == "" {
		return errors.New("system setting revision changed fields are required")
	}
	return nil
}

func systemSettingUpdateColumns(setting model.SystemSetting) map[string]any {
	return map[string]any{
		"database_gateway_mode":             strings.TrimSpace(setting.DatabaseGatewayMode),
		"database_gateway_client_tls_mode":  strings.TrimSpace(setting.DatabaseGatewayClientTLSMode),
		"web_rdp_enabled":                   setting.WebRDPEnabled,
		"web_rdp_connect_timeout_seconds":   setting.WebRDPConnectTimeoutSeconds,
		"web_rdp_allow_unrecorded":          setting.WebRDPAllowUnrecorded,
		"recording_enabled":                 setting.RecordingEnabled,
		"recording_record_input":            setting.RecordingRecordInput,
		"recording_record_commands":         setting.RecordingRecordCommands,
		"recording_retention_days":          setting.RecordingRetentionDays,
		"recording_max_replay_bytes":        setting.RecordingMaxReplayBytes,
		"recording_cleanup_batch_size":      setting.RecordingCleanupBatchSize,
		"database_max_client_message_bytes": setting.DatabaseMaxClientMessageBytes,
		"revision":                          setting.Revision,
		"updated_by_id":                     strings.TrimSpace(setting.UpdatedByID),
		"updated_by_username":               strings.TrimSpace(setting.UpdatedByUsername),
		"updated_at":                        setting.UpdatedAt,
	}
}
