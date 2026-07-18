package store

import (
	"context"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestAIAccessTokenPersistsOnlyHashes(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	if err := db.Create(&model.User{ID: "ai-user", Username: "ai-user", Status: "active"}).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	now := time.Now().UTC()
	token := model.AIAccessToken{
		ID: "ai-token", UserID: "ai-user", Name: "agent",
		AccessTokenHash: "access-hash", RefreshTokenHash: "refresh-hash",
		AccessExpiresAt: now.Add(time.Hour), RefreshExpiresAt: now.Add(24 * time.Hour),
	}
	if err := NewDBStore(db).CreateAIAccessToken(context.Background(), token); err != nil {
		t.Fatalf("create token with hashes only: %v", err)
	}
	for _, column := range []string{"access_token", "refresh_token"} {
		if db.Migrator().HasColumn("ai_access_tokens", column) {
			t.Fatalf("reversible secret column %q exists", column)
		}
	}
}
