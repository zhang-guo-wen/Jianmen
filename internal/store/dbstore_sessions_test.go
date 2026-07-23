package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/storage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetUserSessionAuthDetail_Permanent(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	require.NoError(t, err)
	storage.AutoMigrate(db)

	user := model.User{ID: "u1", Username: "testuser", Status: "active"}
	require.NoError(t, db.Create(&user).Error)

	sess := model.UserSession{
		ID: "us1", UserID: "u1", SessionSeq: 1, SessionID: "00001",
		Type: "permanent", Status: "active", CreatedBy: "",
	}
	require.NoError(t, db.Create(&sess).Error)

	store := &DBStore{db: db}
	detail, err := store.GetUserSessionAuthDetail(context.Background(), "00001")
	require.NoError(t, err)
	assert.Equal(t, "normal", detail.AuthorizationType)
	assert.Equal(t, "testuser", detail.Username)
	assert.Equal(t, "active", detail.EffectiveStatus)
}

func TestGetUserSessionAuthDetail_Temporary(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	require.NoError(t, err)
	storage.AutoMigrate(db)

	user := model.User{ID: "u1", Username: "tempuser", Status: "active"}
	require.NoError(t, db.Create(&user).Error)

	sess := model.UserSession{
		ID: "us2", UserID: "u1", SessionSeq: 2, SessionID: "00002",
		Type: "temporary", Status: "active", CreatedBy: "admin",
		ExpiresAt: timePtr(time.Now().Add(1 * time.Hour)),
	}
	require.NoError(t, db.Create(&sess).Error)

	ta := model.TemporaryAccount{
		ID: "ta1", SessionID: "00002", Type: model.TemporaryAccountTypeUser,
		AuthorizedUserID: "u1", Status: "active", Remark: "临时排查",
		StartsAt: time.Now(),
	}
	require.NoError(t, db.Create(&ta).Error)

	store := &DBStore{db: db}
	detail, err := store.GetUserSessionAuthDetail(context.Background(), "00002")
	require.NoError(t, err)
	assert.Equal(t, "temporary", detail.AuthorizationType)
	assert.Equal(t, "临时排查", detail.Remark)
	assert.Equal(t, "admin", detail.AuthorizedBy)
}

func TestGetUserSessionAuthDetail_AI(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	require.NoError(t, err)
	storage.AutoMigrate(db)

	user := model.User{ID: "u1", Username: "aiuser", Status: "active"}
	require.NoError(t, db.Create(&user).Error)

	sess := model.UserSession{
		ID: "us3", UserID: "u1", SessionSeq: 3, SessionID: "00003",
		Type: "temporary", Status: "active", CreatedBy: "system",
	}
	require.NoError(t, db.Create(&sess).Error)

	ta := model.TemporaryAccount{
		ID: "ta2", SessionID: "00003", Type: model.TemporaryAccountTypeAI,
		AuthorizedUserID: "u1", Status: "active", Remark: "AI自动操作",
		StartsAt: time.Now(),
	}
	require.NoError(t, db.Create(&ta).Error)

	store := &DBStore{db: db}
	detail, err := store.GetUserSessionAuthDetail(context.Background(), "00003")
	require.NoError(t, err)
	assert.Equal(t, "ai", detail.AuthorizationType)
	assert.Equal(t, "AI自动操作", detail.Remark)
}

func TestGetUserSessionAuthDetail_NotFound(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	require.NoError(t, err)
	storage.AutoMigrate(db)

	store := &DBStore{db: db}
	_, err = store.GetUserSessionAuthDetail(context.Background(), "99999")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestGetUserSessionAuthDetail_Expired(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	require.NoError(t, err)
	storage.AutoMigrate(db)

	user := model.User{ID: "u1", Username: "expireduser", Status: "active"}
	require.NoError(t, db.Create(&user).Error)

	sess := model.UserSession{
		ID: "us4", UserID: "u1", SessionSeq: 4, SessionID: "00004",
		Type: "permanent", Status: "active",
		ExpiresAt: timePtr(time.Now().Add(-1 * time.Hour)),
	}
	require.NoError(t, db.Create(&sess).Error)

	store := &DBStore{db: db}
	detail, err := store.GetUserSessionAuthDetail(context.Background(), "00004")
	require.NoError(t, err)
	assert.Equal(t, "expired", detail.EffectiveStatus)
	assert.Equal(t, "active", detail.Status) // 原始状态不变
}

func timePtr(t time.Time) *time.Time { return &t }
