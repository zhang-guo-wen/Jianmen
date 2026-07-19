package webrdp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/objectstore"
	"jianmen/internal/rbac"
	"jianmen/internal/store"
)

type auditRepositoryStub struct {
	listItems  []store.AuditSessionView
	listTotal  int64
	listErr    error
	listParams []store.AuditListParams
	listCtx    context.Context

	session     *model.AuditSession
	sessionErr  error
	sessionCtx  context.Context
	artifact    model.AuditArtifact
	artifactErr error

	events   []*model.AuditRDPChannelEvent
	eventErr error
	begun    []*model.AuditSession
}

func (s *auditRepositoryStub) BeginRDPAuditSession(
	_ context.Context,
	session *model.AuditSession,
	_ *model.AuditArtifact,
) error {
	copy := *session
	s.begun = append(s.begun, &copy)
	return nil
}

func (s *auditRepositoryStub) ActivateRDPAuditSession(context.Context, string) error {
	return nil
}

func (s *auditRepositoryStub) FinishAuditSession(
	context.Context,
	string,
	string,
	string,
	string,
	string,
	time.Time,
) error {
	return nil
}

func (s *auditRepositoryStub) UpdateAuditArtifact(
	context.Context,
	*model.AuditArtifact,
) error {
	return nil
}

func (s *auditRepositoryStub) CreateAuditRDPChannelEvent(
	_ context.Context,
	event *model.AuditRDPChannelEvent,
) error {
	if s.eventErr != nil {
		return s.eventErr
	}
	copy := *event
	s.events = append(s.events, &copy)
	return nil
}

func (s *auditRepositoryStub) GetAuditSession(ctx context.Context, _ string) (*model.AuditSession, error) {
	s.sessionCtx = ctx
	return s.session, s.sessionErr
}

func (s *auditRepositoryStub) ListAuditSessions(
	ctx context.Context,
	params store.AuditListParams,
) ([]store.AuditSessionView, int64, error) {
	s.listCtx = ctx
	s.listParams = append(s.listParams, params)
	if s.listErr != nil {
		return nil, 0, s.listErr
	}
	total := s.listTotal
	if total == 0 {
		total = int64(len(s.listItems))
	}
	if params.Page > 1 {
		return nil, total, nil
	}
	return append([]store.AuditSessionView(nil), s.listItems...), total, nil
}

func (s *auditRepositoryStub) AuditArtifactBySession(
	context.Context,
	string,
	string,
) (model.AuditArtifact, error) {
	return s.artifact, s.artifactErr
}

type memoryObjectStore struct {
	content  []byte
	info     objectstore.Info
	statErr  error
	openErr  error
	statKeys []string
	openKeys []string
}

func (s *memoryObjectStore) Put(
	context.Context,
	string,
	io.Reader,
	int64,
	string,
) (objectstore.Info, error) {
	return objectstore.Info{}, errors.New("unexpected object put")
}

func (s *memoryObjectStore) Open(
	_ context.Context,
	key string,
) (objectstore.Reader, error) {
	s.openKeys = append(s.openKeys, key)
	if s.openErr != nil {
		return nil, s.openErr
	}
	return &memoryObjectReader{Reader: bytes.NewReader(s.content)}, nil
}

func (s *memoryObjectStore) Stat(
	_ context.Context,
	key string,
) (objectstore.Info, error) {
	s.statKeys = append(s.statKeys, key)
	if s.statErr != nil {
		return objectstore.Info{}, s.statErr
	}
	info := s.info
	if info.Size == 0 {
		info.Size = int64(len(s.content))
	}
	return info, nil
}

func (s *memoryObjectStore) Delete(context.Context, string) error {
	return errors.New("unexpected object delete")
}

type memoryObjectReader struct {
	*bytes.Reader
}

func (*memoryObjectReader) Close() error { return nil }

func TestAuditListFiltersEverySessionByResourcePermission(t *testing.T) {
	audit := &auditRepositoryStub{
		listItems: []store.AuditSessionView{
			{
				ID: "session-a", Protocol: "rdp", ResourceID: "account-a",
				AccountID: "account-a", Username: "alice",
			},
			{
				ID: "session-b", Protocol: "rdp", ResourceID: "account-b",
				AccountID: "account-b", Username: "bob",
			},
			{
				ID: "session-c", Protocol: "rdp", AccountID: "account-c",
				Username: "carol",
			},
		},
		artifact: model.AuditArtifact{ObjectKey: "private/recording/object-key.guac"},
	}
	authorizer := allowRDP(
		"account-a",
		rbac.ActionRDPRecordingView,
	)
	authorizer.allowed["account-c|"+rbac.ActionRDPRecordingView] = true
	handler := &Handler{audit: audit, authorizer: authorizer}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/audit/rdp?user_id=user-1&account_id=account-a&result=succeeded"+
			"&from=2026-07-01T00:00:00Z&to=2026-07-20T00:00:00Z",
		nil,
	)

	handler.AuditList(recorder, request, AuthenticatedSubject{UserID: "reviewer-1"})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response struct {
		Items []store.AuditSessionView `json:"items"`
		Total int                      `json:"total"`
	}
	if err := decodeResponse(recorder, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Total != 2 ||
		len(response.Items) != 2 ||
		response.Items[0].ID != "session-a" ||
		response.Items[1].ID != "session-c" {
		t.Fatalf("filtered response = %#v", response)
	}
	if len(authorizer.calls) != 3 {
		t.Fatalf("authorization calls = %d, want 3", len(authorizer.calls))
	}
	for _, call := range authorizer.calls {
		if call.userID != "reviewer-1" ||
			call.resourceType != model.ResourceTypeHostAccount ||
			len(call.actions) != 1 ||
			call.actions[0] != rbac.ActionRDPRecordingView {
			t.Fatalf("authorization call = %#v", call)
		}
	}
	if len(audit.listParams) != 1 {
		t.Fatalf("list calls = %d, want 1", len(audit.listParams))
	}
	if audit.listCtx != request.Context() {
		t.Fatal("audit list did not receive request context")
	}
	params := audit.listParams[0]
	if params.Protocol != "rdp" ||
		params.UserID != "user-1" ||
		params.AccountID != "account-a" ||
		params.Outcome != "succeeded" ||
		params.StartedFrom == nil ||
		params.StartedTo == nil {
		t.Fatalf("audit list params = %#v", params)
	}
	responseBody := recorder.Body.String()
	if strings.Contains(responseBody, "object_key") ||
		strings.Contains(responseBody, "private/recording/object-key.guac") {
		t.Fatalf("audit list leaked object key: %s", responseBody)
	}
}

func TestRecordingDownloadRequiresResourceAuthorization(t *testing.T) {
	audit := readyRecordingAudit()
	objects := &memoryObjectStore{content: []byte("0123456789")}
	handler := &Handler{
		audit:      audit,
		objects:    objects,
		authorizer: &authorizerStub{allowed: map[string]bool{}},
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/audit/rdp/session-1/recording",
		nil,
	)

	handler.AuditItem(recorder, request, AuthenticatedSubject{UserID: "user-1"})

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	if audit.sessionCtx != request.Context() {
		t.Fatal("audit item did not receive request context")
	}
	if len(objects.statKeys) != 0 || len(objects.openKeys) != 0 {
		t.Fatal("recording object was accessed before authorization")
	}
}

func TestRecordingDownloadSupportsHTTPRange(t *testing.T) {
	audit := readyRecordingAudit()
	lastModified := time.Date(2026, 7, 19, 8, 0, 0, 0, time.UTC)
	objects := &memoryObjectStore{
		content: []byte("0123456789"),
		info: objectstore.Info{
			Size:         10,
			ContentType:  "application/vnd.apache.guacamole.recording",
			ETag:         "etag-1",
			LastModified: lastModified,
		},
	}
	handler := &Handler{
		audit:   audit,
		objects: objects,
		authorizer: allowRDP(
			"account-1",
			rbac.ActionRDPRecordingView,
		),
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/audit/rdp/session-1/recording",
		nil,
	)
	request.Header.Set("Range", "bytes=2-5")

	handler.AuditItem(recorder, request, AuthenticatedSubject{UserID: "user-1"})

	if recorder.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusPartialContent, recorder.Body.String())
	}
	if recorder.Body.String() != "2345" {
		t.Fatalf("range body = %q, want %q", recorder.Body.String(), "2345")
	}
	if got := recorder.Header().Get("Content-Range"); got != "bytes 2-5/10" {
		t.Fatalf("Content-Range = %q, want %q", got, "bytes 2-5/10")
	}
	if got := recorder.Header().Get("Content-Type"); got != audit.artifact.ContentType {
		t.Fatalf("Content-Type = %q, want %q", got, audit.artifact.ContentType)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if len(objects.statKeys) != 1 ||
		len(objects.openKeys) != 1 ||
		objects.statKeys[0] != audit.artifact.ObjectKey ||
		objects.openKeys[0] != audit.artifact.ObjectKey {
		t.Fatalf("object calls: stat=%#v open=%#v", objects.statKeys, objects.openKeys)
	}
	if strings.Contains(recorder.Body.String(), audit.artifact.ObjectKey) {
		t.Fatal("recording response leaked object key")
	}
}

func TestRecordingDownloadRejectsRecordingThatIsNotReady(t *testing.T) {
	audit := readyRecordingAudit()
	audit.session.RecordingStatus = model.RecordingStatusUploading
	objects := &memoryObjectStore{content: []byte("must-not-be-opened")}
	handler := &Handler{
		audit:   audit,
		objects: objects,
		authorizer: allowRDP(
			"account-1",
			rbac.ActionRDPRecordingView,
		),
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/audit/rdp/session-1/recording",
		nil,
	)

	handler.AuditItem(recorder, request, AuthenticatedSubject{UserID: "user-1"})

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if len(objects.statKeys) != 0 || len(objects.openKeys) != 0 {
		t.Fatal("not-ready recording object was accessed")
	}
}

func TestRecordingDownloadRejectsTamperedObject(t *testing.T) {
	audit := readyRecordingAudit()
	objects := &memoryObjectStore{
		content: []byte("012345678X"),
		info:    objectstore.Info{Size: 10},
	}
	handler := &Handler{
		audit:   audit,
		objects: objects,
		authorizer: allowRDP(
			"account-1",
			rbac.ActionRDPRecordingView,
		),
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/audit/rdp/session-1/recording",
		nil,
	)

	handler.AuditItem(recorder, request, AuthenticatedSubject{UserID: "user-1"})

	if recorder.Code != http.StatusConflict {
		t.Fatalf(
			"status = %d, want %d: %s",
			recorder.Code,
			http.StatusConflict,
			recorder.Body.String(),
		)
	}
	if strings.Contains(recorder.Body.String(), "012345678X") {
		t.Fatal("tampered recording content was returned")
	}
}

func readyRecordingAudit() *auditRepositoryStub {
	recording := []byte("0123456789")
	sum := sha256.Sum256(recording)
	return &auditRepositoryStub{
		session: &model.AuditSession{
			ID: "session-1", Protocol: "rdp",
			ResourceType: model.ResourceTypeHostAccount,
			ResourceID:   "account-1", AccountID: "account-1",
			RecordingStatus: model.RecordingStatusReady,
		},
		artifact: model.AuditArtifact{
			ID: "artifact-1", AuditSessionID: "session-1",
			Kind:        model.AuditArtifactKindRecording,
			Format:      model.AuditArtifactFormatGuac,
			ObjectKey:   "rdp/2026/07/19/session-1/recording.guac",
			ContentType: "application/vnd.apache.guacamole.recording",
			SizeBytes:   int64(len(recording)),
			SHA256:      hex.EncodeToString(sum[:]),
			Status:      model.RecordingStatusReady,
		},
	}
}

func decodeResponse(recorder *httptest.ResponseRecorder, value any) error {
	return json.NewDecoder(recorder.Body).Decode(value)
}
