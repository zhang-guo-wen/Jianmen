package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestListAuditDBQueryPreviewsEnforcesStoreBounds(t *testing.T) {
	repository, closeStore := newAuditRetentionTestStore(t)
	defer closeStore()

	now := time.Now().UTC()
	queries := make([]model.AuditDBQuery, 0, auditDBQueryPreviewMaxPageSize+1)
	for i := 0; i < auditDBQueryPreviewMaxPageSize+1; i++ {
		queries = append(queries, model.AuditDBQuery{
			ID: fmt.Sprintf("query-%03d", i), AuditSessionID: "session-bounded",
			Timestamp: now.Add(time.Duration(i) * time.Millisecond),
			SQLText:   fmt.Sprintf("SELECT %d", i),
		})
	}
	if err := repository.db.CreateInBatches(&queries, 50).Error; err != nil {
		t.Fatalf("create database queries: %v", err)
	}

	items, total, err := repository.ListAuditDBQueryPreviews(
		context.Background(),
		"session-bounded",
		AuditDBQueryPreviewParams{Limit: 10_000, Offset: -10},
	)
	if err != nil {
		t.Fatalf("list database query previews: %v", err)
	}
	if total != int64(len(queries)) || len(items) != auditDBQueryPreviewMaxPageSize {
		t.Fatalf("items = %d, total = %d; want %d, %d", len(items), total, auditDBQueryPreviewMaxPageSize, len(queries))
	}
	if items[0].SQLText != "SELECT 0" {
		t.Fatalf("negative offset was not normalized: first item = %#v", items[0])
	}
}

func TestListAuditDBQueryPreviewsSearchesOnlyBoundedPrefix(t *testing.T) {
	repository, closeStore := newAuditRetentionTestStore(t)
	defer closeStore()

	now := time.Now().UTC()
	queries := []model.AuditDBQuery{
		{
			ID: "query-prefix", AuditSessionID: "session-search", Timestamp: now,
			SQLText: "SELECT bounded_needle",
		},
		{
			ID: "query-suffix", AuditSessionID: "session-search", Timestamp: now.Add(time.Second),
			SQLText: strings.Repeat("x", auditDBQueryPreviewMaxCharacters) + " bounded_needle",
		},
	}
	if err := repository.db.Create(&queries).Error; err != nil {
		t.Fatalf("create database queries: %v", err)
	}

	items, total, err := repository.ListAuditDBQueryPreviews(
		context.Background(),
		"session-search",
		AuditDBQueryPreviewParams{Search: "BOUNDED_NEEDLE", Limit: 10},
	)
	if err != nil {
		t.Fatalf("search database query previews: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != "query-prefix" {
		t.Fatalf("bounded prefix search result = %#v, total = %d", items, total)
	}
}

func TestListAuditDBQueryPreviewsSearchTreatsLikeMetacharactersLiterally(t *testing.T) {
	repository, closeStore := newAuditRetentionTestStore(t)
	defer closeStore()

	now := time.Now().UTC()
	queries := []model.AuditDBQuery{
		{
			ID: "query-percent", AuditSessionID: "session-literal-search", Timestamp: now,
			SQLText: "SELECT 'discount 10%!'",
		},
		{
			ID: "query-underscore", AuditSessionID: "session-literal-search", Timestamp: now.Add(time.Second),
			SQLText: "SELECT customer_id",
		},
		{
			ID: "query-wildcard-decoy", AuditSessionID: "session-literal-search", Timestamp: now.Add(2 * time.Second),
			SQLText: "SELECT customerXid, 'discount 100'",
		},
	}
	if err := repository.db.Create(&queries).Error; err != nil {
		t.Fatalf("create database queries: %v", err)
	}

	tests := []struct {
		name   string
		search string
		wantID string
	}{
		{name: "percent", search: "10%!", wantID: "query-percent"},
		{name: "underscore", search: "customer_id", wantID: "query-underscore"},
		{name: "escape character", search: "%!", wantID: "query-percent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, total, err := repository.ListAuditDBQueryPreviews(
				context.Background(),
				"session-literal-search",
				AuditDBQueryPreviewParams{Search: tt.search, Limit: 10},
			)
			if err != nil {
				t.Fatalf("search database query previews: %v", err)
			}
			if total != 1 || len(items) != 1 || items[0].ID != tt.wantID {
				t.Fatalf("literal search result = %#v, total = %d, want %s", items, total, tt.wantID)
			}
		})
	}
}

func TestListAuditDBQueryPreviewsRejectsNilContext(t *testing.T) {
	repository, closeStore := newAuditRetentionTestStore(t)
	defer closeStore()

	if _, _, err := repository.ListAuditDBQueryPreviews(
		nil,
		"session-nil-context",
		AuditDBQueryPreviewParams{},
	); err == nil {
		t.Fatal("ListAuditDBQueryPreviews() error = nil, want nil context error")
	}
}
