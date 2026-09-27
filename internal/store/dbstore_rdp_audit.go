package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

func (s *DBStore) BeginRDPAuditSession(
	ctx context.Context,
	session *model.AuditSession,
	artifact *model.AuditArtifact,
) error {
	if session == nil {
		return errors.New("RDP audit session is required")
	}
	s.prepareAuditSessionLease(session)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(session).Error; err != nil {
			return fmt.Errorf("create RDP audit session: %w", err)
		}
		if artifact != nil {
			artifact.AuditSessionID = session.ID
			if err := tx.Create(artifact).Error; err != nil {
				return fmt.Errorf("create RDP recording index: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.trackAuditSessionLease(session)
	return nil
}

func (s *DBStore) ActivateRDPAuditSession(ctx context.Context, id string) error {
	result := s.db.WithContext(ctx).Model(&model.AuditSession{}).
		Where("id = ? AND state = ?", strings.TrimSpace(id), "started").
		Updates(map[string]any{
			"outcome": model.AuditOutcomeActive,
		})
	if result.Error != nil {
		return fmt.Errorf("activate RDP audit session: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("activate RDP audit session %q: %w", id, gorm.ErrRecordNotFound)
	}
	return nil
}
