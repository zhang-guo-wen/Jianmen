package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAuditQueryServiceDBProtocolWhitelist(t *testing.T) {
	allowed := map[string]string{
		"":           "mysql,postgres,postgresql,redis",
		"mysql":      "mysql",
		"postgres":   "postgres,postgresql",
		"postgresql": "postgres,postgresql",
		"redis":      "redis",
	}
	for input, want := range allowed {
		t.Run("allow_"+input, func(t *testing.T) {
			repository := newAuditQuerySecurityRepository()
			query := newAuditQuerySecurityService(t, repository, []string{AuditQueryDBActionView})
			if _, _, err := query.ListDB(context.Background(), "db-auditor", AuditSessionListParams{Protocol: input}); err != nil {
				t.Fatalf("ListDB(%q): %v", input, err)
			}
			if repository.listSessions != 1 || repository.listParams.Protocol != want {
				t.Fatalf("ListDB(%q) calls=%d protocol=%q, want 1 and %q", input, repository.listSessions, repository.listParams.Protocol, want)
			}
		})
	}

	for _, input := range []string{"ssh", "sftp", "rdp", "unknown", "mysql,ssh", "mysql,postgres", "postgresql,redis"} {
		t.Run("reject_"+input, func(t *testing.T) {
			repository := newAuditQuerySecurityRepository()
			query := newAuditQuerySecurityService(t, repository, []string{AuditQueryDBActionView})
			if _, _, err := query.ListDB(context.Background(), "db-auditor", AuditSessionListParams{Protocol: input}); !errors.Is(err, ErrAuditQueryInvalidProtocol) {
				t.Fatalf("ListDB(%q) error=%v, want invalid protocol", input, err)
			}
			if repository.listSessions != 0 {
				t.Fatalf("ListDB(%q) repository calls=%d, want 0", input, repository.listSessions)
			}
		})
	}
}

func TestAuditQueryServiceAuthorizesMetadataBeforeFullSession(t *testing.T) {
	tests := []struct {
		name              string
		requestedProtocol string
		metadata          AuditSessionAccessMetadata
		actions           []string
		metadataErr       error
		wantMetadata      int
		wantFull          int
		wantOK            bool
	}{
		{name: "ended SSH authorized", requestedProtocol: "ssh", metadata: auditMetadata("ssh", "ended"), actions: []string{AuditQueryActionView}, wantMetadata: 1, wantFull: 1, wantOK: true},
		{name: "active DB session view authorized", requestedProtocol: "mysql", metadata: auditMetadata("mysql", "started"), actions: []string{AuditQuerySessionView}, wantMetadata: 1, wantFull: 1, wantOK: true},
		{name: "denied", requestedProtocol: "ssh", metadata: auditMetadata("ssh", "ended"), wantMetadata: 1},
		{name: "not found", requestedProtocol: "ssh", metadataErr: ErrAuditArtifactUnavailable, wantMetadata: 1},
		{name: "invalid state", requestedProtocol: "ssh", metadata: auditMetadata("ssh", "pending"), actions: []string{AuditQueryActionView}, wantMetadata: 1},
		{name: "RDP actual protocol", requestedProtocol: "ssh", metadata: auditMetadata("rdp", "ended"), actions: []string{AuditQueryActionView}, wantMetadata: 1},
		{name: "unknown actual protocol", requestedProtocol: "ssh", metadata: auditMetadata("telnet", "ended"), actions: []string{AuditQueryActionView}, wantMetadata: 1},
		{name: "unsupported RDP URL", requestedProtocol: "rdp", metadata: auditMetadata("rdp", "ended"), actions: []string{AuditQueryActionView}},
		{name: "SSH URL DB session mismatch", requestedProtocol: "ssh", metadata: auditMetadata("mysql", "ended"), actions: []string{AuditQueryDBActionView}, wantMetadata: 1},
		{name: "DB URL SSH session mismatch", requestedProtocol: "mysql", metadata: auditMetadata("ssh", "ended"), actions: []string{AuditQueryActionView}, wantMetadata: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := newAuditQuerySecurityRepository()
			repository.metadata = tt.metadata
			repository.session = auditSessionFromMetadata(tt.metadata)
			repository.metadataErr = tt.metadataErr
			query := newAuditQuerySecurityService(t, repository, tt.actions)

			session, err := query.AuthorizedSession(context.Background(), "user-1", tt.requestedProtocol, "session-1")
			if tt.wantOK {
				if err != nil || session.ID != "session-1" {
					t.Fatalf("AuthorizedSession() session=%#v error=%v", session, err)
				}
			} else if !errors.Is(err, ErrAuditArtifactUnavailable) {
				t.Fatalf("AuthorizedSession() error=%v, want unavailable", err)
			}
			if repository.metadataReads != tt.wantMetadata || repository.fullSessionReads != tt.wantFull {
				t.Fatalf("reads metadata=%d full=%d, want %d/%d", repository.metadataReads, repository.fullSessionReads, tt.wantMetadata, tt.wantFull)
			}
		})
	}
}

func TestAuditQueryServiceRejectsCrossFamilyChildReads(t *testing.T) {
	tests := []struct {
		name     string
		metadata AuditSessionAccessMetadata
		call     func(*AuditQueryService) error
	}{
		{
			name:     "SSH session queries",
			metadata: auditMetadata("ssh", "ended"),
			call: func(query *AuditQueryService) error {
				_, _, err := query.DBQueryEvents(context.Background(), "user-1", "ssh", "session-1", AuditDBQueryPreviewParams{})
				return err
			},
		},
		{
			name:     "DB session commands",
			metadata: auditMetadata("mysql", "ended"),
			call: func(query *AuditQueryService) error {
				_, _, err := query.SSHCommands(context.Background(), "user-1", "mysql", "session-1", Page{})
				return err
			},
		},
		{
			name:     "DB session files",
			metadata: auditMetadata("postgres", "ended"),
			call: func(query *AuditQueryService) error {
				_, _, err := query.SFTPEvents(context.Background(), "user-1", "postgres", "session-1", Page{})
				return err
			},
		},
		{
			name:     "RDP session commands",
			metadata: auditMetadata("rdp", "ended"),
			call: func(query *AuditQueryService) error {
				_, _, err := query.SSHCommands(context.Background(), "user-1", "ssh", "session-1", Page{})
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := newAuditQuerySecurityRepository()
			repository.metadata = tt.metadata
			repository.session = auditSessionFromMetadata(tt.metadata)
			query := newAuditQuerySecurityService(t, repository, []string{AuditQueryActionView, AuditQueryDBActionView})
			if err := tt.call(query); !errors.Is(err, ErrAuditArtifactUnavailable) {
				t.Fatalf("cross-family error=%v, want unavailable", err)
			}
			if repository.commands != 0 || repository.files != 0 || repository.queries != 0 {
				t.Fatalf("child reads commands=%d files=%d queries=%d, want 0", repository.commands, repository.files, repository.queries)
			}
		})
	}
}

func TestAuditQueryServiceChecksContextAfterRepositoryReads(t *testing.T) {
	tests := []struct {
		name     string
		cancelOn string
		call     func(context.Context, *AuditQueryService) error
	}{
		{name: "SSH list", cancelOn: "sessions", call: func(ctx context.Context, query *AuditQueryService) error {
			_, _, err := query.ListSSH(ctx, "user-1", AuditSessionListParams{})
			return err
		}},
		{name: "DB list", cancelOn: "sessions", call: func(ctx context.Context, query *AuditQueryService) error {
			_, _, err := query.ListDB(ctx, "user-1", AuditSessionListParams{})
			return err
		}},
		{name: "operation list", cancelOn: "operations", call: func(ctx context.Context, query *AuditQueryService) error {
			_, _, err := query.ListOperations(ctx, "user-1", AuditEventListParams{})
			return err
		}},
		{name: "login list", cancelOn: "logins", call: func(ctx context.Context, query *AuditQueryService) error {
			_, _, err := query.ListLogins(ctx, "user-1", LoginAuditListParams{})
			return err
		}},
		{name: "metadata", cancelOn: "metadata", call: func(ctx context.Context, query *AuditQueryService) error {
			_, err := query.AuthorizedSession(ctx, "user-1", "ssh", "session-1")
			return err
		}},
		{name: "full session", cancelOn: "full", call: func(ctx context.Context, query *AuditQueryService) error {
			_, err := query.AuthorizedSession(ctx, "user-1", "ssh", "session-1")
			return err
		}},
		{name: "commands", cancelOn: "commands", call: func(ctx context.Context, query *AuditQueryService) error {
			_, _, err := query.SSHCommands(ctx, "user-1", "ssh", "session-1", Page{})
			return err
		}},
		{name: "SFTP events", cancelOn: "files", call: func(ctx context.Context, query *AuditQueryService) error {
			_, _, err := query.SFTPEvents(ctx, "user-1", "ssh", "session-1", Page{})
			return err
		}},
		{name: "DB queries", cancelOn: "queries", call: func(ctx context.Context, query *AuditQueryService) error {
			_, _, err := query.DBQueryEvents(ctx, "user-1", "mysql", "session-1", AuditDBQueryPreviewParams{})
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := newAuditQuerySecurityRepository()
			if tt.cancelOn == "queries" {
				repository.metadata = auditMetadata("mysql", "ended")
				repository.session = auditSessionFromMetadata(repository.metadata)
			}
			ctx, cancel := context.WithCancel(context.Background())
			repository.cancel = cancel
			repository.cancelOn = tt.cancelOn
			query := newAuditQuerySecurityService(t, repository, []string{AuditQueryActionView, AuditQueryDBActionView})
			if err := tt.call(ctx, query); !errors.Is(err, context.Canceled) {
				t.Fatalf("error=%v, want context canceled", err)
			}
		})
	}
}

func auditMetadata(protocol, state string) AuditSessionAccessMetadata {
	return AuditSessionAccessMetadata{ID: "session-1", Protocol: protocol, State: state}
}

func auditSessionFromMetadata(metadata AuditSessionAccessMetadata) AuditSession {
	return AuditSession{
		ID: metadata.ID, Protocol: metadata.Protocol,
		ProtocolSubtype: metadata.ProtocolSubtype, State: metadata.State,
	}
}

func newAuditQuerySecurityService(t *testing.T, repository AuditQueryRepository, actions []string) *AuditQueryService {
	t.Helper()
	query, err := NewAuditQueryService(repository, &auditQueryTestAuthorizer{actions: actions})
	if err != nil {
		t.Fatalf("new audit query service: %v", err)
	}
	return query
}

type auditQuerySecurityRepository struct {
	metadata AuditSessionAccessMetadata
	session  AuditSession

	metadataErr error
	cancel      context.CancelFunc
	cancelOn    string

	listParams AuditSessionListParams

	listSessions, metadataReads, fullSessionReads int
	commands, files, queries, operations, logins  int
}

func newAuditQuerySecurityRepository() *auditQuerySecurityRepository {
	metadata := auditMetadata("ssh", "ended")
	return &auditQuerySecurityRepository{metadata: metadata, session: auditSessionFromMetadata(metadata)}
}

func (r *auditQuerySecurityRepository) after(name string) {
	if r.cancelOn == name && r.cancel != nil {
		r.cancel()
	}
}

func (r *auditQuerySecurityRepository) ListAuditSessions(_ context.Context, params AuditSessionListParams) ([]AuditSessionListItem, int64, error) {
	r.listSessions++
	r.listParams = params
	r.after("sessions")
	return []AuditSessionListItem{{ID: "sensitive-session"}}, 1, nil
}

func (r *auditQuerySecurityRepository) GetAuditSessionAccessMetadata(context.Context, string) (AuditSessionAccessMetadata, error) {
	r.metadataReads++
	r.after("metadata")
	return r.metadata, r.metadataErr
}

func (r *auditQuerySecurityRepository) GetFullAuditSession(context.Context, string) (AuditSession, error) {
	r.fullSessionReads++
	r.after("full")
	return r.session, nil
}

func (r *auditQuerySecurityRepository) ListSSHCommands(context.Context, string, Page) ([]AuditSSHCommand, int64, error) {
	r.commands++
	r.after("commands")
	return []AuditSSHCommand{{ID: "command-1"}}, 1, nil
}

func (r *auditQuerySecurityRepository) ListSFTPEvents(context.Context, string, Page) ([]AuditSFTPEvent, int64, error) {
	r.files++
	r.after("files")
	return []AuditSFTPEvent{{ID: "file-1"}}, 1, nil
}

func (r *auditQuerySecurityRepository) ListDBQueryPreviews(context.Context, string, AuditDBQueryPreviewParams) ([]AuditDBQueryPreview, int64, error) {
	r.queries++
	r.after("queries")
	return []AuditDBQueryPreview{{ID: "query-1", Timestamp: time.Now()}}, 1, nil
}

func (r *auditQuerySecurityRepository) ListAuditEvents(context.Context, AuditEventListParams) ([]AuditEvent, int64, error) {
	r.operations++
	r.after("operations")
	return []AuditEvent{{ID: "operation-1"}}, 1, nil
}

func (r *auditQuerySecurityRepository) ListLoginAuditLogs(context.Context, LoginAuditListParams) ([]LoginAuditLog, int64, error) {
	r.logins++
	r.after("logins")
	return []LoginAuditLog{{ID: "login-1"}}, 1, nil
}
