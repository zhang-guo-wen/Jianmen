package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"jianmen/internal/model"
)

type SystemSettingsBootstrap struct {
	Settings SystemSettings
	Revision int64
}

// Bootstrap initializes the singleton and activates the persisted revision.
// Runtime assembly should use PrepareBootstrap and ActivateBootstrap when it
// must validate infrastructure-dependent configuration before activation.
func (s *SystemSettingsService) Bootstrap(
	ctx context.Context,
	baseline SystemSettings,
) (SystemSettingsState, error) {
	prepared, err := s.PrepareBootstrap(ctx, baseline)
	if err != nil {
		return SystemSettingsState{}, err
	}
	return s.ActivateBootstrap(ctx, prepared)
}

// PrepareBootstrap initializes the singleton only when it does not exist and
// returns the persisted candidate without marking it effective.
func (s *SystemSettingsService) PrepareBootstrap(
	ctx context.Context,
	baseline SystemSettings,
) (SystemSettingsBootstrap, error) {
	if ctx == nil {
		return SystemSettingsBootstrap{}, errors.New("prepare system settings bootstrap: nil context")
	}
	if err := validateSystemSettings(baseline); err != nil {
		return SystemSettingsBootstrap{}, err
	}

	now := s.now().UTC()
	snapshotJSON, err := marshalSystemSettings(baseline)
	if err != nil {
		return SystemSettingsBootstrap{}, err
	}
	changedFieldsJSON, err := json.Marshal(systemSettingFieldNames)
	if err != nil {
		return SystemSettingsBootstrap{}, fmt.Errorf("marshal initial system setting fields: %w", err)
	}
	initial := systemSettingModel(baseline, SystemSettingsActor{
		ID: "system", Username: "system",
	})
	initial.CreatedAt = now
	initial.UpdatedAt = now
	persisted, _, err := s.repository.InitializeSystemSetting(
		ctx,
		initial,
		model.SystemSettingRevision{
			SnapshotJSON: snapshotJSON, ChangedFieldsJSON: string(changedFieldsJSON),
			UpdatedByID: initial.UpdatedByID, UpdatedByUsername: initial.UpdatedByUsername,
			CreatedAt: now,
		},
	)
	if err != nil {
		return SystemSettingsBootstrap{}, fmt.Errorf("initialize system settings: %w", err)
	}
	prepared := SystemSettingsBootstrap{
		Settings: systemSettingsFromModel(persisted),
		Revision: persisted.Revision,
	}
	if prepared.Revision < 1 {
		return SystemSettingsBootstrap{}, fmt.Errorf(
			"%w: persisted revision must be positive",
			ErrInvalidSystemSettings,
		)
	}
	if err := validateSystemSettings(prepared.Settings); err != nil {
		return SystemSettingsBootstrap{}, fmt.Errorf("validate persisted system settings: %w", err)
	}
	return prepared, nil
}

// ActivateBootstrap atomically marks the prepared revision applied and only
// then publishes it as this process's effective configuration.
func (s *SystemSettingsService) ActivateBootstrap(
	ctx context.Context,
	prepared SystemSettingsBootstrap,
) (SystemSettingsState, error) {
	if ctx == nil {
		return SystemSettingsState{}, errors.New("activate system settings bootstrap: nil context")
	}
	if prepared.Revision < 1 {
		return SystemSettingsState{}, fmt.Errorf(
			"%w: prepared revision must be positive",
			ErrInvalidSystemSettings,
		)
	}
	if err := validateSystemSettings(prepared.Settings); err != nil {
		return SystemSettingsState{}, err
	}

	applied, marked, err := s.repository.MarkSystemSettingApplied(
		ctx,
		prepared.Revision,
		s.now().UTC(),
	)
	if err != nil {
		return SystemSettingsState{}, fmt.Errorf("mark system settings applied: %w", err)
	}
	if !marked ||
		applied.Revision != prepared.Revision ||
		applied.AppliedRevision != prepared.Revision ||
		systemSettingsFromModel(applied) != prepared.Settings {
		return SystemSettingsState{}, fmt.Errorf(
			"%w: revision %d changed during bootstrap",
			ErrSystemSettingsRevisionConflict,
			prepared.Revision,
		)
	}

	s.mu.Lock()
	s.effective = prepared.Settings
	s.effectiveRevision = prepared.Revision
	s.bootstrapped = true
	s.mu.Unlock()
	return s.stateFromModel(applied)
}
