package guacd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"sort"
	"sync"
	"time"
)

const readyProbeInterval = 50 * time.Millisecond
const ownershipProbeTimeout = 200 * time.Millisecond

// Manager owns one managed guacd process.
type Manager struct {
	logger          *slog.Logger
	command         string
	readyAddress    string
	shutdownTimeout time.Duration
	cmd             *exec.Cmd
	stdout          *logWriter
	stderr          *logWriter
	done            chan struct{}

	mu       sync.Mutex
	stopping bool
	waitErr  error

	closeMu sync.Mutex
}

// Start launches guacd and waits until ReadyAddress accepts TCP connections.
// The process is stopped when ctx is canceled. Runtime exits are reported by
// Wait.
func Start(ctx context.Context, cfg Config, logger *slog.Logger) (*Manager, error) {
	if ctx == nil {
		return nil, fmt.Errorf("guacd context is required")
	}
	normalized, err := cfg.normalized()
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}

	manager := newManager(normalized, logger)
	if !normalized.Enabled {
		close(manager.done)
		logger.Debug("managed guacd is disabled")
		return manager, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("start managed guacd: %w", err)
	}
	if err := ensureReadyAddressAvailable(ctx, normalized.ReadyAddress); err != nil {
		return nil, err
	}
	if err := manager.launch(normalized); err != nil {
		return nil, err
	}
	go manager.stopWhenCanceled(ctx)

	if err := manager.waitUntilReady(ctx, normalized.StartupTimeout); err != nil {
		stopErr := manager.Close()
		return nil, errors.Join(fmt.Errorf("start managed guacd: %w", err), stopErr)
	}
	logger.Info(
		"managed guacd is ready",
		"command", normalized.Command,
		"address", normalized.ReadyAddress,
	)
	return manager, nil
}

func ensureReadyAddressAvailable(ctx context.Context, address string) error {
	probeCtx, cancel := context.WithTimeout(ctx, ownershipProbeTimeout)
	defer cancel()

	listener, err := (&net.ListenConfig{}).Listen(probeCtx, "tcp", address)
	if err == nil {
		if closeErr := listener.Close(); closeErr != nil {
			return fmt.Errorf("release guacd ready address probe: %w", closeErr)
		}
		return nil
	}
	if ctx.Err() != nil {
		return fmt.Errorf("probe guacd ready address: %w", ctx.Err())
	}
	return fmt.Errorf("guacd ready address already in use: %s: %w", address, err)
}

func newManager(cfg Config, logger *slog.Logger) *Manager {
	return &Manager{
		logger:          logger,
		command:         cfg.Command,
		readyAddress:    cfg.ReadyAddress,
		shutdownTimeout: cfg.ShutdownTimeout,
		done:            make(chan struct{}),
	}
}

func (m *Manager) launch(cfg Config) error {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Dir = cfg.WorkDir
	cmd.Env = mergedEnvironment(cfg.Env)
	cmd.WaitDelay = cfg.ShutdownTimeout
	configureProcess(cmd)

	m.stdout = &logWriter{logger: m.logger, level: slog.LevelInfo, stream: "stdout"}
	m.stderr = &logWriter{logger: m.logger, level: slog.LevelInfo, stream: "stderr"}
	cmd.Stdout = m.stdout
	cmd.Stderr = m.stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start guacd command %q: %w", cfg.Command, err)
	}
	m.cmd = cmd
	m.logger.Info("managed guacd started", "command", cfg.Command, "pid", cmd.Process.Pid)
	go m.reap()
	return nil
}

func (m *Manager) reap() {
	err := m.cmd.Wait()
	m.mu.Lock()
	if !m.stopping {
		if err == nil {
			err = fmt.Errorf("guacd exited unexpectedly")
		} else {
			err = fmt.Errorf("guacd exited unexpectedly: %w", err)
		}
		m.waitErr = err
	}
	waitErr := m.waitErr
	m.mu.Unlock()

	m.stdout.flush()
	m.stderr.flush()
	if waitErr != nil {
		m.logger.Error("managed guacd exited", "command", m.command, "error", waitErr)
	} else {
		m.logger.Info("managed guacd stopped", "command", m.command)
	}
	close(m.done)
}

func (m *Manager) stopWhenCanceled(ctx context.Context) {
	select {
	case <-ctx.Done():
		m.logger.Info("stopping managed guacd after context cancellation", "error", ctx.Err())
		if err := m.Close(); err != nil {
			m.logger.Error("failed to stop managed guacd", "error", err)
		}
	case <-m.done:
	}
}

func (m *Manager) waitUntilReady(ctx context.Context, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(readyProbeInterval)
	defer ticker.Stop()

	var lastErr error
	for {
		connection, err := (&net.Dialer{Timeout: readyProbeInterval}).DialContext(
			ctx,
			"tcp",
			m.readyAddress,
		)
		if err == nil {
			_ = connection.Close()
			select {
			case <-m.done:
				if waitErr := m.waitResult(); waitErr != nil {
					return waitErr
				}
				return fmt.Errorf("guacd stopped while becoming ready")
			default:
				return nil
			}
		}
		lastErr = err

		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for guacd readiness: %w", ctx.Err())
		case <-m.done:
			if ctx.Err() != nil {
				return fmt.Errorf("wait for guacd readiness: %w", ctx.Err())
			}
			if waitErr := m.waitResult(); waitErr != nil {
				return waitErr
			}
			return fmt.Errorf("guacd stopped before becoming ready")
		case <-timer.C:
			return fmt.Errorf(
				"timed out waiting for guacd at %s: %w",
				m.readyAddress,
				lastErr,
			)
		case <-ticker.C:
		}
	}
}

// Wait blocks until the managed process has been reaped. It returns an error
// when the process exits without Close or context cancellation.
func (m *Manager) Wait() error {
	<-m.done
	return m.waitResult()
}

func (m *Manager) waitResult() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.waitErr
}

// Close stops the process and waits for it to be reaped. It is safe to call
// more than once. A failed attempt does not prevent a later retry.
func (m *Manager) Close() error {
	m.closeMu.Lock()
	defer m.closeMu.Unlock()
	return m.stop()
}

func (m *Manager) stop() error {
	m.mu.Lock()
	m.stopping = true
	cmd := m.cmd
	m.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	select {
	case <-m.done:
		return nil
	default:
	}

	if err := terminateProcess(cmd.Process); err != nil && !errors.Is(err, os.ErrProcessDone) {
		m.logger.Warn("graceful guacd stop failed; forcing stop", "error", err)
		if killErr := forceKillProcess(cmd.Process); killErr != nil &&
			!errors.Is(killErr, os.ErrProcessDone) {
			return errors.Join(
				fmt.Errorf("gracefully stop guacd: %w", err),
				fmt.Errorf("force stop guacd: %w", killErr),
			)
		}
	}
	if m.waitForDone(m.shutdownTimeout) {
		return nil
	}

	m.logger.Warn("guacd did not stop before deadline; forcing stop")
	if err := forceKillProcess(cmd.Process); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("force stop guacd: %w", err)
	}
	if !m.waitForDone(m.shutdownTimeout) {
		return fmt.Errorf("guacd process was not reaped after forced stop")
	}
	return nil
}

func (m *Manager) waitForDone(timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-m.done:
		return true
	case <-timer.C:
		return false
	}
}

func mergedEnvironment(overrides map[string]string) []string {
	environment := os.Environ()
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		environment = append(environment, key+"="+overrides[key])
	}
	return environment
}
