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
	SuperAdmin      bool
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

type AdminSetupSessionRecord struct {
	SessionID  string
	UserID     string
	SecretHash string
	CSRFHash   string
	ExpiresAt  time.Time
	CreatedAt  time.Time
}

type AdminAuthRepository interface {
	AdminInitialized(ctx context.Context) (bool, error)
	FindAdminLoginCredential(ctx context.Context, username string) (AdminLoginCredential, bool, error)
	FindAdminEncryptionKeyCredential(ctx context.Context, userID string) (AdminLoginCredential, bool, error)
	PersistAdminLoginState(
		ctx context.Context,
		userID, expectedPasswordHash, mysqlNativeHash string,
		loggedInAt time.Time,
	) error
	SetupInitialAdmin(ctx context.Context, record AdminSetupRecord, session AdminSetupSessionRecord) error
	ClaimAdminEncryptionKey(
		ctx context.Context,
		userID, expectedPasswordHash string,
		claimedAt time.Time,
	) error
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

// VerifiedAdminLogin does not export password material. The verified hash
// version and protocol verifier remain private until CompleteLogin persists state.
type VerifiedAdminLogin struct {
	UserID       string
	Username     string
	passwordHash string
	verifier     string
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
		UserID:       credential.UserID,
		Username:     credential.Username,
		passwordHash: credential.PasswordHash,
		verifier:     verifier,
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
	if err := s.repository.PersistAdminLoginState(
		ctx,
		login.UserID,
		login.passwordHash,
		login.verifier,
		now,
	); err != nil {
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
	sessionRecord, session, err := prepareInitialAdminSession(record.UserID, now)
	if err != nil {
		return CreatedBrowserSession{}, fmt.Errorf("%w: %w", ErrAdminSessionCreate, err)
	}
	if err := s.repository.SetupInitialAdmin(ctx, record, sessionRecord); err != nil {
		return CreatedBrowserSession{}, fmt.Errorf("setup initial admin: %w", err)
	}
	return session, nil
}

func prepareInitialAdminSession(
	userID string,
	now time.Time,
) (AdminSetupSessionRecord, CreatedBrowserSession, error) {
	secret, err := newBrowserSecret()
	if err != nil {
		return AdminSetupSessionRecord{}, CreatedBrowserSession{}, err
	}
	csrf, err := newBrowserSecret()
	if err != nil {
		return AdminSetupSessionRecord{}, CreatedBrowserSession{}, err
	}
	sessionID := model.NewID()
	expiresAt := now.Add(browserSessionTTL)
	record := AdminSetupSessionRecord{
		SessionID:  sessionID,
		UserID:     userID,
		SecretHash: browserSecretHash(secret),
		CSRFHash:   browserSecretHash(csrf),
		ExpiresAt:  expiresAt,
		CreatedAt:  now,
	}
	created := CreatedBrowserSession{
		SessionID: sessionID,
		Secret:    secret,
		CSRFToken: csrf,
		ExpiresAt: expiresAt,
	}
	return record, created, nil
}

func (s *AdminAuthService) ClaimEncryptionKey(
	ctx context.Context,
	userID string,
	password string,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	userID = strings.TrimSpace(userID)
	credential, found, err := s.repository.FindAdminEncryptionKeyCredential(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("find admin encryption key credential: %w", err)
	}
	hash := invalidAdminPasswordHash
	if found && strings.TrimSpace(credential.PasswordHash) != "" {
		hash = credential.PasswordHash
	}
	passwordMatches := VerifyAdminPassword(hash, password)
	now := s.now().UTC()
	if !found || !passwordMatches || credential.Status != "active" || !credential.SuperAdmin ||
		(credential.ExpiresAt != nil && !credential.ExpiresAt.After(now)) {
		return "", ErrAdminEncryptionKeyDenied
	}
	key, err := s.keyReader.ReadAdminEncryptionKey(ctx)
	if err != nil {
		return "", fmt.Errorf("read admin encryption key: %w", err)
	}
	if len(key) != 32 {
		return "", fmt.Errorf("read admin encryption key: invalid AES-256 key length %d", len(key))
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := s.repository.ClaimAdminEncryptionKey(ctx, userID, credential.PasswordHash, now); err != nil {
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
