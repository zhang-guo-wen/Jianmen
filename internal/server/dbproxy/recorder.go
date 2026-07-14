package dbproxy

import (
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"jianmen/internal/model"
)

func copyClientToUpstream(client net.Conn, upstream net.Conn, observer queryObserver) {
	defer func() { recover() }()
	buf := make([]byte, 32*1024)
	for {
		n, err := client.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			if decision := observer.ObserveClientBytes(data); decision != nil && !decision.Allowed {
				return
			}
			if _, werr := upstream.Write(data); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func copyUpstreamToClient(client net.Conn, upstream net.Conn, observer queryObserver) {
	defer func() { recover() }()
	buf := make([]byte, 32*1024)
	for {
		n, err := upstream.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			observer.ObserveServerBytes(data)
			if _, werr := client.Write(data); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

type connectionRecorder struct {
	mu             sync.Mutex
	id             string
	protocol       string
	metaPath       string
	meta           DBConnectionMeta
	file           *os.File
	seq            int64
	startedAt      time.Time
	audit          auditWriter
	auditSessionID string
}

func (g *Gateway) newRecorder(conn *gatewayConn, auditSessionID string) (*connectionRecorder, error) {
	id := model.NewID()
	startedAt := time.Now().UTC()
	// 查找操作者用户名
	authUser := conn.userID
	if g.db != nil {
		var u model.User
		if err := g.db.First(&u, "id = ?", conn.userID).Error; err == nil {
			authUser = u.Username
		}
	}

	// 文件录制是可选的：即使文件创建失败，DB 审计仍然需要工作。
	var file *os.File
	var metaPath string
	meta := DBConnectionMeta{
		ID:           id,
		Name:         conn.accountName,
		Protocol:     conn.protocol,
		ClientAddr:   "",
		UpstreamAddr: conn.upstreamAddr,
		StartedAt:    startedAt.Format(time.RFC3339Nano),
		AccountName:  conn.accountUser,
		InstanceName: conn.instanceName,
		AuthUser:     authUser,
	}

	dir := filepath.Join(g.replayDir, "db", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		g.logger.Warn("db gateway cannot create replay directory, queries file will be skipped", "dir", dir, "error", err)
	} else {
		metaPath = filepath.Join(dir, "meta.json")
		f, err := os.OpenFile(filepath.Join(dir, "queries.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			g.logger.Warn("db gateway cannot create queries file, queries file will be skipped", "path", filepath.Join(dir, "queries.jsonl"), "error", err)
		} else {
			file = f
		}
	}

	recorder := &connectionRecorder{
		id:             id,
		protocol:       conn.protocol,
		metaPath:       metaPath,
		meta:           meta,
		file:           file,
		startedAt:      startedAt,
		audit:          g.audit,
		auditSessionID: auditSessionID,
	}
	// 元数据写入失败不影响审计，仅记录日志
	if err := recorder.writeMetaLocked(); err != nil {
		g.logger.Warn("db gateway cannot write meta file", "path", metaPath, "error", err)
	}
	return recorder, nil
}

func (r *connectionRecorder) StartQuery(sql string, detail map[string]any) (queryRecord, queryDecision) {
	if r == nil || strings.TrimSpace(sql) == "" {
		return queryRecord{}, allowQuery()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	startedAt := time.Now().UTC()
	queryKind := classifyQueryKind(sql)
	record := queryRecord{
		seq:       r.seq,
		protocol:  r.protocol,
		sql:       sql,
		queryKind: queryKind,
		detail:    detail,
		startedAt: startedAt,
	}
	decision := allowQuery()
	startDetail := mergeDetails(detail, map[string]any{"query_kind": queryKind})
	r.writeQueryEventLocked(DBQueryEvent{
		Type:         queryEventTypeStarted,
		ConnectionID: r.id,
		Seq:          record.seq,
		Protocol:     r.protocol,
		SQL:          sql,
		QueryKind:    queryKind,
		Detail:       startDetail,
		StartedAt:    startedAt.UnixMilli(),
		Status:       queryStatusUnknown,
	})
	if !decision.Allowed {
		r.writeFinishLocked(record, queryFinish{
			Status:       decision.Status,
			ErrorCode:    decision.ErrorCode,
			ErrorMessage: decision.ErrorMessage,
			Detail:       decision.Detail,
		})
	}
	return record, decision
}

func (r *connectionRecorder) FinishQuery(record queryRecord, finish queryFinish) {
	if r == nil || record.seq == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writeFinishLocked(record, finish)
}

func (r *connectionRecorder) writeFinishLocked(record queryRecord, finish queryFinish) {
	if finish.Status == "" {
		finish.Status = queryStatusUnknown
	}
	completedAt := time.Now().UTC()
	r.writeQueryEventLocked(DBQueryEvent{
		Type:         queryEventTypeFinished,
		ConnectionID: r.id,
		Seq:          record.seq,
		Protocol:     record.protocol,
		SQL:          record.sql,
		QueryKind:    record.queryKind,
		Detail:       mergeDetails(record.detail, finish.Detail),
		StartedAt:    record.startedAt.UnixMilli(),
		CompletedAt:  completedAt.UnixMilli(),
		DurationMs:   completedAt.Sub(record.startedAt).Milliseconds(),
		Status:       finish.Status,
		ErrorCode:    finish.ErrorCode,
		ErrorMessage: finish.ErrorMessage,
		RowsAffected: finish.RowsAffected,
		Rows:         finish.Rows,
	})
	if r.audit != nil && r.auditSessionID != "" {
		if err := r.audit.CreateAuditDBQuery(&model.AuditDBQuery{
			AuditSessionID: r.auditSessionID,
			Timestamp:      completedAt,
			SQLText:        record.sql,
			QueryKind:      record.queryKind,
			DurationMs:     completedAt.Sub(record.startedAt).Milliseconds(),
		}); err != nil {
			// 审计记录写入失败不应中断业务，但需要记录日志便于排查
			slog.Warn("db gateway failed to write audit db query", "session_id", r.auditSessionID, "error", err)
		}
	}
}

func (r *connectionRecorder) writeQueryEventLocked(event DBQueryEvent) {
	if r.file == nil {
		return
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return
	}
	_, _ = r.file.Write(append(raw, '\n'))
}

func (r *connectionRecorder) writeMetaLocked() error {
	if r.metaPath == "" {
		return nil
	}
	raw, err := json.MarshalIndent(r.meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.metaPath, raw, 0o644)
}

func (r *connectionRecorder) RecordQuery(sql string, detail map[string]any) {
	record, decision := r.StartQuery(sql, detail)
	if decision.Allowed {
		r.FinishQuery(record, queryFinish{Status: queryStatusUnknown})
	}
}

func (r *connectionRecorder) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// 写入连接结束时间和时长
	endedAt := time.Now().UTC()
	r.meta.EndedAt = endedAt.Format(time.RFC3339Nano)
	r.meta.DurationMs = endedAt.Sub(r.startedAt).Milliseconds()
	r.writeMetaLocked()
	if r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}
