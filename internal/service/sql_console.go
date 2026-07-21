package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

var (
	ErrSQLConsoleForbidden   = errors.New("SQL console access forbidden")
	ErrSQLConsoleNotFound    = errors.New("database account not found")
	ErrSQLConsoleUnavailable = errors.New("database account is unavailable")
	ErrSQLConsoleAudit       = errors.New("SQL console audit failed")
	ErrSQLConsoleExecution   = errors.New("SQL execution failed")
	ErrSQLConsoleSession     = errors.New("SQL console session not found")
)

type SQLConsoleRepository interface {
	FindActiveDatabaseAccount(context.Context, string) (model.DatabaseAccount, bool, error)
	CreateAuditSession(context.Context, *model.AuditSession) error
	CreateAuditDBQuery(context.Context, *model.AuditDBQuery) error
	UpdateAuditDBQueryDuration(context.Context, string, int64) error
	FinishAuditSession(context.Context, string, string, string, string, string, time.Time) error
}

type SQLConsoleAuthorizer interface {
	AuthorizeConnection(context.Context, string, []string, string, string) (bool, error)
}

type SQLConsoleActor struct {
	UserID, Username, ClientIP string
}

type SQLConsoleRequest struct {
	SessionID, Database, SQL string
	ConfirmWrite             bool
}

type SQLConsoleResult struct {
	AuditSessionID string   `json:"audit_session_id"`
	QueryKind      string   `json:"query_kind"`
	ReadOnly       bool     `json:"read_only"`
	Columns        []string `json:"columns"`
	Rows           [][]any  `json:"rows"`
	RowCount       int      `json:"row_count"`
	RowsAffected   int64    `json:"rows_affected"`
	Truncated      bool     `json:"truncated"`
	DurationMs     int64    `json:"duration_ms"`
}

type SQLConsoleService struct {
	repository SQLConsoleRepository
	authorizer SQLConsoleAuthorizer
	executor   SQLConsoleExecutor
	now        func() time.Time
	sessionsMu sync.Mutex
	sessions   map[string]*sqlConsoleSession
	idleTTL    time.Duration
}

func NewSQLConsoleService(
	repository SQLConsoleRepository,
	authorizer SQLConsoleAuthorizer,
	executor SQLConsoleExecutor,
) (*SQLConsoleService, error) {
	if repository == nil {
		return nil, errors.New("SQL console repository is required")
	}
	if authorizer == nil {
		return nil, errors.New("SQL console authorizer is required")
	}
	if executor == nil {
		return nil, errors.New("SQL console executor is required")
	}
	return &SQLConsoleService{
		repository: repository,
		authorizer: authorizer,
		executor:   executor,
		now:        time.Now,
		sessions:   make(map[string]*sqlConsoleSession),
		idleTTL:    15 * time.Minute,
	}, nil
}

func (s *SQLConsoleService) Execute(
	ctx context.Context,
	actor SQLConsoleActor,
	request SQLConsoleRequest,
) (SQLConsoleResult, error) {
	if ctx == nil || strings.TrimSpace(actor.UserID) == "" || strings.TrimSpace(request.SessionID) == "" {
		return SQLConsoleResult{}, ErrSQLConsoleInvalid
	}
	webSession, err := s.sessionForActor(strings.TrimSpace(request.SessionID), strings.TrimSpace(actor.UserID))
	if err != nil {
		return SQLConsoleResult{}, err
	}
	policy, err := inspectSQLStatement(request.SQL)
	if err != nil {
		return SQLConsoleResult{}, err
	}
	if !policy.ReadOnly && !request.ConfirmWrite {
		return SQLConsoleResult{}, ErrSQLConsoleWriteConfirmation
	}
	action := rbac.ActionDBQuery
	if !policy.ReadOnly {
		action = rbac.ActionDBExecute
	}
	allowed, err := s.authorizer.AuthorizeConnection(
		ctx,
		strings.TrimSpace(actor.UserID),
		[]string{action},
		model.ResourceTypeDatabaseAccount,
		webSession.accountID,
	)
	if err != nil {
		return SQLConsoleResult{}, fmt.Errorf("authorize SQL console: %w", err)
	}
	if !allowed {
		return SQLConsoleResult{}, ErrSQLConsoleForbidden
	}
	account, now, err := s.loadSQLConsoleAccount(ctx, webSession.accountID)
	if err != nil {
		return SQLConsoleResult{}, err
	}
	database := strings.TrimSpace(request.Database)
	if database == "" || !webSession.databaseAllowed(database) {
		return SQLConsoleResult{}, ErrSQLConsoleInvalid
	}

	session := newSQLConsoleAuditSession(actor, account, now)
	if err := s.repository.CreateAuditSession(ctx, session); err != nil {
		return SQLConsoleResult{}, fmt.Errorf("%w: create session: %v", ErrSQLConsoleAudit, err)
	}
	query := &model.AuditDBQuery{
		AuditSessionID:   session.ID,
		Timestamp:        now,
		SQLText:          policy.SQL,
		OriginalSQLBytes: int64(len(policy.SQL)),
		QueryKind:        policy.QueryKind,
	}
	if err := s.repository.CreateAuditDBQuery(ctx, query); err != nil {
		s.finishSQLConsoleAudit(ctx, session.ID, model.AuditOutcomeFailed, "audit_query_failed", err.Error())
		return SQLConsoleResult{}, fmt.Errorf("%w: create query: %v", ErrSQLConsoleAudit, err)
	}

	started := s.now()
	execution, executionErr := webSession.connection.Execute(ctx, database, policy.SQL, policy.ReadOnly)
	duration := s.now().Sub(started).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	auditContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.repository.UpdateAuditDBQueryDuration(auditContext, query.ID, duration); err != nil {
		s.finishSQLConsoleAudit(auditContext, session.ID, model.AuditOutcomeFailed, "audit_update_failed", err.Error())
		return SQLConsoleResult{}, fmt.Errorf("%w: update query: %v", ErrSQLConsoleAudit, err)
	}
	if executionErr != nil {
		s.finishSQLConsoleAudit(auditContext, session.ID, model.AuditOutcomeFailed, "query_failed", boundedSQLConsoleError(executionErr))
		return SQLConsoleResult{}, fmt.Errorf("%w: %v", ErrSQLConsoleExecution, executionErr)
	}
	if err := s.repository.FinishAuditSession(
		auditContext, session.ID, model.AuditOutcomeSucceeded, "", "",
		model.RecordingStatusNone, s.now().UTC(),
	); err != nil {
		return SQLConsoleResult{}, fmt.Errorf("%w: finish session: %v", ErrSQLConsoleAudit, err)
	}
	return SQLConsoleResult{
		AuditSessionID: session.ID,
		QueryKind:      policy.QueryKind,
		ReadOnly:       policy.ReadOnly,
		Columns:        execution.Columns,
		Rows:           execution.Rows,
		RowCount:       len(execution.Rows),
		RowsAffected:   execution.RowsAffected,
		Truncated:      execution.Truncated,
		DurationMs:     duration,
	}, nil
}

func newSQLConsoleAuditSession(actor SQLConsoleActor, account model.DatabaseAccount, started time.Time) *model.AuditSession {
	instance := account.Instance
	return &model.AuditSession{
		UserID:          strings.TrimSpace(actor.UserID),
		Username:        strings.TrimSpace(actor.Username),
		Protocol:        strings.ToLower(strings.TrimSpace(instance.Protocol)),
		ProtocolSubtype: "web_sql",
		ResourceType:    model.ResourceTypeDatabaseAccount,
		ResourceID:      account.ID,
		AccountID:       account.ID,
		TargetName:      instance.Name,
		TargetAddress:   fmt.Sprintf("%s:%d", instance.Address, effectiveDatabasePort(instance)),
		AccountName:     account.UniqueName,
		AccountUsername: account.Username,
		ClientIP:        strings.TrimSpace(actor.ClientIP),
		StartedAt:       started,
		State:           "started",
		Outcome:         model.AuditOutcomeActive,
		RecordingStatus: model.RecordingStatusNone,
	}
}

func (s *SQLConsoleService) finishSQLConsoleAudit(ctx context.Context, id, outcome, code, message string) {
	auditContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = s.repository.FinishAuditSession(
		auditContext, id, outcome, code, boundedSQLConsoleError(errors.New(message)),
		model.RecordingStatusNone, s.now().UTC(),
	)
}

func boundedSQLConsoleError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if len(message) > 1024 {
		return message[:1024]
	}
	return message
}
