package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/service"
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
	if err := db.Create(&model.TemporaryAccount{
		ID:        "ai-temporary",
		SessionID: "ai-session",
		Type:      model.TemporaryAccountTypeAI,
		Username:  "ai-agent",
		Status:    "active",
	}).Error; err != nil {
		t.Fatalf("create temporary account: %v", err)
	}

	now := time.Now().UTC()
	token := model.AIAccessToken{
		ID: "ai-token", UserID: "ai-user", TemporaryAccountID: "ai-temporary", Name: "agent",
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

func TestAIAccessTokenConcurrentRefreshHasSingleWinner(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("open SQL database: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	now := time.Now().UTC()
	user := model.User{ID: "refresh-user", Username: "refresh-user", Status: "active"}
	account := model.TemporaryAccount{
		ID: "refresh-temporary", SessionID: "refresh-session", Type: model.TemporaryAccountTypeAI,
		Username: "refresh-agent", AuthorizedUserID: user.ID, Status: "active", StartsAt: now.Add(-time.Hour),
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create temporary account: %v", err)
	}
	issued, err := service.IssueAIAccessToken(now, time.Hour, 24*time.Hour)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	repository := NewDBStore(db)
	if err := repository.CreateAIAccessToken(context.Background(), model.AIAccessToken{
		ID: "refresh-token", UserID: user.ID, TemporaryAccountID: account.ID, Name: "agent",
		AccessTokenHash: issued.AccessTokenHash, RefreshTokenHash: issued.RefreshTokenHash,
		AccessExpiresAt: issued.AccessExpiresAt, RefreshExpiresAt: issued.RefreshExpiresAt,
	}); err != nil {
		t.Fatalf("create AI token: %v", err)
	}
	tokenService, err := service.NewAIAccessTokenService(repository)
	if err != nil {
		t.Fatalf("new token service: %v", err)
	}

	const callers = 12
	type refreshResult struct {
		value service.RefreshedAIAccessToken
		err   error
	}
	results := make([]refreshResult, callers)
	start := make(chan struct{})
	var group sync.WaitGroup
	for index := range results {
		index := index
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			results[index].value, results[index].err = tokenService.Refresh(
				context.Background(), issued.RefreshToken, now, time.Hour, 24*time.Hour,
			)
		}()
	}
	close(start)
	group.Wait()

	successes := 0
	var winner service.RefreshedAIAccessToken
	for index, result := range results {
		if result.err == nil {
			successes++
			winner = result.value
			continue
		}
		if !errors.Is(result.err, ErrAIAccessTokenInvalid) {
			t.Fatalf("refresh %d error = %v, want invalid", index, result.err)
		}
		if result.value.AccessToken != "" || result.value.RefreshToken != "" {
			t.Fatalf("refresh %d returned plaintext on failure: %#v", index, result.value)
		}
	}
	if successes != 1 {
		t.Fatalf("successful refreshes = %d, want 1", successes)
	}
	if winner.AccessToken == "" || winner.RefreshToken == "" {
		t.Fatalf("winner lacks credentials: %#v", winner)
	}
	if _, err := tokenService.Refresh(
		context.Background(), issued.RefreshToken, now, time.Hour, 24*time.Hour,
	); !errors.Is(err, ErrAIAccessTokenInvalid) {
		t.Fatalf("old refresh token reused after race: %v", err)
	}

	var stored model.AIAccessToken
	if err := db.First(&stored, "id = ?", "refresh-token").Error; err != nil {
		t.Fatalf("load rotated token: %v", err)
	}
	if stored.AccessTokenHash != service.HashAIAccessToken(winner.AccessToken) ||
		stored.RefreshTokenHash != service.HashAIAccessToken(winner.RefreshToken) {
		t.Fatal("database hashes do not match the single successful rotation")
	}
}
