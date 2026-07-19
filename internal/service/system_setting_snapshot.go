package service

import (
	"encoding/json"
	"fmt"

	"jianmen/internal/config"
)

type systemSettingsSnapshot struct {
	DatabaseGatewayMode           string `json:"database_gateway_mode"`
	WebRDPEnabled                 bool   `json:"web_rdp_enabled"`
	WebRDPConnectTimeoutSeconds   int    `json:"web_rdp_connect_timeout_seconds"`
	WebRDPAllowUnrecorded         bool   `json:"web_rdp_allow_unrecorded"`
	RecordingEnabled              bool   `json:"recording_enabled"`
	RecordingRecordInput          bool   `json:"recording_record_input"`
	RecordingRecordCommands       bool   `json:"recording_record_commands"`
	RecordingRetentionDays        int    `json:"recording_retention_days"`
	RecordingMaxReplayBytes       int64  `json:"recording_max_replay_bytes"`
	RecordingCleanupBatchSize     int    `json:"recording_cleanup_batch_size"`
	DatabaseMaxClientMessageBytes int    `json:"database_max_client_message_bytes"`
}

func marshalSystemSettings(settings SystemSettings) (string, error) {
	encoded, err := json.Marshal(snapshotFromSystemSettings(settings))
	if err != nil {
		return "", fmt.Errorf("marshal system settings snapshot: %w", err)
	}
	return string(encoded), nil
}

func unmarshalSystemSettings(encoded string) (SystemSettings, error) {
	var snapshot systemSettingsSnapshot
	if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
		return SystemSettings{}, err
	}
	if snapshot.DatabaseGatewayMode == "" {
		snapshot.DatabaseGatewayMode = config.DatabaseGatewayModeUnified
	}
	return snapshot.systemSettings(), nil
}

func snapshotFromSystemSettings(settings SystemSettings) systemSettingsSnapshot {
	return systemSettingsSnapshot{
		DatabaseGatewayMode:           settings.DatabaseGatewayMode,
		WebRDPEnabled:                 settings.WebRDPEnabled,
		WebRDPConnectTimeoutSeconds:   settings.WebRDPConnectTimeoutSeconds,
		WebRDPAllowUnrecorded:         settings.WebRDPAllowUnrecorded,
		RecordingEnabled:              settings.RecordingEnabled,
		RecordingRecordInput:          settings.RecordingRecordInput,
		RecordingRecordCommands:       settings.RecordingRecordCommands,
		RecordingRetentionDays:        settings.RecordingRetentionDays,
		RecordingMaxReplayBytes:       settings.RecordingMaxReplayBytes,
		RecordingCleanupBatchSize:     settings.RecordingCleanupBatchSize,
		DatabaseMaxClientMessageBytes: settings.DatabaseMaxClientMessageBytes,
	}
}

func (snapshot systemSettingsSnapshot) systemSettings() SystemSettings {
	databaseMaxClientMessageBytes := snapshot.DatabaseMaxClientMessageBytes
	if databaseMaxClientMessageBytes == 0 {
		databaseMaxClientMessageBytes = defaultDatabaseMaxClientMessageBytes
	}
	return SystemSettings{
		DatabaseGatewayMode:           snapshot.DatabaseGatewayMode,
		WebRDPEnabled:                 snapshot.WebRDPEnabled,
		WebRDPConnectTimeoutSeconds:   snapshot.WebRDPConnectTimeoutSeconds,
		WebRDPAllowUnrecorded:         snapshot.WebRDPAllowUnrecorded,
		RecordingEnabled:              snapshot.RecordingEnabled,
		RecordingRecordInput:          snapshot.RecordingRecordInput,
		RecordingRecordCommands:       snapshot.RecordingRecordCommands,
		RecordingRetentionDays:        snapshot.RecordingRetentionDays,
		RecordingMaxReplayBytes:       snapshot.RecordingMaxReplayBytes,
		RecordingCleanupBatchSize:     snapshot.RecordingCleanupBatchSize,
		DatabaseMaxClientMessageBytes: databaseMaxClientMessageBytes,
	}
}
