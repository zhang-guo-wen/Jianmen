package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestDeleteUserTombstoneBlocksAuthenticationAndIdentity(t *testing.T) {
	repository, db := newActiveMarkerSecurityStore(t)
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("correct horse"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	token := "user-api-token"
	tokenDigest := sha256.Sum256([]byte(token))
	user := model.User{
		ID:           "tombstone-user",
		Username:     "tombstone-user",
		PasswordHash: string(passwordHash),
		TokenHash:    hex.EncodeToString(tokenDigest[:]),
		Status:       "active",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	ctx := model.WithAuditUserID(context.Background(), "admin-actor")
	if err := repository.DeleteUser(ctx, user); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var tombstone model.User
	if err := db.First(&tombstone, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("load user tombstone: %v", err)
	}
	if tombstone.ActiveMarker != nil {
		t.Fatalf("active_marker = %v, want NULL", tombstone.ActiveMarker)
	}
	if tombstone.Status != "disabled" {
		t.Fatalf("status = %q, want disabled", tombstone.Status)
	}
	if tombstone.UpdatedBy != "admin-actor" {
		t.Fatalf("updated_by = %q, want admin-actor", tombstone.UpdatedBy)
	}

	if _, err := repository.AuthenticateDirect(ctx, user.Username, "correct horse"); err == nil {
		t.Fatal("password authentication accepted a deleted user")
	}
	if _, err := repository.Authenticate(ctx, user.Username, token); err == nil {
		t.Fatal("token authentication accepted a deleted user")
	}
	if subject, found, err := repository.FindIdentitySubject(ctx, user.ID); err != nil || found {
		t.Fatalf("identity by id = (%#v, %v, %v), want not found", subject, found, err)
	}
	if subject, found, err := repository.FindIdentitySubjectByTokenHash(ctx, user.TokenHash); err != nil || found {
		t.Fatalf("identity by token = (%#v, %v, %v), want not found", subject, found, err)
	}
}

func TestDatabaseAccountReadsFailClosedForAccountAndParentTombstones(t *testing.T) {
	repository, db := newActiveMarkerSecurityStore(t)
	instance := model.DatabaseInstance{
		ID: "db-parent", Name: "db-parent", Protocol: "mysql",
		Address: "127.0.0.1", Port: 3306, Status: "active",
	}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create database instance: %v", err)
	}
	deletedAccount := model.DatabaseAccount{
		ID: "deleted-account", InstanceID: instance.ID, UniqueName: "deleted-account",
		Username: "deleted_user", Status: "active", ResourceID: "D101",
	}
	parentHiddenAccount := model.DatabaseAccount{
		ID: "parent-hidden-account", InstanceID: instance.ID, UniqueName: "parent-hidden-account",
		Username: "parent_hidden", Status: "active", ResourceID: "D102",
	}
	if err := db.Create(&[]model.DatabaseAccount{deletedAccount, parentHiddenAccount}).Error; err != nil {
		t.Fatalf("create database accounts: %v", err)
	}

	ctx := model.WithAuditUserID(context.Background(), "database-admin")
	if err := repository.DeleteDatabaseAccount(ctx, deletedAccount.ID); err != nil {
		t.Fatalf("delete database account: %v", err)
	}
	var tombstone model.DatabaseAccount
	if err := db.First(&tombstone, "id = ?", deletedAccount.ID).Error; err != nil {
		t.Fatalf("load database account tombstone: %v", err)
	}
	if tombstone.ActiveMarker != nil || tombstone.Status != "disabled" || tombstone.UpdatedBy != "database-admin" {
		t.Fatalf("database account tombstone = %#v", tombstone)
	}
	if account, found, err := repository.FindActiveDatabaseAccount(ctx, deletedAccount.ID); err != nil || found {
		t.Fatalf("deleted account lookup = (%#v, %v, %v), want not found", account, found, err)
	}

	if result := softDeleteWhere(ctx, db, "database_instances", "id = ?", instance.ID); result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("soft-delete parent instance = rows %d, error %v", result.RowsAffected, result.Error)
	}
	if account, found, err := repository.FindActiveDatabaseAccount(ctx, parentHiddenAccount.ID); err != nil || found {
		t.Fatalf("account under deleted parent = (%#v, %v, %v), want not found", account, found, err)
	}
	if _, err := repository.DatabaseAccountProbePassword(ctx, parentHiddenAccount.ID); err == nil {
		t.Fatal("password probe exposed an account under a deleted parent")
	}
}

func newActiveMarkerSecurityStore(t *testing.T) (*DBStore, *gorm.DB) {
	t.Helper()
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return NewDBStore(db), db
}
