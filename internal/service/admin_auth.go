package service

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/util"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrAdminAlreadyInitialized   = errors.New("admin is already initialized")
	ErrAdminInvalidCredentials   = errors.New("invalid admin credentials")
	ErrAdminInvalidSetup         = errors.New("invalid admin setup")
	ErrAdminLoginStatePersist    = errors.New("persist admin login state")
	ErrAdminSessionCreate        = errors.New("create admin browser session")
	ErrAdminSetupNotCompleted    = errors.New("admin setup is not completed")
	ErrAdminEncryptionKeyClaimed = errors.New("admin encryption key already claimed")
	ErrAdminEncryptionKeyDenied  = errors.New("admin encryption key claim denied")
)

const invalidAdminPasswordHash = "$2a$10$7EqJtq98hPqEX7fNZaFWoO5jHhJ5wHWuE6nH.8w4V.qB5n5kLxC6K"

type AdminLoginCredential struct {
	UserID          string
	Username        string
	PasswordHash    string
	MySQLNativeHash string
	Status          string
	ExpiresAt       *time.Time
}

type AdminSetupRecord struct {
	UserID          string
	Username        string
	PasswordHash    string
	MySQLNativeHash string
	DisplayName     string
	Email           string
	Status          string
	SuperAdmin      bool
	CreatedAt       time.Time
}

type AdminAuthRepository interface {
	AdminInitialized(ctx context.Context) (bool, error)
	FindAdminLoginCredential(ctx context.Context, username string) (AdminLoginCredential, bool, error)
	PersistAdminLoginState(ctx context.Context, userID, mysqlNativeHash string, loggedInAt time.Time) error
	SetupInitialAdmin(ctx context.Context, record AdminSetupRecord) error
	ValidateAdminEncryptionKeyClaimer(ctx context.Context, userID string) error
	ClaimAdminEncryptionKey(ctx context.Context, userID string, claimedAt time.Time) error
}

type AdminEncryptionKeyReader interface {
	ReadAdminEncryptionKey(ctx context.Context) ([]byte, error)
}

type AdminSetupInput struct {
	Username    string
	Password    string
	Email       string
	DisplayName string
}

// VerifiedAdminLogin deliberately omits password and password hashes. The
// unexported verifier is carried only until CompleteLogin persists state.
type VerifiedAdminLogin struct {
	UserID   string
	Username string
	verifier string
}

type AdminAuthService struct {
	repository      AdminAuthRepository
	browserSessions *BrowserSessionService
	keyReader       AdminEncryptionKeyReader
	now             func() time.Time
}

func NewAdminAuthService(
	repository AdminAuthRepository,
	browserSessions *BrowserSessionService,
	keyReader AdminEncryptionKeyReader,
) (*AdminAuthService, error) {
	if repository == nil {
		return nil, errors.New("admin auth repository is required")
	}
	if browserSessions == nil {
		return nil, errors.New("admin browser session service is required")
	}
	if keyReader == nil {
		return nil, errors.New("admin encryption key reader is required")
	}
	return &AdminAuthService{
		repository:      repository,
		browserSessions: browserSessions,
		keyReader:       keyReader,
		now:             func() time.Time { return time.Now().UTC() },
	}, nil
}

func (s *AdminAuthService) Initialized(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	initialized, err := s.repository.AdminInitialized(ctx)
	if err != nil {
		return false, fmt.Errorf("check admin initialization: %w", err)
	}
	return initialized, nil
}

func (s *AdminAuthService) VerifyLogin(
	ctx context.Context,
	username string,
	password string,
) (VerifiedAdminLogin, error) {
	if err := ctx.Err(); err != nil {
		return VerifiedAdminLogin{}, err
	}
	username = strings.TrimSpace(username)
	credential, found, err := s.repository.FindAdminLoginCredential(ctx, username)
	if err != nil {
		return VerifiedAdminLogin{}, fmt.Errorf("find admin login credential: %w", err)
	}

	hash := invalidAdminPasswordHash
	if found && strings.TrimSpace(credential.PasswordHash) != "" {
		hash = credential.PasswordHash
	}
	passwordMatches := VerifyAdminPassword(hash, password)
	now := s.now().UTC()
	if !found || !passwordMatches || credential.Status != "active" ||
		(credential.ExpiresAt != nil && !credential.ExpiresAt.After(now)) {
		return VerifiedAdminLogin{}, ErrAdminInvalidCredentials
	}

	verifier := credential.MySQLNativeHash
	if strings.TrimSpace(verifier) == "" {
		verifier = util.MySQLNativePasswordHash(password)
	}
	return VerifiedAdminLogin{
		UserID:   credential.UserID,
		Username: credential.Username,
		verifier: verifier,
	}, nil
}

func (s *AdminAuthService) CompleteLogin(
	ctx context.Context,
	login VerifiedAdminLogin,
) (CreatedBrowserSession, error) {
	if err := ctx.Err(); err != nil {
		return CreatedBrowserSession{}, err
	}
	now := s.now().UTC()
	if err := s.repository.PersistAdminLoginState(ctx, login.UserID, login.verifier, now); err != nil {
		return CreatedBrowserSession{}, fmt.Errorf("%w: %w", ErrAdminLoginStatePersist, err)
	}
	session, err := s.browserSessions.Create(ctx, login.UserID)
	if err != nil {
		return CreatedBrowserSession{}, fmt.Errorf("%w: %w", ErrAdminSessionCreate, err)
	}
	return session, nil
}

func (s *AdminAuthService) Setup(
	ctx context.Context,
	input AdminSetupInput,
) (CreatedBrowserSession, error) {
	if err := ctx.Err(); err != nil {
		return CreatedBrowserSession{}, err
	}
	username := strings.TrimSpace(input.Username)
	password := input.Password
	if username == "" || strings.TrimSpace(password) == "" {
		return CreatedBrowserSession{}, fmt.Errorf("%w: username and password are required", ErrAdminInvalidSetup)
	}
	if len(password) < 8 {
		return CreatedBrowserSession{}, fmt.Errorf("%w: password must be at least 8 characters", ErrAdminInvalidSetup)
	}
	passwordHash, err := HashAdminPassword(password)
	if err != nil {
		return CreatedBrowserSession{}, err
	}
	now := s.now().UTC()
	record := AdminSetupRecord{
		UserID:          model.NewID(),
		Username:        username,
		PasswordHash:    passwordHash,
		MySQLNativeHash: util.MySQLNativePasswordHash(password),
		DisplayName:     strings.TrimSpace(input.DisplayName),
		Email:           strings.TrimSpace(input.Email),
		Status:          "active",
		SuperAdmin:      true,
		CreatedAt:       now,
	}
	if err := s.repository.SetupInitialAdmin(ctx, record); err != nil {
		return CreatedBrowserSession{}, fmt.Errorf("setup initial admin: %w", err)
	}
	session, err := s.browserSessions.Create(ctx, record.UserID)
	if err != nil {
		return CreatedBrowserSession{}, fmt.Errorf("%w: %v", ErrAdminSessionCreate, err)
	}
	return session, nil
}

func (s *AdminAuthService) ClaimEncryptionKey(ctx context.Context, userID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	userID = strings.TrimSpace(userID)
	if err := s.repository.ValidateAdminEncryptionKeyClaimer(ctx, userID); err != nil {
		return "", fmt.Errorf("validate admin encryption key claim: %w", err)
	}
	key, err := s.keyReader.ReadAdminEncryptionKey(ctx)
	if err != nil {
		return "", fmt.Errorf("read admin encryption key: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := s.repository.ClaimAdminEncryptionKey(ctx, userID, s.now().UTC()); err != nil {
		return "", fmt.Errorf("claim admin encryption key: %w", err)
	}
	return hex.EncodeToString(key), nil
}

func HashAdminPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash admin password: %w", err)
	}
	return string(hash), nil
}

func VerifyAdminPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
