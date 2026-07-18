package store

import (
	"context"
	"errors"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/storage"

	"gorm.io/gorm"
)

type postgresUniqueConstraintTestError struct{}

func (postgresUniqueConstraintTestError) Error() string {
	return "duplicate key value violates unique constraint"
}
func (postgresUniqueConstraintTestError) SQLState() string { return "23505" }

func newUserStoreTest(t *testing.T) (*DBStore, *gorm.DB) {
	t.Helper()
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return NewDBStore(db), db
}

func TestDBStoreUsersDetectsDuplicateNameAndSearches(t *testing.T) {
	store, _ := newUserStoreTest(t)
	ctx := context.Background()
	if _, err := store.CreateUser(ctx, model.User{ID: "u1", Username: "alice", Status: "active"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	exists, err := store.UsernameExists(ctx, "alice", "")
	if err != nil || !exists {
		t.Fatalf("username exists = %v, %v; want true, nil", exists, err)
	}
	users, total, err := store.SearchUsers(ctx, "ali", 1, 20)
	if err != nil || total != 1 || len(users) != 1 || users[0].ID != "u1" {
		t.Fatalf("search users = %#v, %d, %v", users, total, err)
	}
}

func TestDBStoreCreateUserReturnsRepositoryConflictMarker(t *testing.T) {
	store, _ := newUserStoreTest(t)
	ctx := context.Background()
	if _, err := store.CreateUser(ctx, model.User{ID: "u1", Username: "alice", Status: "active"}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	_, err := store.CreateUser(ctx, model.User{ID: "u2", Username: "alice", Status: "active"})
	var marker interface{ Conflict() bool }
	if !errors.As(err, &marker) || !marker.Conflict() {
		t.Fatalf("duplicate username error = %v, want repository conflict marker", err)
	}
	if errors.Is(err, errors.New("user already exists")) {
		t.Fatal("store error unexpectedly matched a service sentinel")
	}
}

func TestDBStoreUpdateUserReturnsRepositoryConflictMarker(t *testing.T) {
	store, _ := newUserStoreTest(t)
	ctx := context.Background()
	if _, err := store.CreateUser(ctx, model.User{ID: "u1", Username: "alice", Status: "active"}); err != nil {
		t.Fatalf("create alice: %v", err)
	}
	user, err := store.CreateUser(ctx, model.User{ID: "u2", Username: "bob", Status: "active"})
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}
	user.Username = "alice"
	_, err = store.UpdateUser(ctx, user)
	var marker interface{ Conflict() bool }
	if !errors.As(err, &marker) || !marker.Conflict() {
		t.Fatalf("duplicate username update error = %v, want repository conflict marker", err)
	}
}

func TestIsUniqueConstraintErrorRecognizesPostgresSQLState(t *testing.T) {
	if !isUniqueConstraintError(postgresUniqueConstraintTestError{}) {
		t.Fatal("SQLSTATE 23505 was not recognized as a unique constraint")
	}
}

func TestDBStoreDeleteUserRollsBackRolesWhenUserDeleteFails(t *testing.T) {
	store, db := newUserStoreTest(t)
	ctx := context.Background()
	user := model.User{ID: "u1", Username: "alice", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := db.Create(&model.UserRole{ID: "ur1", UserID: user.ID, RoleID: "role-1"}).Error; err != nil {
		t.Fatalf("create user role: %v", err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_user_delete BEFORE DELETE ON users BEGIN SELECT RAISE(ABORT, 'reject delete'); END`).Error; err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if err := store.DeleteUser(ctx, user); err == nil {
		t.Fatal("delete user succeeded despite rejecting trigger")
	}
	var count int64
	if err := db.Model(&model.UserRole{}).Where("id = ?", "ur1").Count(&count).Error; err != nil {
		t.Fatalf("count user roles: %v", err)
	}
	if count != 1 {
		t.Fatalf("user roles after rollback = %d, want 1", count)
	}
}
