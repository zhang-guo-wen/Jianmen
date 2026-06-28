package store

import (
	"context"
	"os"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"jianmen/internal/config"
	"jianmen/internal/crypto"
	"jianmen/internal/model"
	"jianmen/internal/storage"
	"jianmen/internal/util"
)

func TestCompactUsernameAuthIntegration(t *testing.T) {
	dbPath := "../../data/bastion_test.db"
	// Clean up from previous failed runs
	os.Remove(dbPath)
	os.Remove(dbPath + "-wal")
	os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath)

	// Initialize encryption with a test data directory
	tmpDir := t.TempDir()
	if _, err := crypto.Init(tmpDir); err != nil {
		t.Fatalf("crypto init: %v", err)
	}

	cfg := &config.Config{Admin: config.AdminConfig{Token: "dev-admin-token"}}
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: dbPath})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := storage.BootstrapMetadata(db, cfg); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	// Create a test user with bcrypt-hashed password "admin"
	pwHash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	testUser := model.User{
		ID:           "test-compact-auth-user",
		Username:     "compact-test-user",
		PasswordHash: string(pwHash),
		Status:       "active",
	}
	if err := db.Create(&testUser).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create an active session with session_id "00001"
	session := model.UserSession{
		UserID:    "test-compact-auth-user",
		SessionID: "00001",
		Status:    "active",
		ExpiresAt: ptrTime(time.Now().Add(24 * time.Hour)),
	}
	if err := db.Create(&session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Create a host and host account with resource_id "0001"
	hostID := "test-compact-auth-host"
	acctID := "test-compact-auth-acct"
	host := model.Host{
		ID:      hostID,
		Name:    "compact-test-host",
		Address: "127.0.0.1:22",
		Status:  "enabled",
	}
	if err := db.Create(&host).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}
	acct := model.HostAccount{
		ID:          acctID,
		HostID:      hostID,
		Username:    "root",
		AuthType:    "password",
		Password:    model.NewEncryptedField("test"),
		Status:      "active",
		ResourceSeq: 1,
		ResourceID:  util.ResourceIDFromSeq(util.PrefixHost, 1),
	}
	if err := db.Create(&acct).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}

	// Build compact username using the actual resource_id
	compactUser := "H" + acct.ResourceID + "00001"

	s := NewDBStore(db, cfg.Admin.Token)
	user, err := s.Authenticate(context.Background(), compactUser, "admin")
	if err != nil {
		t.Fatalf("AUTH FAILED: %v", err)
	}
	t.Logf("AUTH SUCCESS: username=%s target=%s", user.Username, user.RequestedTargetID)
}

func ptrTime(t time.Time) *time.Time { return &t }
