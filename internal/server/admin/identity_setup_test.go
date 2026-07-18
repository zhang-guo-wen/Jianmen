package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/storage"

	"gorm.io/gorm"
)

func TestInitSetupPersistsExactlyOneSuperAdministrator(t *testing.T) {
	server, db := newAdminDBTestServer(t)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/init/setup",
		strings.NewReader(`{"username":"admin","password":"secure-password","email":"admin@example.com"}`),
	)
	response := httptest.NewRecorder()
	server.handleInitSetup(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("setup status = %d, want %d; body=%s", response.Code, http.StatusCreated, response.Body.String())
	}

	var users []model.User
	if err := db.Find(&users).Error; err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("user count = %d, want 1", len(users))
	}
	if !users[0].IsSuperAdmin {
		t.Fatal("setup user is not persisted as super administrator")
	}

	secondRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/init/setup",
		strings.NewReader(`{"username":"other","password":"secure-password"}`),
	)
	secondResponse := httptest.NewRecorder()
	server.handleInitSetup(secondResponse, secondRequest)
	if secondResponse.Code != http.StatusForbidden {
		t.Fatalf("second setup status = %d, want %d; body=%s", secondResponse.Code, http.StatusForbidden, secondResponse.Body.String())
	}

	var superAdminCount int64
	if err := db.Model(&model.User{}).Where("is_super_admin = ?", true).Count(&superAdminCount).Error; err != nil {
		t.Fatalf("count super administrators: %v", err)
	}
	if superAdminCount != 1 {
		t.Fatalf("super administrator count = %d, want 1", superAdminCount)
	}
}

func TestInitSetupConcurrentRequestsCreateExactlyOneSuperAdministrator(t *testing.T) {
	servers, db := newConcurrentInitSetupTestServers(t)

	const workers = 8
	start := make(chan struct{})
	statuses := make(chan int, workers)
	var wg sync.WaitGroup
	for index := 0; index < workers; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			request := httptest.NewRequest(
				http.MethodPost,
				"/api/init/setup",
				strings.NewReader(`{"username":"admin-`+string(rune('a'+index))+`","password":"secure-password"}`),
			)
			response := httptest.NewRecorder()
			servers[index%len(servers)].handleInitSetup(response, request)
			statuses <- response.Code
		}(index)
	}
	close(start)
	wg.Wait()
	close(statuses)

	created := 0
	forbidden := 0
	for status := range statuses {
		switch status {
		case http.StatusCreated:
			created++
		case http.StatusForbidden:
			forbidden++
		default:
			t.Errorf("concurrent setup status = %d, want %d or %d", status, http.StatusCreated, http.StatusForbidden)
		}
	}
	if created != 1 || forbidden != workers-1 {
		t.Fatalf("created = %d, forbidden = %d, want 1 and %d", created, forbidden, workers-1)
	}

	var users int64
	if err := db.Model(&model.User{}).Count(&users).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if users != 1 {
		t.Fatalf("user count = %d, want 1", users)
	}
	var superAdministrators int64
	if err := db.Model(&model.User{}).Where("is_super_admin = ?", true).Count(&superAdministrators).Error; err != nil {
		t.Fatalf("count super administrators: %v", err)
	}
	if superAdministrators != 1 {
		t.Fatalf("super administrator count = %d, want 1", superAdministrators)
	}
	var setupGuards int64
	if err := db.Model(&model.SystemInitialization{}).Count(&setupGuards).Error; err != nil {
		t.Fatalf("count setup guards: %v", err)
	}
	if setupGuards != 1 {
		t.Fatalf("setup guard count = %d, want 1", setupGuards)
	}
}

func TestAcquireSetupSlotHonorsContextCancellation(t *testing.T) {
	server := &Server{}
	release, err := server.acquireSetupSlot(context.Background())
	if err != nil {
		t.Fatalf("acquire setup slot: %v", err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := server.acquireSetupSlot(ctx)
		result <- err
	}()
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("acquire canceled setup slot error = %v, want context canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("acquire setup slot did not return after context cancellation")
	}
}

func TestInitSetupAfterUpgradePreservesExistingUserAndRejectsSetup(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open legacy sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("create legacy user schema: %v", err)
	}
	existing := model.User{ID: "existing", Username: "existing", Status: "active"}
	if err := db.Create(&existing).Error; err != nil {
		t.Fatalf("create existing user: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}

	server, _ := newAdminDBTestServer(t)
	server.db = db
	server.dataDir = ""
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/init/setup",
		strings.NewReader(`{"username":"new-admin","password":"secure-password"}`),
	)
	response := httptest.NewRecorder()
	server.handleInitSetup(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("setup status = %d, want %d; body=%s", response.Code, http.StatusForbidden, response.Body.String())
	}

	var users []model.User
	if err := db.Find(&users).Error; err != nil {
		t.Fatalf("list upgraded users: %v", err)
	}
	if len(users) != 1 || users[0].ID != existing.ID || users[0].IsSuperAdmin {
		t.Fatalf("upgraded users changed unexpectedly: %#v", users)
	}
	var setupGuards int64
	if err := db.Model(&model.SystemInitialization{}).Count(&setupGuards).Error; err != nil {
		t.Fatalf("count setup guards: %v", err)
	}
	if setupGuards != 1 {
		t.Fatalf("setup guard count = %d, want 1", setupGuards)
	}
}

func newConcurrentInitSetupTestServers(t *testing.T) ([]*Server, *gorm.DB) {
	t.Helper()

	path := filepath.ToSlash(filepath.Join(t.TempDir(), "setup.db"))
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)"
	primaryDB, err := storage.Open(storage.Config{
		Driver:       storage.DriverSQLite,
		DSN:          dsn,
		MaxOpenConns: 16,
		MaxIdleConns: 16,
	})
	if err != nil {
		t.Fatalf("open primary concurrent sqlite: %v", err)
	}
	if err := storage.AutoMigrate(primaryDB); err != nil {
		t.Fatalf("automigrate concurrent sqlite: %v", err)
	}
	secondaryDB, err := storage.Open(storage.Config{
		Driver:       storage.DriverSQLite,
		DSN:          dsn,
		MaxOpenConns: 16,
		MaxIdleConns: 16,
	})
	if err != nil {
		t.Fatalf("open secondary concurrent sqlite: %v", err)
	}

	servers := make([]*Server, 0, 2)
	for _, db := range []*gorm.DB{primaryDB, secondaryDB} {
		server, _ := newAdminDBTestServer(t)
		server.db = db
		server.dataDir = ""
		servers = append(servers, server)
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatalf("get concurrent sql db: %v", err)
		}
		t.Cleanup(func() {
			if err := sqlDB.Close(); err != nil {
				t.Errorf("close concurrent sqlite: %v", err)
			}
		})
	}
	return servers, primaryDB
}
