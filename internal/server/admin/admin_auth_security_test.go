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
	const password = "claim-password"
	passwordHash, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	users := []model.User{
		{ID: "claim-admin", Username: "claim-admin", PasswordHash: passwordHash, Status: "active", IsSuperAdmin: true},
		{ID: "claim-regular", Username: "claim-regular", PasswordHash: passwordHash, Status: "active"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	seedAdminSetupGuard(t, db)
	if _, err := server.adminAuth.ClaimEncryptionKey(context.Background(), "claim-regular", password); !errors.Is(err, service.ErrAdminEncryptionKeyDenied) {
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
			_, err := server.adminAuth.ClaimEncryptionKey(context.Background(), "claim-admin", password)
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
	const password = "retry-password"
	passwordHash, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	admin := model.User{
		ID: "retry-admin", Username: "retry-admin", PasswordHash: passwordHash,
		Status: "active", IsSuperAdmin: true,
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	seedAdminSetupGuard(t, db)
	keyPath := filepath.Join(server.dataDir, "encryption.key")
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	if _, err := server.adminAuth.ClaimEncryptionKey(context.Background(), admin.ID, password); err == nil {
		t.Fatal("claim succeeded while encryption key was unreadable")
	}
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := server.adminAuth.ClaimEncryptionKey(context.Background(), admin.ID, password); err != nil {
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
	if _, err := server.adminAuth.ClaimEncryptionKey(ctx, "canceled", "password"); !errors.Is(err, context.Canceled) {
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

type observingAdminEncryptionKeyReader struct {
	key          []byte
	calls        int
	beforeReturn func()
}

func (r *observingAdminEncryptionKeyReader) ReadAdminEncryptionKey(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.calls++
	if r.beforeReturn != nil {
		r.beforeReturn()
	}
	return append([]byte(nil), r.key...), nil
}

func TestEncryptionKeyClaimRequiresPasswordActiveUnexpiredSuperAdmin(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	const password = "current-password"
	passwordHash, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	admin := model.User{
		ID: "reauth-admin", Username: "reauth-admin", PasswordHash: passwordHash,
		Status: "active", IsSuperAdmin: true,
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	seedAdminSetupGuard(t, db)
	reader := &observingAdminEncryptionKeyReader{key: make([]byte, 32)}
	adminAuth, err := service.NewAdminAuthService(store.NewDBStore(db), server.browserSessions, reader)
	if err != nil {
		t.Fatal(err)
	}

	for name, candidate := range map[string]string{"missing": "", "wrong": "wrong-password"} {
		t.Run(name, func(t *testing.T) {
			if _, err := adminAuth.ClaimEncryptionKey(context.Background(), admin.ID, candidate); !errors.Is(err, service.ErrAdminEncryptionKeyDenied) {
				t.Fatalf("claim error = %v, want denied", err)
			}
		})
	}
	expiredAt := time.Now().UTC().Add(-time.Minute)
	if err := db.Model(&model.User{}).Where("id = ?", admin.ID).Update("expires_at", expiredAt).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := adminAuth.ClaimEncryptionKey(context.Background(), admin.ID, password); !errors.Is(err, service.ErrAdminEncryptionKeyDenied) {
		t.Fatalf("expired claim error = %v, want denied", err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", admin.ID).
		Updates(map[string]any{"expires_at": nil, "is_super_admin": false}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := adminAuth.ClaimEncryptionKey(context.Background(), admin.ID, password); !errors.Is(err, service.ErrAdminEncryptionKeyDenied) {
		t.Fatalf("demoted claim error = %v, want denied", err)
	}
	if reader.calls != 0 {
		t.Fatalf("rejected claims read encryption key %d times", reader.calls)
	}
	assertAdminEncryptionKeyClaimCount(t, db, 0)
}

func TestEncryptionKeyClaimRechecksPasswordHashVersion(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	const oldPassword = "old-password"
	oldHash, err := hashPassword(oldPassword)
	if err != nil {
		t.Fatal(err)
	}
	newHash, err := hashPassword("new-password")
	if err != nil {
		t.Fatal(err)
	}
	admin := model.User{
		ID: "key-password-race", Username: "key-password-race", PasswordHash: oldHash,
		Status: "active", IsSuperAdmin: true,
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	seedAdminSetupGuard(t, db)
	reader := &observingAdminEncryptionKeyReader{
		key: make([]byte, 32),
		beforeReturn: func() {
			if err := db.Model(&model.User{}).Where("id = ?", admin.ID).Update("password_hash", newHash).Error; err != nil {
				t.Errorf("rotate password hash: %v", err)
			}
		},
	}
	adminAuth, err := service.NewAdminAuthService(store.NewDBStore(db), server.browserSessions, reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adminAuth.ClaimEncryptionKey(context.Background(), admin.ID, oldPassword); !errors.Is(err, service.ErrAdminEncryptionKeyDenied) {
		t.Fatalf("claim error = %v, want denied after password rotation", err)
	}
	assertAdminEncryptionKeyClaimCount(t, db, 0)
}

func TestEncryptionKeyInvalidLengthDoesNotConsumeClaim(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	const password = "length-password"
	passwordHash, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	admin := model.User{
		ID: "key-length-admin", Username: "key-length-admin", PasswordHash: passwordHash,
		Status: "active", IsSuperAdmin: true,
	}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatal(err)
	}
	seedAdminSetupGuard(t, db)
	reader := &observingAdminEncryptionKeyReader{key: make([]byte, 31)}
	adminAuth, err := service.NewAdminAuthService(store.NewDBStore(db), server.browserSessions, reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalidLength := range []int{0, 31, 33} {
		reader.key = make([]byte, invalidLength)
		if _, err := adminAuth.ClaimEncryptionKey(context.Background(), admin.ID, password); err == nil {
			t.Fatalf("claim accepted encryption key length %d", invalidLength)
		}
		assertAdminEncryptionKeyClaimCount(t, db, 0)
	}
	reader.key = make([]byte, 32)
	if _, err := adminAuth.ClaimEncryptionKey(context.Background(), admin.ID, password); err != nil {
		t.Fatalf("claim with valid key failed: %v", err)
	}
	assertAdminEncryptionKeyClaimCount(t, db, 1)
}

func TestCompleteLoginRejectsPasswordHashRotationBeforeStateWrite(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	oldHash, err := hashPassword("old-password")
	if err != nil {
		t.Fatal(err)
	}
	newHash, err := hashPassword("new-password")
	if err != nil {
		t.Fatal(err)
	}
	user := model.User{
		ID: "login-password-race", Username: "login-password-race",
		PasswordHash: oldHash, Status: "active",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	login, err := server.adminAuth.VerifyLogin(context.Background(), user.Username, "old-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", user.ID).Update("password_hash", newHash).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := server.adminAuth.CompleteLogin(context.Background(), login); !errors.Is(err, service.ErrAdminInvalidCredentials) {
		t.Fatalf("complete login error = %v, want invalid credentials", err)
	}
	if err := db.First(&user, "id = ?", user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if user.LastLoginAt != nil || user.MySQLNativeHash != "" {
		t.Fatalf("rotated login wrote state: last_login=%v mysql_hash=%q", user.LastLoginAt, user.MySQLNativeHash)
	}
	var sessions int64
	if err := db.Model(&model.AdminSession{}).Where("user_id = ?", user.ID).Count(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf("rotated login created %d sessions", sessions)
	}
}

func seedAdminSetupGuard(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Create(&model.SystemInitialization{
		Key: model.SystemInitializationSetup, FullAudit: model.FullAudit{CreatedAt: time.Now().UTC()},
	}).Error; err != nil {
		t.Fatalf("seed admin setup guard: %v", err)
	}
}

func assertAdminEncryptionKeyClaimCount(t *testing.T, db *gorm.DB, want int64) {
	t.Helper()
	var count int64
	if err := db.Model(&model.SystemInitialization{}).
		Where("key = ?", "encryption_key_claimed").
		Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("encryption key claims = %d, want %d", count, want)
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
