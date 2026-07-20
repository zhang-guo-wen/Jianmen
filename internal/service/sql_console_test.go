package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

type sqlConsoleRepositoryStub struct {
	account        model.DatabaseAccount
	found          bool
	sessions       []*model.AuditSession
	queries        []*model.AuditDBQuery
	finished       []string
	duration       int64
	createAuditErr error
}

func (s *sqlConsoleRepositoryStub) FindActiveDatabaseAccount(context.Context, string) (model.DatabaseAccount, bool, error) {
	return s.account, s.found, nil
}

func (s *sqlConsoleRepositoryStub) CreateAuditSession(_ context.Context, session *model.AuditSession) error {
	if s.createAuditErr != nil {
		return s.createAuditErr
	}
	if session.ID == "" {
		session.ID = "audit-session"
	}
	s.sessions = append(s.sessions, session)
	return nil
}

func (s *sqlConsoleRepositoryStub) CreateAuditDBQuery(_ context.Context, query *model.AuditDBQuery) error {
	if query.ID == "" {
		query.ID = "audit-query"
	}
	s.queries = append(s.queries, query)
	return nil
}

func (s *sqlConsoleRepositoryStub) UpdateAuditDBQueryDuration(_ context.Context, _ string, duration int64) error {
	s.duration = duration
	return nil
}

func (s *sqlConsoleRepositoryStub) FinishAuditSession(_ context.Context, _ string, outcome, _, _, _ string, _ time.Time) error {
	s.finished = append(s.finished, outcome)
	return nil
}

type sqlConsoleAuthorizerStub struct {
	allowed bool
	actions []string
}

func (s *sqlConsoleAuthorizerStub) AuthorizeConnection(_ context.Context, _ string, actions []string, _, _ string) (bool, error) {
	s.actions = append([]string(nil), actions...)
	return s.allowed, nil
}

type sqlConsoleExecutorStub struct {
	called   bool
	readOnly bool
	result   SQLConsoleExecution
	err      error
}

func (s *sqlConsoleExecutorStub) Execute(_ context.Context, _ model.DatabaseAccount, _, _ string, readOnly bool) (SQLConsoleExecution, error) {
	s.called = true
	s.readOnly = readOnly
	return s.result, s.err
}

func newSQLConsoleServiceFixture(t *testing.T) (*SQLConsoleService, *sqlConsoleRepositoryStub, *sqlConsoleAuthorizerStub, *sqlConsoleExecutorStub) {
	t.Helper()
	repository := &sqlConsoleRepositoryStub{
		found: true,
		account: model.DatabaseAccount{
			ID: "account-1", UniqueName: "reporting", Username: "reader",
			Password: model.NewEncryptedField("secret"), Status: "active",
			Instance: model.DatabaseInstance{
				ID: "instance-1", Name: "orders", Protocol: "postgres",
				Address: "db.internal", Port: 5432, Status: "active",
			},
		},
	}
	authorizer := &sqlConsoleAuthorizerStub{allowed: true}
	executor := &sqlConsoleExecutorStub{
		result: SQLConsoleExecution{
			Columns: []string{"id"}, Rows: [][]any{{"1"}},
		},
	}
	sqlService, err := NewSQLConsoleService(repository, authorizer, executor)
	if err != nil {
		t.Fatalf("NewSQLConsoleService() error = %v", err)
	}
	current := time.Unix(100, 0)
	sqlService.now = func() time.Time {
		current = current.Add(5 * time.Millisecond)
		return current
	}
	return sqlService, repository, authorizer, executor
}

func TestSQLConsoleExecuteReadQueryAuditsBeforeExecution(t *testing.T) {
	sqlService, repository, authorizer, executor := newSQLConsoleServiceFixture(t)
	result, err := sqlService.Execute(
		context.Background(),
		SQLConsoleActor{UserID: "user-1", Username: "alice", ClientIP: "192.0.2.10"},
		SQLConsoleRequest{AccountID: "account-1", Database: "app", SQL: "SELECT id FROM users"},
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !executor.called || !executor.readOnly {
		t.Fatalf("executor = called %t, readOnly %t", executor.called, executor.readOnly)
	}
	if len(authorizer.actions) != 1 || authorizer.actions[0] != rbac.ActionDBQuery {
		t.Fatalf("actions = %#v", authorizer.actions)
	}
	if len(repository.sessions) != 1 || len(repository.queries) != 1 {
		t.Fatalf("audit writes = sessions %d, queries %d", len(repository.sessions), len(repository.queries))
	}
	if len(repository.finished) != 1 || repository.finished[0] != model.AuditOutcomeSucceeded {
		t.Fatalf("finished outcomes = %#v", repository.finished)
	}
	if result.RowCount != 1 || result.AuditSessionID != "audit-session" || result.QueryKind != "select" {
		t.Fatalf("result = %#v", result)
	}
}

func TestSQLConsoleExecuteWriteRequiresConfirmationAndPermission(t *testing.T) {
	sqlService, repository, authorizer, executor := newSQLConsoleServiceFixture(t)
	request := SQLConsoleRequest{AccountID: "account-1", SQL: "DELETE FROM jobs WHERE id = 1"}
	if _, err := sqlService.Execute(context.Background(), SQLConsoleActor{UserID: "user-1"}, request); !errors.Is(err, ErrSQLConsoleWriteConfirmation) {
		t.Fatalf("Execute() error = %v", err)
	}
	if executor.called || len(repository.sessions) != 0 {
		t.Fatal("unconfirmed write reached execution or audit")
	}

	request.ConfirmWrite = true
	if _, err := sqlService.Execute(context.Background(), SQLConsoleActor{UserID: "user-1"}, request); err != nil {
		t.Fatalf("confirmed Execute() error = %v", err)
	}
	if len(authorizer.actions) != 1 || authorizer.actions[0] != rbac.ActionDBExecute {
		t.Fatalf("actions = %#v", authorizer.actions)
	}
}

func TestSQLConsoleExecuteFailsClosedWhenAuditCannotStart(t *testing.T) {
	sqlService, repository, _, executor := newSQLConsoleServiceFixture(t)
	repository.createAuditErr = errors.New("metadata unavailable")
	_, err := sqlService.Execute(
		context.Background(),
		SQLConsoleActor{UserID: "user-1"},
		SQLConsoleRequest{AccountID: "account-1", SQL: "SELECT 1"},
	)
	if !errors.Is(err, ErrSQLConsoleAudit) {
		t.Fatalf("Execute() error = %v", err)
	}
	if executor.called {
		t.Fatal("executor ran without an audit session")
	}
}

func TestSQLConsoleExecuteRejectsExpiredAccount(t *testing.T) {
	sqlService, repository, _, executor := newSQLConsoleServiceFixture(t)
	expired := time.Unix(1, 0)
	repository.account.ExpiresAt = &expired
	_, err := sqlService.Execute(
		context.Background(),
		SQLConsoleActor{UserID: "user-1"},
		SQLConsoleRequest{AccountID: "account-1", SQL: "SELECT 1"},
	)
	if !errors.Is(err, ErrSQLConsoleUnavailable) {
		t.Fatalf("Execute() error = %v", err)
	}
	if executor.called {
		t.Fatal("expired account reached executor")
	}
}
