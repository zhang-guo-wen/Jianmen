package store

import (
	"context"
	"os"
	"testing"

	"gorm.io/gorm"
	"jianmen/internal/config"
	"jianmen/internal/storage"
)

func TestCompactUsernameAuthIntegration(t *testing.T) {
	// Open the real database
	cfg := &config.Config{Admin: config.AdminConfig{Token: "dev-admin-token"}}
	db, err := storage.OpenOrCreateDB(cfg)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	s := NewDBStore(db, cfg.Admin.Token)
	user, err := s.Authenticate(context.Background(), "H000100001", "admin")
	if err != nil {
		t.Fatalf("AUTH FAILED: %v", err)
	}
	t.Logf("AUTH SUCCESS: username=%s target=%s", user.Username, user.RequestedTargetID)
	
	// Clean up test file
	os.Remove("../../data/bastion.db")
}
