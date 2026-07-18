package admin

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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
	server, db := newConcurrentInitSetupTestServer(t)

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
			server.handleInitSetup(response, request)
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

func newConcurrentInitSetupTestServer(t *testing.T) (*Server, *gorm.DB) {
	t.Helper()

	server, _ := newAdminDBTestServer(t)
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "setup.db"))
	db, err := storage.Open(storage.Config{
		Driver:       storage.DriverSQLite,
		DSN:          "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)",
		MaxOpenConns: 16,
		MaxIdleConns: 16,
	})
	if err != nil {
		t.Fatalf("open concurrent sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("automigrate concurrent sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get concurrent sql db: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close concurrent sqlite: %v", err)
		}
	})
	server.db = db
	server.superAdminIDs = nil
	server.dataDir = ""
	return server, db
}
