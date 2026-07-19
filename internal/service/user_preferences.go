package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"jianmen/internal/model"
)

var ErrInvalidUserPreference = errors.New("invalid user preference")

type UserPreferenceRepository interface {
	UserPreference(context.Context, string) (model.UserPreference, error)
	SaveUserPreference(context.Context, model.UserPreference) (model.UserPreference, error)
}

type UserPreferencePatch struct {
	Theme              *string `json:"theme"`
	SSHClient          *string `json:"ssh_client"`
	SSHClientPath      *string `json:"ssh_client_path"`
	TerminalFontFamily *string `json:"terminal_font_family"`
	TerminalFontSize   *int    `json:"terminal_font_size"`
}

type UserPreferenceService struct {
	repository UserPreferenceRepository
}

func NewUserPreferenceService(repository UserPreferenceRepository) (*UserPreferenceService, error) {
	if repository == nil {
		return nil, errors.New("user preference repository is required")
	}
	return &UserPreferenceService{repository: repository}, nil
}

func (s *UserPreferenceService) Get(ctx context.Context, userID string) (model.UserPreference, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return model.UserPreference{}, fmt.Errorf("%w: user id is required", ErrInvalidUserPreference)
	}
	if err := ctx.Err(); err != nil {
		return model.UserPreference{}, err
	}

	preference, err := s.repository.UserPreference(ctx, userID)
	if err != nil {
		return model.UserPreference{}, fmt.Errorf("load user preference: %w", err)
	}
	preference.UserID = userID
	return normalizeStoredUserPreference(preference), nil
}

func (s *UserPreferenceService) Update(
	ctx context.Context,
	userID string,
	input UserPreferencePatch,
) (model.UserPreference, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return model.UserPreference{}, fmt.Errorf("%w: user id is required", ErrInvalidUserPreference)
	}
	if err := ctx.Err(); err != nil {
		return model.UserPreference{}, err
	}

	preference, err := s.repository.UserPreference(ctx, userID)
	if err != nil {
		return model.UserPreference{}, fmt.Errorf("load user preference: %w", err)
	}
	preference.UserID = userID
	applyUserPreferencePatch(&preference, input)
	preference = normalizeStoredUserPreference(preference)
	if message := validateUserPreference(preference); message != "" {
		return model.UserPreference{}, fmt.Errorf("%w: %s", ErrInvalidUserPreference, message)
	}

	updated, err := s.repository.SaveUserPreference(ctx, preference)
	if err != nil {
		return model.UserPreference{}, fmt.Errorf("save user preference: %w", err)
	}
	updated.UserID = userID
	return normalizeStoredUserPreference(updated), nil
}

func applyUserPreferencePatch(preference *model.UserPreference, input UserPreferencePatch) {
	if input.Theme != nil {
		preference.Theme = strings.ToLower(strings.TrimSpace(*input.Theme))
	}
	if input.SSHClient != nil {
		preference.SSHClient = strings.ToLower(strings.TrimSpace(*input.SSHClient))
	}
	if input.SSHClientPath != nil {
		preference.SSHClientPath = strings.TrimSpace(*input.SSHClientPath)
	}
	if input.TerminalFontFamily != nil {
		preference.TerminalFontFamily = strings.TrimSpace(*input.TerminalFontFamily)
	}
	if input.TerminalFontSize != nil {
		preference.TerminalFontSize = *input.TerminalFontSize
	}
}

func normalizeStoredUserPreference(preference model.UserPreference) model.UserPreference {
	preference.Theme = strings.TrimSpace(preference.Theme)
	preference.SSHClient = strings.TrimSpace(preference.SSHClient)
	preference.SSHClientPath = strings.TrimSpace(preference.SSHClientPath)
	preference.TerminalFontFamily = strings.TrimSpace(preference.TerminalFontFamily)

	if preference.Theme == "" {
		preference.Theme = "light"
	}
	if preference.TerminalFontFamily == "" {
		preference.TerminalFontFamily = "Cascadia Mono, Consolas, monospace"
	}
	if preference.TerminalFontSize == 0 {
		preference.TerminalFontSize = 14
	}
	return preference
}

func validateUserPreference(preference model.UserPreference) string {
	validThemes := map[string]bool{"system": true, "light": true, "dark": true}
	if !validThemes[preference.Theme] {
		return "theme must be system, light, or dark"
	}
	validClients := map[string]bool{"": true, "default": true, "xshell": true, "putty": true, "securecrt": true, "mobaxterm": true, "winterm": true, "system": true}
	if !validClients[preference.SSHClient] {
		return "unsupported ssh client"
	}
	if preference.TerminalFontSize < 10 || preference.TerminalFontSize > 30 {
		return "terminal_font_size must be between 10 and 30"
	}
	if len(preference.SSHClientPath) > 512 || len(preference.TerminalFontFamily) > 128 {
		return "preference value is too long"
	}
	return ""
}
