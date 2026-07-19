package dbproxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"jianmen/internal/model"
)

const (
	observerErrorWriteTimeout = 250 * time.Millisecond
	relayPendingDrainTimeout  = 5 * time.Second
)

var (
	errRelayObserverPanic = errors.New("database proxy observer panic")
	errRelayDrainComplete = errors.New("database proxy response drain complete")
)

func copyClientToUpstream(client net.Conn, upstream net.Conn, observer queryObserver) {
	relay := newRelayCoordinator(client, upstream)
	if decision := relay.copyClientToUpstream(observer); decision != nil {
		relay.writeObserverError(observer, *decision)
	}
	relay.close()
}

func copyUpstreamToClient(client net.Conn, upstream net.Conn, observer queryObserver) {
	relay := newRelayCoordinator(client, upstream)
	if decision := relay.copyUpstreamToClient(observer); decision != nil {
		relay.writeObserverError(observer, *decision)
	}
	relay.close()
}

type relayCoordinator struct {
	client        net.Conn
	upstream      net.Conn
	clientWriteMu sync.Mutex
	terminal      bool
	closeOnce     sync.Once
	drainClient   atomic.Bool
}

type relaySide uint8

const (
	relayClientSide relaySide = iota
	relayServerSide
)

type relayResult struct {
	side     relaySide
	decision *queryDecision
	err      error
}

func newRelayCoordinator(client, upstream net.Conn) *relayCoordinator {
	return &relayCoordinator{client: client, upstream: upstream}
}

func relayGatewayConnection(client, upstream net.Conn, observer queryObserver) {
	relayGatewayConnectionWithDrainTimeout(client, upstream, observer, relayPendingDrainTimeout)
}

func relayGatewayConnectionWithDrainTimeout(
	client,
	upstream net.Conn,
	observer queryObserver,
	drainTimeout time.Duration,
) {
	// The relay owns the concurrency boundary. Normalize every observer here so
	// callers cannot accidentally expose a stateful protocol observer to both
	// copy goroutines without synchronization.
	observer = serializeRelayObserver(observer)
	relay := newRelayCoordinator(client, upstream)
	results := make(chan relayResult, 2)
	go func() {
		results <- relay.runClientToUpstream(observer)
	}()
	go func() {
		results <- relay.runUpstreamToClient(observer)
	}()

	first := <-results
	if first.side == relayClientSide && errors.Is(first.err, io.EOF) {
		relay.drainClient.Store(true)
		if !observerHasPending(observer) {
			_ = client.SetWriteDeadline(time.Now().Add(observerErrorWriteTimeout))
			_ = upstream.SetReadDeadline(time.Now())
			second := <-results
			relay.handleRelayTermination(observer, second)
			relay.close()
			return
		}
		relay.waitForPendingDrain(observer, results, drainTimeout)
		return
	}
	relay.handleRelayTermination(observer, first)
	relay.close()
	<-results
	abortObserverIfPending(observer, observerErrorRelay)
}

func (r *relayCoordinator) waitForPendingDrain(
	observer queryObserver,
	results <-chan relayResult,
	timeout time.Duration,
) {
	if timeout <= 0 {
		timeout = relayPendingDrainTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case second := <-results:
		r.handleRelayTermination(observer, second)
		r.close()
	case <-timer.C:
		abortObserverIfPending(observer, observerErrorDrainTimeout)
		decision := newObserverRelayDecision()
		decision.ErrorCode = observerErrorDrainTimeout
		r.writeObserverError(observer, *decision)
		r.close()
		<-results
	}
}

func (r *relayCoordinator) copyClientToUpstream(observer queryObserver) (decision *queryDecision) {
	result := r.runClientToUpstream(observer)
	if result.decision != nil {
		if errors.Is(result.err, errRelayObserverPanic) {
			abortObserverIfPending(observer, observerErrorRelay)
		}
		return result.decision
	}
	if result.err != nil {
		hadPending := observerHasPending(observer)
		abortObserverIfPending(observer, observerErrorRelay)
		if hadPending || errors.Is(result.err, errRelayObserverPanic) {
			return newObserverRelayDecision()
		}
	}
	return nil
}

func (r *relayCoordinator) runClientToUpstream(observer queryObserver) (result relayResult) {
	result.side = relayClientSide
	defer func() {
		if recover() != nil {
			result.err = errRelayObserverPanic
			result.decision = newObserverRelayDecision()
		}
	}()
	buf := make([]byte, 32*1024)
	for {
		n, err := r.client.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			if relayObserver, ok := observer.(relayClientQueryObserver); ok {
				forward, observed := relayObserver.ObserveClientRelayBytes(data)
				if len(forward) > 0 {
					if werr := writeRelayBytes(r.upstream, forward); werr != nil {
						result.err = werr
						return result
					}
				}
				if observed != nil && !observed.Allowed {
					result.decision = observed
					return result
				}
			} else {
				if observed := observer.ObserveClientBytes(data); observed != nil && !observed.Allowed {
					result.decision = observed
					return result
				}
				if werr := writeRelayBytes(r.upstream, data); werr != nil {
					result.err = werr
					return result
				}
			}
		}
		if err != nil {
			result.err = err
			return result
		}
	}
}

func (r *relayCoordinator) copyUpstreamToClient(observer queryObserver) (decision *queryDecision) {
	result := r.runUpstreamToClient(observer)
	if result.decision != nil {
		if errors.Is(result.err, errRelayObserverPanic) {
			abortObserverIfPending(observer, observerErrorRelay)
		}
		return result.decision
	}
	if result.err != nil && !errors.Is(result.err, errRelayDrainComplete) {
		hadPending := observerHasPending(observer)
		abortObserverIfPending(observer, observerErrorRelay)
		if hadPending || errors.Is(result.err, errRelayObserverPanic) {
			return newObserverRelayDecision()
		}
	}
	return nil
}

func (r *relayCoordinator) runUpstreamToClient(observer queryObserver) (result relayResult) {
	result.side = relayServerSide
	defer func() {
		if recover() != nil {
			result.err = errRelayObserverPanic
			result.decision = newObserverRelayDecision()
		}
	}()
	buf := make([]byte, 32*1024)
	for {
		n, err := r.upstream.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			if relayObserver, ok := observer.(relayServerQueryObserver); ok {
				forward, observed := relayObserver.ObserveServerRelayBytes(data)
				if len(forward) > 0 {
					r.armDrainWriteDeadline()
					if werr := r.writeClient(forward); werr != nil {
						result.err = werr
						return result
					}
				}
				if observed != nil && !observed.Allowed {
					result.decision = observed
					return result
				}
			} else {
				if observed := observer.ObserveServerBytes(data); observed != nil && !observed.Allowed {
					result.decision = observed
					return result
				}
				r.armDrainWriteDeadline()
				if werr := r.writeClient(data); werr != nil {
					result.err = werr
					return result
				}
			}
			if r.drainClient.Load() && !observerHasPending(observer) {
				result.err = errRelayDrainComplete
				return result
			}
		}
		if err != nil {
			result.err = err
			return result
		}
	}
}

func (r *relayCoordinator) armDrainWriteDeadline() {
	if r.drainClient.Load() {
		_ = r.client.SetWriteDeadline(time.Now().Add(observerErrorWriteTimeout))
	}
}

func (r *relayCoordinator) handleRelayTermination(observer queryObserver, result relayResult) {
	if result.decision != nil {
		if errors.Is(result.err, errRelayObserverPanic) {
			abortObserverIfPending(observer, observerErrorRelay)
		}
		r.writeObserverError(observer, *result.decision)
		return
	}
	if result.err == nil || errors.Is(result.err, errRelayDrainComplete) {
		return
	}
	if observerHasPending(observer) {
		abortObserverIfPending(observer, observerErrorRelay)
		r.writeObserverError(observer, *newObserverRelayDecision())
	}
}

func observerHasPending(observer queryObserver) (pending bool) {
	defer func() {
		if recover() != nil {
			slog.Error("database proxy observer failed while checking pending work")
			pending = true
		}
	}()
	lifecycle, ok := observer.(queryObserverLifecycle)
	return ok && lifecycle.HasPending()
}

func abortObserverIfPending(observer queryObserver, code string) {
	defer func() {
		if recover() != nil {
			slog.Error("database proxy observer failed while aborting pending work")
		}
	}()
	lifecycle, ok := observer.(queryObserverLifecycle)
	if ok && lifecycle.HasPending() {
		lifecycle.Abort(code)
	}
}

func (r *relayCoordinator) writeObserverError(observer queryObserver, decision queryDecision) {
	response := observer.ErrorResponse(decision)
	deadline := time.Now().Add(observerErrorWriteTimeout)
	for !r.clientWriteMu.TryLock() {
		if !time.Now().Before(deadline) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	defer r.clientWriteMu.Unlock()
	r.terminal = true
	if len(response) == 0 {
		return
	}
	if err := r.client.SetWriteDeadline(deadline); err != nil {
		return
	}
	_ = writeRelayBytes(r.client, response)
}

func (r *relayCoordinator) writeClient(data []byte) error {
	r.clientWriteMu.Lock()
	defer r.clientWriteMu.Unlock()
	if r.terminal {
		return net.ErrClosed
	}
	return writeRelayBytes(r.client, data)
}

func (r *relayCoordinator) close() {
	r.closeOnce.Do(func() {
		_ = r.client.Close()
		_ = r.upstream.Close()
	})
}

func writeRelayBytes(connection net.Conn, data []byte) error {
	for len(data) > 0 {
		written, err := connection.Write(data)
		if written > 0 {
			data = data[written:]
		}
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrNoProgress
		}
	}
	return nil
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
	onFatal        func(error)
	fatalOnce      sync.Once
	logger         *slog.Logger
}

func (r *connectionRecorder) StartQuery(sql string, detail map[string]any) (queryRecord, queryDecision) {
	if r == nil || strings.TrimSpace(sql) == "" {
		return queryRecord{}, allowQuery()
	}
	if r.protocol == "mysql" || r.protocol == "postgres" {
		sql = redactDatabaseSQL(sql)
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
	if err := r.writeQueryEventLocked(DBQueryEvent{
		Type:         queryEventTypeStarted,
		ConnectionID: r.id,
		Seq:          record.seq,
		Protocol:     r.protocol,
		SQL:          sql,
		QueryKind:    queryKind,
		Detail:       startDetail,
		StartedAt:    startedAt.UnixMilli(),
		Status:       queryStatusUnknown,
	}); err != nil {
		r.reportFatal(fmt.Errorf("write database query start event: %w", err))
		return record, *newObserverFatalDecision(observerErrorAuditFailure, "database audit recording failed")
	}
	if !decision.Allowed {
		r.writeFinishLocked(record, queryFinish{
			Status:       decision.Status,
			ErrorCode:    decision.ErrorCode,
			ErrorMessage: "database proxy policy denied query",
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
	if err := r.writeQueryEventLocked(DBQueryEvent{
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
	}); err != nil {
		r.reportFatal(fmt.Errorf("write database query finish event: %w", err))
		return
	}
	if r.audit != nil && r.auditSessionID != "" {
		if err := r.audit.CreateAuditDBQuery(&model.AuditDBQuery{
			AuditSessionID: r.auditSessionID,
			Timestamp:      completedAt,
			SQLText:        record.sql,
			QueryKind:      record.queryKind,
			DurationMs:     completedAt.Sub(record.startedAt).Milliseconds(),
		}); err != nil {
			r.reportFatal(fmt.Errorf("write database query audit record: %w", err))
		}
	}
}

func (r *connectionRecorder) writeQueryEventLocked(event DBQueryEvent) error {
	if r.file == nil {
		return errors.New("database query audit file is unavailable")
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := r.file.Write(append(raw, '\n')); err != nil {
		return err
	}
	return nil
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
	metaErr := r.writeMetaLocked()
	if r.file == nil {
		if metaErr != nil {
			r.reportFatal(fmt.Errorf("write database replay end metadata: %w", metaErr))
		}
		return metaErr
	}
	closeErr := r.file.Close()
	r.file = nil
	if metaErr != nil {
		r.reportFatal(fmt.Errorf("write database replay end metadata: %w", metaErr))
	}
	if closeErr != nil {
		r.reportFatal(fmt.Errorf("close database query audit file: %w", closeErr))
	}
	return errors.Join(metaErr, closeErr)
}

func (r *connectionRecorder) reportFatal(err error) {
	if r == nil || err == nil {
		return
	}
	if r.logger != nil {
		r.logger.Error("database audit recording failed; terminating session", "session", r.auditSessionID, "error", err)
	}
	r.fatalOnce.Do(func() {
		if r.onFatal != nil {
			r.onFatal(err)
		}
	})
}
