package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

type SQLConsoleSessionInfo struct {
	ID              string   `json:"id"`
	Databases       []string `json:"databases"`
	DefaultDatabase string   `json:"default_database"`
}

type sqlConsoleSession struct {
	id         string
	userID     string
	accountID  string
	connection SQLConsoleConnection
	databases  map[string]struct{}
	timer      *time.Timer
	expiresAt  time.Time
}

func (s *SQLConsoleService) CreateSession(
	ctx context.Context,
	actor SQLConsoleActor,
	accountID string,
) (SQLConsoleSessionInfo, error) {
	userID := strings.TrimSpace(actor.UserID)
	accountID = strings.TrimSpace(accountID)
	if ctx == nil || userID == "" || accountID == "" {
		return SQLConsoleSessionInfo{}, ErrSQLConsoleInvalid
	}
	allowed, err := s.authorizer.AuthorizeConnection(
		ctx, userID, []string{rbac.ActionDBQuery}, model.ResourceTypeDatabaseAccount, accountID,
	)
	if err != nil {
		return SQLConsoleSessionInfo{}, fmt.Errorf("authorize SQL console: %w", err)
	}
	if !allowed {
		return SQLConsoleSessionInfo{}, ErrSQLConsoleForbidden
	}
	account, _, err := s.loadSQLConsoleAccount(ctx, accountID)
	if err != nil {
		return SQLConsoleSessionInfo{}, err
	}
	connection, err := s.executor.Connect(ctx, account)
	if err != nil {
		return SQLConsoleSessionInfo{}, fmt.Errorf("%w: %v", ErrSQLConsoleExecution, err)
	}
	databases := connection.Databases()
	session := &sqlConsoleSession{
		id:         uuid.NewString(),
		userID:     userID,
		accountID:  accountID,
		connection: connection,
		databases:  make(map[string]struct{}, len(databases)),
	}
	for _, database := range databases {
		session.databases[database] = struct{}{}
	}
	s.sessionsMu.Lock()
	s.sessions[session.id] = session
	session.expiresAt = time.Now().Add(s.idleTTL)
	session.timer = time.AfterFunc(s.idleTTL, func() { s.expireSession(session.id) })
	s.sessionsMu.Unlock()
	return SQLConsoleSessionInfo{
		ID:              session.id,
		Databases:       databases,
		DefaultDatabase: connection.DefaultDatabase(),
	}, nil
}

func (s *SQLConsoleService) CloseSession(ctx context.Context, actor SQLConsoleActor, id string) error {
	if ctx == nil || strings.TrimSpace(actor.UserID) == "" || strings.TrimSpace(id) == "" {
		return ErrSQLConsoleInvalid
	}
	s.sessionsMu.Lock()
	session, ok := s.sessions[strings.TrimSpace(id)]
	if !ok || session.userID != strings.TrimSpace(actor.UserID) {
		s.sessionsMu.Unlock()
		return ErrSQLConsoleSession
	}
	delete(s.sessions, session.id)
	if session.timer != nil {
		session.timer.Stop()
	}
	s.sessionsMu.Unlock()
	if err := session.connection.Close(); err != nil {
		return fmt.Errorf("close SQL console connection: %w", err)
	}
	return nil
}

func (s *SQLConsoleService) Close() {
	s.sessionsMu.Lock()
	sessions := make([]*sqlConsoleSession, 0, len(s.sessions))
	for id, session := range s.sessions {
		delete(s.sessions, id)
		if session.timer != nil {
			session.timer.Stop()
		}
		sessions = append(sessions, session)
	}
	s.sessionsMu.Unlock()
	for _, session := range sessions {
		_ = session.connection.Close()
	}
}

func (s *SQLConsoleService) sessionForActor(id, userID string) (*sqlConsoleSession, error) {
	s.sessionsMu.Lock()
	defer s.sessionsMu.Unlock()
	session, ok := s.sessions[id]
	if !ok || session.userID != userID {
		return nil, ErrSQLConsoleSession
	}
	if session.timer != nil {
		session.expiresAt = time.Now().Add(s.idleTTL)
		session.timer.Reset(s.idleTTL)
	}
	return session, nil
}

func (s *SQLConsoleService) expireSession(id string) {
	s.sessionsMu.Lock()
	session, ok := s.sessions[id]
	if ok && time.Now().Before(session.expiresAt) {
		remaining := time.Until(session.expiresAt)
		if session.timer != nil {
			session.timer.Reset(remaining)
		}
		s.sessionsMu.Unlock()
		return
	}
	if ok {
		delete(s.sessions, id)
	}
	s.sessionsMu.Unlock()
	if ok {
		_ = session.connection.Close()
	}
}

func (s *SQLConsoleService) loadSQLConsoleAccount(
	ctx context.Context,
	accountID string,
) (model.DatabaseAccount, time.Time, error) {
	account, found, err := s.repository.FindActiveDatabaseAccount(ctx, strings.TrimSpace(accountID))
	if err != nil {
		return model.DatabaseAccount{}, time.Time{}, fmt.Errorf("load database account: %w", err)
	}
	if !found {
		return model.DatabaseAccount{}, time.Time{}, ErrSQLConsoleNotFound
	}
	now := s.now().UTC()
	if account.Status != "active" || account.Instance.Status != "active" ||
		account.ExpiresAt != nil && !account.ExpiresAt.After(now) || account.Password.GetPlaintext() == "" {
		return model.DatabaseAccount{}, time.Time{}, ErrSQLConsoleUnavailable
	}
	return account, now, nil
}

func (s *sqlConsoleSession) databaseAllowed(database string) bool {
	_, ok := s.databases[database]
	return ok
}
