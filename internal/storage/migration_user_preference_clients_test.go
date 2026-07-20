package storage

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"jianmen/internal/model"
)

type userPreferenceBeforeClientPlatforms struct {
	UserID             string `gorm:"primaryKey;size:64"`
	Theme              string `gorm:"size:32;not null;default:light"`
	SSHClient          string `gorm:"size:32"`
	SSHClientPath      string `gorm:"size:512"`
	TerminalFontFamily string `gorm:"size:128"`
	TerminalFontSize   int    `gorm:"not null;default:14"`
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (userPreferenceBeforeClientPlatforms) TableName() string { return "user_preferences" }

func TestMigrateUserPreferenceClientsAddsFieldsAndPreservesRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:user-preference-client-migration?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(&userPreferenceBeforeClientPlatforms{}); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	legacy := userPreferenceBeforeClientPlatforms{
		UserID: "user-1", Theme: "dark", SSHClient: "putty", SSHClientPath: `C:\\putty.exe`, TerminalFontSize: 16,
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatalf("create legacy preference: %v", err)
	}

	if err := migrateUserPreferenceClients(db); err != nil {
		t.Fatalf("migrate user preferences: %v", err)
	}
	for _, field := range []string{
		"SSHClientPlatform", "DBClient", "DBClientPlatform", "DBClientPath", "DBClientCAFilePath",
	} {
		if !db.Migrator().HasColumn(&model.UserPreference{}, field) {
			t.Errorf("missing migrated column %s", field)
		}
	}

	var got model.UserPreference
	if err := db.First(&got, "user_id = ?", legacy.UserID).Error; err != nil {
		t.Fatalf("load migrated preference: %v", err)
	}
	if got.Theme != legacy.Theme || got.SSHClient != legacy.SSHClient || got.SSHClientPath != legacy.SSHClientPath {
		t.Fatalf("legacy values changed: %#v", got)
	}
	if got.SSHClientPlatform != "windows" || got.DBClientPlatform != "windows" {
		t.Fatalf("platform defaults = %q, %q; want windows", got.SSHClientPlatform, got.DBClientPlatform)
	}
}
