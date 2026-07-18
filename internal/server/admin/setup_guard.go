package admin

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

func (s *Server) acquireSetupSlot(ctx context.Context) (func(), error) {
	s.setupOnce.Do(func() {
		s.setupSlot = make(chan struct{}, 1)
		s.setupSlot <- struct{}{}
	})

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("acquire setup slot: %w", ctx.Err())
	case <-s.setupSlot:
	}

	var releaseOnce sync.Once
	return func() {
		releaseOnce.Do(func() {
			s.setupSlot <- struct{}{}
		})
	}, nil
}

func runSetupTransaction(
	ctx context.Context,
	db *gorm.DB,
	operation func(*gorm.DB) error,
) error {
	const maxAttempts = 8
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("run setup transaction: %w", err)
		}
		lastErr = db.WithContext(ctx).Transaction(operation)
		if lastErr == nil {
			return nil
		}
		if !isRetryableSetupError(lastErr) {
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
			return fmt.Errorf("retry setup transaction: %w", ctx.Err())
		case <-timer.C:
		}
	}
	return fmt.Errorf("setup transaction retries exhausted: %w", lastErr)
}

func isRetryableSetupError(err error) bool {
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"database is locked",
		"sqlite_busy",
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
