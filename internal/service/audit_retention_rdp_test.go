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

type fakeAuditObjectStorage struct {
	events    *[]string
	deleteErr error
}

func (f *fakeAuditObjectStorage) Delete(_ context.Context, key string) error {
	if f.events != nil {
		*f.events = append(*f.events, "object:"+key)
	}
	return f.deleteErr
}

func TestAuditRetentionDeletesRDPObjectBeforeDatabase(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	var events []string
	repository := &fakeAuditRetentionRepository{
		sessions: []model.AuditSession{
			endedAuditSession("rdp-expired", "", now.AddDate(0, 0, -31)),
		},
		events: &events,
		artifacts: map[string][]model.AuditArtifact{
			"rdp-expired": {
				{ID: "artifact-1", ObjectKey: "rdp/recording.guac"},
			},
		},
	}
	cleaner, err := NewAuditRetentionService(
		repository,
		&fakeAuditReplayStorage{events: &events},
		NewAuditPolicy(30, false),
		AuditRetentionOptions{
			BatchSize:     10,
			ObjectStorage: &fakeAuditObjectStorage{events: &events},
		},
	)
	if err != nil {
		t.Fatalf("NewAuditRetentionService: %v", err)
	}

	result, err := cleaner.RunOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !slices.Equal(events, []string{
		"object:rdp/recording.guac",
		"database:rdp-expired",
	}) {
		t.Fatalf("cleanup events = %v", events)
	}
	if result.Deleted != 1 || result.FreedBytes != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestAuditRetentionKeepsRDPIndexWhenObjectDeletionFails(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	var events []string
	repository := &fakeAuditRetentionRepository{
		sessions: []model.AuditSession{
			endedAuditSession("rdp-expired", "", now.AddDate(0, 0, -31)),
		},
		events: &events,
		artifacts: map[string][]model.AuditArtifact{
			"rdp-expired": {
				{ID: "artifact-1", ObjectKey: "rdp/recording.guac"},
			},
		},
	}
	cleaner, err := NewAuditRetentionService(
		repository,
		&fakeAuditReplayStorage{events: &events},
		NewAuditPolicy(30, false),
		AuditRetentionOptions{
			BatchSize: 10,
			ObjectStorage: &fakeAuditObjectStorage{
				events:    &events,
				deleteErr: errors.New("object store unavailable"),
			},
		},
	)
	if err != nil {
		t.Fatalf("NewAuditRetentionService: %v", err)
	}

	result, err := cleaner.RunOnce(context.Background(), now)
	if err == nil || !strings.Contains(err.Error(), "object store unavailable") {
		t.Fatalf("RunOnce error = %v", err)
	}
	if !slices.Equal(events, []string{
		"object:rdp/recording.guac",
		"failed:rdp-expired",
	}) {
		t.Fatalf("cleanup events = %v", events)
	}
	if len(repository.deleted) != 0 || result.Deleted != 0 {
		t.Fatalf("deleted=%v result=%+v", repository.deleted, result)
	}
}

func TestAuditRetentionDeletesDeniedRDPWithoutReplayOrArtifact(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	var events []string
	repository := &fakeAuditRetentionRepository{
		sessions: []model.AuditSession{
			endedAuditSession("rdp-denied", "", now.AddDate(0, 0, -31)),
		},
		events: &events,
	}
	cleaner, err := NewAuditRetentionService(
		repository,
		&fakeAuditReplayStorage{events: &events},
		NewAuditPolicy(30, false),
		AuditRetentionOptions{BatchSize: 10},
	)
	if err != nil {
		t.Fatalf("NewAuditRetentionService: %v", err)
	}

	result, err := cleaner.RunOnce(context.Background(), now)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !slices.Equal(events, []string{"database:rdp-denied"}) {
		t.Fatalf("cleanup events = %v", events)
	}
	if result.Deleted != 1 {
		t.Fatalf("result = %+v", result)
	}
}
