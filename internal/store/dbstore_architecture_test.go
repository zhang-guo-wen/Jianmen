package store

import (
	"context"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"jianmen/internal/config"
	"jianmen/internal/crypto"
	"jianmen/internal/model"
	"jianmen/internal/storage"
	"jianmen/internal/util"
)

func TestDBStoreSyncsResourcesAndUsesSequenceFloor(t *testing.T) {
	db, err := storage.Open(storage.Config{
		Driver: storage.DriverSQLite,
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	host := model.Host{ID: "host-1", Name: "app-1", Address: "10.0.0.1", Port: 22}
	if err := db.Create(&host).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}
	legacyAccount := model.HostAccount{
		ID:          "acct-7",
		HostID:      host.ID,
		Username:    "legacy",
		ResourceSeq: 7,
		ResourceID:  util.ResourceIDFromSeq(util.PrefixHost, 7),
	}
	if err := db.Create(&legacyAccount).Error; err != nil {
		t.Fatalf("create legacy account: %v", err)
	}

	st := NewDBStore(db)
	target, err := st.AddTarget(config.Target{
		ID:       "acct-8",
		HostID:   host.ID,
		Host:     host.Address,
		Port:     host.Port,
		Name:     "operations",
		Username: "root",
	})
	if err != nil {
		t.Fatalf("add target: %v", err)
	}
	if target.ResourceSeq != 8 {
		t.Fatalf("resource seq = %d, want 8", target.ResourceSeq)
	}
	if target.Name != "operations" || target.Username != "root" {
		t.Fatalf("target identity = name:%q username:%q", target.Name, target.Username)
	}
	targetConfig, err := st.TargetConfig(target.ID)
	if err != nil {
		t.Fatalf("load target config: %v", err)
	}
	if targetConfig.Name != "operations" || targetConfig.HostName != host.Name {
		t.Fatalf("target config labels = account:%q host:%q", targetConfig.Name, targetConfig.HostName)
	}

	var resource model.Resource
	if err := db.First(&resource, "type = ? AND resource_id = ?", model.ResourceTypeHostAccount, target.ID).Error; err != nil {
		t.Fatalf("load target resource: %v", err)
	}
	if resource.ParentID != host.ID {
		t.Fatalf("resource parent = %q, want %q", resource.ParentID, host.ID)
	}

	var hostResource model.Resource
	if err := db.First(&hostResource, "type = ? AND resource_id = ?", model.ResourceTypeHost, host.ID).Error; err != nil {
		t.Fatalf("load host resource: %v", err)
	}

	if err := st.DeleteTarget(target.ID); err != nil {
		t.Fatalf("delete target: %v", err)
	}
	var count int64
	if err := db.Model(&model.Resource{}).
		Where("type = ? AND resource_id = ?", model.ResourceTypeHostAccount, target.ID).
		Count(&count).Error; err != nil {
		t.Fatalf("count deleted target resource: %v", err)
	}
	if count != 0 {
		t.Fatalf("target resource count after delete = %d, want 0", count)
	}
}

func TestDBStoreUserSessionsUseGlobalCompactSequence(t *testing.T) {
	db, err := storage.Open(storage.Config{
		Driver: storage.DriverSQLite,
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	pw1, err := bcrypt.GenerateFromPassword([]byte("pw1"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash pw1: %v", err)
	}
	pw2, err := bcrypt.GenerateFromPassword([]byte("pw2"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash pw2: %v", err)
	}
	users := []model.User{
		{ID: "u1", Username: "u1", PasswordHash: string(pw1), Status: "active"},
		{ID: "u2", Username: "u2", PasswordHash: string(pw2), Status: "active"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}
	if err := db.Create(&model.Host{ID: "host-auth", Name: "host-auth", Address: "127.0.0.1", Port: 22, Status: "active"}).Error; err != nil {
		t.Fatalf("create host: %v", err)
	}
	account := model.HostAccount{
		ID:          "acct-auth",
		HostID:      "host-auth",
		Username:    "root",
		Status:      "active",
		ResourceSeq: 1,
		ResourceID:  util.ResourceIDFromSeq(util.PrefixHost, 1),
	}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}

	st := NewDBStore(db)
	first, err := st.CreateUserSession(model.UserSession{UserID: "u1", Type: "permanent", Status: "active"})
	if err != nil {
		t.Fatalf("create first session: %v", err)
	}
	second, err := st.CreateUserSession(model.UserSession{UserID: "u2", Type: "permanent", Status: "active"})
	if err != nil {
		t.Fatalf("create second session: %v", err)
	}
	if first.SessionID == second.SessionID {
		t.Fatalf("session IDs should be globally unique: first=%s second=%s", first.SessionID, second.SessionID)
	}

	compactUsername := util.PrefixHost + account.ResourceID + second.SessionID
	authenticated, err := st.Authenticate(context.Background(), compactUsername, "pw2")
	if err != nil {
		t.Fatalf("authenticate second user: %v", err)
	}
	if authenticated.ID != "u2" {
		t.Fatalf("authenticated user = %q, want u2", authenticated.ID)
	}
}

func TestDBStoreRejectsDuplicateAccountNamesPerParent(t *testing.T) {
	db, err := storage.Open(storage.Config{
		Driver: storage.DriverSQLite,
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	// crypto 初始化确保 EncryptedField 可以工作
	tmpDir := t.TempDir()
	if _, err := crypto.Init(tmpDir); err != nil {
		t.Fatalf("crypto init: %v", err)
	}
	st := NewDBStore(db)

	_, err = st.AddTarget(config.Target{
		ID:       "host-acct-1",
		HostID:   "host-dup",
		Host:     "127.0.0.1",
		Port:     22,
		Username: "root",
	})
	if err != nil {
		t.Fatalf("add target: %v", err)
	}
	_, err = st.AddTarget(config.Target{
		ID:       "host-acct-2",
		HostID:   "host-dup",
		Host:     "127.0.0.1",
		Port:     22,
		Username: "root",
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate target error = %v, want already exists", err)
	}

	instance, err := st.AddDatabaseInstance("orders", "mysql", "127.0.0.1", 3306, "", "")
	if err != nil {
		t.Fatalf("add database instance: %v", err)
	}
	if _, err := st.AddDatabaseAccount(instance.ID, "app", "pass1", "", "", nil); err != nil {
		t.Fatalf("add database account: %v", err)
	}
	_, err = st.AddDatabaseAccount(instance.ID, "app", "pass2", "", "", nil)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate database account error = %v, want already exists", err)
	}
}

func TestDBStoreListCountsAndDefaultTargetAreSetBased(t *testing.T) {
	db, err := storage.Open(storage.Config{
		Driver: storage.DriverSQLite,
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}

	hosts := []model.Host{
		{ID: "host-disabled", Name: "disabled", Address: "10.0.0.10", Port: 22, Status: "disabled"},
		{ID: "host-active", Name: "active", Address: "10.0.0.11", Port: 22, Status: "active"},
	}
	if err := db.Create(&hosts).Error; err != nil {
		t.Fatalf("create hosts: %v", err)
	}
	accounts := []model.HostAccount{
		{ID: "acct-disabled-host", HostID: "host-disabled", Username: "root", Status: "active", ResourceSeq: 1, ResourceID: util.ResourceIDFromSeq(util.PrefixHost, 1)},
		{ID: "acct-active-1", HostID: "host-active", Username: "root", Status: "active", ResourceSeq: 2, ResourceID: util.ResourceIDFromSeq(util.PrefixHost, 2)},
		{ID: "acct-active-2", HostID: "host-active", Username: "deploy", Status: "disabled", ResourceSeq: 3, ResourceID: util.ResourceIDFromSeq(util.PrefixHost, 3)},
	}
	if err := db.Create(&accounts).Error; err != nil {
		t.Fatalf("create host accounts: %v", err)
	}

	st := NewDBStore(db)
	hostViews := st.Hosts()
	countByHost := make(map[string]int, len(hostViews))
	for _, view := range hostViews {
		countByHost[view.ID] = view.AccountCount
	}
	if countByHost["host-disabled"] != 1 || countByHost["host-active"] != 2 {
		t.Fatalf("host account counts = %#v, want disabled=1 active=2", countByHost)
	}

	target, err := st.DefaultTarget(context.Background(), model.User{ID: "u"})
	if err != nil {
		t.Fatalf("default target: %v", err)
	}
	if target.ID != "acct-active-1" {
		t.Fatalf("default target = %q, want acct-active-1", target.ID)
	}

	dbInstances := []model.DatabaseInstance{
		{ID: "db1", Name: "db1", Protocol: "mysql", Address: "10.0.1.1", Port: 3306, Status: "active"},
		{ID: "db2", Name: "db2", Protocol: "postgres", Address: "10.0.1.2", Port: 5432, Status: "active"},
	}
	if err := db.Create(&dbInstances).Error; err != nil {
		t.Fatalf("create database instances: %v", err)
	}
	dbAccounts := []model.DatabaseAccount{
		{ID: "dbacct-1", InstanceID: "db1", UniqueName: "dbacct-1", Username: "app", Status: "active", ResourceSeq: 1, ResourceID: util.ResourceIDFromSeq(util.PrefixDatabase, 1)},
		{ID: "dbacct-2", InstanceID: "db1", UniqueName: "dbacct-2", Username: "report", Status: "active", ResourceSeq: 2, ResourceID: util.ResourceIDFromSeq(util.PrefixDatabase, 2)},
		{ID: "dbacct-3", InstanceID: "db2", UniqueName: "dbacct-3", Username: "app", Status: "active", ResourceSeq: 3, ResourceID: util.ResourceIDFromSeq(util.PrefixDatabase, 3)},
	}
	if err := db.Create(&dbAccounts).Error; err != nil {
		t.Fatalf("create database accounts: %v", err)
	}
	instanceViews := st.DatabaseInstances()
	countByInstance := make(map[string]int, len(instanceViews))
	for _, view := range instanceViews {
		countByInstance[view.ID] = view.AccountCount
	}
	if countByInstance["db1"] != 2 || countByInstance["db2"] != 1 {
		t.Fatalf("database account counts = %#v, want db1=2 db2=1", countByInstance)
	}
}

func TestDBStoreTokenAuthRequiresActiveUser(t *testing.T) {
	db, err := storage.Open(storage.Config{
		Driver: storage.DriverSQLite,
		DSN:    ":memory:",
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	users := []model.User{
		{ID: "active-token-user", Username: "active-token-user", TokenHash: tokenHash("active-token"), Status: "active"},
		{ID: "disabled-token-user", Username: "disabled-token-user", TokenHash: tokenHash("disabled-token"), Status: "disabled"},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}

	st := NewDBStore(db)
	user, err := st.Authenticate(context.Background(), "not-compact", "active-token")
	if err != nil {
		t.Fatalf("active token authenticate: %v", err)
	}
	if user.ID != "active-token-user" {
		t.Fatalf("active token user = %q, want active-token-user", user.ID)
	}
	if _, err := st.Authenticate(context.Background(), "not-compact", "disabled-token"); err == nil {
		t.Fatal("disabled token authenticated successfully")
	}
}
