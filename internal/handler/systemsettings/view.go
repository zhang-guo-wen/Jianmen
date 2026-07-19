package systemsettings

import (
	"errors"
	"time"

	"jianmen/internal/service"
)

type settingsValues struct {
	WebRDPEnabled                 bool  `json:"web_rdp_enabled"`
	WebRDPConnectTimeoutSeconds   int   `json:"web_rdp_connect_timeout_seconds"`
	WebRDPAllowUnrecorded         bool  `json:"web_rdp_allow_unrecorded"`
	RecordingEnabled              bool  `json:"recording_enabled"`
	RecordingRecordInput          bool  `json:"recording_record_input"`
	RecordingRecordCommands       bool  `json:"recording_record_commands"`
	RecordingRetentionDays        int   `json:"recording_retention_days"`
	RecordingMaxReplayBytes       int64 `json:"recording_max_replay_bytes"`
	RecordingCleanupBatchSize     int   `json:"recording_cleanup_batch_size"`
	DatabaseMaxClientMessageBytes int   `json:"database_max_client_message_bytes"`
}

type updateRequest struct {
	Settings         *settingsValuesRequest `json:"settings"`
	ExpectedRevision *int64                 `json:"expected_revision"`
	ConfirmRisk      bool                   `json:"confirm_risk"`
}

type settingsValuesRequest struct {
	WebRDPEnabled                 *bool  `json:"web_rdp_enabled"`
	WebRDPConnectTimeoutSeconds   *int   `json:"web_rdp_connect_timeout_seconds"`
	WebRDPAllowUnrecorded         *bool  `json:"web_rdp_allow_unrecorded"`
	RecordingEnabled              *bool  `json:"recording_enabled"`
	RecordingRecordInput          *bool  `json:"recording_record_input"`
	RecordingRecordCommands       *bool  `json:"recording_record_commands"`
	RecordingRetentionDays        *int   `json:"recording_retention_days"`
	RecordingMaxReplayBytes       *int64 `json:"recording_max_replay_bytes"`
	RecordingCleanupBatchSize     *int   `json:"recording_cleanup_batch_size"`
	DatabaseMaxClientMessageBytes *int   `json:"database_max_client_message_bytes"`
}

type stateResponse struct {
	Desired           settingsValues `json:"desired"`
	Effective         settingsValues `json:"effective"`
	Revision          int64          `json:"revision"`
	EffectiveRevision int64          `json:"effective_revision"`
	PendingRestart    bool           `json:"pending_restart"`
	UpdatedByID       string         `json:"updated_by_id,omitempty"`
	UpdatedByUsername string         `json:"updated_by_username,omitempty"`
	UpdatedAt         *time.Time     `json:"updated_at,omitempty"`
	AppliedAt         *time.Time     `json:"applied_at,omitempty"`
	Infrastructure    infrastructure `json:"infrastructure"`
}

type infrastructure struct {
	Guacd         guacdInfrastructure         `json:"guacd"`
	Directories   directoryInfrastructure     `json:"directories"`
	ObjectStorage objectStorageInfrastructure `json:"object_storage"`
}

type guacdInfrastructure struct {
	Address string `json:"address"`
}

type directoryInfrastructure struct {
	SpoolDir           string `json:"spool_dir"`
	GuacdRecordingRoot string `json:"guacd_recording_root"`
	LocalDriveRoot     string `json:"local_drive_root"`
	GuacdDriveRoot     string `json:"guacd_drive_root"`
	ReplayDir          string `json:"replay_dir"`
}

type objectStorageInfrastructure struct {
	Provider                  string `json:"provider"`
	LocalDir                  string `json:"local_dir"`
	Endpoint                  string `json:"endpoint"`
	Bucket                    string `json:"bucket"`
	Region                    string `json:"region"`
	Prefix                    string `json:"prefix"`
	Secure                    bool   `json:"secure"`
	PathStyle                 bool   `json:"path_style"`
	AutoCreateBucket          bool   `json:"auto_create_bucket"`
	AccessKeyIDConfigured     bool   `json:"access_key_id_configured"`
	SecretAccessKeyConfigured bool   `json:"secret_access_key_configured"`
	SessionTokenConfigured    bool   `json:"session_token_configured"`
	CredentialsConfigured     bool   `json:"credentials_configured"`
}

type diagnosticResponse struct {
	OK        bool   `json:"ok"`
	Message   string `json:"message"`
	LatencyMS int64  `json:"latency_ms"`
}

type revisionResponse struct {
	ID            string         `json:"id"`
	Revision      int64          `json:"revision"`
	Snapshot      settingsValues `json:"snapshot"`
	ChangedFields []string       `json:"changed_fields"`
	UpdatedByID   string         `json:"updated_by_id,omitempty"`
	ActorUsername string         `json:"actor_username,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

type revisionListResponse struct {
	Items []revisionResponse `json:"items"`
}

func (v settingsValuesRequest) toService() (service.SystemSettings, error) {
	if v.WebRDPEnabled == nil || v.WebRDPConnectTimeoutSeconds == nil ||
		v.WebRDPAllowUnrecorded == nil || v.RecordingEnabled == nil ||
		v.RecordingRecordInput == nil || v.RecordingRecordCommands == nil ||
		v.RecordingRetentionDays == nil || v.RecordingMaxReplayBytes == nil ||
		v.RecordingCleanupBatchSize == nil ||
		v.DatabaseMaxClientMessageBytes == nil {
		return service.SystemSettings{}, errors.New("all system settings fields are required")
	}
	return service.SystemSettings{
		WebRDPEnabled:                 *v.WebRDPEnabled,
		WebRDPConnectTimeoutSeconds:   *v.WebRDPConnectTimeoutSeconds,
		WebRDPAllowUnrecorded:         *v.WebRDPAllowUnrecorded,
		RecordingEnabled:              *v.RecordingEnabled,
		RecordingRecordInput:          *v.RecordingRecordInput,
		RecordingRecordCommands:       *v.RecordingRecordCommands,
		RecordingRetentionDays:        *v.RecordingRetentionDays,
		RecordingMaxReplayBytes:       *v.RecordingMaxReplayBytes,
		RecordingCleanupBatchSize:     *v.RecordingCleanupBatchSize,
		DatabaseMaxClientMessageBytes: *v.DatabaseMaxClientMessageBytes,
	}, nil
}

func mapValues(value service.SystemSettings) settingsValues {
	return settingsValues{
		WebRDPEnabled:                 value.WebRDPEnabled,
		WebRDPConnectTimeoutSeconds:   value.WebRDPConnectTimeoutSeconds,
		WebRDPAllowUnrecorded:         value.WebRDPAllowUnrecorded,
		RecordingEnabled:              value.RecordingEnabled,
		RecordingRecordInput:          value.RecordingRecordInput,
		RecordingRecordCommands:       value.RecordingRecordCommands,
		RecordingRetentionDays:        value.RecordingRetentionDays,
		RecordingMaxReplayBytes:       value.RecordingMaxReplayBytes,
		RecordingCleanupBatchSize:     value.RecordingCleanupBatchSize,
		DatabaseMaxClientMessageBytes: value.DatabaseMaxClientMessageBytes,
	}
}

func mapRevision(revision service.SystemSettingsRevision) revisionResponse {
	return revisionResponse{
		ID: revision.ID, Revision: revision.Revision,
		Snapshot: mapValues(revision.Snapshot), ChangedFields: revision.ChangedFields,
		UpdatedByID: revision.UpdatedByID, ActorUsername: revision.UpdatedByUsername,
		CreatedAt: revision.CreatedAt,
	}
}

func mapInfrastructure(
	value service.SystemSettingsRuntimeInfrastructure,
) infrastructure {
	storage := value.ObjectStorage
	credentialsConfigured := storage.AccessKeyIDConfigured && storage.SecretAccessKeyConfigured
	return infrastructure{
		Guacd: guacdInfrastructure{Address: value.GuacdAddress},
		Directories: directoryInfrastructure{
			SpoolDir: value.SpoolDir, GuacdRecordingRoot: value.GuacdRecordingRoot,
			LocalDriveRoot: value.LocalDriveRoot, GuacdDriveRoot: value.GuacdDriveRoot,
			ReplayDir: value.ReplayDir,
		},
		ObjectStorage: objectStorageInfrastructure{
			Provider: storage.Provider, LocalDir: storage.LocalDir,
			Endpoint: storage.Endpoint, Bucket: storage.Bucket, Region: storage.Region,
			Prefix: storage.Prefix, Secure: storage.Secure, PathStyle: storage.PathStyle,
			AutoCreateBucket:          storage.AutoCreateBucket,
			AccessKeyIDConfigured:     storage.AccessKeyIDConfigured,
			SecretAccessKeyConfigured: storage.SecretAccessKeyConfigured,
			SessionTokenConfigured:    storage.SessionTokenConfigured,
			CredentialsConfigured:     credentialsConfigured,
		},
	}
}
