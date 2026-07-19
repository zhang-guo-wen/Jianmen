package dbproxy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jianmen/internal/model"
)

func (g *Gateway) newRecorder(
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
		if err := g.db.First(&user, "id = ?", conn.userID).Error; err == nil {
			authUser = user.Username
		}
	}

	meta := DBConnectionMeta{
		ID:           id,
		Name:         conn.accountName,
		Protocol:     conn.protocol,
		UpstreamAddr: conn.upstreamAddr,
		StartedAt:    startedAt.Format(time.RFC3339Nano),
		AccountName:  conn.accountUser,
		InstanceName: conn.instanceName,
		AuthUser:     authUser,
	}
	dir := filepath.Join(g.replayDir, "db", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create database replay directory: %w", err)
	}
	metaPath := filepath.Join(dir, "meta.json")
	file, err := os.OpenFile(filepath.Join(dir, "queries.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("create database query audit file: %w", err)
	}

	recorder := &connectionRecorder{
		id:                    id,
		protocol:              conn.protocol,
		maxClientMessageBytes: normalizeMaxClientMessageBytes(g.cfg.MaxClientMessageBytes),
		metaPath:              metaPath,
		meta:                  meta,
		file:                  file,
		startedAt:             startedAt,
		audit:                 g.audit,
		auditSessionID:        auditSessionID,
		onFatal:               onFatal,
		logger:                g.logger,
	}
	if err := recorder.writeMetaLocked(); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write database replay metadata: %w", err)
	}
	return recorder, nil
}
