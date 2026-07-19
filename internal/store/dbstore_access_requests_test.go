package store

import (
	"context"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/storage"
)

func TestFindActiveAccessRequestSelectsApprovalCoveringAllActions(t *testing.T) {
	db, err := storage.Open(storage.Config{
		Driver: storage.DriverSQLite,
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.AccessRequest{}); err != nil {
		t.Fatalf("migrate access requests: %v", err)
	}

	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	startedAt := now.Add(-time.Hour)
	longExpiry := now.Add(8 * time.Hour)
	matchingExpiry := now.Add(4 * time.Hour)
	requests := []model.AccessRequest{
		{
			ID: "longer-but-limited", RequesterID: "user-1",
			ResourceType: model.ResourceTypeHostAccount, ResourceID: "account-1",
			Protocol: "rdp", Status: model.AccessRequestApproved,
			ActionsJSON:    `["rdp:connect"]`,
			AccessStartsAt: &startedAt, AccessExpiresAt: &longExpiry,
		},
		{
			ID: "matching", RequesterID: "user-1",
			ResourceType: model.ResourceTypeHostAccount, ResourceID: "account-1",
			Protocol: "rdp", Status: model.AccessRequestApproved,
			ActionsJSON:    `["rdp:connect","rdp:file:upload"]`,
			AccessStartsAt: &startedAt, AccessExpiresAt: &matchingExpiry,
		},
	}
	if err := db.Create(&requests).Error; err != nil {
		t.Fatalf("create access requests: %v", err)
	}

	request, found, err := NewDBStore(db).FindActiveAccessRequest(
		context.Background(),
		"user-1",
		model.ResourceTypeHostAccount,
		"account-1",
		"rdp",
		now,
		[]string{rbac.ActionRDPConnect, rbac.ActionRDPFileUpload},
	)
	if err != nil {
		t.Fatalf("FindActiveAccessRequest() error = %v", err)
	}
	if !found || request.ID != "matching" {
		t.Fatalf("FindActiveAccessRequest() = (%q, %t), want matching", request.ID, found)
	}
}
