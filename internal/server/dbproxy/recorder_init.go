package dbproxy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jianmen/internal/model"
)

func (g *Gateway) newRecorder(
	ctx context.Context,
	conn *gatewayConn,
	auditSessionID string,
	onFatal func(error),
) (*connectionRecorder, error) {
	if onFatal == nil {
		return nil, errors.New("database audit fatal error handler is required")
	}
	id := strings.TrimSpace(auditSessionID)
	if id == "" {
		id = model.NewID()
	}
	startedAt := time.Now().UTC()
	authUser := conn.userID
	if g.db != nil {
		var user model.User
		if err := g.db.WithContext(ctx).First(&user, "id = ? AND active_marker = ?", conn.userID, model.ActiveMarkerValue).Error; err == nil {
			authUser = user.Username
		}
	}

	meta := DBConnectionMeta{
		ID:                    id,
		Name:                  conn.accountName,
		Protocol:              conn.protocol,
		UpstreamAddr:          conn.upstreamAddr,
		StartedAt:             startedAt.Format(time.RFC3339Nano),
		AccountName:           conn.accountUser,
		InstanceName:          conn.instanceName,
		AuthUser:              authUser,
		QueryDataFile:         databaseAuditDataFileName,
		AuditRedactionEnabled: g.cfg.AuditRedactionEnabled,
		AuditPreviewBytes:     g.cfg.EffectiveAuditPreviewBytes(),
	}
	dir := filepath.Join(g.replayDir, "db", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create database replay directory: %w", err)
	}
	metaPath := filepath.Join(dir, "meta.json")
	file, err := os.OpenFile(filepath.Join(dir, "queries.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create database query audit file: %w", err)
	}
	artifactFile, err := os.OpenFile(
		filepath.Join(dir, databaseAuditDataFileName),
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		0o600,
	)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("create database full audit file: %w", err)
	}

	recorder := &connectionRecorder{
		ctx:                   ctx,
		id:                    id,
		protocol:              conn.protocol,
		maxClientMessageBytes: normalizeMaxClientMessageBytes(g.cfg.MaxClientMessageBytes),
		auditPreviewBytes:     g.cfg.EffectiveAuditPreviewBytes(),
		auditRedactionEnabled: g.cfg.AuditRedactionEnabled,
		metaPath:              metaPath,
		meta:                  meta,
		file:                  file,
		artifact:              newDatabaseAuditArtifactFile(artifactFile, g.cfg.AuditRedactionEnabled),
		artifactRequired:      true,
		startedAt:             startedAt,
		audit:                 g.audit,
		auditSessionID:        auditSessionID,
		onFatal:               onFatal,
		logger:                g.logger,
	}
	if err := recorder.writeMetaLocked(); err != nil {
		_ = file.Close()
		_ = recorder.artifact.close()
		return nil, fmt.Errorf("write database replay metadata: %w", err)
	}
	return recorder, nil
}
