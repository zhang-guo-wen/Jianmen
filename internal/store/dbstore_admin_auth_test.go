package store

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/service"
	"jianmen/internal/storage"

	mysqlgorm "gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestAdminSetupLockUsesMySQLForUpdateWithoutAffectedRowSemantics(t *testing.T) {
	sqliteDB, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := sqliteDB.DB()
	if err != nil {
		t.Fatal(err)
	}
	mysqlDB, err := gorm.Open(mysqlgorm.New(mysqlgorm.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}

	var setup model.SystemInitialization
	statement := adminSetupLockQuery(mysqlDB).
		Limit(1).
		Find(&setup).
		Statement
	sql := strings.ToUpper(statement.SQL.String())
	if !strings.Contains(sql, "FOR UPDATE") {
		t.Fatalf("MySQL setup lock SQL = %q, want FOR UPDATE", statement.SQL.String())
	}
	if len(statement.Vars) < 1 || statement.Vars[0] != model.SystemInitializationSetup {
		t.Fatalf("MySQL setup lock vars = %#v, want bound setup key", statement.Vars)
	}

	var user model.User
	userStatement := adminEncryptionKeyClaimerLockQuery(
		mysqlDB,
		"admin-id",
		"verified-password-hash",
		time.Unix(123, 0).UTC(),
	).Limit(1).Find(&user).Statement
	userSQL := strings.ToUpper(userStatement.SQL.String())
	if !strings.Contains(userSQL, "FOR UPDATE") ||
		!strings.Contains(userSQL, "PASSWORD_HASH =") ||
		!strings.Contains(userSQL, "IS_SUPER_ADMIN =") ||
		!strings.Contains(userSQL, "EXPIRES_AT") {
		t.Fatalf("MySQL claimer lock SQL = %q, want locked credential-version recheck", userStatement.SQL.String())
	}
}

func TestAdminEncryptionKeyDuplicateInsertMapsToClaimed(t *testing.T) {
	for _, duplicate := range []error{
		postgresUniqueConstraintTestError{},
		errors.New("Error 1062 (23000): Duplicate entry 'encryption_key_claimed' for key 'PRIMARY'"),
	} {
		err := mapAdminEncryptionKeyClaimInsertError(duplicate)
		if !errors.Is(err, service.ErrAdminEncryptionKeyClaimed) {
			t.Fatalf("duplicate insert error = %v, want claimed sentinel", err)
		}
	}
}

func TestFindAdminLoginCredentialMissingDoesNotEmitRecordNotFound(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	observedDB := db.Session(&gorm.Session{Logger: gormlogger.New(
		log.New(&logs, "", 0),
		gormlogger.Config{LogLevel: gormlogger.Info},
	)})
	repository := NewDBStore(observedDB)

	credential, found, err := repository.FindAdminLoginCredential(
		context.Background(),
		`missing' OR 1=1 --`,
	)
	if err != nil || found || credential.UserID != "" {
		t.Fatalf("missing credential = %#v found=%v err=%v", credential, found, err)
	}
	if strings.Contains(strings.ToLower(logs.String()), "record not found") {
		t.Fatalf("missing credential emitted record-not-found log: %s", logs.String())
	}
}
