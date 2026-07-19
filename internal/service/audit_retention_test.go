package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
)

type fakeAuditRetentionRepository struct {
	sessions  []model.AuditSession
	events    *[]string
	deleted   []string
	failed    []string
	claimErr  error
	deleteErr error
	markErr   error
}

func (f *fakeAuditRetentionRepository) ClaimAuditSessionsForCleanup(
	_ context.Context,
	cutoff time.Time,
	claimedAt time.Time,
	_ time.Time,
	limit int,
) ([]model.AuditSession, error) {
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	var claimed []model.AuditSession
	for index := range f.sessions {
		session := &f.sessions[index]
		if len(claimed) == limit {
			break
		}
		if session.State != "ended" || session.EndedAt == nil || session.EndedAt.After(cutoff) {
			continue
		}
		if session.CleanupStatus != "" && session.CleanupStatus != "ready" {
			continue
		}
		session.CleanupStatus = "pending"
		session.CleanupAt = &claimedAt
		claimed = append(claimed, *session)
	}
	return claimed, nil
}

func (f *fakeAuditRetentionRepository) MarkAuditSessionCleanupFailed(
	_ context.Context,
	id string,
	_ time.Time,
	_ string,
) error {
	if f.events != nil {
		*f.events = append(*f.events, "failed:"+id)
	}
	f.failed = append(f.failed, id)
	if f.markErr != nil {
		return f.markErr
	}
	for index := range f.sessions {
		if f.sessions[index].ID == id {
			f.sessions[index].CleanupStatus = "failed"
		}
	}
	return nil
}

func (f *fakeAuditRetentionRepository) DeleteClaimedAuditSession(_ context.Context, id string) error {
	if f.events != nil {
		*f.events = append(*f.events, "database:"+id)
	}
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, id)
	for index := range f.sessions {
		if f.sessions[index].ID == id {
			f.sessions[index].State = "deleted"
		}
	}
	return nil
}

type fakeAuditReplayStorage struct {
	events    *[]string
	usage     int64
	sizes     map[string]int64
	deleteErr map[string]error
	usageErr  error
}

func (f *fakeAuditReplayStorage) UsageBytes(context.Context) (int64, error) {
	return f.usage, f.usageErr
}

func (f *fakeAuditReplayStorage) DeleteSession(_ context.Context, sessionDir string) (int64, error) {
	if f.events != nil {
		*f.events = append(*f.events, "replay:"+sessionDir)
	}
	if err := f.deleteErr[sessionDir]; err != nil {
		return 0, err
	}
	size := f.sizes[sessionDir]
	delete(f.sizes, sessionDir)
	return size, nil
}

func endedAuditSession(id, replayDir string, endedAt time.Time) model.AuditSession {
	return model.AuditSession{
		ID:            id,
		State:         "ended",
		EndedAt:       &endedAt,
		ReplayDir:     replayDir,
		CleanupStatus: "ready",
	}
}

func TestAuditRetentionDeletesReplayBeforeDatabase(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	var events []string
	repository := &fakeAuditRetentionRepository{
		sessions: []model.AuditSession{
			endedAuditSession("expired", "ssh/expired", now.AddDate(0, 0, -31)),
			endedAuditSession("retained", "ssh/retained", now.AddDate(0, 0, -1)),
			{ID: "active", State: "started", ReplayDir: "ssh/active", StartedAt: now.AddDate(0, 0, -60)},
		},
		events: &events,
	}
	replay := &fakeAuditReplayStorage{
		events: &events,
		sizes:  map[string]int64{"ssh/expired": 41},
	}
	cleaner, err := NewAuditRetentionService(repository, replay, NewAuditPolicy(30, false), AuditRetentionOptions{BatchSize: 10})
	if err != nil {
		t.Fatalf("NewAuditRetentionService: %v", err)
	}

	result, err := cleaner.RunOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !slices.Equal(events, []string{"replay:ssh/expired", "database:expired"}) {
		t.Fatalf("cleanup events = %v", events)
	}
	if !slices.Equal(repository.deleted, []string{"expired"}) {
		t.Fatalf("deleted sessions = %v", repository.deleted)
	}
	if result.Deleted != 1 || result.FreedBytes != 41 {
		t.Fatalf("result = %+v", result)
	}
}

func TestAuditRetentionKeepsDatabaseWhenReplayDeletionFails(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	var events []string
	repository := &fakeAuditRetentionRepository{
		sessions: []model.AuditSession{
			endedAuditSession("expired", "ssh/expired", now.AddDate(0, 0, -31)),
		},
		events: &events,
	}
	replay := &fakeAuditReplayStorage{
		events:    &events,
		sizes:     map[string]int64{},
		deleteErr: map[string]error{"ssh/expired": errors.New("access denied")},
	}
	cleaner, err := NewAuditRetentionService(repository, replay, NewAuditPolicy(30, false), AuditRetentionOptions{BatchSize: 10})
	if err != nil {
		t.Fatalf("NewAuditRetentionService: %v", err)
	}

	result, err := cleaner.RunOnce(context.Background(), now)
	if err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("RunOnce error = %v", err)
	}
	if !slices.Equal(events, []string{"replay:ssh/expired", "failed:expired"}) {
		t.Fatalf("cleanup events = %v", events)
	}
	if len(repository.deleted) != 0 || !slices.Equal(repository.failed, []string{"expired"}) {
		t.Fatalf("deleted=%v failed=%v", repository.deleted, repository.failed)
	}
	if result.Deleted != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestAuditRetentionLeavesPendingClaimWhenDatabaseDeletionFails(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	repository := &fakeAuditRetentionRepository{
		sessions:  []model.AuditSession{endedAuditSession("expired", "ssh/expired", now.AddDate(0, 0, -31))},
		deleteErr: errors.New("database unavailable"),
	}
	replay := &fakeAuditReplayStorage{sizes: map[string]int64{"ssh/expired": 23}}
	cleaner, err := NewAuditRetentionService(repository, replay, NewAuditPolicy(30, false), AuditRetentionOptions{BatchSize: 10})
	if err != nil {
		t.Fatalf("NewAuditRetentionService: %v", err)
	}

	result, err := cleaner.RunOnce(context.Background(), now)
	if err == nil || !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("RunOnce error = %v", err)
	}
	if repository.sessions[0].CleanupStatus != "pending" {
		t.Fatalf("cleanup status = %q", repository.sessions[0].CleanupStatus)
	}
	if len(repository.failed) != 0 || result.FreedBytes != 23 || result.Deleted != 0 {
		t.Fatalf("failed=%v result=%+v", repository.failed, result)
	}
}

func TestAuditRetentionEnforcesByteQuotaOldestFirst(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	repository := &fakeAuditRetentionRepository{
		sessions: []model.AuditSession{
			endedAuditSession("oldest", "db/oldest", now.AddDate(0, 0, -20)),
			endedAuditSession("newer", "db/newer", now.AddDate(0, 0, -10)),
			endedAuditSession("newest", "db/newest", now.AddDate(0, 0, -1)),
		},
	}
	replay := &fakeAuditReplayStorage{
		usage: 200,
		sizes: map[string]int64{
			"db/oldest": 70,
			"db/newer":  50,
			"db/newest": 80,
		},
	}
	cleaner, err := NewAuditRetentionService(
		repository,
		replay,
		NewAuditPolicy(3650, false),
		AuditRetentionOptions{BatchSize: 10, MaxReplayBytes: 100},
	)
	if err != nil {
		t.Fatalf("NewAuditRetentionService: %v", err)
	}

	result, err := cleaner.RunOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !slices.Equal(repository.deleted, []string{"oldest", "newer"}) {
		t.Fatalf("deleted sessions = %v", repository.deleted)
	}
	if result.Deleted != 2 || result.FreedBytes != 120 || result.UsageBytes != 80 || result.QuotaExceeded {
		t.Fatalf("result = %+v", result)
	}
}

func TestAuditRetentionQuotaHonorsBatchLimit(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	repository := &fakeAuditRetentionRepository{
		sessions: []model.AuditSession{
			endedAuditSession("one", "db/one", now.AddDate(0, 0, -3)),
			endedAuditSession("two", "db/two", now.AddDate(0, 0, -2)),
			endedAuditSession("three", "db/three", now.AddDate(0, 0, -1)),
		},
	}
	replay := &fakeAuditReplayStorage{
		usage: 300,
		sizes: map[string]int64{"db/one": 50, "db/two": 50, "db/three": 50},
	}
	cleaner, err := NewAuditRetentionService(
		repository,
		replay,
		NewAuditPolicy(3650, false),
		AuditRetentionOptions{BatchSize: 2, MaxReplayBytes: 100},
	)
	if err != nil {
		t.Fatalf("NewAuditRetentionService: %v", err)
	}

	result, err := cleaner.RunOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(repository.deleted) != 2 || result.Claimed != 2 || !result.QuotaExceeded || result.UsageBytes != 200 {
		t.Fatalf("deleted=%v result=%+v", repository.deleted, result)
	}
}
