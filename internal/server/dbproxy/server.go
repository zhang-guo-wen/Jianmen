package dbproxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/model"
)

type Manager struct {
	proxies   []config.DatabaseProxyConfig
	replayDir string
	logger    *slog.Logger
}

type connectionRecorder struct {
	mu       sync.Mutex
	id       string
	protocol string
	file     *os.File
	seq      int64
}

type queryEvent struct {
	ConnectionID string         `json:"connection_id"`
	Seq          int64          `json:"seq"`
	Protocol     string         `json:"protocol"`
	SQL          string         `json:"sql"`
	Detail       map[string]any `json:"detail,omitempty"`
	ObservedAt   int64          `json:"observed_at"`
}

func NewManager(proxies []config.DatabaseProxyConfig, replayDir string, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	enabled := make([]config.DatabaseProxyConfig, 0, len(proxies))
	for _, proxy := range proxies {
		if proxy.Enabled {
			enabled = append(enabled, proxy)
		}
	}
	return &Manager{proxies: enabled, replayDir: replayDir, logger: logger}
}

func (m *Manager) Enabled() bool {
	return len(m.proxies) > 0
}

func (m *Manager) ListenAndServe(ctx context.Context) error {
	if len(m.proxies) == 0 {
		<-ctx.Done()
		return nil
	}

	errCh := make(chan error, len(m.proxies))
	var wg sync.WaitGroup
	for _, proxy := range m.proxies {
		proxy := proxy
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := m.serveProxy(ctx, proxy); err != nil && ctx.Err() == nil {
				errCh <- err
			}
		}()
	}

	go func() {
		wg.Wait()
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		wg.Wait()
		return nil
	case err, ok := <-errCh:
		if !ok {
			return nil
		}
		return err
	}
}

func (m *Manager) serveProxy(ctx context.Context, proxy config.DatabaseProxyConfig) error {
	listener, err := net.Listen("tcp", proxy.ListenAddr)
	if err != nil {
		return err
	}
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
		m.copyClientToUpstream(upstream, client, observer, accountAuth)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		done <- struct{}{}
	}()

	select {
	case <-ctx.Done():
	case <-done:
	}
}

func (m *Manager) copyClientToUpstream(dst io.Writer, src io.Reader, observer queryObserver, accountAuth *accountAuthState) {
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
				m.logger.Info("database account auth accepted", "user", accountAuth.user)
			}
			observer.ObserveClientBytes(data)
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
	meta := map[string]any{
		"id":            id,
		"name":          proxy.Name,
		"protocol":      proxy.Protocol,
		"client_addr":   clientAddr,
		"upstream_addr": proxy.UpstreamAddr,
		"started_at":    startedAt.Format(time.RFC3339Nano),
	}
	if raw, err := json.MarshalIndent(meta, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0o644)
	}
	file, err := os.OpenFile(filepath.Join(dir, "queries.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	return &connectionRecorder{
		id:       id,
		protocol: proxy.Protocol,
		file:     file,
	}, nil
}

func (r *connectionRecorder) RecordQuery(sql string, detail map[string]any) {
	if r == nil || strings.TrimSpace(sql) == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file == nil {
		return
	}
	r.seq++
	event := queryEvent{
		ConnectionID: r.id,
		Seq:          r.seq,
		Protocol:     r.protocol,
		SQL:          sql,
		Detail:       detail,
		ObservedAt:   time.Now().UTC().UnixMilli(),
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return
	}
	_, _ = r.file.Write(append(raw, '\n'))
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
