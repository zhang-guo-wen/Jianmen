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
	"jianmen/internal/service"
)

var sqliteBrowserSessionWriteSlot = func() chan struct{} {
	slot := make(chan struct{}, 1)
	slot <- struct{}{}
	return slot
}()

func (s *DBStore) CreateAdminSession(ctx context.Context, session model.AdminSession) error {
	return s.withBrowserSessionWrite(ctx, func() error {
		return s.db.WithContext(ctx).Create(&session).Error
	})
}

func (s *DBStore) FindActiveAdminSessionBySecretHash(ctx context.Context, secretHash string, now time.Time) (service.BrowserSessionSubject, bool, error) {
	var session model.AdminSession
	err := s.db.WithContext(ctx).Scopes(ActiveScope).
		Where("secret_hash = ? AND revoked_at IS NULL AND expires_at > ?", strings.TrimSpace(secretHash), now).
		First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return service.BrowserSessionSubject{}, false, nil
	}
	if err != nil {
		return service.BrowserSessionSubject{}, false, fmt.Errorf("find active admin session: %w", err)
	}
	return service.BrowserSessionSubject{SessionID: session.ID, UserID: session.UserID, CSRFHash: session.CSRFHash}, true, nil
}

func (s *DBStore) RevokeAdminSession(ctx context.Context, sessionID string, now time.Time) error {
	return s.withBrowserSessionWrite(ctx, func() error {
		return s.db.WithContext(ctx).Model(&model.AdminSession{}).Scopes(ActiveScope).
			Where("id = ? AND revoked_at IS NULL", strings.TrimSpace(sessionID)).
			Update("revoked_at", now).Error
	})
}

func (s *DBStore) CreateWebSocketTicket(ctx context.Context, ticket model.WebSocketTicket) error {
	return s.withBrowserSessionWrite(ctx, func() error {
		return s.db.WithContext(ctx).Create(&ticket).Error
	})
}

func (s *DBStore) ConsumeWebSocketTicket(ctx context.Context, secretHash, purpose, targetID string, now time.Time) (service.WebSocketTicketSubject, bool, error) {
	var subject service.WebSocketTicketSubject
	err := s.withBrowserSessionWrite(ctx, func() error {
		var found bool
		var err error
		subject, found, err = s.consumeWebSocketTicketOnce(ctx, secretHash, purpose, targetID, now)
		if err == nil && !found {
			return gorm.ErrRecordNotFound
		}
		return err
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return service.WebSocketTicketSubject{}, false, nil
	}
	if err != nil {
		return service.WebSocketTicketSubject{}, false, fmt.Errorf("consume websocket ticket: %w", err)
	}
	return subject, true, nil
}

func (s *DBStore) consumeWebSocketTicketOnce(ctx context.Context, secretHash, purpose, targetID string, now time.Time) (service.WebSocketTicketSubject, bool, error) {
	var ticket model.WebSocketTicket
	var session model.AdminSession
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ticketQuery := tx.Scopes(ActiveScope).Where(
			"secret_hash = ? AND purpose = ? AND target_id = ? AND consumed_at IS NULL AND expires_at > ?",
			strings.TrimSpace(secretHash), strings.TrimSpace(purpose), strings.TrimSpace(targetID), now,
		)
		if s.db.Dialector.Name() != "sqlite" {
			ticketQuery = ticketQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := ticketQuery.First(&ticket).Error; err != nil {
			return err
		}
		sessionQuery := tx.Scopes(ActiveScope).Where("id = ? AND revoked_at IS NULL AND expires_at > ?", ticket.SessionID, now)
		if s.db.Dialector.Name() != "sqlite" {
			sessionQuery = sessionQuery.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := sessionQuery.First(&session).Error; err != nil {
			return err
		}
		activeSession := tx.Model(&model.AdminSession{}).Scopes(ActiveScope).
			Select("1").
			Where("id = ? AND revoked_at IS NULL AND expires_at > ?", ticket.SessionID, now)
		result := tx.Model(&model.WebSocketTicket{}).Scopes(ActiveScope).
			Where("id = ? AND consumed_at IS NULL", ticket.ID).
			Where("EXISTS (?)", activeSession).
			Update("consumed_at", now)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return service.WebSocketTicketSubject{}, false, nil
	}
	if err != nil {
		return service.WebSocketTicketSubject{}, false, err
	}
	return service.WebSocketTicketSubject{
		BrowserSessionSubject: service.BrowserSessionSubject{
			SessionID: session.ID,
			UserID:    session.UserID,
			CSRFHash:  session.CSRFHash,
		},
		Purpose:      ticket.Purpose,
		TargetID:     ticket.TargetID,
		ConnectionID: ticket.ConnectionID,
	}, true, nil
}

func (s *DBStore) withBrowserSessionWrite(ctx context.Context, operation func() error) error {
	if s.db.Dialector.Name() == "sqlite" {
		select {
		case <-ctx.Done():
			return fmt.Errorf("serialize browser session write: %w", ctx.Err())
		case <-sqliteBrowserSessionWriteSlot:
		}
		defer func() { sqliteBrowserSessionWriteSlot <- struct{}{} }()
	}

	const maxAttempts = 8
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = operation()
		if lastErr == nil || !isRetryableBrowserSessionError(lastErr) {
			return lastErr
		}
		delay := time.Duration(5*(1<<attempt)) * time.Millisecond
		if delay > 100*time.Millisecond {
			delay = 100 * time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("browser session write retries exhausted: %w", lastErr)
}

func isRetryableBrowserSessionError(err error) bool {
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"database is locked",
		"database table is locked",
		"sqlite_busy",
		"sqlite_locked",
		"deadlock",
		"serialization failure",
		"could not serialize",
		"lock wait timeout",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
