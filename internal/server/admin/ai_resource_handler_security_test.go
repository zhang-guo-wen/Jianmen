package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/pkg/apiresp"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
)

func TestAIResourceCredentialsHideAllRejectedResourceStates(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	const actorID = "ai-credential-actor"
	seedConnectionAction(t, db, actorID, rbac.ActionSessionConnect)

	now := time.Now().UTC()
	expired := now.Add(-time.Minute)
	host := model.Host{
		ID: "ai-credential-host", Name: "ai-credential-host",
		Address: "127.0.0.1", Port: 22, Protocol: "ssh", Status: "active",
	}
	accounts := []model.HostAccount{
		{ID: "credential-unauthorized", HostID: host.ID, Name: "unauthorized", Username: "root", AuthType: "password", Password: model.NewEncryptedField("secret"), Status: "active", ResourceID: "H101"},
		{ID: "credential-disabled", HostID: host.ID, Name: "disabled", Username: "root", AuthType: "password", Password: model.NewEncryptedField("secret"), Status: "disabled", ResourceID: "H102"},
		{ID: "credential-expired", HostID: host.ID, Name: "expired", Username: "root", AuthType: "password", Password: model.NewEncryptedField("secret"), Status: "active", ExpiresAt: &expired, ResourceID: "H103"},
	}
	if err := db.Create(&host).Error; err != nil {
		t.Fatalf("create credential host: %v", err)
	}
	for index := range accounts {
		if err := db.Create(&accounts[index]).Error; err != nil {
			t.Fatalf("create credential account %q: %v", accounts[index].ID, err)
		}
	}

	instance := model.DatabaseInstance{
		ID: "credential-instance", Name: "credential-instance", Protocol: "mysql",
		Address: "127.0.0.1", Port: 3306, TLSMode: "disable", Status: "active",
	}
	databaseAccount := model.DatabaseAccount{
		ID: "credential-type-mismatch", InstanceID: instance.ID, UniqueName: "credential-type-mismatch",
		Username: "db-user", Password: model.NewEncryptedField("secret"), Status: "active", ResourceID: "D101",
	}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatalf("create credential database instance: %v", err)
	}
	if err := db.Create(&databaseAccount).Error; err != nil {
		t.Fatalf("create credential database account: %v", err)
	}

	for _, resourceID := range []string{
		"credential-missing",
		"credential-disabled",
		"credential-expired",
		databaseAccount.ID,
	} {
		if err := db.Create(&model.ResourceGrant{
			ID: "grant-" + resourceID, PrincipalType: "user", PrincipalID: actorID,
			ResourceType: model.ResourceTypeHostAccount, ResourceID: resourceID,
			Effect: model.PermissionEffectAllow,
		}).Error; err != nil {
			t.Fatalf("create credential grant %q: %v", resourceID, err)
		}
	}

	tests := []struct {
		name string
		id   string
	}{
		{name: "unauthorized", id: accounts[0].ID},
		{name: "not found", id: "credential-missing"},
		{name: "disabled", id: accounts[1].ID},
		{name: "expired", id: accounts[2].ID},
		{name: "type mismatch", id: databaseAccount.ID},
	}
	var want credentialErrorSignature
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := withTestUser(
				httptest.NewRequest(
					http.MethodPost,
					"/api/ai/resources/"+model.ResourceTypeHostAccount+"/"+test.id+"/credentials",
					nil,
				),
				actorID,
				actorID,
			)
			response := httptest.NewRecorder()
			server.handleAIResources(response, request)
			got := decodeCredentialErrorSignature(t, response)
			if index == 0 {
				want = got
				return
			}
			if got != want {
				t.Fatalf("credential rejection = %#v, want %#v", got, want)
			}
		})
	}
	if want.Status != http.StatusNotFound ||
		want.Code != http.StatusNotFound ||
		want.ErrorCode != apiresp.CodeNotFound ||
		want.Message != aiResourceNotFoundMessage {
		t.Fatalf("credential rejection signature = %#v", want)
	}
	var credentialCount int64
	if err := db.Model(&model.ConnectionPassword{}).Count(&credentialCount).Error; err != nil {
		t.Fatalf("count connection passwords: %v", err)
	}
	if credentialCount != 0 {
		t.Fatalf("issued %d credentials for rejected resources", credentialCount)
	}
}

type credentialErrorSignature struct {
	Status    int
	Code      int
	ErrorCode string
	Message   string
}

func decodeCredentialErrorSignature(t *testing.T, response *httptest.ResponseRecorder) credentialErrorSignature {
	t.Helper()
	var envelope apiresp.ErrorEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode credential error: %v; body=%s", err, response.Body.String())
	}
	return credentialErrorSignature{
		Status: response.Code, Code: envelope.Code,
		ErrorCode: envelope.Error.Code, Message: envelope.Error.Message,
	}
}

type aiHandlerRepositoryStub struct {
	host        service.AIHostAccountMetadata
	listHostErr error
	hostErr     error
}

func (r *aiHandlerRepositoryStub) ListHostAccounts(context.Context) ([]service.AIHostAccountMetadata, error) {
	if r.listHostErr != nil {
		return nil, r.listHostErr
	}
	if r.host.ID == "" {
		return []service.AIHostAccountMetadata{}, nil
	}
	return []service.AIHostAccountMetadata{r.host}, nil
}

func (r *aiHandlerRepositoryStub) HostAccount(context.Context, string) (service.AIHostAccountMetadata, error) {
	if r.hostErr != nil {
		return service.AIHostAccountMetadata{}, r.hostErr
	}
	return r.host, nil
}

func (*aiHandlerRepositoryStub) ListDatabaseAccounts(context.Context) ([]service.AIDatabaseAccountMetadata, error) {
	return []service.AIDatabaseAccountMetadata{}, nil
}

func (*aiHandlerRepositoryStub) DatabaseAccount(context.Context, string) (service.AIDatabaseAccountMetadata, error) {
	return service.AIDatabaseAccountMetadata{}, service.ErrAIResourceNotFound
}

type aiHandlerAuthorizerStub struct {
	allowed bool
	err     error
}

func (a *aiHandlerAuthorizerStub) AuthorizeAIResources(
	_ context.Context,
	_ string,
	requests []service.AIResourceAuthorizationRequest,
) ([]service.AIResourceAuthorizationDecision, error) {
	if a.err != nil {
		return nil, a.err
	}
	decisions := make([]service.AIResourceAuthorizationDecision, len(requests))
	for index := range decisions {
		decisions[index].Allowed = a.allowed
	}
	return decisions, nil
}

type aiHandlerSessionStub struct {
	err error
}

func (s *aiHandlerSessionStub) GetOrCreateAIResourceSession(context.Context, string) (service.AIResourceSession, error) {
	if s.err != nil {
		return service.AIResourceSession{}, s.err
	}
	return service.AIResourceSession{ID: "abc12", Seq: 1}, nil
}

func TestAIResourceHandlersDoNotExposeInternalErrors(t *testing.T) {
	const resourceID = "handler-host"
	host := service.AIHostAccountMetadata{
		ID: resourceID, Protocol: "ssh", Status: "enabled",
		LifecycleStatus: "active", ParentStatus: "active", ResourceID: "H201",
	}
	tests := []struct {
		name       string
		path       string
		invoke     func(*Server, http.ResponseWriter, *http.Request)
		repository *aiHandlerRepositoryStub
		authorizer *aiHandlerAuthorizerStub
		sessions   *aiHandlerSessionStub
		sentinel   string
	}{
		{
			name: "repository", path: "/api/ai/resources",
			invoke: func(server *Server, w http.ResponseWriter, r *http.Request) {
				server.listAIResources(w, r)
			},
			repository: &aiHandlerRepositoryStub{listHostErr: errors.New("repository-sentinel")},
			authorizer: &aiHandlerAuthorizerStub{allowed: true},
			sessions:   &aiHandlerSessionStub{},
			sentinel:   "repository-sentinel",
		},
		{
			name: "authorization", path: "/api/ai/resources/host_account/" + resourceID,
			invoke: func(server *Server, w http.ResponseWriter, r *http.Request) {
				server.getAIResource(w, r, model.ResourceTypeHostAccount, resourceID)
			},
			repository: &aiHandlerRepositoryStub{host: host},
			authorizer: &aiHandlerAuthorizerStub{err: errors.New("authorization-sentinel")},
			sessions:   &aiHandlerSessionStub{},
			sentinel:   "authorization-sentinel",
		},
		{
			name: "session", path: "/api/ai/resources/host_account/" + resourceID + "/session",
			invoke: func(server *Server, w http.ResponseWriter, r *http.Request) {
				server.issueAIResourceSession(w, r, model.ResourceTypeHostAccount, resourceID)
			},
			repository: &aiHandlerRepositoryStub{host: host},
			authorizer: &aiHandlerAuthorizerStub{allowed: true},
			sessions:   &aiHandlerSessionStub{err: errors.New("session-sentinel")},
			sentinel:   "session-sentinel",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resourceService, err := service.NewAIResourceService(test.repository, test.authorizer, test.sessions)
			if err != nil {
				t.Fatalf("new AI resource service: %v", err)
			}
			var logs bytes.Buffer
			server := &Server{
				aiResources: resourceService,
				logger:      slog.New(slog.NewTextHandler(&logs, nil)),
			}
			request := withTestUser(httptest.NewRequest(http.MethodGet, test.path, nil), "actor", "actor")
			response := httptest.NewRecorder()
			test.invoke(server, response, request)

			assertAIResourceUnavailable(t, response, test.sentinel)
			if !strings.Contains(logs.String(), test.sentinel) {
				t.Fatalf("controlled log does not contain internal sentinel: %s", logs.String())
			}
		})
	}
}

func TestAIResourceHandlerSanitizesContextErrors(t *testing.T) {
	resourceService, err := service.NewAIResourceService(
		&aiHandlerRepositoryStub{},
		&aiHandlerAuthorizerStub{allowed: true},
		&aiHandlerSessionStub{},
	)
	if err != nil {
		t.Fatalf("new AI resource service: %v", err)
	}
	var logs bytes.Buffer
	server := &Server{
		aiResources: resourceService,
		logger:      slog.New(slog.NewTextHandler(&logs, nil)),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := withTestUser(
		httptest.NewRequest(http.MethodGet, "/api/ai/resources", nil).WithContext(ctx),
		"actor",
		"actor",
	)
	response := httptest.NewRecorder()
	server.listAIResources(response, request)
	assertAIResourceUnavailable(t, response, context.Canceled.Error())
}

func assertAIResourceUnavailable(t *testing.T, response *httptest.ResponseRecorder, sentinel string) {
	t.Helper()
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusInternalServerError, response.Body.String())
	}
	if strings.Contains(response.Body.String(), sentinel) {
		t.Fatalf("response exposes internal sentinel %q: %s", sentinel, response.Body.String())
	}
	var envelope apiresp.ErrorEnvelope
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode AI resource error: %v", err)
	}
	if envelope.Code != http.StatusInternalServerError ||
		envelope.Error.Code != apiresp.CodeInternal ||
		envelope.Error.Message != aiResourceUnavailableMessage {
		t.Fatalf("AI resource error = %#v", envelope)
	}
}
