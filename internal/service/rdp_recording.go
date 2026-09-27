package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/objectstore"
)

const (
	rdpRecordingFilename    = "recording.guac"
	rdpRecordingContentType = "application/vnd.apache.guacamole.recording"
)

type RDPAuditRepository interface {
	BeginRDPAuditSession(ctx context.Context, session *model.AuditSession, artifact *model.AuditArtifact) error
	ActivateRDPAuditSession(ctx context.Context, id string) error
	FinishAuditSession(ctx context.Context, id, outcome, failureCode, failureMessage, recordingStatus string, endedAt time.Time) error
	UpdateAuditArtifact(ctx context.Context, artifact *model.AuditArtifact) error
}

type RDPRecordingConfig struct {
	SpoolRoot          string
	GuacdRecordingRoot string
	LocalDriveRoot     string
	GuacdDriveRoot     string
	AllowUnrecorded    bool
}

type BeginRDPAuditInput struct {
	ID            string
	UserSessionID string
	UserID        string
	Username      string
	Target        WebRDPTarget
	ClientIP      string
	Policy        WebRDPChannelPolicy
}

type DeniedRDPAuditInput struct {
	ID             string
	UserSessionID  string
	UserID         string
	Username       string
	TargetID       string
	ClientIP       string
	FailureCode    string
	FailureMessage string
}

type RDPAuditHandle struct {
	Session        model.AuditSession
	Artifact       *model.AuditArtifact
	LocalPath      string
	GuacdPath      string
	RecordingName  string
	LocalDrivePath string
	GuacdDrivePath string
}

type RDPRecordingService struct {
	config               RDPRecordingConfig
	repository           RDPAuditRepository
	objects              objectstore.Store
	now                  func() time.Time
	recordingWaitTimeout time.Duration
}

func NewRDPRecordingService(
	config RDPRecordingConfig,
	repository RDPAuditRepository,
	objects objectstore.Store,
) (*RDPRecordingService, error) {
	if repository == nil {
		return nil, errors.New("RDP audit repository is required")
	}
	if objects == nil {
		return nil, errors.New("RDP recording object store is required")
	}
	if strings.TrimSpace(config.SpoolRoot) == "" || strings.TrimSpace(config.GuacdRecordingRoot) == "" {
		return nil, errors.New("RDP recording roots are required")
	}
	return &RDPRecordingService{
		config: config, repository: repository, objects: objects,
		now:                  func() time.Time { return time.Now().UTC() },
		recordingWaitTimeout: 5 * time.Second,
	}, nil
}

// Begin prepares the guacd spool and atomically writes the audit session and
// object-storage index before any downstream connection is attempted.
func (s *RDPRecordingService) Begin(
	ctx context.Context,
	input BeginRDPAuditInput,
) (*RDPAuditHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = model.NewID()
	}
	startedAt := s.now().UTC()
	localDir, err := safeSessionDir(s.config.SpoolRoot, id)
	if err != nil {
		return nil, err
	}
	recordingEnabled := true
	if err := os.MkdirAll(localDir, 0o700); err != nil {
		if !s.config.AllowUnrecorded {
			return nil, fmt.Errorf("prepare RDP recording spool: %w", err)
		}
		recordingEnabled = false
	}
	policyJSON, err := json.Marshal(input.Policy)
	if err != nil {
		return nil, fmt.Errorf("encode RDP policy snapshot: %w", err)
	}
	session := model.AuditSession{
		ID: id, UserSessionID: input.UserSessionID,
		UserID: input.UserID, Username: input.Username,
		Protocol: "rdp", ProtocolSubtype: "web-rdp",
		ResourceType: model.ResourceTypeHostAccount, ResourceID: input.Target.ID,
		HostID: input.Target.HostID, AccountID: input.Target.ID,
		TargetName:    input.Target.HostName,
		TargetAddress: fmt.Sprintf("%s:%d", input.Target.Address, input.Target.Port),
		AccountName:   input.Target.Username, AccountUsername: input.Target.Username,
		ClientIP: input.ClientIP, StartedAt: startedAt, State: "started",
		Outcome: model.AuditOutcomeConnecting, PolicySnapshot: string(policyJSON),
		RecordingStatus: model.RecordingStatusNone,
	}
	handle := &RDPAuditHandle{
		Session: session, RecordingName: rdpRecordingFilename,
		LocalPath: filepath.Join(localDir, rdpRecordingFilename),
		GuacdPath: posixJoin(s.config.GuacdRecordingRoot, id),
	}
	if recordingEnabled {
		objectKey := path.Join(
			"rdp", startedAt.Format("2006"), startedAt.Format("01"), startedAt.Format("02"),
			id, rdpRecordingFilename,
		)
		handle.Artifact = &model.AuditArtifact{
			Kind: model.AuditArtifactKindRecording, Format: model.AuditArtifactFormatGuac,
			ObjectKey: objectKey, ContentType: rdpRecordingContentType,
			Status: model.RecordingStatusPending,
		}
		handle.Session.RecordingStatus = model.RecordingStatusPending
	}
	if input.Policy.DriveMapping {
		if strings.TrimSpace(s.config.LocalDriveRoot) == "" ||
			strings.TrimSpace(s.config.GuacdDriveRoot) == "" {
			cleanupEmptySpool(localDir)
			return nil, errors.New("RDP drive roots are required when drive mapping is enabled")
		}
		driveDir, driveErr := safeSessionDir(s.config.LocalDriveRoot, id)
		if driveErr != nil {
			cleanupEmptySpool(localDir)
			return nil, driveErr
		}
		if driveErr = os.MkdirAll(driveDir, 0o700); driveErr != nil {
			cleanupEmptySpool(localDir)
			return nil, fmt.Errorf("prepare RDP drive spool: %w", driveErr)
		}
		handle.LocalDrivePath = driveDir
		handle.GuacdDrivePath = posixJoin(s.config.GuacdDriveRoot, id)
	}
	if err := s.repository.BeginRDPAuditSession(ctx, &handle.Session, handle.Artifact); err != nil {
		cleanupEmptySpool(localDir)
		cleanupEmptySpool(handle.LocalDrivePath)
		return nil, err
	}
	return handle, nil
}

func (s *RDPRecordingService) Activate(ctx context.Context, handle *RDPAuditHandle) error {
	if handle == nil {
		return errors.New("RDP audit handle is required")
	}
	return s.repository.ActivateRDPAuditSession(ctx, handle.Session.ID)
}

// RecordDenied persists an access-control denial without allocating a
// recording or local spool. Denied attempts are final audit sessions and can
// therefore be searched through the same RDP audit surface.
func (s *RDPRecordingService) RecordDenied(
	ctx context.Context,
	input DeniedRDPAuditInput,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	input.ID = strings.TrimSpace(input.ID)
	if input.ID == "" {
		input.ID = model.NewID()
	}
	input.UserID = strings.TrimSpace(input.UserID)
	input.TargetID = strings.TrimSpace(input.TargetID)
	if input.UserID == "" || input.TargetID == "" {
		return errors.New("denied RDP audit user and target are required")
	}
	now := s.now().UTC()
	session := model.AuditSession{
		ID: input.ID, UserSessionID: strings.TrimSpace(input.UserSessionID),
		UserID: input.UserID, Username: strings.TrimSpace(input.Username),
		Protocol: "rdp", ProtocolSubtype: "web-rdp",
		ResourceType: model.ResourceTypeHostAccount, ResourceID: input.TargetID,
		AccountID: input.TargetID, TargetName: input.TargetID,
		ClientIP: strings.TrimSpace(input.ClientIP), StartedAt: now, EndedAt: &now,
		State: "ended", Outcome: model.AuditOutcomeDenied,
		FailureCode:     strings.TrimSpace(input.FailureCode),
		FailureMessage:  truncateAuditFailure(input.FailureMessage),
		RecordingStatus: model.RecordingStatusNone,
	}
	if session.FailureCode == "" {
		session.FailureCode = "access_denied"
	}
	if err := s.repository.BeginRDPAuditSession(ctx, &session, nil); err != nil {
		return fmt.Errorf("record denied RDP audit session: %w", err)
	}
	return nil
}

// Finish uploads the immutable guacd recording and stores only its object key
// and integrity metadata in the database.
func (s *RDPRecordingService) Finish(
	ctx context.Context,
	handle *RDPAuditHandle,
	outcome string,
	failureCode string,
	failureMessage string,
) error {
	if handle == nil {
		return errors.New("RDP audit handle is required")
	}
	recordingStatus := model.RecordingStatusNone
	var recordingErr error
	if handle.Artifact != nil {
		recordingStatus, recordingErr = s.uploadRecording(ctx, handle)
	}
	finishErr := s.repository.FinishAuditSession(
		ctx, handle.Session.ID, outcome, failureCode,
		truncateAuditFailure(failureMessage), recordingStatus, s.now().UTC(),
	)
	driveCleanupErr := s.cleanupDriveSpool(handle)
	return errors.Join(recordingErr, finishErr, driveCleanupErr)
}

func (s *RDPRecordingService) uploadRecording(
	ctx context.Context,
	handle *RDPAuditHandle,
) (string, error) {
	artifact := handle.Artifact
	artifact.Status = model.RecordingStatusUploading
	if err := s.repository.UpdateAuditArtifact(ctx, artifact); err != nil {
		return model.RecordingStatusFailed, err
	}
	info, err := waitForRecording(ctx, handle.LocalPath, s.recordingWaitTimeout)
	if err != nil {
		return s.failArtifact(ctx, artifact, err)
	}
	file, err := openStableRecording(handle.LocalPath, info)
	if err != nil {
		return s.failArtifact(ctx, artifact, fmt.Errorf("open RDP recording: %w", err))
	}
	hash := sha256.New()
	_, putErr := s.objects.Put(
		ctx,
		artifact.ObjectKey,
		io.TeeReader(file, hash),
		info.Size(),
		artifact.ContentType,
	)
	closeErr := file.Close()
	if putErr != nil {
		return s.failArtifact(ctx, artifact, fmt.Errorf("upload RDP recording: %w", putErr))
	}
	if closeErr != nil {
		return s.failArtifact(ctx, artifact, fmt.Errorf("close RDP recording: %w", closeErr))
	}
	now := s.now().UTC()
	artifact.SizeBytes = info.Size()
	artifact.SHA256 = hex.EncodeToString(hash.Sum(nil))
	artifact.Status = model.RecordingStatusReady
	artifact.CompletedAt = &now
	artifact.ErrorMessage = ""
	if err := s.repository.UpdateAuditArtifact(ctx, artifact); err != nil {
		return model.RecordingStatusFailed, err
	}
	_ = os.Remove(handle.LocalPath)
	cleanupEmptySpool(filepath.Dir(handle.LocalPath))
	return model.RecordingStatusReady, nil
}

func (s *RDPRecordingService) failArtifact(
	ctx context.Context,
	artifact *model.AuditArtifact,
	err error,
) (string, error) {
	artifact.Status = model.RecordingStatusFailed
	artifact.ErrorMessage = truncateAuditFailure(err.Error())
	updateErr := s.repository.UpdateAuditArtifact(ctx, artifact)
	if updateErr != nil {
		return model.RecordingStatusFailed, errors.Join(err, updateErr)
	}
	return model.RecordingStatusFailed, err
}

func safeSessionDir(root, sessionID string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || sessionID == "." || sessionID == ".." ||
		strings.ContainsAny(sessionID, `/\`) || strings.ContainsRune(sessionID, '\x00') {
		return "", errors.New("RDP session id is not a safe path segment")
	}
	root, err := filepath.Abs(filepath.Clean(strings.TrimSpace(root)))
	if err != nil {
		return "", fmt.Errorf("resolve RDP spool root: %w", err)
	}
	dir := filepath.Join(root, sessionID)
	relative, err := filepath.Rel(root, dir)
	if err != nil || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("RDP spool path escapes configured root")
	}
	return dir, nil
}

func posixJoin(root, sessionID string) string {
	root = strings.ReplaceAll(strings.TrimSpace(root), `\`, "/")
	return path.Join(root, sessionID)
}

func waitForRecording(ctx context.Context, filename string, timeout time.Duration) (os.FileInfo, error) {
	const (
		pollInterval     = 50 * time.Millisecond
		quiescencePeriod = 300 * time.Millisecond
	)
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var lastSize int64 = -1
	var lastModified time.Time
	var stableSince time.Time
	for {
		info, err := os.Lstat(filename)
		if err == nil && info.Mode().IsRegular() && info.Size() > 0 {
			now := time.Now()
			if info.Size() == lastSize &&
				info.ModTime().Equal(lastModified) {
				if !stableSince.IsZero() &&
					now.Sub(stableSince) >= quiescencePeriod {
					return info, nil
				}
			} else {
				lastSize = info.Size()
				lastModified = info.ModTime()
				stableSince = now
			}
		} else {
			lastSize = -1
			lastModified = time.Time{}
			stableSince = time.Time{}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, errors.New("RDP recording was not produced by guacd")
		case <-ticker.C:
		}
	}
}

func openStableRecording(filename string, expected os.FileInfo) (*os.File, error) {
	if expected == nil ||
		!expected.Mode().IsRegular() ||
		expected.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("RDP recording is not a regular file")
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	actual, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !actual.Mode().IsRegular() || !os.SameFile(expected, actual) {
		_ = file.Close()
		return nil, errors.New("RDP recording changed before upload")
	}
	return file, nil
}

func cleanupEmptySpool(dir string) {
	if strings.TrimSpace(dir) != "" {
		_ = os.Remove(dir)
	}
}

func (s *RDPRecordingService) cleanupDriveSpool(handle *RDPAuditHandle) error {
	if strings.TrimSpace(handle.LocalDrivePath) == "" {
		return nil
	}
	dir, err := safeSessionDir(s.config.LocalDriveRoot, handle.Session.ID)
	if err != nil {
		return fmt.Errorf("resolve RDP drive cleanup path: %w", err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove RDP drive spool: %w", err)
	}
	return nil
}

func truncateAuditFailure(message string) string {
	message = strings.TrimSpace(message)
	if len(message) > 1024 {
		return message[:1024]
	}
	return message
}
