package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestCreateAuditDBQueryPreCanceledContextWritesNoRow(t *testing.T) {
	repository, closeStore := newAuditRetentionTestStore(t)
	defer closeStore()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	query := &model.AuditDBQuery{
		ID:             "query-canceled",
		AuditSessionID: "session-canceled",
		Timestamp:      time.Now().UTC(),
		SQLText:        "SELECT 1",
	}
	if err := repository.CreateAuditDBQuery(ctx, query); !errors.Is(err, context.Canceled) {
		t.Fatalf("CreateAuditDBQuery() error = %v, want context canceled", err)
	}

	var count int64
	if err := repository.db.Model(&model.AuditDBQuery{}).
		Where("id = ?", query.ID).
		Count(&count).Error; err != nil {
		t.Fatalf("count canceled database audit query: %v", err)
	}
	if count != 0 {
		t.Fatalf("canceled database audit query rows = %d, want 0", count)
	}
}

func TestCompleteAuditDBQueryPersistsResultAndPreview(t *testing.T) {
	repository, closeStore := newAuditRetentionTestStore(t)
	defer closeStore()

	query := &model.AuditDBQuery{
		ID:             "query-result",
		AuditSessionID: "session-result",
		Timestamp:      time.Now().UTC(),
		SQLText:        "UPDATE jobs SET state = 'done'",
	}
	if err := repository.CreateAuditDBQuery(context.Background(), query); err != nil {
		t.Fatalf("CreateAuditDBQuery() error = %v", err)
	}
	if query.Status != model.AuditDBQueryStatusUnknown {
		t.Fatalf("initial status = %q, want unknown", query.Status)
	}
	rowsAffected := int64(0)
	if err := repository.CompleteAuditDBQuery(
		context.Background(),
		query.ID,
		model.AuditDBQueryResult{
			DurationMs:   17,
			Status:       model.AuditDBQueryStatusError,
			ErrorCode:    "23505",
			ErrorMessage: "postgres upstream error",
			RowsAffected: &rowsAffected,
		},
	); err != nil {
		t.Fatalf("CompleteAuditDBQuery() error = %v", err)
	}

	items, total, err := repository.ListAuditDBQueryPreviews(
		context.Background(),
		query.AuditSessionID,
		AuditDBQueryPreviewParams{Limit: 10},
	)
	if err != nil {
		t.Fatalf("ListAuditDBQueryPreviews() error = %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("items = %#v, total = %d", items, total)
	}
	got := items[0]
	if got.Status != model.AuditDBQueryStatusError ||
		got.ErrorCode != "23505" ||
		got.ErrorMessage != "postgres upstream error" ||
		got.DurationMs != 17 ||
		got.RowsAffected == nil ||
		*got.RowsAffected != 0 ||
		got.Rows != nil {
		t.Fatalf("preview result = %#v", got)
	}
}

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
