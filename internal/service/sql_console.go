package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"

	"jianmen/internal/auditartifact"
	"jianmen/internal/config"
	"jianmen/internal/databaseaudit"
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
	CompleteAuditDBQuery(context.Context, string, model.AuditDBQueryResult) error
	FinishAuditSession(context.Context, string, string, string, string, string, time.Time) error
}

type SQLConsoleAuthorizer interface {
	AuthorizeConnection(context.Context, string, []string, string, string) (bool, error)
}

// SQLConsoleUserSessionProvider resolves the short authenticated session ID
// shared with native database connections for audit correlation.
type SQLConsoleUserSessionProvider interface {
	GetOrCreateActivePermanentUserSession(context.Context, string) (model.UserSession, error)
}

type SQLConsoleActor struct {
	UserID, Username, ClientIP string
}

type SQLConsoleRequest struct {
	SessionID, Database, SQL string
	ConfirmWrite             bool
}

type SQLConsoleAuditOptions struct {
	ReplayDir        string
	PreviewBytes     int
	RedactionEnabled bool
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
	repository   SQLConsoleRepository
	authorizer   SQLConsoleAuthorizer
	executor     SQLConsoleExecutor
	userSessions SQLConsoleUserSessionProvider
	now          func() time.Time
	sessionsMu   sync.Mutex
	sessions     map[string]*sqlConsoleSession
	idleTTL      time.Duration
	auditOptions SQLConsoleAuditOptions
}

func NewSQLConsoleService(
	repository SQLConsoleRepository,
	authorizer SQLConsoleAuthorizer,
	executor SQLConsoleExecutor,
	userSessions SQLConsoleUserSessionProvider,
	auditOptions ...SQLConsoleAuditOptions,
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
	if isNilSQLConsoleUserSessionProvider(userSessions) {
		return nil, errors.New("SQL console user session provider is required")
	}
	if len(auditOptions) > 1 {
		return nil, errors.New("SQL console accepts at most one audit option set")
	}
	options := SQLConsoleAuditOptions{}
	if len(auditOptions) == 1 {
		options = auditOptions[0]
		options.ReplayDir = strings.TrimSpace(options.ReplayDir)
		if options.PreviewBytes == 0 {
			options.PreviewBytes = config.DefaultDatabaseAuditPreviewBytes
		}
		if options.PreviewBytes < config.MinDatabaseAuditPreviewBytes || options.PreviewBytes > config.MaxDatabaseAuditPreviewBytes {
			return nil, fmt.Errorf("SQL console audit preview bytes must be between %d and %d", config.MinDatabaseAuditPreviewBytes, config.MaxDatabaseAuditPreviewBytes)
		}
	}
	return &SQLConsoleService{
		repository:   repository,
		authorizer:   authorizer,
		executor:     executor,
		userSessions: userSessions,
		now:          time.Now,
		sessions:     make(map[string]*sqlConsoleSession),
		idleTTL:      15 * time.Minute,
		auditOptions: options,
	}, nil
}

func isNilSQLConsoleUserSessionProvider(provider SQLConsoleUserSessionProvider) bool {
	if provider == nil {
		return true
	}
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
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
	session.UserSessionID = webSession.userSessionID
	querySQL := policy.SQL
	var artifact sqlConsoleAuditArtifact
	if s.auditOptions.ReplayDir != "" {
		session.ID = model.NewID()
		artifact, err = writeSQLConsoleAuditArtifact(s.auditOptions, session.ID, policy.SQL)
		if err != nil {
			return SQLConsoleResult{}, fmt.Errorf("%w: write complete query file: %v", ErrSQLConsoleAudit, err)
		}
		session.ReplayDir = artifact.replayDir
		querySQL = artifact.preview
	}
	if err := s.repository.CreateAuditSession(ctx, session); err != nil {
		artifact.remove()
		return SQLConsoleResult{}, fmt.Errorf("%w: create session: %v", ErrSQLConsoleAudit, err)
	}
	query := &model.AuditDBQuery{
		AuditSessionID:    session.ID,
		Timestamp:         now,
		SQLText:           querySQL,
		OriginalSQLBytes:  int64(len(policy.SQL)),
		SQLTruncated:      artifact.truncated,
		SQLLogOffset:      artifact.offset,
		SQLLogBytes:       artifact.bytes,
		AuditDataRedacted: artifact.redacted,
		QueryKind:         policy.QueryKind,
		Status:            model.AuditDBQueryStatusUnknown,
	}
	if err := s.repository.CreateAuditDBQuery(ctx, query); err != nil {
		artifact.remove()
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
	queryResult := model.AuditDBQueryResult{
		DurationMs: duration,
		Status:     model.AuditDBQueryStatusSuccess,
	}
	if executionErr != nil {
		queryResult.Status = model.AuditDBQueryStatusError
		queryResult.ErrorCode, queryResult.ErrorMessage = sqlConsoleAuditError(executionErr)
	} else if policy.ReadOnly {
		rows := int64(len(execution.Rows))
		queryResult.Rows = &rows
	} else if execution.RowsAffectedKnown {
		rowsAffected := execution.RowsAffected
		queryResult.RowsAffected = &rowsAffected
	}
	if err := s.repository.CompleteAuditDBQuery(auditContext, query.ID, queryResult); err != nil {
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

// Metadata returns schema metadata using the cached connection and does not
// create a separate audit session.
func (s *SQLConsoleService) Metadata(
	ctx context.Context,
	actor SQLConsoleActor,
	sessionID, database string,
) (SQLConsoleMetadata, error) {
	if ctx == nil || strings.TrimSpace(actor.UserID) == "" || strings.TrimSpace(sessionID) == "" {
		return SQLConsoleMetadata{}, ErrSQLConsoleInvalid
	}
	webSession, err := s.sessionForActor(strings.TrimSpace(sessionID), strings.TrimSpace(actor.UserID))
	if err != nil {
		return SQLConsoleMetadata{}, err
	}
	allowed, err := s.authorizer.AuthorizeConnection(
		ctx,
		strings.TrimSpace(actor.UserID),
		[]string{rbac.ActionDBQuery},
		model.ResourceTypeDatabaseAccount,
		webSession.accountID,
	)
	if err != nil {
		return SQLConsoleMetadata{}, fmt.Errorf("authorize SQL console: %w", err)
	}
	if !allowed {
		return SQLConsoleMetadata{}, ErrSQLConsoleForbidden
	}
	_, _, err = s.loadSQLConsoleAccount(ctx, webSession.accountID)
	if err != nil {
		return SQLConsoleMetadata{}, err
	}
	database = strings.TrimSpace(database)
	if database == "" || !webSession.databaseAllowed(database) {
		return SQLConsoleMetadata{}, ErrSQLConsoleInvalid
	}
	return webSession.connection.Metadata(ctx, database)
}

const sqlConsoleAuditWriterBuffer = 256 * 1024

type sqlConsoleAuditArtifact struct {
	replayDir string
	preview   string
	offset    int64
	bytes     int64
	truncated bool
	redacted  bool
}

func (a sqlConsoleAuditArtifact) remove() {
	if a.replayDir != "" {
		_ = os.RemoveAll(a.replayDir)
	}
}

func writeSQLConsoleAuditArtifact(options SQLConsoleAuditOptions, sessionID string, sql string) (sqlConsoleAuditArtifact, error) {
	dir := filepath.Join(options.ReplayDir, "db", sessionID)
	artifact := sqlConsoleAuditArtifact{replayDir: dir}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return sqlConsoleAuditArtifact{}, fmt.Errorf("create SQL console replay directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(dir, "query-data.bin"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		artifact.remove()
		return sqlConsoleAuditArtifact{}, fmt.Errorf("create SQL console query data: %w", err)
	}
	writer, err := auditartifact.NewWriter(file, sqlConsoleAuditWriterBuffer)
	if err != nil {
		_ = file.Close()
		artifact.remove()
		return sqlConsoleAuditArtifact{}, fmt.Errorf("open SQL console query data: %w", err)
	}
	result, captureErr := databaseaudit.CaptureSQL(writer, databaseaudit.Policy{
		PreviewBytes: options.PreviewBytes, RedactionEnabled: options.RedactionEnabled,
	}, []byte(sql))
	if artifactErr := errors.Join(captureErr, writer.Close()); artifactErr != nil {
		artifact.remove()
		return sqlConsoleAuditArtifact{}, fmt.Errorf("persist SQL console query data: %w", artifactErr)
	}
	artifact.preview, artifact.offset, artifact.bytes = result.Preview, result.Section.Offset, result.Section.Bytes
	artifact.truncated, artifact.redacted = result.Truncated, result.Redacted
	return artifact, nil
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

func sqlConsoleAuditError(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	var mysqlError *mysqlDriver.MySQLError
	if errors.As(err, &mysqlError) {
		return strconv.FormatUint(uint64(mysqlError.Number), 10), boundedSQLConsoleError(err)
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		return strings.TrimSpace(postgresError.Code), boundedSQLConsoleError(err)
	}
	return "query_failed", boundedSQLConsoleError(err)
}
