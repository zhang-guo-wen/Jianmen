package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"jianmen/internal/model"
)

type browserSessionMemoryRepository struct {
	mu       sync.Mutex
	sessions map[string]model.AdminSession
	tickets  map[string]model.WebSocketTicket
}

func (r *browserSessionMemoryRepository) CreateAdminSession(_ context.Context, session model.AdminSession) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[session.ID] = session
	return nil
}

func (r *browserSessionMemoryRepository) FindActiveAdminSessionBySecretHash(_ context.Context, hash string, now time.Time) (BrowserSessionSubject, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, session := range r.sessions {
		if session.SecretHash == hash && session.RevokedAt == nil && session.ExpiresAt.After(now) {
			return BrowserSessionSubject{SessionID: session.ID, UserID: session.UserID, CSRFHash: session.CSRFHash}, true, nil
		}
	}
	return BrowserSessionSubject{}, false, nil
}

func (r *browserSessionMemoryRepository) RevokeAdminSession(_ context.Context, sessionID string, now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	session := r.sessions[sessionID]
	session.RevokedAt = &now
	r.sessions[sessionID] = session
	return nil
}

func (r *browserSessionMemoryRepository) CreateWebSocketTicket(_ context.Context, ticket model.WebSocketTicket) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tickets[ticket.SecretHash] = ticket
	return nil
}

func (r *browserSessionMemoryRepository) ConsumeWebSocketTicket(_ context.Context, secretHash, purpose, targetID string, now time.Time) (WebSocketTicketSubject, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ticket, ok := r.tickets[secretHash]
	if !ok || ticket.Purpose != purpose || ticket.TargetID != targetID || ticket.ConsumedAt != nil || !ticket.ExpiresAt.After(now) {
		return WebSocketTicketSubject{}, false, nil
	}
	ticket.ConsumedAt = &now
	r.tickets[secretHash] = ticket
	session := r.sessions[ticket.SessionID]
	if session.RevokedAt != nil || !session.ExpiresAt.After(now) {
		return WebSocketTicketSubject{}, false, nil
	}
	return WebSocketTicketSubject{
		BrowserSessionSubject: BrowserSessionSubject{SessionID: session.ID, UserID: session.UserID, CSRFHash: session.CSRFHash},
		Purpose:               ticket.Purpose,
		TargetID:              ticket.TargetID,
		ConnectionID:          ticket.ConnectionID,
	}, true, nil
}

func TestBrowserSessionServiceLifecycle(t *testing.T) {
	repository := &browserSessionMemoryRepository{sessions: map[string]model.AdminSession{}, tickets: map[string]model.WebSocketTicket{}}
	service, err := NewBrowserSessionService(repository)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	created, err := service.Create(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Secret) != 64 || len(created.CSRFToken) != 64 {
		t.Fatalf("expected 32-byte hex secrets")
	}
	subject, found, err := service.Authenticate(context.Background(), created.Secret)
	if err != nil || !found || subject.UserID != "user-1" {
		t.Fatalf("authenticate = %#v, %v, %v", subject, found, err)
	}
	if !service.ValidCSRF(subject, created.CSRFToken) || service.ValidCSRF(subject, "wrong") {
		t.Fatal("csrf validation mismatch")
	}

	ticket, err := service.CreateWebSocketTicket(context.Background(), subject, "target-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := service.ConsumeWebSocketTicket(context.Background(), ticket, "target-2"); err != nil || found {
		t.Fatal("ticket target binding was not enforced")
	}
	if _, found, err := service.ConsumeWebSocketTicket(context.Background(), ticket, "target-1"); err != nil || !found {
		t.Fatalf("consume = %v, %v", found, err)
	}
	if _, found, err := service.ConsumeWebSocketTicket(context.Background(), ticket, "target-1"); err != nil || found {
		t.Fatal("ticket was consumed more than once")
	}
	rdpTicket, err := service.CreateScopedWebSocketTicket(context.Background(), subject, WebSocketPurposeRDP, "target-1", "connection-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := service.ConsumeWebSocketTicket(context.Background(), rdpTicket, "target-1"); err != nil || found {
		t.Fatal("RDP ticket was accepted by terminal consumer")
	}
	scoped, found, err := service.ConsumeScopedWebSocketTicket(context.Background(), rdpTicket, WebSocketPurposeRDP, "target-1")
	if err != nil || !found || scoped.ConnectionID != "connection-1" {
		t.Fatalf("consume scoped ticket = %#v, %v, %v", scoped, found, err)
	}
	if err := service.Revoke(context.Background(), subject.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := service.Authenticate(context.Background(), created.Secret); found {
		t.Fatal("revoked session remained active")
	}
}

func TestBrowserSessionServiceExpiryAndConcurrentTicketConsume(t *testing.T) {
	repository := &browserSessionMemoryRepository{sessions: map[string]model.AdminSession{}, tickets: map[string]model.WebSocketTicket{}}
	svc, err := NewBrowserSessionService(repository)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	created, err := svc.Create(context.Background(), "user-1")
	if err != nil {
		t.Fatal(err)
	}
	subject, found, err := svc.Authenticate(context.Background(), created.Secret)
	if err != nil || !found {
		t.Fatalf("authenticate = %v, %v", found, err)
	}
	ticket, err := svc.CreateWebSocketTicket(context.Background(), subject, "target-1")
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan bool, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, found, err := svc.ConsumeWebSocketTicket(context.Background(), ticket, "target-1")
			results <- err == nil && found
		}()
	}
	wg.Wait()
	close(results)
	consumed := 0
	for result := range results {
		if result {
			consumed++
		}
	}
	if consumed != 1 {
		t.Fatalf("ticket consumers = %d, want exactly one", consumed)
	}
	now = now.Add(browserSessionTTL + time.Second)
	if _, found, err := svc.Authenticate(context.Background(), created.Secret); err != nil || found {
		t.Fatalf("expired session accepted: found=%v err=%v", found, err)
	}
}

func TestBrowserSessionServiceValidCSRF(t *testing.T) {
	svc, err := NewBrowserSessionService(&browserSessionMemoryRepository{sessions: map[string]model.AdminSession{}, tickets: map[string]model.WebSocketTicket{}})
	if err != nil {
		t.Fatal(err)
	}
	hash := browserSecretHash("csrf-token")
	subject := BrowserSessionSubject{CSRFHash: hash}
	if !svc.ValidCSRF(subject, "csrf-token") {
		t.Fatal("matching CSRF token was rejected")
	}
	if svc.ValidCSRF(subject, "different-token") {
		t.Fatal("mismatched CSRF token was accepted")
	}
	if svc.ValidCSRF(BrowserSessionSubject{}, "csrf-token") || svc.ValidCSRF(subject, "") {
		t.Fatal("empty CSRF value was accepted")
	}
}
