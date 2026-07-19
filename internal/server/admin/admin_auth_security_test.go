package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/service"
	"jianmen/internal/store"

	"gorm.io/gorm"
)

func TestLoginRejectsUnknownWrongDisabledAndExpiredCredentialsUniformly(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	passwordHash, err := hashPassword("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	expiredAt := time.Now().UTC().Add(-time.Minute)
	users := []model.User{
		{ID: "active", Username: "active", PasswordHash: passwordHash, Status: "active"},
		{ID: "disabled", Username: "disabled", PasswordHash: passwordHash, Status: "disabled"},
		{ID: "expired", Username: "expired", PasswordHash: passwordHash, Status: "active", ExpiresAt: &expiredAt},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		username string
		password string
	}{
		{username: "missing", password: "correct-password"},
		{username: "active", password: "wrong-password"},
		{username: "disabled", password: "correct-password"},
		{username: "expired", password: "correct-password"},
	}
	for _, testCase := range cases {
		t.Run(testCase.username, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(
				`{"username":"`+testCase.username+`","password":"`+testCase.password+`","captcha_payload":"verified"}`,
			))
			response := httptest.NewRecorder()
			server.handleLogin(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusUnauthorized, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), "invalid username or password") {
				t.Fatalf("credential failure disclosed a distinct reason: %s", response.Body.String())
			}
			if len(response.Result().Cookies()) != 0 {
				t.Fatalf("credential failure returned cookies: %#v", response.Result().Cookies())
			}
		})
	}
}

type failingAdminLoginStateRepository struct {
	service.AdminAuthRepository
	err error
}

func (r failingAdminLoginStateRepository) PersistAdminLoginState(
	context.Context,
	string,
	string,
	time.Time,
) error {
	return r.err
}

func TestLoginStateRepositoryFailureDoesNotIssueCookie(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	passwordHash, err := hashPassword("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	user := model.User{ID: "persist-failure", Username: "persist-failure", PasswordHash: passwordHash, Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	repository := failingAdminLoginStateRepository{
		AdminAuthRepository: store.NewDBStore(db),
		err:                 errors.New("injected persistence failure"),
	}
	keyReader, err := store.NewFileAdminEncryptionKeyReader(server.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	adminAuth, err := service.NewAdminAuthService(repository, server.browserSessions, keyReader)
	if err != nil {
		t.Fatal(err)
	}
	server.adminAuth = adminAuth

	request := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(
		`{"username":"persist-failure","password":"correct-password","captcha_payload":"verified"}`,
	))
	response := httptest.NewRecorder()
	server.handleLogin(response, request)

	if response.Code != http.StatusInternalServerError || len(response.Result().Cookies()) != 0 {
		t.Fatalf("response = %d cookies=%#v body=%s", response.Code, response.Result().Cookies(), response.Body.String())
	}
	var sessions int64
	if err := db.Model(&model.AdminSession{}).Where("user_id = ?", user.ID).Count(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf("sessions = %d, want 0", sessions)
	}
}

func TestEncryptionKeyClaimConcurrentSingleWinnerAndRejectsUnauthorized(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	users := []model.User{
		{ID: "claim-admin", Username: "claim-admin", Status: "active", IsSuperAdmin: true},
		{ID: "claim-regular", Username: "claim-regular", Status: "active"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := server.adminAuth.ClaimEncryptionKey(context.Background(), "claim-regular"); !errors.Is(err, service.ErrAdminEncryptionKeyDenied) {
		t.Fatalf("regular claim error = %v, want access denied", err)
	}

	const workers = 8
	start := make(chan struct{})
	results := make(chan error, workers)
	var wait sync.WaitGroup
	for index := 0; index < workers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := server.adminAuth.ClaimEncryptionKey(context.Background(), "claim-admin")
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	winners := 0
	alreadyClaimed := 0
	for err := range results {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, service.ErrAdminEncryptionKeyClaimed):
			alreadyClaimed++
		default:
			t.Fatalf("unexpected claim error: %v", err)
		}
	}
	if winners != 1 || alreadyClaimed != workers-1 {
		t.Fatalf("winners=%d claimed=%d, want 1 and %d", winners, alreadyClaimed, workers-1)
	}
}

func TestEncryptionKeyReadFailureDoesNotConsumeClaim(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	admin := model.User{ID: "retry-admin", Username: "retry-admin", Status: "active", IsSuperAdmin: true}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(server.dataDir, "encryption.key")
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	if _, err := server.adminAuth.ClaimEncryptionKey(context.Background(), admin.ID); err == nil {
		t.Fatal("claim succeeded while encryption key was unreadable")
	}
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := server.adminAuth.ClaimEncryptionKey(context.Background(), admin.ID); err != nil {
		t.Fatalf("claim after restoring key failed: %v", err)
	}
}

func TestCanceledAdminAuthOperationsDoNotWrite(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := server.adminAuth.Setup(ctx, service.AdminSetupInput{
		Username: "canceled", Password: "secure-password",
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("setup error = %v, want context canceled", err)
	}
	if _, err := server.adminAuth.ClaimEncryptionKey(ctx, "canceled"); !errors.Is(err, context.Canceled) {
		t.Fatalf("claim error = %v, want context canceled", err)
	}
	var users, claims int64
	if err := db.Model(&model.User{}).Count(&users).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.SystemInitialization{}).Count(&claims).Error; err != nil {
		t.Fatal(err)
	}
	if users != 0 || claims != 0 {
		t.Fatalf("canceled operations wrote users=%d initialization_records=%d", users, claims)
	}
}

func TestSetupUserCreateFailureRollsBackInitializationGuard(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	callbackName := "test:fail_initial_admin_create"
	if err := db.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "User" {
			tx.AddError(errors.New("injected user create failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Callback().Create().Remove(callbackName)
	})

	_, err := server.adminAuth.Setup(context.Background(), service.AdminSetupInput{
		Username: "rollback-admin", Password: "secure-password",
	})
	if err == nil {
		t.Fatal("setup unexpectedly succeeded")
	}
	var users, guards int64
	if err := db.Model(&model.User{}).Count(&users).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.SystemInitialization{}).Count(&guards).Error; err != nil {
		t.Fatal(err)
	}
	if users != 0 || guards != 0 {
		t.Fatalf("failed setup left users=%d guards=%d", users, guards)
	}
}
