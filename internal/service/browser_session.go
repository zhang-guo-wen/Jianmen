package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/model"
)

const (
	browserSessionTTL   = 12 * time.Hour
	webSocketTicketTTL  = 30 * time.Second
	browserSecretLength = 32

	WebSocketPurposeTerminal = "web-terminal"
	WebSocketPurposeRDP      = "web-rdp"
)

var ErrBrowserSessionInvalid = errors.New("browser session is invalid")

// BrowserSessionSubject contains only the state needed by browser protocol
// adapters.  It deliberately does not expose raw credentials.
type BrowserSessionSubject struct {
	SessionID string
	UserID    string
	CSRFHash  string
}

// BrowserSessionRepository is owned by this service's consumer boundary.
type BrowserSessionRepository interface {
	CreateAdminSession(ctx context.Context, session model.AdminSession) error
	FindActiveAdminSessionBySecretHash(ctx context.Context, secretHash string, now time.Time) (BrowserSessionSubject, bool, error)
	RevokeAdminSession(ctx context.Context, sessionID string, now time.Time) error
	CreateWebSocketTicket(ctx context.Context, ticket model.WebSocketTicket) error
	ConsumeWebSocketTicket(ctx context.Context, secretHash, purpose, targetID string, now time.Time) (WebSocketTicketSubject, bool, error)
}

// WebSocketTicketSubject is returned only after a purpose-bound, target-bound
// ticket has been atomically consumed.
type WebSocketTicketSubject struct {
	BrowserSessionSubject
	Purpose      string
	TargetID     string
	ConnectionID string
}

type BrowserSessionService struct {
	repository BrowserSessionRepository
	now        func() time.Time
}

type CreatedBrowserSession struct {
	SessionID string
	Secret    string
	CSRFToken string
	ExpiresAt time.Time
}

func NewBrowserSessionService(repository BrowserSessionRepository) (*BrowserSessionService, error) {
	if repository == nil {
		return nil, errors.New("browser session repository is required")
	}
	return &BrowserSessionService{repository: repository, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *BrowserSessionService) Create(ctx context.Context, userID string) (CreatedBrowserSession, error) {
	if err := ctx.Err(); err != nil {
		return CreatedBrowserSession{}, err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return CreatedBrowserSession{}, errors.New("browser session user id is required")
	}
	secret, err := newBrowserSecret()
	if err != nil {
		return CreatedBrowserSession{}, err
	}
	csrf, err := newBrowserSecret()
	if err != nil {
		return CreatedBrowserSession{}, err
	}
	now := s.now().UTC()
	expiresAt := now.Add(browserSessionTTL)
	session := model.AdminSession{ID: model.NewID(), UserID: userID, SecretHash: browserSecretHash(secret), CSRFHash: browserSecretHash(csrf), ExpiresAt: expiresAt}
	if err := s.repository.CreateAdminSession(ctx, session); err != nil {
		return CreatedBrowserSession{}, fmt.Errorf("create browser session: %w", err)
	}
	return CreatedBrowserSession{SessionID: session.ID, Secret: secret, CSRFToken: csrf, ExpiresAt: expiresAt}, nil
}

func (s *BrowserSessionService) Authenticate(ctx context.Context, secret string) (BrowserSessionSubject, bool, error) {
	if err := ctx.Err(); err != nil {
		return BrowserSessionSubject{}, false, err
	}
	if strings.TrimSpace(secret) == "" {
		return BrowserSessionSubject{}, false, nil
	}
	subject, found, err := s.repository.FindActiveAdminSessionBySecretHash(ctx, browserSecretHash(secret), s.now().UTC())
	if err != nil {
		return BrowserSessionSubject{}, false, fmt.Errorf("find browser session: %w", err)
	}
	return subject, found, nil
}

func (s *BrowserSessionService) ValidCSRF(subject BrowserSessionSubject, token string) bool {
	expected := strings.TrimSpace(subject.CSRFHash)
	if expected == "" || strings.TrimSpace(token) == "" {
		return false
	}
	provided := browserSecretHash(token)
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func (s *BrowserSessionService) Revoke(ctx context.Context, sessionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(sessionID) == "" {
		return ErrBrowserSessionInvalid
	}
	if err := s.repository.RevokeAdminSession(ctx, sessionID, s.now().UTC()); err != nil {
		return fmt.Errorf("revoke browser session: %w", err)
	}
	return nil
}

func (s *BrowserSessionService) CreateWebSocketTicket(ctx context.Context, subject BrowserSessionSubject, targetID string) (string, error) {
	return s.CreateScopedWebSocketTicket(ctx, subject, WebSocketPurposeTerminal, targetID, "")
}

func (s *BrowserSessionService) CreateScopedWebSocketTicket(
	ctx context.Context,
	subject BrowserSessionSubject,
	purpose string,
	targetID string,
	connectionID string,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(subject.SessionID) == "" || strings.TrimSpace(subject.UserID) == "" {
		return "", ErrBrowserSessionInvalid
	}
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return "", errors.New("websocket ticket target id is required")
	}
	purpose = strings.TrimSpace(purpose)
	if purpose == "" {
		return "", errors.New("websocket ticket purpose is required")
	}
	secret, err := newBrowserSecret()
	if err != nil {
		return "", err
	}
	now := s.now().UTC()
	ticket := model.WebSocketTicket{
		ID:           model.NewID(),
		SessionID:    subject.SessionID,
		Purpose:      purpose,
		TargetID:     targetID,
		ConnectionID: strings.TrimSpace(connectionID),
		SecretHash:   browserSecretHash(secret),
		ExpiresAt:    now.Add(webSocketTicketTTL),
	}
	if err := s.repository.CreateWebSocketTicket(ctx, ticket); err != nil {
		return "", fmt.Errorf("create websocket ticket: %w", err)
	}
	return secret, nil
}

func (s *BrowserSessionService) ConsumeWebSocketTicket(ctx context.Context, secret, targetID string) (BrowserSessionSubject, bool, error) {
	ticket, found, err := s.ConsumeScopedWebSocketTicket(ctx, secret, WebSocketPurposeTerminal, targetID)
	return ticket.BrowserSessionSubject, found, err
}

func (s *BrowserSessionService) ConsumeScopedWebSocketTicket(
	ctx context.Context,
	secret string,
	purpose string,
	targetID string,
) (WebSocketTicketSubject, bool, error) {
	if err := ctx.Err(); err != nil {
		return WebSocketTicketSubject{}, false, err
	}
	if strings.TrimSpace(secret) == "" || strings.TrimSpace(purpose) == "" || strings.TrimSpace(targetID) == "" {
		return WebSocketTicketSubject{}, false, nil
	}
	subject, found, err := s.repository.ConsumeWebSocketTicket(
		ctx,
		browserSecretHash(secret),
		strings.TrimSpace(purpose),
		strings.TrimSpace(targetID),
		s.now().UTC(),
	)
	if err != nil {
		return WebSocketTicketSubject{}, false, fmt.Errorf("consume websocket ticket: %w", err)
	}
	return subject, found, nil
}

func newBrowserSecret() (string, error) {
	bytes := make([]byte, browserSecretLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate browser secret: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func browserSecretHash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
