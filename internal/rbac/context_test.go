package rbac

import (
	"context"
	"errors"
	"testing"

	"jianmen/internal/model"
)

func TestCheckerHasPermissionContextPropagatesCancellation(t *testing.T) {
	db := newTestDB(t)
	seedRBAC(t, db, "u-context", []model.Permission{
		{ID: "p-context", Action: ActionSessionConnect, Effect: model.PermissionEffectAllow},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewChecker(db).HasPermissionContext(
		ctx,
		"u-context",
		ActionSessionConnect,
		"",
		"",
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("has permission error = %v, want context canceled", err)
	}
}

func TestResourceGrantCheckerHasGrantContextPropagatesCancellation(t *testing.T) {
	db := newTestDB(t)
	if err := db.Create(&model.ResourceGrant{
		ID:            "grant-context",
		PrincipalType: "user",
		PrincipalID:   "u-context",
		ResourceType:  model.ResourceTypeHostAccount,
		ResourceID:    "account-context",
		Effect:        model.PermissionEffectAllow,
	}).Error; err != nil {
		t.Fatalf("create grant: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewResourceGrantChecker(db).HasGrantContext(
		ctx,
		"u-context",
		model.ResourceTypeHostAccount,
		"account-context",
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("has grant error = %v, want context canceled", err)
	}
}
