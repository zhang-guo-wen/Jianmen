package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAuditQueryServiceDeniesBeforeRepositoryRead(t *testing.T) {
	repository := &auditQueryTestRepository{}
	service, err := NewAuditQueryService(repository, &auditQueryTestAuthorizer{allowed: false})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	if _, _, err := service.ListSSH(context.Background(), "user-1", AuditSessionListParams{}); !errors.Is(err, ErrAuditQueryForbidden) {
		t.Fatalf("ListSSH error = %v, want forbidden", err)
	}
	if repository.listSessions != 0 {
		t.Fatalf("repository reads = %d, want 0", repository.listSessions)
	}
	if _, _, err := service.ListDB(context.Background(), "user-1", AuditSessionListParams{}); !errors.Is(err, ErrAuditQueryForbidden) {
		t.Fatalf("ListDB error = %v, want forbidden", err)
	}
	if _, _, err := service.ListOperations(context.Background(), "user-1", AuditEventListParams{}); !errors.Is(err, ErrAuditQueryForbidden) {
		t.Fatalf("ListOperations error = %v, want forbidden", err)
	}
	if _, _, err := service.ListLogins(context.Background(), "user-1", LoginAuditListParams{}); !errors.Is(err, ErrAuditQueryForbidden) {
		t.Fatalf("ListLogins error = %v, want forbidden", err)
	}
	if repository.listSessions != 0 || repository.events != 0 || repository.logins != 0 {
		t.Fatalf("repository reads = sessions:%d events:%d logins:%d, want 0:0:0", repository.listSessions, repository.events, repository.logins)
	}
	if _, err := service.AuthorizedSession(context.Background(), "user-1", "ssh", "session-1"); !errors.Is(err, ErrAuditQueryForbidden) {
		t.Fatalf("AuthorizedSession error = %v, want forbidden", err)
	}
	if repository.getSession != 0 {
		t.Fatalf("session reads = %d, want 0", repository.getSession)
	}
}

func TestAuditQueryServiceAppliesActiveAndEndedSessionPolicy(t *testing.T) {
	tests := []struct {
		name, protocol, state string
		actions               []string
		wantErr               bool
	}{
		{name: "active SSH allows session view", protocol: "ssh", state: "started", actions: []string{AuditQuerySessionView}},
		{name: "ended SSH requires audit view", protocol: "ssh", state: "ended", actions: []string{AuditQuerySessionView}, wantErr: true},
		{name: "active DB allows database audit view", protocol: "mysql", state: "started", actions: []string{AuditQueryDBActionView}},
		{name: "ended DB rejects SSH audit view", protocol: "postgres", state: "ended", actions: []string{AuditQueryActionView}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &auditQueryTestRepository{session: AuditSession{ID: "session-1", Protocol: tt.protocol, State: tt.state}}
			service, err := NewAuditQueryService(repository, &auditQueryTestAuthorizer{actions: tt.actions})
			if err != nil {
				t.Fatalf("new service: %v", err)
			}
			_, err = service.AuthorizedSession(context.Background(), "user-1", tt.protocol, "session-1")
			if tt.wantErr {
				if !errors.Is(err, ErrAuditQueryForbidden) {
					t.Fatalf("error = %v, want forbidden", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("AuthorizedSession: %v", err)
			}
		})
	}
}

func TestAuditQueryServiceFailsClosedOnDependencyAndContextErrors(t *testing.T) {
	t.Run("authorizer error prevents repository read", func(t *testing.T) {
		repository := &auditQueryTestRepository{}
		service, err := NewAuditQueryService(repository, &auditQueryTestAuthorizer{err: errors.New("authorizer unavailable")})
		if err != nil {
			t.Fatalf("new service: %v", err)
		}
		if _, _, err := service.ListDB(context.Background(), "user-1", AuditSessionListParams{}); err == nil {
			t.Fatal("ListDB unexpectedly succeeded")
		}
		if repository.listSessions != 0 {
			t.Fatalf("repository reads = %d, want 0", repository.listSessions)
		}
	})
	t.Run("canceled context prevents authorizer and repository", func(t *testing.T) {
		repository := &auditQueryTestRepository{}
		authorizer := &auditQueryTestAuthorizer{allowed: true}
		service, err := NewAuditQueryService(repository, authorizer)
		if err != nil {
			t.Fatalf("new service: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, _, err := service.ListLogins(ctx, "user-1", LoginAuditListParams{}); err == nil {
			t.Fatal("ListLogins unexpectedly succeeded")
		}
		if authorizer.calls != 0 || repository.logins != 0 {
			t.Fatalf("calls = authorizer:%d repository:%d, want 0:0", authorizer.calls, repository.logins)
		}
	})
	t.Run("repository error is returned without allowing data", func(t *testing.T) {
		service, err := NewAuditQueryService(&auditQueryTestRepository{err: errors.New("repository path /private")}, &auditQueryTestAuthorizer{allowed: true})
		if err != nil {
			t.Fatalf("new service: %v", err)
		}
		if _, _, err := service.ListOperations(context.Background(), "user-1", AuditEventListParams{}); err == nil {
			t.Fatal("ListOperations unexpectedly succeeded")
		}
	})
}

func TestAuditQueryServicePreservesDBPreviewEventSemantics(t *testing.T) {
	repository := &auditQueryTestRepository{
		session: AuditSession{ID: "db-1", Protocol: "mysql", State: "ended"},
		queries: []AuditDBQueryPreview{{Timestamp: time.Unix(10, 0), SQLText: "SELECT 1", QueryKind: "select", DurationMs: 3}},
	}
	service, err := NewAuditQueryService(repository, &auditQueryTestAuthorizer{actions: []string{AuditQueryDBActionView}})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	events, total, err := service.DBQueryEvents(context.Background(), "user-1", "mysql", "db-1", AuditDBQueryPreviewParams{Limit: 1, Offset: 7})
	if err != nil {
		t.Fatalf("DBQueryEvents: %v", err)
	}
	if total != 1 || len(events) != 2 || events[0].Seq != 7 || events[0].SQL != "SELECT 1" || events[1].SQL != "" {
		t.Fatalf("events = %#v total=%d", events, total)
	}
}

type auditQueryTestAuthorizer struct {
	actions []string
	allowed bool
	err     error
	calls   int
}

func (a *auditQueryTestAuthorizer) AuthorizeAuditQuery(_ context.Context, _ string, requested []string) (bool, error) {
	a.calls++
	if a.err != nil {
		return false, a.err
	}
	if a.allowed {
		return true, nil
	}
	for _, request := range requested {
		for _, action := range a.actions {
			if request == action {
				return true, nil
			}
		}
	}
	return false, nil
}

type auditQueryTestRepository struct {
	session                                  AuditSession
	queries                                  []AuditDBQueryPreview
	err                                      error
	listSessions, getSession, events, logins int
}

func (r *auditQueryTestRepository) ListAuditSessions(context.Context, AuditSessionListParams) ([]AuditSessionListItem, int64, error) {
	r.listSessions++
	return nil, 0, r.err
}
func (r *auditQueryTestRepository) GetAuditSession(context.Context, string) (AuditSession, error) {
	r.getSession++
	if r.err != nil {
		return AuditSession{}, r.err
	}
	return r.session, nil
}
func (r *auditQueryTestRepository) ListSSHCommands(context.Context, string, Page) ([]AuditSSHCommand, int64, error) {
	return nil, 0, r.err
}
func (r *auditQueryTestRepository) ListSFTPEvents(context.Context, string, Page) ([]AuditSFTPEvent, int64, error) {
	return nil, 0, r.err
}
func (r *auditQueryTestRepository) ListDBQueryPreviews(context.Context, string, AuditDBQueryPreviewParams) ([]AuditDBQueryPreview, int64, error) {
	return r.queries, int64(len(r.queries)), r.err
}
func (r *auditQueryTestRepository) ListAuditEvents(context.Context, AuditEventListParams) ([]AuditEvent, int64, error) {
	r.events++
	return nil, 0, r.err
}
func (r *auditQueryTestRepository) ListLoginAuditLogs(context.Context, LoginAuditListParams) ([]LoginAuditLog, int64, error) {
	r.logins++
	return nil, 0, r.err
}
