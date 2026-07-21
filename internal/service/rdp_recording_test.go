package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/objectstore"
)

type rdpAuditFinishCall struct {
	id              string
	outcome         string
	failureCode     string
	failureMessage  string
	recordingStatus string
	endedAt         time.Time
}

type rdpAuditRepositoryFake struct {
	order           []string
	beginSession    *model.AuditSession
	beginArtifact   *model.AuditArtifact
	artifactStates  []model.AuditArtifact
	finish          *rdpAuditFinishCall
	beginErr        error
	activateErr     error
	updateErrAt     map[int]error
	finishErr       error
	updateCalls     int
	activateCalls   int
	recoveryItems   []RDPRecordingRecoveryItem
	recoveryBatches [][]RDPRecordingRecoveryItem
	recoveryErr     error
	recoveryCalls   []bool
	finishCalls     int
}

func (r *rdpAuditRepositoryFake) BeginRDPAuditSession(
	_ context.Context,
	session *model.AuditSession,
	artifact *model.AuditArtifact,
) error {
	r.order = append(r.order, "audit.begin")
	if r.beginErr != nil {
		return r.beginErr
	}
	sessionCopy := *session
	r.beginSession = &sessionCopy
	if artifact != nil {
		if artifact.ID == "" {
			artifact.ID = "artifact-1"
		}
		artifact.AuditSessionID = session.ID
		artifactCopy := *artifact
		r.beginArtifact = &artifactCopy
	}
	return nil
}

func (r *rdpAuditRepositoryFake) ActivateRDPAuditSession(
	_ context.Context,
	_ string,
) error {
	r.order = append(r.order, "audit.activate")
	r.activateCalls++
	return r.activateErr
}

func (r *rdpAuditRepositoryFake) FinishAuditSession(
	_ context.Context,
	id string,
	outcome string,
	failureCode string,
	failureMessage string,
	recordingStatus string,
	endedAt time.Time,
) error {
	r.order = append(r.order, "audit.finish")
	r.finishCalls++
	r.finish = &rdpAuditFinishCall{
		id: id, outcome: outcome, failureCode: failureCode,
		failureMessage:  failureMessage,
		recordingStatus: recordingStatus, endedAt: endedAt,
	}
	return r.finishErr
}

func (r *rdpAuditRepositoryFake) UpdateAuditArtifact(
	_ context.Context,
	artifact *model.AuditArtifact,
) error {
	r.updateCalls++
	r.order = append(r.order, "artifact."+artifact.Status)
	artifactCopy := *artifact
	r.artifactStates = append(r.artifactStates, artifactCopy)
	if err := r.updateErrAt[r.updateCalls]; err != nil {
		return err
	}
	return nil
}

func (r *rdpAuditRepositoryFake) ClaimRecoverableRDPRecordings(
	_ context.Context,
	includeInterrupted bool,
	_ time.Time,
	_ time.Time,
) ([]RDPRecordingRecoveryItem, error) {
	r.recoveryCalls = append(r.recoveryCalls, includeInterrupted)
	if r.recoveryErr != nil {
		return nil, r.recoveryErr
	}
	if len(r.recoveryBatches) > 0 {
		items := r.recoveryBatches[0]
		r.recoveryBatches = r.recoveryBatches[1:]
		return append([]RDPRecordingRecoveryItem(nil), items...), nil
	}
	items := append([]RDPRecordingRecoveryItem(nil), r.recoveryItems...)
	r.recoveryItems = nil
	return items, nil
}

type rdpObjectStoreFake struct {
	repository  *rdpAuditRepositoryFake
	putErr      error
	putCalls    int
	putKey      string
	putBody     []byte
	putSize     int64
	contentType string
}

func (s *rdpObjectStoreFake) Put(
	_ context.Context,
	key string,
	src io.Reader,
	size int64,
	contentType string,
) (objectstore.Info, error) {
	s.putCalls++
	s.repository.order = append(s.repository.order, "object.put")
	s.putKey = key
	s.putSize = size
	s.contentType = contentType
	body, err := io.ReadAll(src)
	if err != nil {
		return objectstore.Info{}, err
	}
	s.putBody = body
	if s.putErr != nil {
		return objectstore.Info{}, s.putErr
	}
	return objectstore.Info{
		Key: key, Size: int64(len(body)), ContentType: contentType,
	}, nil
}

func (*rdpObjectStoreFake) Open(context.Context, string) (objectstore.Reader, error) {
	return nil, errors.New("not implemented")
}

func (*rdpObjectStoreFake) Stat(context.Context, string) (objectstore.Info, error) {
	return objectstore.Info{}, errors.New("not implemented")
}

func (*rdpObjectStoreFake) Delete(context.Context, string) error {
	return errors.New("not implemented")
}

func newRDPRecordingServiceForTest(
	t *testing.T,
	repository *rdpAuditRepositoryFake,
	objects objectstore.Store,
	now time.Time,
) (*RDPRecordingService, RDPRecordingConfig) {
	t.Helper()
	root := t.TempDir()
	config := RDPRecordingConfig{
		SpoolRoot:          filepath.Join(root, "recordings"),
		GuacdRecordingRoot: "/recordings",
		LocalDriveRoot:     filepath.Join(root, "drives"),
		GuacdDriveRoot:     "/drives",
	}
	service, err := NewRDPRecordingService(config, repository, objects)
	if err != nil {
		t.Fatalf("NewRDPRecordingService() error = %v", err)
	}
	service.now = func() time.Time { return now }
	return service, config
}

func validBeginRDPAuditInput() BeginRDPAuditInput {
	return BeginRDPAuditInput{
		ID: "session-1", UserSessionID: "login-session-1",
		UserID: "user-1", Username: "alice",
		Target: WebRDPTarget{
			ID: "account-1", HostID: "host-1", HostName: "windows-prod",
			Protocol: "rdp", Address: "10.0.0.8", Port: 3389,
			Username: "Administrator", Password: "secret",
		},
		ClientIP: "192.0.2.8",
		Policy: WebRDPChannelPolicy{
			ClipboardRead: true,
			FileUpload:    true,
		},
	}
}

func TestRDPRecordingServiceBeginCreatesAuditAndObjectIndexBeforeConnection(t *testing.T) {
	now := time.Date(2026, 7, 19, 11, 0, 0, 0, time.UTC)
	repository := &rdpAuditRepositoryFake{}
	objects := &rdpObjectStoreFake{repository: repository}
	service, config := newRDPRecordingServiceForTest(t, repository, objects, now)
	input := validBeginRDPAuditInput()
	input.Policy.DriveMapping = true

	handle, err := service.Begin(context.Background(), input)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if !reflect.DeepEqual(repository.order, []string{"audit.begin"}) {
		t.Fatalf("call order = %#v", repository.order)
	}
	if repository.beginSession == nil || repository.beginArtifact == nil {
		t.Fatalf("audit/index were not created: session=%#v artifact=%#v",
			repository.beginSession, repository.beginArtifact)
	}
	session := repository.beginSession
	if session.ID != input.ID ||
		session.Protocol != "rdp" ||
		session.ResourceType != model.ResourceTypeHostAccount ||
		session.ResourceID != input.Target.ID ||
		session.AccountID != input.Target.ID ||
		session.Outcome != model.AuditOutcomeConnecting ||
		session.RecordingStatus != model.RecordingStatusPending {
		t.Fatalf("audit session = %#v", session)
	}
	artifact := repository.beginArtifact
	wantKey := "rdp/2026/07/19/session-1/recording.guac"
	if artifact.AuditSessionID != input.ID ||
		artifact.ObjectKey != wantKey ||
		artifact.Status != model.RecordingStatusPending ||
		artifact.Format != model.AuditArtifactFormatGuac {
		t.Fatalf("recording index = %#v", artifact)
	}
	if objects.putCalls != 0 {
		t.Fatalf("object was uploaded before session: calls = %d", objects.putCalls)
	}
	if handle.LocalPath != filepath.Join(config.SpoolRoot, input.ID, rdpRecordingFilename) ||
		handle.GuacdPath != "/recordings/session-1" ||
		handle.LocalDrivePath != filepath.Join(config.LocalDriveRoot, input.ID) ||
		handle.GuacdDrivePath != "/drives/session-1" {
		t.Fatalf("audit handle paths = %#v", handle)
	}

	sessionJSON, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	artifactJSON, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	for _, encoded := range [][]byte{sessionJSON, artifactJSON} {
		if bytes.Contains(encoded, []byte(config.SpoolRoot)) ||
			bytes.Contains(encoded, []byte(config.LocalDriveRoot)) {
			t.Fatalf("local spool path leaked in JSON: %s", encoded)
		}
	}
	if bytes.Contains(artifactJSON, []byte(wantKey)) {
		t.Fatalf("object key leaked in JSON: %s", artifactJSON)
	}
}

func TestRDPRecordingServiceFinishUploadsHashAndMarksReady(t *testing.T) {
	now := time.Date(2026, 7, 19, 11, 0, 0, 0, time.UTC)
	repository := &rdpAuditRepositoryFake{}
	objects := &rdpObjectStoreFake{repository: repository}
	service, _ := newRDPRecordingServiceForTest(t, repository, objects, now)
	handle, err := service.Begin(context.Background(), validBeginRDPAuditInput())
	if err != nil {
		t.Fatal(err)
	}
	recording := []byte("1234.sync,1.0;\n4.size,4.1024,3.768;\n")
	if err = os.WriteFile(handle.LocalPath, recording, 0o600); err != nil {
		t.Fatal(err)
	}

	if err = service.Finish(
		context.Background(),
		handle,
		model.AuditOutcomeSucceeded,
		"",
		"",
	); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	wantOrder := []string{
		"audit.begin",
		"artifact.uploading",
		"object.put",
		"artifact.ready",
		"audit.finish",
	}
	if !reflect.DeepEqual(repository.order, wantOrder) {
		t.Fatalf("call order = %#v, want %#v", repository.order, wantOrder)
	}
	if objects.putCalls != 1 ||
		objects.putKey != handle.Artifact.ObjectKey ||
		objects.putSize != int64(len(recording)) ||
		objects.contentType != rdpRecordingContentType ||
		!bytes.Equal(objects.putBody, recording) {
		t.Fatalf("object upload = %#v", objects)
	}
	sum := sha256.Sum256(recording)
	if handle.Artifact.Status != model.RecordingStatusReady ||
		handle.Artifact.SizeBytes != int64(len(recording)) ||
		handle.Artifact.SHA256 != hex.EncodeToString(sum[:]) ||
		handle.Artifact.CompletedAt == nil {
		t.Fatalf("ready artifact = %#v", handle.Artifact)
	}
	if repository.finish == nil ||
		repository.finish.outcome != model.AuditOutcomeSucceeded ||
		repository.finish.recordingStatus != model.RecordingStatusReady {
		t.Fatalf("finish call = %#v", repository.finish)
	}
	if _, err = os.Stat(handle.LocalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("local recording remains after upload: %v", err)
	}
}

func TestRDPRecordingServiceMissingRecordingFailsClosed(t *testing.T) {
	now := time.Date(2026, 7, 19, 11, 0, 0, 0, time.UTC)
	repository := &rdpAuditRepositoryFake{}
	objects := &rdpObjectStoreFake{repository: repository}
	service, _ := newRDPRecordingServiceForTest(t, repository, objects, now)
	service.recordingWaitTimeout = 10 * time.Millisecond
	handle, err := service.Begin(context.Background(), validBeginRDPAuditInput())
	if err != nil {
		t.Fatal(err)
	}

	err = service.Finish(
		context.Background(),
		handle,
		model.AuditOutcomeFailed,
		"recording_missing",
		"guacd disconnected",
	)
	if err == nil || !strings.Contains(err.Error(), "not produced") {
		t.Fatalf("Finish() error = %v, want missing recording", err)
	}
	if objects.putCalls != 0 {
		t.Fatalf("missing recording was uploaded: calls = %d", objects.putCalls)
	}
	if len(repository.artifactStates) != 2 ||
		repository.artifactStates[0].Status != model.RecordingStatusUploading ||
		repository.artifactStates[1].Status != model.RecordingStatusFailed {
		t.Fatalf("artifact states = %#v", repository.artifactStates)
	}
	if repository.finish == nil ||
		repository.finish.recordingStatus != model.RecordingStatusFailed {
		t.Fatalf("finish call = %#v", repository.finish)
	}
}
