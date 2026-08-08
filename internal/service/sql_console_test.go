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
	userSession    model.UserSession
	userSessionErr error
	sessions       []*model.AuditSession
	queries        []*model.AuditDBQuery
	finished       []string
	duration       int64
	queryResults   []model.AuditDBQueryResult
	createAuditErr error
}

func (s *sqlConsoleRepositoryStub) FindActiveDatabaseAccount(context.Context, string) (model.DatabaseAccount, bool, error) {
	return s.account, s.found, nil
}

func (s *sqlConsoleRepositoryStub) GetOrCreateActivePermanentUserSession(_ context.Context, _ string) (model.UserSession, error) {
	return s.userSession, s.userSessionErr
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

func (s *sqlConsoleRepositoryStub) CompleteAuditDBQuery(_ context.Context, _ string, result model.AuditDBQueryResult) error {
	s.duration = result.DurationMs
	s.queryResults = append(s.queryResults, result)
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
	connects   int
	connection *sqlConsoleConnectionStub
}

type sqlConsoleConnectionStub struct {
	called   int
	closed   int
	readOnly bool
	result   SQLConsoleExecution
	err      error
	metadata SQLConsoleMetadata
	metaErr  error
}

func (s *sqlConsoleExecutorStub) Connect(context.Context, model.DatabaseAccount) (SQLConsoleConnection, error) {
	s.connects++
	return s.connection, nil
}

func (s *sqlConsoleConnectionStub) Databases() []string     { return []string{"app", "reporting"} }
func (s *sqlConsoleConnectionStub) DefaultDatabase() string { return "app" }
func (s *sqlConsoleConnectionStub) Metadata(context.Context, string) (SQLConsoleMetadata, error) {
	return s.metadata, s.metaErr
}
func (s *sqlConsoleConnectionStub) Close() error {
	s.closed++
	return nil
}

func (s *sqlConsoleConnectionStub) Execute(_ context.Context, _, _ string, readOnly bool) (SQLConsoleExecution, error) {
	s.called++
	s.readOnly = readOnly
	return s.result, s.err
}

func newSQLConsoleServiceFixture(t *testing.T) (*SQLConsoleService, *sqlConsoleRepositoryStub, *sqlConsoleAuthorizerStub, *sqlConsoleExecutorStub) {
	t.Helper()
	repository := &sqlConsoleRepositoryStub{
		found: true,
		userSession: model.UserSession{
			ID: "user-session-1", UserID: "user-1", SessionID: "00001",
			Type: "permanent", Status: "active",
		},
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
	connection := &sqlConsoleConnectionStub{
		result: SQLConsoleExecution{
			Columns: []string{"id"}, Rows: [][]any{{"1"}},
		},
	}
	executor := &sqlConsoleExecutorStub{connection: connection}
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

func createSQLConsoleTestSession(t *testing.T, sqlService *SQLConsoleService) string {
	t.Helper()
	session, err := sqlService.CreateSession(
		context.Background(), SQLConsoleActor{UserID: "user-1"}, "account-1",
	)
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	return session.ID
}

func TestSQLConsoleExecuteReadQueryAuditsBeforeExecution(t *testing.T) {
	sqlService, repository, authorizer, executor := newSQLConsoleServiceFixture(t)
	sessionID := createSQLConsoleTestSession(t, sqlService)
	result, err := sqlService.Execute(
		context.Background(),
		SQLConsoleActor{UserID: "user-1", Username: "alice", ClientIP: "192.0.2.10"},
		SQLConsoleRequest{SessionID: sessionID, Database: "app", SQL: "SELECT id FROM users"},
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if executor.connection.called != 1 || !executor.connection.readOnly {
		t.Fatalf("executor = called %d, readOnly %t", executor.connection.called, executor.connection.readOnly)
	}
	if len(authorizer.actions) != 1 || authorizer.actions[0] != rbac.ActionDBQuery {
		t.Fatalf("actions = %#v", authorizer.actions)
	}
	if len(repository.sessions) != 1 || len(repository.queries) != 1 {
		t.Fatalf("audit writes = sessions %d, queries %d", len(repository.sessions), len(repository.queries))
	}
	if repository.sessions[0].UserSessionID != "user-session-1" {
		t.Fatalf("audit session user_session_id = %q, want user-session-1", repository.sessions[0].UserSessionID)
	}
	if len(repository.finished) != 1 || repository.finished[0] != model.AuditOutcomeSucceeded {
		t.Fatalf("finished outcomes = %#v", repository.finished)
	}
	if len(repository.queryResults) != 1 ||
		repository.queryResults[0].Status != model.AuditDBQueryStatusSuccess ||
		repository.queryResults[0].Rows == nil ||
		*repository.queryResults[0].Rows != 1 ||
		repository.queryResults[0].RowsAffected != nil {
		t.Fatalf("query audit result = %#v, want successful read with one returned row", repository.queryResults)
	}
	if result.RowCount != 1 || result.AuditSessionID != "audit-session" || result.QueryKind != "select" {
		t.Fatalf("result = %#v", result)
	}
}

func TestSQLConsoleExecuteAuditsDespiteUserSessionLookupFailure(t *testing.T) {
	sqlService, repository, _, executor := newSQLConsoleServiceFixture(t)
	repository.userSessionErr = errors.New("user session lookup failed")
	sessionID := createSQLConsoleTestSession(t, sqlService)
	if _, err := sqlService.Execute(
		context.Background(),
		SQLConsoleActor{UserID: "user-1", Username: "alice"},
		SQLConsoleRequest{SessionID: sessionID, Database: "app", SQL: "SELECT 1"},
	); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if executor.connection.called != 1 {
		t.Fatalf("executor called = %d, want 1", executor.connection.called)
	}
	if len(repository.sessions) != 1 || repository.sessions[0].UserSessionID != "" {
		t.Fatalf("audit sessions = %#v, want one session without user_session_id", repository.sessions)
	}
}

func TestSQLConsoleExecutePersistsRealExecutionFailure(t *testing.T) {
	sqlService, repository, _, executor := newSQLConsoleServiceFixture(t)
	sessionID := createSQLConsoleTestSession(t, sqlService)
	executor.connection.err = errors.New("upstream rejected statement")

	_, err := sqlService.Execute(
		context.Background(),
		SQLConsoleActor{UserID: "user-1"},
		SQLConsoleRequest{SessionID: sessionID, Database: "app", SQL: "SELECT missing FROM absent"},
	)
	if !errors.Is(err, ErrSQLConsoleExecution) {
		t.Fatalf("Execute() error = %v, want SQL execution error", err)
	}
	if len(repository.queryResults) != 1 {
		t.Fatalf("query audit result count = %d, want 1", len(repository.queryResults))
	}
	auditResult := repository.queryResults[0]
	if auditResult.Status != model.AuditDBQueryStatusError ||
		auditResult.ErrorCode != "query_failed" ||
		auditResult.ErrorMessage != "upstream rejected statement" ||
		auditResult.Rows != nil ||
		auditResult.RowsAffected != nil {
		t.Fatalf("query audit result = %#v", auditResult)
	}
	if len(repository.finished) != 1 || repository.finished[0] != model.AuditOutcomeFailed {
		t.Fatalf("finished outcomes = %#v, want failed", repository.finished)
	}
}

func TestSQLConsoleExecuteWriteRequiresConfirmationAndPermission(t *testing.T) {
	sqlService, repository, authorizer, executor := newSQLConsoleServiceFixture(t)
	sessionID := createSQLConsoleTestSession(t, sqlService)
	request := SQLConsoleRequest{SessionID: sessionID, Database: "app", SQL: "DELETE FROM jobs WHERE id = 1"}
	if _, err := sqlService.Execute(context.Background(), SQLConsoleActor{UserID: "user-1"}, request); !errors.Is(err, ErrSQLConsoleWriteConfirmation) {
		t.Fatalf("Execute() error = %v", err)
	}
	if executor.connection.called != 0 || len(repository.sessions) != 0 {
		t.Fatal("unconfirmed write reached execution or audit")
	}

	request.ConfirmWrite = true
	executor.connection.result = SQLConsoleExecution{RowsAffected: 3, RowsAffectedKnown: true}
	if _, err := sqlService.Execute(context.Background(), SQLConsoleActor{UserID: "user-1"}, request); err != nil {
		t.Fatalf("confirmed Execute() error = %v", err)
	}
	if len(authorizer.actions) != 1 || authorizer.actions[0] != rbac.ActionDBExecute {
		t.Fatalf("actions = %#v", authorizer.actions)
	}
	if len(repository.queryResults) != 1 ||
		repository.queryResults[0].Status != model.AuditDBQueryStatusSuccess ||
		repository.queryResults[0].RowsAffected == nil ||
		*repository.queryResults[0].RowsAffected != 3 ||
		repository.queryResults[0].Rows != nil {
		t.Fatalf("query audit result = %#v, want successful write affecting three rows", repository.queryResults)
	}
}

func TestSQLConsoleExecuteFailsClosedWhenAuditCannotStart(t *testing.T) {
	sqlService, repository, _, executor := newSQLConsoleServiceFixture(t)
	sessionID := createSQLConsoleTestSession(t, sqlService)
	repository.createAuditErr = errors.New("metadata unavailable")
	_, err := sqlService.Execute(
		context.Background(),
		SQLConsoleActor{UserID: "user-1"},
		SQLConsoleRequest{SessionID: sessionID, Database: "app", SQL: "SELECT 1"},
	)
	if !errors.Is(err, ErrSQLConsoleAudit) {
		t.Fatalf("Execute() error = %v", err)
	}
	if executor.connection.called != 0 {
		t.Fatal("executor ran without an audit session")
	}
}

func TestSQLConsoleExecuteRejectsExpiredAccount(t *testing.T) {
	sqlService, repository, _, executor := newSQLConsoleServiceFixture(t)
	expired := time.Unix(1, 0)
	repository.account.ExpiresAt = &expired
	_, err := sqlService.CreateSession(
		context.Background(),
		SQLConsoleActor{UserID: "user-1"},
		"account-1",
	)
	if !errors.Is(err, ErrSQLConsoleUnavailable) {
		t.Fatalf("Execute() error = %v", err)
	}
	if executor.connects != 0 {
		t.Fatal("expired account reached executor")
	}
}

func TestSQLConsoleSessionReusesConnectionAcrossExecutions(t *testing.T) {
	sqlService, _, _, executor := newSQLConsoleServiceFixture(t)
	sessionID := createSQLConsoleTestSession(t, sqlService)
	for range 2 {
		if _, err := sqlService.Execute(
			context.Background(), SQLConsoleActor{UserID: "user-1"},
			SQLConsoleRequest{SessionID: sessionID, Database: "app", SQL: "SELECT 1"},
		); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	}
	if executor.connects != 1 || executor.connection.called != 2 {
		t.Fatalf("connects = %d, executions = %d", executor.connects, executor.connection.called)
	}
}

func TestSQLConsoleSessionRejectsDatabaseOutsideConnectedCatalog(t *testing.T) {
	sqlService, _, _, executor := newSQLConsoleServiceFixture(t)
	sessionID := createSQLConsoleTestSession(t, sqlService)
	_, err := sqlService.Execute(
		context.Background(), SQLConsoleActor{UserID: "user-1"},
		SQLConsoleRequest{SessionID: sessionID, Database: "secret", SQL: "SELECT 1"},
	)
	if !errors.Is(err, ErrSQLConsoleInvalid) || executor.connection.called != 0 {
		t.Fatalf("Execute() error = %v, executions = %d", err, executor.connection.called)
	}
}

func TestSQLConsoleCloseSessionReleasesConnection(t *testing.T) {
	sqlService, _, _, executor := newSQLConsoleServiceFixture(t)
	sessionID := createSQLConsoleTestSession(t, sqlService)
	if err := sqlService.CloseSession(
		context.Background(), SQLConsoleActor{UserID: "user-1"}, sessionID,
	); err != nil {
		t.Fatalf("CloseSession() error = %v", err)
	}
	if executor.connection.closed != 1 {
		t.Fatalf("connection closes = %d, want 1", executor.connection.closed)
	}
	_, err := sqlService.Execute(
		context.Background(), SQLConsoleActor{UserID: "user-1"},
		SQLConsoleRequest{SessionID: sessionID, Database: "app", SQL: "SELECT 1"},
	)
	if !errors.Is(err, ErrSQLConsoleSession) {
		t.Fatalf("Execute() after close error = %v", err)
	}
}

func TestSQLConsoleServiceMetadata(t *testing.T) {
	svc, _, authorizer, executor := newSQLConsoleServiceFixture(t)
	executor.connection.metadata = SQLConsoleMetadata{
		Tables: []SQLConsoleTableMeta{{Name: "users", Columns: []SQLConsoleColumnMeta{{Name: "id", Type: "bigint"}}}},
	}
	session, err := svc.CreateSession(context.Background(), SQLConsoleActor{UserID: "user-1", Username: "alice", ClientIP: "127.0.0.1"}, "account-1")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	meta, err := svc.Metadata(context.Background(), SQLConsoleActor{UserID: "user-1", Username: "alice", ClientIP: "127.0.0.1"}, session.ID, "app")
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if len(meta.Tables) != 1 || meta.Tables[0].Name != "users" {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
	if len(authorizer.actions) != 1 || authorizer.actions[0] != rbac.ActionDBQuery {
		t.Fatalf("unexpected rbac actions: %v", authorizer.actions)
	}
	if executor.connects != 1 {
		t.Fatalf("connects = %d, want 1(元数据复用会话连接,不重新 Connect)", executor.connects)
	}
}

func TestSQLConsoleServiceMetadataForbidden(t *testing.T) {
	svc, _, authorizer, _ := newSQLConsoleServiceFixture(t)
	session, err := svc.CreateSession(context.Background(), SQLConsoleActor{UserID: "user-1", Username: "alice"}, "account-1")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	authorizer.allowed = false
	_, err = svc.Metadata(context.Background(), SQLConsoleActor{UserID: "user-1", Username: "alice"}, session.ID, "app")
	if err != ErrSQLConsoleForbidden {
		t.Fatalf("期望 ErrSQLConsoleForbidden,实际 %v", err)
	}
}

func TestSQLConsoleServiceMetadataPropagatesConnectionError(t *testing.T) {
	svc, _, _, executor := newSQLConsoleServiceFixture(t)
	session, err := svc.CreateSession(context.Background(), SQLConsoleActor{UserID: "user-1"}, "account-1")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	executor.connection.metaErr = errors.New("metadata upstream failed")
	_, err = svc.Metadata(context.Background(), SQLConsoleActor{UserID: "user-1"}, session.ID, "app")
	if err == nil || err.Error() != "metadata upstream failed" {
		t.Fatalf("metadata error = %v, want upstream error", err)
	}
}
