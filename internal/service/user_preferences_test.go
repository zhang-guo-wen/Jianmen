package service

import (
	"context"
	"errors"
	"testing"

	"jianmen/internal/model"
)

type userPreferenceRepositoryStub struct {
	readPreference model.UserPreference
	savePreference model.UserPreference
	readErr        error
	saveErr        error
	readCalls      int
	saveCalls      int
	readContexts   []context.Context
	saveContexts   []context.Context
}

func (s *userPreferenceRepositoryStub) UserPreference(
	ctx context.Context,
	_ string,
) (model.UserPreference, error) {
	s.readCalls++
	s.readContexts = append(s.readContexts, ctx)
	if s.readErr != nil {
		return model.UserPreference{}, s.readErr
	}
	return s.readPreference, nil
}

func (s *userPreferenceRepositoryStub) SaveUserPreference(ctx context.Context, preference model.UserPreference) (model.UserPreference, error) {
	s.saveCalls++
	s.saveContexts = append(s.saveContexts, ctx)
	s.savePreference = preference
	if s.saveErr != nil {
		return model.UserPreference{}, s.saveErr
	}
	return preference, nil
}

func TestNewUserPreferenceServiceRejectsNilDependency(t *testing.T) {
	if _, err := NewUserPreferenceService(nil); err == nil {
		t.Fatal("constructor accepted nil dependency")
	}
}

func TestUserPreferenceServiceGetMergesDefaults(t *testing.T) {
	repository := &userPreferenceRepositoryStub{
		readPreference: model.UserPreference{
			UserID: "u1",
		},
	}
	service, err := NewUserPreferenceService(repository)
	if err != nil {
		t.Fatalf("new user preference service: %v", err)
	}

	preference, err := service.Get(context.Background(), " u1 ")
	if err != nil {
		t.Fatalf("get user preference: %v", err)
	}
	if preference.UserID != "u1" || preference.Theme != "light" || preference.TerminalFontSize != 14 {
		t.Fatalf("unexpected preference: %#v", preference)
	}
	if repository.readCalls != 1 {
		t.Fatalf("read calls = %d, want 1", repository.readCalls)
	}
}

func TestUserPreferenceServiceUpdateMergesPartialAndValidates(t *testing.T) {
	repository := &userPreferenceRepositoryStub{
		readPreference: model.UserPreference{
			UserID:             "u1",
			Theme:              "light",
			SSHClient:          "default",
			TerminalFontFamily: "Cascadia Mono, Consolas, monospace",
			TerminalFontSize:   14,
		},
	}
	service, err := NewUserPreferenceService(repository)
	if err != nil {
		t.Fatalf("new user preference service: %v", err)
	}

	theme := "DARK"
	fontSize := 16
	preference, err := service.Update(
		context.Background(),
		"u1",
		UserPreferencePatch{
			Theme:            &theme,
			TerminalFontSize: &fontSize,
		},
	)
	if err != nil {
		t.Fatalf("update user preference: %v", err)
	}
	if preference.Theme != "dark" || preference.TerminalFontSize != 16 || preference.SSHClient != "default" {
		t.Fatalf("unexpected merged preference: %#v", preference)
	}
	if repository.savePreference.Theme != "dark" || repository.savePreference.TerminalFontSize != 16 {
		t.Fatalf("unexpected saved preference: %#v", repository.savePreference)
	}
	if repository.saveCalls != 1 || repository.readCalls != 1 {
		t.Fatalf("store calls save=%d read=%d, want read=1 save=1", repository.readCalls, repository.saveCalls)
	}
}

func TestUserPreferenceServiceRejectsInvalidValues(t *testing.T) {
	repository := &userPreferenceRepositoryStub{
		readPreference: model.UserPreference{UserID: "u1"},
	}
	service, err := NewUserPreferenceService(repository)
	if err != nil {
		t.Fatalf("new user preference service: %v", err)
	}

	theme := "neon"
	if _, err := service.Update(context.Background(), "u1", UserPreferencePatch{Theme: &theme}); !errors.Is(err, ErrInvalidUserPreference) {
		t.Fatalf("invalid theme error = %v, want %v", err, ErrInvalidUserPreference)
	}

	fontSize := 1
	if _, err := service.Update(context.Background(), "u1", UserPreferencePatch{TerminalFontSize: &fontSize}); !errors.Is(err, ErrInvalidUserPreference) {
		t.Fatalf("invalid font size error = %v, want %v", err, ErrInvalidUserPreference)
	}
}

func TestUserPreferenceServiceRejectsMissingUserID(t *testing.T) {
	service, err := NewUserPreferenceService(&userPreferenceRepositoryStub{})
	if err != nil {
		t.Fatalf("new user preference service: %v", err)
	}

	if _, err := service.Get(context.Background(), "   "); !errors.Is(err, ErrInvalidUserPreference) {
		t.Fatalf("empty user id get error = %v, want %v", err, ErrInvalidUserPreference)
	}
	if _, err := service.Update(context.Background(), "", UserPreferencePatch{}); !errors.Is(err, ErrInvalidUserPreference) {
		t.Fatalf("empty user id update error = %v, want %v", err, ErrInvalidUserPreference)
	}
}

func TestUserPreferenceServicePropagatesReadAndSaveErrors(t *testing.T) {
	readFailure := errors.New("read preference failed")
	repository := &userPreferenceRepositoryStub{readErr: readFailure}
	service, err := NewUserPreferenceService(repository)
	if err != nil {
		t.Fatalf("new user preference service: %v", err)
	}
	if _, err := service.Get(context.Background(), "u1"); !errors.Is(err, readFailure) {
		t.Fatalf("read error = %v, want %v", err, readFailure)
	}

	repository = &userPreferenceRepositoryStub{
		readPreference: model.UserPreference{UserID: "u1"},
		saveErr:        errors.New("save preference failed"),
	}
	service, err = NewUserPreferenceService(repository)
	if err != nil {
		t.Fatalf("new user preference service: %v", err)
	}
	_, err = service.Update(context.Background(), "u1", UserPreferencePatch{})
	if !errors.Is(err, repository.saveErr) {
		t.Fatalf("save error = %v, want %v", err, repository.saveErr)
	}
}

func TestUserPreferenceServicePropagatesContextCancellation(t *testing.T) {
	repository := &userPreferenceRepositoryStub{
		readPreference: model.UserPreference{UserID: "u1"},
	}
	service, err := NewUserPreferenceService(repository)
	if err != nil {
		t.Fatalf("new user preference service: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Get(ctx, "u1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled get error = %v, want %v", err, context.Canceled)
	}
	if repository.readCalls != 0 {
		t.Fatalf("read called despite canceled context: %d", repository.readCalls)
	}
	if _, err := service.Update(ctx, "u1", UserPreferencePatch{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled update error = %v, want %v", err, context.Canceled)
	}
	if repository.readCalls != 0 {
		t.Fatalf("read called despite canceled context during update: %d", repository.readCalls)
	}
}
