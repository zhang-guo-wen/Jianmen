package dbproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/model"
	rbaccheck "jianmen/internal/rbac"

	"gorm.io/gorm"
)

type Manager struct {
	applyMu           sync.Mutex
	mu                sync.Mutex
	desired           []config.DatabaseProxyConfig
	running           map[string]*runningProxy
	rootCtx           context.Context
	errCh             chan<- error
	proxies           []config.DatabaseProxyConfig
	replayDir         string
	logger            *slog.Logger
	permissionChecker permissionChecker
}

type runningProxy struct {
	proxy  config.DatabaseProxyConfig
	cancel context.CancelFunc
	done   chan struct{}
}

type permissionChecker interface {
	HasPermission(userID, action, resourceType, resourceID string) (bool, error)
}

type connectionRecorder struct {
	mu       sync.Mutex
	id       string
	protocol string
	metaPath string
	meta     DBConnectionMeta
	file     *os.File
	seq      int64
	policy   *sqlPolicy
}

func NewManager(proxies []config.DatabaseProxyConfig, replayDir string, logger *slog.Logger, dbs ...*gorm.DB) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	enabled := enabledDatabaseProxies(proxies)
	var checker permissionChecker
	if len(dbs) > 0 && dbs[0] != nil {
		checker = rbaccheck.NewChecker(dbs[0])
	}
	return &Manager{
		desired:           enabled,
		running:           make(map[string]*runningProxy),
		proxies:           enabled,
		replayDir:         replayDir,
		logger:            logger,
		permissionChecker: checker,
	}
}

func (m *Manager) Enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.desired) > 0 || len(m.running) > 0
}

func (m *Manager) ListenAndServe(ctx context.Context) error {
	errCh := make(chan error, 1)
	m.mu.Lock()
	if m.rootCtx != nil {
		m.mu.Unlock()
		return errors.New("database proxy manager is already running")
	}
	m.rootCtx = ctx
	m.errCh = errCh
	desired := append([]config.DatabaseProxyConfig(nil), m.desired...)
	m.mu.Unlock()

	if err := m.Apply(desired); err != nil {
		m.stopAll()
		return err
	}

	select {
	case <-ctx.Done():
		m.stopAll()
		return nil
	case err := <-errCh:
		m.stopAll()
		return err
	}
}

func (m *Manager) Apply(proxies []config.DatabaseProxyConfig) error {
	enabled := enabledDatabaseProxies(proxies)

	m.applyMu.Lock()
	defer m.applyMu.Unlock()

	m.mu.Lock()
	m.desired = enabled
	m.proxies = enabled
	ctx := m.rootCtx
	if ctx == nil || ctx.Err() != nil {
		m.mu.Unlock()
		return nil
	}

	desiredByName := make(map[string]config.DatabaseProxyConfig, len(enabled))
	for _, proxy := range enabled {
		desiredByName[proxy.Name] = proxy
	}

	toStop := make([]*runningProxy, 0)
	toStart := make([]config.DatabaseProxyConfig, 0)
	for name, running := range m.running {
		next, keep := desiredByName[name]
		if !keep || !reflect.DeepEqual(running.proxy, next) {
			toStop = append(toStop, running)
			delete(m.running, name)
		}
	}
	for name, proxy := range desiredByName {
		if _, ok := m.running[name]; !ok {
			toStart = append(toStart, proxy)
		}
	}
	m.mu.Unlock()

	stopRunningProxies(toStop)

	started := make(map[string]*runningProxy, len(toStart))
	for _, proxy := range toStart {
		running, err := m.startProxy(ctx, proxy)
		if err != nil {
			stopRunningProxies(mapValues(started))
			return err
		}
		started[proxy.Name] = running
	}

	m.mu.Lock()
	for name, running := range started {
		m.running[name] = running
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) startProxy(parent context.Context, proxy config.DatabaseProxyConfig) (*runningProxy, error) {
	listener, err := net.Listen("tcp", proxy.ListenAddr)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	running := &runningProxy{
		proxy:  proxy,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go func() {
		defer close(running.done)
		if err := m.serveProxy(ctx, proxy, listener); err != nil && ctx.Err() == nil {
			m.reportProxyError(err)
		}
	}()
	return running, nil
}

func (m *Manager) serveProxy(ctx context.Context, proxy config.DatabaseProxyConfig, listener net.Listener) error {
	defer listener.Close()

	m.logger.Info("database proxy listening",
		"name", proxy.Name,
		"protocol", proxy.Protocol,
		"listen", proxy.ListenAddr,
		"upstream", proxy.UpstreamAddr,
	)

	var wg sync.WaitGroup
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "closed") {
				wg.Wait()
				return nil
			}
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.handleConn(ctx, proxy, conn)
		}()
	}
}

func (m *Manager) reportProxyError(err error) {
	m.mu.Lock()
	errCh := m.errCh
	m.mu.Unlock()
	if errCh == nil {
		return
	}
	select {
	case errCh <- err:
	default:
	}
}

func (m *Manager) stopAll() {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()

	m.mu.Lock()
	toStop := make([]*runningProxy, 0, len(m.running))
	for _, running := range m.running {
		toStop = append(toStop, running)
	}
	m.running = make(map[string]*runningProxy)
	m.rootCtx = nil
	m.errCh = nil
	m.mu.Unlock()

	stopRunningProxies(toStop)
}

func stopRunningProxies(proxies []*runningProxy) {
	for _, proxy := range proxies {
		proxy.cancel()
	}
	for _, proxy := range proxies {
		<-proxy.done
	}
}

func mapValues[K comparable, V any](values map[K]V) []V {
	out := make([]V, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func enabledDatabaseProxies(proxies []config.DatabaseProxyConfig) []config.DatabaseProxyConfig {
	enabled := make([]config.DatabaseProxyConfig, 0, len(proxies))
	for _, proxy := range proxies {
		if proxy.Enabled {
			enabled = append(enabled, proxy)
		}
	}
	return enabled
}

func (m *Manager) handleConn(ctx context.Context, proxy config.DatabaseProxyConfig, client net.Conn) {
	defer client.Close()

	upstream, err := net.DialTimeout("tcp", proxy.UpstreamAddr, 10*time.Second)
	if err != nil {
		m.logger.Warn("database upstream connect failed", "name", proxy.Name, "upstream", proxy.UpstreamAddr, "error", err)
		return
	}
	defer upstream.Close()

	recorder, err := m.newConnectionRecorder(proxy, client.RemoteAddr().String())
	if err != nil {
		m.logger.Warn("database recorder init failed", "name", proxy.Name, "error", err)
	} else {
		defer recorder.Close()
	}
	observer := newQueryObserver(proxy.Protocol, recorder)
	accountAuth, err := newAccountAuth(proxy.Protocol, proxy.AllowedUsers)
	if err != nil {
		m.logger.Warn("database account auth init failed", "name", proxy.Name, "error", err)
		return
	}

	m.logger.Info("database connection started",
		"name", proxy.Name,
		"protocol", proxy.Protocol,
		"client", client.RemoteAddr().String(),
		"upstream", proxy.UpstreamAddr,
	)
	defer m.logger.Info("database connection ended", "name", proxy.Name, "client", client.RemoteAddr().String())

	done := make(chan struct{}, 2)
	go func() {
		m.copyClientToUpstream(proxy, upstream, client, client, observer, accountAuth, recorder)
		done <- struct{}{}
	}()
	go func() {
		m.copyUpstreamToClient(client, upstream, observer)
		done <- struct{}{}
	}()

	select {
	case <-ctx.Done():
	case <-done:
	}
}

func (m *Manager) copyClientToUpstream(proxy config.DatabaseProxyConfig, dst io.Writer, src io.Reader, client io.Writer, observer queryObserver, accountAuth *accountAuthState, recorder *connectionRecorder) {
	buf := make([]byte, 32*1024)
	var pendingAuthBytes []byte
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			if accountAuth.Enabled() && !accountAuth.Ready() {
				pendingAuthBytes = append(pendingAuthBytes, data...)
				ready, err := accountAuth.ObserveClientBytes(data)
				if err != nil {
					m.logger.Warn("database account auth rejected", "error", err)
					return
				}
				if !ready {
					continue
				}
				data = pendingAuthBytes
				pendingAuthBytes = nil
				observation := accountAuth.Observation()
				if recorder != nil {
					recorder.UpdateAuth(observation, accountAuth.Enforced())
				}
				if accountAuth.Enforced() {
					resourceID := rbaccheck.DatabaseAccountResourceID(proxy.Name, proxy.ListenAddr, observation.User)
					if err := m.authorizeDatabaseConnect(proxy, observation, resourceID); err != nil {
						m.logger.Warn("database rbac connect rejected",
							"name", proxy.Name,
							"user", observation.User,
							"resource_type", model.ResourceTypeDatabaseAccount,
							"resource_id", resourceID,
							"error", err,
						)
						return
					}
				}
				m.logger.Info("database account auth accepted", "user", observation.User, "observation", observation.Observation)
			}
			if decision := observer.ObserveClientBytes(data); decision != nil && !decision.Allowed {
				if response := observer.ErrorResponse(*decision); len(response) > 0 {
					_, _ = client.Write(response)
				}
				m.logger.Warn("database query rejected by policy", "error_code", decision.ErrorCode, "error", decision.ErrorMessage)
				return
			}
			if _, err := dst.Write(data); err != nil {
				return
			}
		}
		if readErr != nil {
			return
		}
	}
}

func (m *Manager) authorizeDatabaseConnect(proxy config.DatabaseProxyConfig, observation loginObservation, resourceID string) error {
	if m == nil || m.permissionChecker == nil {
		return nil
	}
	userID := strings.TrimSpace(observation.User)
	if userID == "" {
		return errors.New("database username is not visible for rbac connect check")
	}
	if resourceID == "" {
		resourceID = rbaccheck.DatabaseAccountResourceID(proxy.Name, proxy.ListenAddr, userID)
	}
	allowed, err := m.permissionChecker.HasPermission(userID, rbaccheck.ActionDBConnect, model.ResourceTypeDatabaseAccount, resourceID)
	if err != nil {
		return fmt.Errorf("database rbac connect check failed: %w", err)
	}
	if !allowed {
		return fmt.Errorf("database user %q is not permitted to connect to %s:%s", userID, model.ResourceTypeDatabaseAccount, resourceID)
	}
	return nil
}

func (m *Manager) copyUpstreamToClient(dst io.Writer, src io.Reader, observer queryObserver) {
	buf := make([]byte, 32*1024)
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			observer.ObserveServerBytes(data)
			if _, err := dst.Write(data); err != nil {
				return
			}
		}
		if readErr != nil {
			return
		}
	}
}

func (m *Manager) newConnectionRecorder(proxy config.DatabaseProxyConfig, clientAddr string) (*connectionRecorder, error) {
	id := model.NewID()
	dir := filepath.Join(m.replayDir, "db", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	startedAt := time.Now().UTC()
	meta := DBConnectionMeta{
		ID:                   id,
		Name:                 proxy.Name,
		Protocol:             proxy.Protocol,
		ClientAddr:           clientAddr,
		UpstreamAddr:         proxy.UpstreamAddr,
		StartedAt:            startedAt.Format(time.RFC3339Nano),
		AllowedUsersEnforced: len(proxy.AllowedUsers) > 0,
		QueryPolicy: DBQueryPolicyMeta{
			ReadOnly:          proxy.QueryPolicy.ReadOnly,
			DeniedQueryKinds:  append([]string(nil), proxy.QueryPolicy.DeniedQueryKinds...),
			DeniedSQLPatterns: append([]string(nil), proxy.QueryPolicy.DeniedSQLPatterns...),
			MaxQueryBytes:     proxy.QueryPolicy.MaxQueryBytes,
		},
	}
	file, err := os.OpenFile(filepath.Join(dir, "queries.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	recorder := &connectionRecorder{
		id:       id,
		protocol: proxy.Protocol,
		metaPath: filepath.Join(dir, "meta.json"),
		meta:     meta,
		file:     file,
		policy:   newSQLPolicy(proxy.QueryPolicy),
	}
	if err := recorder.writeMetaLocked(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return recorder, nil
}

func (r *connectionRecorder) UpdateAuth(observation loginObservation, enforced bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.meta.AuthUser = observation.User
	r.meta.Database = observation.Database
	r.meta.ApplicationName = observation.ApplicationName
	r.meta.MySQLConnectAttrs = observation.ConnectAttrs
	r.meta.AuthObservation = observation.Observation
	r.meta.AllowedUsersEnforced = enforced
	_ = r.writeMetaLocked()
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
	decision := r.policy.Evaluate(sql)
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
	if r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}
