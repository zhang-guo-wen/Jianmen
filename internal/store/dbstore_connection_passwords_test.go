package store

import (
	"context"
	"crypto/sha1"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"jianmen/internal/model"
	"jianmen/internal/service"
	"jianmen/internal/storage"
)

func TestConnectionPasswordIsResourceBoundAndReusable(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("permanent-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash user password: %v", err)
	}
	user := model.User{ID: "user-1", Username: "alice", PasswordHash: string(passwordHash), Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	store := NewDBStore(db)
	ctx := context.Background()
	issued, err := service.IssueConnectionPassword(time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("issue password: %v", err)
	}
	credential := model.ConnectionPassword{
		UserID:       user.ID,
		ResourceType: model.ResourceTypeHostAccount,
		ResourceID:   "host-account-1",
		SecretHash:   issued.Hash,
		ExpiresAt:    issued.ExpiresAt,
	}
	if err := store.CreateConnectionPassword(ctx, credential); err != nil {
		t.Fatalf("create password: %v", err)
	}
	if err := store.AuthenticateConnectionPassword(ctx, user.ID, model.ResourceTypeHostAccount, "other-account", issued.Plaintext); err == nil {
		t.Fatal("password authenticated for another resource")
	}
	if err := store.AuthenticateConnectionPassword(ctx, user.ID, model.ResourceTypeHostAccount, "host-account-1", issued.Plaintext); err != nil {
		t.Fatalf("first authentication failed: %v", err)
	}
	if err := store.AuthenticateConnectionPassword(ctx, user.ID, model.ResourceTypeHostAccount, "host-account-1", issued.Plaintext); err != nil {
		t.Fatalf("second authentication failed: %v", err)
	}
	if err := store.AuthenticateConnectionPassword(ctx, user.ID, model.ResourceTypeHostAccount, "host-account-1", "permanent-password"); err != nil {
		t.Fatalf("permanent password no longer works: %v", err)
	}
}

func TestConnectionPasswordExpiresAndCompactSSHIsReusable(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	user := model.User{ID: "user-ssh", Username: "alice", PasswordHash: "invalid", Status: "active"}
	session := model.UserSession{ID: "session-ssh", UserID: user.ID, SessionID: "abcde", Type: "permanent", Status: "active"}
	account := model.HostAccount{ID: "host-account-ssh", HostID: "host-1", ResourceID: "1234", Username: "root", Status: "active"}
	for _, value := range []any{&user, &session, &account} {
		if err := db.Create(value).Error; err != nil {
			t.Fatalf("seed compact auth: %v", err)
		}
	}
	store := NewDBStore(db)
	expired, err := service.IssueConnectionPassword(time.Now().Add(-2*time.Minute), time.Minute)
	if err != nil {
		t.Fatalf("issue expired password: %v", err)
	}
	valid, err := service.IssueConnectionPassword(time.Now(), time.Minute)
	if err != nil {
		t.Fatalf("issue valid password: %v", err)
	}
	for _, issued := range []service.IssuedConnectionPassword{expired, valid} {
		if err := store.CreateConnectionPassword(context.Background(), model.ConnectionPassword{UserID: user.ID, ResourceType: model.ResourceTypeHostAccount, ResourceID: account.ID, SecretHash: issued.Hash, ExpiresAt: issued.ExpiresAt}); err != nil {
			t.Fatalf("save password: %v", err)
		}
	}
	if _, err := store.Authenticate(context.Background(), "H1234abcde", expired.Plaintext); err == nil {
		t.Fatal("expired compact SSH password authenticated")
	}
	authenticated, err := store.Authenticate(context.Background(), "H1234abcde", valid.Plaintext)
	if err != nil {
		t.Fatalf("compact SSH authentication failed: %v", err)
	}
	if authenticated.RequestedTargetID != account.ID {
		t.Fatalf("requested target = %q, want %q", authenticated.RequestedTargetID, account.ID)
	}
	if _, err := store.Authenticate(context.Background(), "H1234abcde", valid.Plaintext); err != nil {
		t.Fatalf("compact SSH password was not reusable: %v", err)
	}
}

func TestMultipleMySQLConnectionPasswordsAuthenticateIndependently(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	user := model.User{ID: "mysql-user", Username: "alice", PasswordHash: "unused", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	store := NewDBStore(db)
	salt := []byte("12345678901234567890")
	issuedPasswords := make([]service.IssuedConnectionPassword, 2)
	for index := range issuedPasswords {
		issued, issueErr := service.IssueConnectionPassword(time.Now(), time.Minute)
		if issueErr != nil {
			t.Fatalf("issue password %d: %v", index, issueErr)
		}
		issuedPasswords[index] = issued
		credential := model.ConnectionPassword{UserID: user.ID, ResourceType: model.ResourceTypeDatabaseAccount, ResourceID: "database-account", SecretHash: issued.Hash, MySQLNativeHash: issued.MySQLNativeHash, ExpiresAt: issued.ExpiresAt}
		if err := store.CreateConnectionPassword(context.Background(), credential); err != nil {
			t.Fatalf("save password %d: %v", index, err)
		}
	}
	for index, issued := range issuedPasswords {
		response := mysqlNativeResponse(issued.Plaintext, salt)
		if err := store.AuthenticateMySQLConnectionPassword(context.Background(), user.ID, "database-account", salt, response); err != nil {
			t.Fatalf("authenticate password %d: %v", index, err)
		}
		if err := store.AuthenticateMySQLConnectionPassword(context.Background(), user.ID, "database-account", salt, response); err != nil {
			t.Fatalf("password %d was not reusable: %v", index, err)
		}
	}
}

func mysqlNativeResponse(password string, salt []byte) []byte {
	stage1 := sha1.Sum([]byte(password))
	stage2 := sha1.Sum(stage1[:])
	input := append(append(make([]byte, 0, len(salt)+len(stage2)), salt...), stage2[:]...)
	scramble := sha1.Sum(input)
	response := make([]byte, sha1.Size)
	for index := range response {
		response[index] = stage1[index] ^ scramble[index]
	}
	return response
}
