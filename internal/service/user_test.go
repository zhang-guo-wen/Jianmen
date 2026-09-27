package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"jianmen/internal/model"

	"golang.org/x/crypto/bcrypt"
)

type fakeUserRepository struct {
	users        map[string]model.User
	created      model.User
	updated      model.User
	deleted      model.User
	usernameUsed bool
	createErr    error
	updateErr    error
}

func (f *fakeUserRepository) SearchUsers(context.Context, string, int, int) ([]model.User, int64, error) {
	users := make([]model.User, 0, len(f.users))
	for _, user := range f.users {
		users = append(users, user)
	}
	return users, int64(len(users)), nil
}

func (f *fakeUserRepository) FindUser(_ context.Context, id string) (model.User, bool, error) {
	user, ok := f.users[id]
	return user, ok, nil
}

func (f *fakeUserRepository) UsernameExists(context.Context, string, string) (bool, error) {
	return f.usernameUsed, nil
}

func (f *fakeUserRepository) CreateUser(_ context.Context, user model.User) (model.User, error) {
	if f.createErr != nil {
		return model.User{}, f.createErr
	}
	user.ID = "created-user"
	f.created = user
	return user, nil
}

func (f *fakeUserRepository) UpdateUser(_ context.Context, user model.User) (model.User, error) {
	if f.updateErr != nil {
		return model.User{}, f.updateErr
	}
	f.updated = user
	f.users[user.ID] = user
	return user, nil
}

type repositoryConflictTestError struct{ cause error }

func (e repositoryConflictTestError) Error() string  { return "repository conflict: " + e.cause.Error() }
func (e repositoryConflictTestError) Unwrap() error  { return e.cause }
func (e repositoryConflictTestError) Conflict() bool { return true }

func (f *fakeUserRepository) DeleteUser(_ context.Context, user model.User) error {
	f.deleted = user
	delete(f.users, user.ID)
	return nil
}

func TestUserServiceCreateRejectsDuplicateUsername(t *testing.T) {
	users, err := NewUserService(&fakeUserRepository{usernameUsed: true})
	if err != nil {
		t.Fatalf("new user service: %v", err)
	}

	_, err = users.Create(context.Background(), UserCreateInput{Username: "alice", Password: "secret"})
	if !errors.Is(err, ErrUserConflict) {
		t.Fatalf("create error = %v, want ErrUserConflict", err)
	}
}

func TestUserServiceCreateRejectsExpiredUser(t *testing.T) {
	users, err := NewUserService(&fakeUserRepository{})
	if err != nil {
		t.Fatalf("new user service: %v", err)
	}
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	users.now = func() time.Time { return now }
	expiresAt := now.Add(-time.Minute)

	_, err = users.Create(context.Background(), UserCreateInput{
		Username: "alice", Password: "secret", ExpiresAt: &expiresAt,
	})
	if !errors.Is(err, ErrInvalidUser) {
		t.Fatalf("create error = %v, want ErrInvalidUser", err)
	}
}

func TestUserServiceMapsRepositoryConflictAndPreservesCause(t *testing.T) {
	cause := errors.New("duplicate username")
	users, err := NewUserService(&fakeUserRepository{createErr: repositoryConflictTestError{cause: cause}})
	if err != nil {
		t.Fatalf("new user service: %v", err)
	}
	_, err = users.Create(context.Background(), UserCreateInput{Username: "alice", Password: "secret"})
	if !errors.Is(err, ErrUserConflict) || !errors.Is(err, cause) {
		t.Fatalf("create error = %v, want conflict and cause in chain", err)
	}
}

func TestUserServiceDoesNotDisableOrDeleteSuperAdmin(t *testing.T) {
	repository := &fakeUserRepository{users: map[string]model.User{
		"super": {ID: "super", Username: "root", Status: "active", IsSuperAdmin: true},
	}}
	users, err := NewUserService(repository)
	if err != nil {
		t.Fatalf("new user service: %v", err)
	}
	disabled := "disabled"

	_, err = users.Update(context.Background(), "super", UserUpdateInput{Status: &disabled})
	if !errors.Is(err, ErrUserForbidden) {
		t.Fatalf("disable error = %v, want ErrUserForbidden", err)
	}
	if err := users.Delete(context.Background(), "super", "another-user"); !errors.Is(err, ErrUserForbidden) {
		t.Fatalf("delete error = %v, want ErrUserForbidden", err)
	}
}

func TestUserServiceReturnsNotFoundForMissingUser(t *testing.T) {
	users, err := NewUserService(&fakeUserRepository{users: map[string]model.User{}})
	if err != nil {
		t.Fatalf("new user service: %v", err)
	}

	_, err = users.Get(context.Background(), "missing")
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("get error = %v, want ErrUserNotFound", err)
	}
}

func TestUserServiceGetDisablesExpiredUserLikeList(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	expiresAt := now.Add(-time.Minute)
	repository := &fakeUserRepository{users: map[string]model.User{
		"expired": {ID: "expired", Username: "alice", Status: "active", ExpiresAt: &expiresAt},
	}}
	users, err := NewUserService(repository)
	if err != nil {
		t.Fatalf("new user service: %v", err)
	}
	users.now = func() time.Time { return now }

	view, err := users.Get(context.Background(), "expired")
	if err != nil {
		t.Fatalf("get expired user: %v", err)
	}
	if view.Status != "disabled" || repository.updated.Status != "disabled" {
		t.Fatalf("status = %q, persisted status = %q, want disabled", view.Status, repository.updated.Status)
	}
}

func TestUserServiceCreatePreservesPasswordWhitespace(t *testing.T) {
	repository := &fakeUserRepository{}
	users, err := NewUserService(repository)
	if err != nil {
		t.Fatalf("new user service: %v", err)
	}
	password := " secret with spaces "
	if _, err := users.Create(context.Background(), UserCreateInput{Username: "alice", Password: password}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(repository.created.PasswordHash), []byte(password)); err != nil {
		t.Fatalf("password hash does not preserve whitespace: %v", err)
	}
}
