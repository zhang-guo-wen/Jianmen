package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"jianmen/internal/model"
)

const (
	defaultAuditCleanupBatch    = 100
	maxAuditCleanupBatch        = 1000
	defaultAuditClaimStaleAfter = 15 * time.Minute
)

// AuditRetentionRepository is defined at the business-logic boundary. Its
// claim operation must persist cleanup intent before returning a session.
type AuditRetentionRepository interface {
	ClaimAuditSessionsForCleanup(
		ctx context.Context,
		cutoff time.Time,
		claimedAt time.Time,
		staleBefore time.Time,
		limit int,
	) ([]model.AuditSession, error)
	MarkAuditSessionCleanupFailed(ctx context.Context, id string, failedAt time.Time, message string) error
	DeleteClaimedAuditSession(ctx context.Context, id string) error
}

// AuditReplayStorage owns filesystem boundary checks and replay deletion.
type AuditReplayStorage interface {
	UsageBytes(ctx context.Context) (int64, error)
	DeleteSession(ctx context.Context, sessionDir string) (int64, error)
}

type AuditRetentionOptions struct {
	BatchSize       int
	MaxReplayBytes  int64
	ClaimStaleAfter time.Duration
}

type AuditRetentionResult struct {
	Claimed       int
	Deleted       int
	FreedBytes    int64
	UsageBytes    int64
	QuotaExceeded bool
}

type AuditRetentionService struct {
	repository      AuditRetentionRepository
	replayStorage   AuditReplayStorage
	policy          AuditPolicy
	batchSize       int
	maxReplayBytes  int64
	claimStaleAfter time.Duration
}

func NewAuditRetentionService(
	repository AuditRetentionRepository,
	replayStorage AuditReplayStorage,
	policy AuditPolicy,
	options AuditRetentionOptions,
) (*AuditRetentionService, error) {
	if repository == nil {
		return nil, errors.New("audit retention repository is required")
	}
	if replayStorage == nil {
		return nil, errors.New("audit replay storage is required")
	}
	if options.BatchSize == 0 {
		options.BatchSize = defaultAuditCleanupBatch
	}
	if options.BatchSize < 1 || options.BatchSize > maxAuditCleanupBatch {
		return nil, fmt.Errorf("audit cleanup batch size must be between 1 and %d", maxAuditCleanupBatch)
	}
	if options.MaxReplayBytes < 0 {
		return nil, errors.New("audit replay byte quota must not be negative")
	}
	if options.ClaimStaleAfter == 0 {
		options.ClaimStaleAfter = defaultAuditClaimStaleAfter
	}
	if options.ClaimStaleAfter < time.Minute {
		return nil, errors.New("audit cleanup claim stale duration must be at least one minute")
	}
	return &AuditRetentionService{
		repository:      repository,
		replayStorage:   replayStorage,
		policy:          policy,
		batchSize:       options.BatchSize,
		maxReplayBytes:  options.MaxReplayBytes,
		claimStaleAfter: options.ClaimStaleAfter,
	}, nil
}

// RunOnce performs one bounded cleanup pass. Retention is enforced first. If a
// byte quota is configured, the oldest remaining ended sessions are then
// removed until the quota is met or the batch is exhausted.
func (s *AuditRetentionService) RunOnce(ctx context.Context, now time.Time) (AuditRetentionResult, error) {
	if ctx == nil {
		return AuditRetentionResult{}, errors.New("run audit retention: nil context")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	result := AuditRetentionResult{}
	var cleanupErrors []error
	remaining := s.batchSize
	staleBefore := now.Add(-s.claimStaleAfter)

	expired, err := s.repository.ClaimAuditSessionsForCleanup(
		ctx,
		s.policy.RetentionCutoff(now),
		now,
		staleBefore,
		remaining,
	)
	if err != nil {
		return result, fmt.Errorf("claim expired audit sessions: %w", err)
	}
	for _, session := range expired {
		if remaining == 0 {
			break
		}
		result.Claimed++
		removed, deleted, cleanupErr := s.cleanupClaimedSession(ctx, now, session)
		result.FreedBytes += removed
		if deleted {
			result.Deleted++
		}
		if cleanupErr != nil {
			cleanupErrors = append(cleanupErrors, cleanupErr)
		}
		remaining--
	}

	if s.maxReplayBytes == 0 || remaining == 0 {
		return result, errors.Join(cleanupErrors...)
	}
	usage, err := s.replayStorage.UsageBytes(ctx)
	if err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("measure replay storage: %w", err))
		return result, errors.Join(cleanupErrors...)
	}
	result.UsageBytes = usage

	for usage > s.maxReplayBytes && remaining > 0 {
		result.QuotaExceeded = true
		candidates, claimErr := s.repository.ClaimAuditSessionsForCleanup(
			ctx,
			now,
			now,
			staleBefore,
			1,
		)
		if claimErr != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("claim quota audit session: %w", claimErr))
			break
		}
		if len(candidates) == 0 {
			break
		}

		session := candidates[0]
		result.Claimed++
		removed, deleted, cleanupErr := s.cleanupClaimedSession(ctx, now, session)
		result.FreedBytes += removed
		if removed >= usage {
			usage = 0
		} else {
			usage -= removed
		}
		if deleted {
			result.Deleted++
		}
		if cleanupErr != nil {
			cleanupErrors = append(cleanupErrors, cleanupErr)
			if removed == 0 {
				measuredUsage, measureErr := s.replayStorage.UsageBytes(ctx)
				if measureErr != nil {
					cleanupErrors = append(cleanupErrors, fmt.Errorf("remeasure replay storage after cleanup failure: %w", measureErr))
					break
				}
				usage = measuredUsage
			}
		}
		remaining--
	}
	result.UsageBytes = usage
	result.QuotaExceeded = usage > s.maxReplayBytes
	return result, errors.Join(cleanupErrors...)
}

func (s *AuditRetentionService) cleanupClaimedSession(
	ctx context.Context,
	now time.Time,
	session model.AuditSession,
) (removedBytes int64, deleted bool, err error) {
	removedBytes, err = s.replayStorage.DeleteSession(ctx, session.ReplayDir)
	if err != nil {
		deleteErr := fmt.Errorf("delete audit session %q replay: %w", session.ID, err)
		if markErr := s.repository.MarkAuditSessionCleanupFailed(ctx, session.ID, now, deleteErr.Error()); markErr != nil {
			return 0, false, errors.Join(deleteErr, fmt.Errorf("record audit session %q cleanup failure: %w", session.ID, markErr))
		}
		return 0, false, deleteErr
	}
	if err := s.repository.DeleteClaimedAuditSession(ctx, session.ID); err != nil {
		// The persisted pending claim is intentionally retained. A later pass
		// will treat the now-missing replay directory as already deleted.
		return removedBytes, false, fmt.Errorf("delete audit session %q database records: %w", session.ID, err)
	}
	return removedBytes, true, nil
}
