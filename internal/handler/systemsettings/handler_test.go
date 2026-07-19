package systemsettings

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"jianmen/internal/service"
)

type fakeSettingsService struct {
	state         service.SystemSettingsState
	update        service.SystemSettingsUpdate
	updateCalled  bool
	updateError   error
	revisions     []service.SystemSettingsRevision
	revisionLimit int
}

func (f *fakeSettingsService) GetState(context.Context) (service.SystemSettingsState, error) {
	return f.state, nil
}

func (f *fakeSettingsService) Update(
	_ context.Context,
	update service.SystemSettingsUpdate,
) (service.SystemSettingsState, error) {
	f.updateCalled = true
	f.update = update
	return f.state, f.updateError
}

func (f *fakeSettingsService) ListRevisions(
	_ context.Context,
	limit int,
) ([]service.SystemSettingsRevision, error) {
	f.revisionLimit = limit
	return f.revisions, nil
}

type fakeDiagnosticsService struct {
	infrastructure service.SystemSettingsRuntimeInfrastructure
	result         service.SystemSettingsDiagnosticResult
}

func (f fakeDiagnosticsService) Infrastructure() service.SystemSettingsRuntimeInfrastructure {
	return f.infrastructure
}

func (f fakeDiagnosticsService) TestGuacd(context.Context) service.SystemSettingsDiagnosticResult {
	return f.result
}

func (f fakeDiagnosticsService) TestObjectStorage(
	context.Context,
) service.SystemSettingsDiagnosticResult {
	return f.result
}

func TestCollectionReturnsStateAndRedactedInfrastructure(t *testing.T) {
	settings := &fakeSettingsService{state: testSystemSettingsState()}
	handler := newTestHandler(t, settings, fakeDiagnosticsService{
		infrastructure: service.SystemSettingsRuntimeInfrastructure{
			GuacdAddress: "127.0.0.1:4822",
			ObjectStorage: service.SystemSettingsObjectStorageInfrastructure{
				Provider: "s3", AccessKeyIDConfigured: true, SecretAccessKeyConfigured: true,
			},
		},
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/system-settings", nil)

	handler.Collection(recorder, request, Subject{})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data stateResponse `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Data.Revision != 3 || !envelope.Data.PendingRestart {
		t.Fatalf("state response = %#v", envelope.Data)
	}
	storage := envelope.Data.Infrastructure.ObjectStorage
	if !storage.CredentialsConfigured || storage.AccessKeyIDConfigured != true {
		t.Fatalf("object storage response = %#v", storage)
	}
}

func TestCollectionUpdatesWithAuthenticatedActor(t *testing.T) {
	settings := &fakeSettingsService{state: testSystemSettingsState()}
	handler := newTestHandler(t, settings, fakeDiagnosticsService{})
	body := []byte(`{
		"settings": {
			"web_rdp_enabled": true,
			"web_rdp_connect_timeout_seconds": 30,
			"web_rdp_allow_unrecorded": false,
			"recording_enabled": true,
			"recording_record_input": false,
			"recording_record_commands": true,
			"recording_retention_days": 30,
			"recording_max_replay_bytes": 1024,
			"recording_cleanup_batch_size": 100
		},
		"expected_revision": 3,
		"confirm_risk": true
	}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/system-settings", bytes.NewReader(body))

	handler.Collection(recorder, request, Subject{UserID: "admin-1", Username: "alice"})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if settings.update.Actor.ID != "admin-1" ||
		settings.update.Actor.Username != "alice" ||
		settings.update.ExpectedRevision != 3 ||
		!settings.update.ConfirmRisk {
		t.Fatalf("update = %#v", settings.update)
	}
}

func TestCollectionMapsRiskConfirmationError(t *testing.T) {
	settings := &fakeSettingsService{
		state:       testSystemSettingsState(),
		updateError: service.ErrSystemSettingsRiskConfirmationRequired,
	}
	handler := newTestHandler(t, settings, fakeDiagnosticsService{})
	body := bytes.ReplaceAll(validUpdateBody(), []byte(`"confirm_risk":true`), []byte(`"confirm_risk":false`))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/system-settings", bytes.NewReader(body))

	handler.Collection(recorder, request, Subject{})

	if recorder.Code != http.StatusPreconditionFailed {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestCollectionMapsRevisionConflict(t *testing.T) {
	settings := &fakeSettingsService{
		state:       testSystemSettingsState(),
		updateError: service.ErrSystemSettingsRevisionConflict,
	}
	handler := newTestHandler(t, settings, fakeDiagnosticsService{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/system-settings",
		bytes.NewReader(validUpdateBody()),
	)

	handler.Collection(recorder, request, Subject{})

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestCollectionRejectsIncompleteSettings(t *testing.T) {
	settings := &fakeSettingsService{state: testSystemSettingsState()}
	handler := newTestHandler(t, settings, fakeDiagnosticsService{})
	body := []byte(`{
		"settings":{
			"web_rdp_connect_timeout_seconds":15,
			"recording_retention_days":30,
			"recording_max_replay_bytes":1024,
			"recording_cleanup_batch_size":100
		},
		"expected_revision":3
	}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/system-settings", bytes.NewReader(body))

	handler.Collection(recorder, request, Subject{})

	if recorder.Code != http.StatusBadRequest || settings.updateCalled {
		t.Fatalf("status = %d, updateCalled = %v, body = %s",
			recorder.Code, settings.updateCalled, recorder.Body.String())
	}
}

func TestCollectionRejectsUnknownJSONField(t *testing.T) {
	settings := &fakeSettingsService{state: testSystemSettingsState()}
	handler := newTestHandler(t, settings, fakeDiagnosticsService{})
	body := bytes.Replace(
		validUpdateBody(),
		[]byte(`"expected_revision":3`),
		[]byte(`"expected_revision":3,"unexpected":true`),
		1,
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/system-settings", bytes.NewReader(body))

	handler.Collection(recorder, request, Subject{})

	if recorder.Code != http.StatusBadRequest || settings.updateCalled {
		t.Fatalf("status = %d, updateCalled = %v", recorder.Code, settings.updateCalled)
	}
}

func TestRevisionsUsesValidatedLimit(t *testing.T) {
	settings := &fakeSettingsService{revisions: []service.SystemSettingsRevision{{
		ID: "revision-4", Revision: 4, Snapshot: testSystemSettingsState().Desired,
		ChangedFields:     []string{"recording_retention_days"},
		UpdatedByUsername: "alice", CreatedAt: time.Now().UTC(),
	}}}
	handler := newTestHandler(t, settings, fakeDiagnosticsService{})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/system-settings/revisions?limit=10",
		nil,
	)

	handler.Revisions(recorder, request)

	if recorder.Code != http.StatusOK || settings.revisionLimit != 10 ||
		!bytes.Contains(recorder.Body.Bytes(), []byte(`"actor_username":"alice"`)) {
		t.Fatalf("status = %d, limit = %d, body = %s",
			recorder.Code, settings.revisionLimit, recorder.Body.String())
	}

	invalidRecorder := httptest.NewRecorder()
	invalidRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/system-settings/revisions?limit=101",
		nil,
	)
	handler.Revisions(invalidRecorder, invalidRequest)
	if invalidRecorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid limit status = %d", invalidRecorder.Code)
	}
}

func TestDiagnosticReturnsProbeResult(t *testing.T) {
	handler := newTestHandler(t, &fakeSettingsService{}, fakeDiagnosticsService{
		result: service.SystemSettingsDiagnosticResult{
			OK: true, Message: "ok", LatencyMS: 12,
		},
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/system-settings/diagnostics/object-storage",
		nil,
	)

	handler.Diagnostic(recorder, request, "object-storage")

	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(`"latency_ms":12`)) {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestDiagnosticReturnsBadGatewayForFailedProbe(t *testing.T) {
	handler := newTestHandler(t, &fakeSettingsService{}, fakeDiagnosticsService{
		result: service.SystemSettingsDiagnosticResult{
			Message: "probe failed", LatencyMS: 45,
		},
	})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/system-settings/diagnostics/guacd",
		nil,
	)

	handler.Diagnostic(recorder, request, "guacd")

	if recorder.Code != http.StatusBadGateway ||
		!bytes.Contains(recorder.Body.Bytes(), []byte(`"latency_ms":45`)) {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func newTestHandler(
	t *testing.T,
	settings SettingsService,
	diagnostics DiagnosticsService,
) *Handler {
	t.Helper()
	handler, err := New(settings, diagnostics)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return handler
}

func testSystemSettingsState() service.SystemSettingsState {
	values := service.SystemSettings{
		WebRDPConnectTimeoutSeconds: 15, RecordingEnabled: true,
		RecordingRecordCommands: true, RecordingRetentionDays: 30,
		RecordingMaxReplayBytes: 1024, RecordingCleanupBatchSize: 100,
	}
	return service.SystemSettingsState{
		Desired: values, Effective: values, Revision: 3, EffectiveRevision: 2,
		PendingRestart: true, UpdatedAt: time.Now().UTC(),
	}
}

func validUpdateBody() []byte {
	return []byte(`{
		"settings":{
			"web_rdp_enabled":false,
			"web_rdp_connect_timeout_seconds":15,
			"web_rdp_allow_unrecorded":false,
			"recording_enabled":true,
			"recording_record_input":false,
			"recording_record_commands":true,
			"recording_retention_days":30,
			"recording_max_replay_bytes":1024,
			"recording_cleanup_batch_size":100
		},
		"expected_revision":3,
		"confirm_risk":true
	}`)
}
