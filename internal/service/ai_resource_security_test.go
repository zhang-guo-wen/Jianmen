package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestAIResourceDependencyFailuresFailClosed(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	host := AIHostAccountMetadata{
		ID: "host", Protocol: "ssh", Status: "enabled", LifecycleStatus: "active", ParentStatus: "active", ResourceID: "H001",
	}
	repositoryErr := errors.New("repository unavailable")
	authorizationErr := errors.New("authorization unavailable")
	sessionErr := errors.New("session unavailable")

	t.Run("host list repository", func(t *testing.T) {
		repository := &aiResourceRepositoryStub{listHostErr: repositoryErr}
		authorizer := &aiResourceAuthorizerStub{}
		service := newAIResourceTestService(t, repository, authorizer, &aiResourceSessionCreatorStub{}, now)
		resources, err := service.List(context.Background(), "actor")
		if resources != nil || !errors.Is(err, repositoryErr) || authorizer.calls != 0 {
			t.Fatalf("resources = %#v, error = %v, authorization calls = %d", resources, err, authorizer.calls)
		}
	})

	t.Run("database list repository", func(t *testing.T) {
		repository := &aiResourceRepositoryStub{listDatabaseErr: repositoryErr}
		authorizer := &aiResourceAuthorizerStub{}
		service := newAIResourceTestService(t, repository, authorizer, &aiResourceSessionCreatorStub{}, now)
		resources, err := service.List(context.Background(), "actor")
		if resources != nil || !errors.Is(err, repositoryErr) || authorizer.calls != 0 {
			t.Fatalf("resources = %#v, error = %v, authorization calls = %d", resources, err, authorizer.calls)
		}
	})

	t.Run("list authorization", func(t *testing.T) {
		repository := &aiResourceRepositoryStub{hosts: []AIHostAccountMetadata{host}}
		authorizer := &aiResourceAuthorizerStub{err: authorizationErr}
		service := newAIResourceTestService(t, repository, authorizer, &aiResourceSessionCreatorStub{}, now)
		resources, err := service.List(context.Background(), "actor")
		if resources != nil || !errors.Is(err, authorizationErr) {
			t.Fatalf("resources = %#v, error = %v", resources, err)
		}
	})

	t.Run("list incomplete decisions", func(t *testing.T) {
		repository := &aiResourceRepositoryStub{hosts: []AIHostAccountMetadata{host}}
		authorizer := &aiResourceAuthorizerStub{decisions: []AIResourceAuthorizationDecision{}}
		service := newAIResourceTestService(t, repository, authorizer, &aiResourceSessionCreatorStub{}, now)
		resources, err := service.List(context.Background(), "actor")
		if resources != nil || err == nil {
			t.Fatalf("resources = %#v, error = %v", resources, err)
		}
	})

	t.Run("get authorization before repository", func(t *testing.T) {
		repository := &aiResourceRepositoryStub{hosts: []AIHostAccountMetadata{host}}
		authorizer := &aiResourceAuthorizerStub{err: authorizationErr}
		service := newAIResourceTestService(t, repository, authorizer, &aiResourceSessionCreatorStub{}, now)
		resource, err := service.Get(context.Background(), "actor", model.ResourceTypeHostAccount, host.ID)
		if resource.ID != "" || !errors.Is(err, authorizationErr) || repository.hostCalls != 0 {
			t.Fatalf("resource = %#v, error = %v, repository calls = %d", resource, err, repository.hostCalls)
		}
	})

	t.Run("get repository", func(t *testing.T) {
		repository := &aiResourceRepositoryStub{hostErr: repositoryErr}
		authorizer := &aiResourceAuthorizerStub{allowed: map[string]bool{
			model.ResourceTypeHostAccount + "/" + host.ID: true,
		}}
		service := newAIResourceTestService(t, repository, authorizer, &aiResourceSessionCreatorStub{}, now)
		resource, err := service.Get(context.Background(), "actor", model.ResourceTypeHostAccount, host.ID)
		if resource.ID != "" || !errors.Is(err, repositoryErr) {
			t.Fatalf("resource = %#v, error = %v", resource, err)
		}
	})

	t.Run("session allocation", func(t *testing.T) {
		repository := &aiResourceRepositoryStub{hosts: []AIHostAccountMetadata{host}}
		authorizer := &aiResourceAuthorizerStub{allowed: map[string]bool{
			model.ResourceTypeHostAccount + "/" + host.ID: true,
		}}
		sessions := &aiResourceSessionCreatorStub{err: sessionErr}
		service := newAIResourceTestService(t, repository, authorizer, sessions, now)
		result, err := service.CreateSession(context.Background(), "actor", model.ResourceTypeHostAccount, host.ID)
		if result.SessionID != "" || result.CompactUsername != "" || !errors.Is(err, sessionErr) || sessions.calls != 1 {
			t.Fatalf("result = %#v, error = %v, session calls = %d", result, err, sessions.calls)
		}
	})
}

func TestAIResourceContextPropagatesToEveryBoundary(t *testing.T) {
	type contextKey string
	const key contextKey = "ai-resource-context"
	ctx := context.WithValue(context.Background(), key, "expected")
	repository := &aiResourceRepositoryStub{
		hosts: []AIHostAccountMetadata{{
			ID: "host", Protocol: "ssh", Status: "enabled", LifecycleStatus: "active", ParentStatus: "active", ResourceID: "H001",
		}},
	}
	authorizer := &aiResourceAuthorizerStub{allowed: map[string]bool{
		model.ResourceTypeHostAccount + "/host": true,
	}}
	sessions := &aiResourceSessionCreatorStub{session: AIResourceSession{ID: "abc12", Seq: 1}}
	service := newAIResourceTestService(t, repository, authorizer, sessions, time.Now())

	if _, err := service.List(ctx, "actor"); err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if _, err := service.CreateSession(ctx, "actor", model.ResourceTypeHostAccount, "host"); err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, received := range append(append(repository.receivedContexts, authorizer.receivedContexts...), sessions.receivedContexts...) {
		if got := received.Value(key); got != "expected" {
			t.Fatalf("propagated context value = %v", got)
		}
	}
}

func TestAIResourceHostProtocolAndStatusFailClosed(t *testing.T) {
	tests := []struct {
		name         string
		protocol     string
		status       string
		parentStatus string
	}{
		{name: "rdp protocol", protocol: "rdp", status: "enabled", parentStatus: "active"},
		{name: "empty protocol", protocol: "", status: "enabled", parentStatus: "active"},
		{name: "unknown protocol", protocol: "telnet", status: "enabled", parentStatus: "active"},
		{name: "pending account", protocol: "ssh", status: "pending", parentStatus: "active"},
		{name: "revoked account", protocol: "ssh", status: "revoked", parentStatus: "active"},
		{name: "empty account status", protocol: "ssh", status: "", parentStatus: "active"},
		{name: "unknown account status", protocol: "ssh", status: "unknown", parentStatus: "active"},
		{name: "pending parent host", protocol: "ssh", status: "enabled", parentStatus: "pending"},
		{name: "empty parent host status", protocol: "ssh", status: "enabled", parentStatus: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const resourceID = "host-unavailable"
			repository := &aiResourceRepositoryStub{
				hosts: []AIHostAccountMetadata{{
					ID: resourceID, Protocol: test.protocol,
					Status: "enabled", LifecycleStatus: test.status, ParentStatus: test.parentStatus,
					ResourceID: "H001",
				}},
			}
			authorizer := &aiResourceAuthorizerStub{allowed: map[string]bool{
				model.ResourceTypeHostAccount + "/" + resourceID: true,
			}}
			sessions := &aiResourceSessionCreatorStub{session: AIResourceSession{ID: "abc12", Seq: 1}}
			service := newAIResourceTestService(t, repository, authorizer, sessions, time.Now())

			resources, err := service.List(context.Background(), "actor")
			if err != nil || len(resources) != 0 {
				t.Fatalf("list resources = %#v, error = %v", resources, err)
			}
			if _, err := service.Get(context.Background(), "actor", model.ResourceTypeHostAccount, resourceID); !errors.Is(err, ErrAIResourceNotFound) {
				t.Fatalf("get error = %v, want not found", err)
			}
			if _, err := service.CreateSession(context.Background(), "actor", model.ResourceTypeHostAccount, resourceID); !errors.Is(err, ErrAIResourceNotFound) {
				t.Fatalf("session error = %v, want not found", err)
			}
			if sessions.calls != 0 {
				t.Fatalf("session creator called %d times", sessions.calls)
			}
		})
	}
}

func TestNewAIResourceServiceRejectsNilDependencies(t *testing.T) {
	repository := &aiResourceRepositoryStub{}
	authorizer := &aiResourceAuthorizerStub{}
	sessions := &aiResourceSessionCreatorStub{}
	var nilRepository *aiResourceRepositoryStub
	var nilAuthorizer *aiResourceAuthorizerStub
	var nilSessions *aiResourceSessionCreatorStub
	tests := []struct {
		name       string
		repository AIResourceRepository
		authorizer AIResourceAuthorizer
		sessions   AIResourceSessionCreator
	}{
		{name: "repository", repository: nilRepository, authorizer: authorizer, sessions: sessions},
		{name: "authorizer", repository: repository, authorizer: nilAuthorizer, sessions: sessions},
		{name: "sessions", repository: repository, authorizer: authorizer, sessions: nilSessions},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if service, err := NewAIResourceService(test.repository, test.authorizer, test.sessions); err == nil || service != nil {
				t.Fatalf("service = %#v, error = %v", service, err)
			}
		})
	}
}
