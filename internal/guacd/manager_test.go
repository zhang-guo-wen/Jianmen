package guacd

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

const helperEnvironmentKey = "GO_WANT_GUACD_HELPER_PROCESS"

func TestStartWaitsForReadinessAndCloseIsIdempotent(t *testing.T) {
	address := unusedLoopbackAddress(t)
	manager, err := Start(context.Background(), helperConfig("serve", address), discardLogger())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatalf("ready address is not accepting connections: %v", err)
	}
	_ = connection.Close()

	if err := manager.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if err := manager.Wait(); err != nil {
		t.Fatalf("Wait() after Close() error = %v", err)
	}
}

func TestContextCancellationStopsProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	manager, err := Start(ctx, helperConfig("serve", unusedLoopbackAddress(t)), discardLogger())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	cancel()
	if err := waitWithTimeout(t, manager); err != nil {
		t.Fatalf("Wait() after cancellation error = %v", err)
	}
}

func TestWaitReportsUnexpectedRuntimeExit(t *testing.T) {
	cfg := helperConfig("serve-then-exit", unusedLoopbackAddress(t))
	manager, err := Start(context.Background(), cfg, discardLogger())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	err = waitWithTimeout(t, manager)
	if err == nil || !strings.Contains(err.Error(), "exited unexpectedly") {
		t.Fatalf("Wait() error = %v, want unexpected exit", err)
	}
}

func TestStartReportsExitBeforeReady(t *testing.T) {
	cfg := helperConfig("exit", unusedLoopbackAddress(t))
	_, err := Start(context.Background(), cfg, discardLogger())
	if err == nil || !strings.Contains(err.Error(), "exited unexpectedly") {
		t.Fatalf("Start() error = %v, want unexpected exit", err)
	}
}

func TestStartTimesOutAndReapsProcess(t *testing.T) {
	cfg := helperConfig("sleep", unusedLoopbackAddress(t))
	cfg.StartupTimeout = 150 * time.Millisecond
	cfg.ShutdownTimeout = time.Second

	startedAt := time.Now()
	_, err := Start(context.Background(), cfg, discardLogger())
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Start() error = %v, want readiness timeout", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 3*time.Second {
		t.Fatalf("Start() took %v, process was not stopped promptly", elapsed)
	}
}

func TestStartDisabledDoesNotRequireCommand(t *testing.T) {
	manager, err := Start(context.Background(), Config{}, discardLogger())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := manager.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestStartRejectsNonLoopbackAddressBeforeLaunch(t *testing.T) {
	cfg := Config{
		Enabled:      true,
		Command:      "command-that-must-not-run",
		ReadyAddress: "0.0.0.0:4822",
	}
	_, err := Start(context.Background(), cfg, discardLogger())
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("Start() error = %v, want loopback validation error", err)
	}
}

func TestStartDoesNotLaunchWithCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg := Config{
		Enabled:      true,
		Command:      "command-that-must-not-run",
		ReadyAddress: "127.0.0.1:4822",
	}
	_, err := Start(ctx, cfg, discardLogger())
	if err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("Start() error = %v, want context cancellation", err)
	}
}

func TestStartRejectsReadyAddressAlreadyInUse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	cfg := Config{
		Enabled:      true,
		Command:      "command-that-must-not-run",
		ReadyAddress: listener.Addr().String(),
	}
	_, err = Start(context.Background(), cfg, discardLogger())
	if err == nil || !strings.Contains(err.Error(), "ready address already in use") {
		t.Fatalf("Start() error = %v, want address-in-use error", err)
	}
}

func TestStartPassesEnvironmentAndWorkingDirectory(t *testing.T) {
	address := unusedLoopbackAddress(t)
	workDir := t.TempDir()
	cfg := helperConfig("report", address)
	cfg.WorkDir = workDir
	cfg.Env["GUACD_HELPER_VALUE"] = "configured"

	manager, err := Start(context.Background(), cfg, discardLogger())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = manager.Close() }()

	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatalf("dial helper: %v", err)
	}
	defer connection.Close()
	reader := bufio.NewReader(connection)
	environmentValue, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read helper environment report: %v", err)
	}
	workingDirectory, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read helper working directory report: %v", err)
	}
	if strings.TrimSpace(environmentValue) != "configured" {
		t.Errorf("environment report = %q", environmentValue)
	}
	if strings.TrimSpace(workingDirectory) != workDir {
		t.Errorf("working directory report = %q, want %q", workingDirectory, workDir)
	}
}

func TestValidateReadyAddress(t *testing.T) {
	tests := []struct {
		name    string
		address string
		wantErr bool
	}{
		{name: "IPv4", address: "127.0.0.1:4822"},
		{name: "IPv6", address: "[::1]:4822"},
		{name: "localhost", address: "localhost:4822"},
		{name: "unspecified IPv4", address: "0.0.0.0:4822", wantErr: true},
		{name: "unspecified IPv6", address: "[::]:4822", wantErr: true},
		{name: "remote", address: "192.0.2.1:4822", wantErr: true},
		{name: "hostname", address: "guacd.internal:4822", wantErr: true},
		{name: "missing host", address: ":4822", wantErr: true},
		{name: "invalid port", address: "127.0.0.1:70000", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateReadyAddress(test.address)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateReadyAddress(%q) error = %v, wantErr %v", test.address, err, test.wantErr)
			}
		})
	}
}

func TestChildOutputUsesSlog(t *testing.T) {
	var output synchronizedBuffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	cfg := helperConfig("output-then-exit", unusedLoopbackAddress(t))

	_, err := Start(context.Background(), cfg, logger)
	if err == nil {
		t.Fatal("Start() error = nil, want helper exit")
	}
	logged := output.String()
	if !strings.Contains(logged, "helper stdout") || !strings.Contains(logged, "helper stderr") {
		t.Fatalf("logged output = %q", logged)
	}
	assertOutputLogLevel(t, logged, "helper stdout", "stdout", "INFO")
	assertOutputLogLevel(t, logged, "helper stderr", "stderr", "INFO")
}

func TestWaitFlushesChildOutputWithoutTrailingNewline(t *testing.T) {
	var output synchronizedBuffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	cfg := helperConfig("partial-output-then-exit", unusedLoopbackAddress(t))

	_, err := Start(context.Background(), cfg, logger)
	if err == nil {
		t.Fatal("Start() error = nil, want helper exit")
	}
	logged := output.String()
	if !strings.Contains(logged, "partial helper stdout") {
		t.Fatalf("logged output missing partial stdout: %q", logged)
	}
	if !strings.Contains(logged, "partial helper stderr") {
		t.Fatalf("logged output missing partial stderr: %q", logged)
	}
}

func assertOutputLogLevel(t *testing.T, logged, message, stream, level string) {
	t.Helper()
	for _, line := range strings.Split(logged, "\n") {
		if !strings.Contains(line, message) {
			continue
		}
		if !strings.Contains(line, "level="+level) || !strings.Contains(line, "stream="+stream) {
			t.Fatalf("log line = %q, want level=%s stream=%s", line, level, stream)
		}
		return
	}
	t.Fatalf("log output does not contain %q: %q", message, logged)
}

type synchronizedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *synchronizedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func helperConfig(mode, address string) Config {
	return Config{
		Enabled:         true,
		Command:         os.Args[0],
		Args:            []string{"-test.run=TestGuacdHelperProcess", "--", mode, address},
		Env:             map[string]string{helperEnvironmentKey: "1"},
		ReadyAddress:    address,
		StartupTimeout:  3 * time.Second,
		ShutdownTimeout: time.Second,
	}
}

func unusedLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve loopback address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release loopback address: %v", err)
	}
	return address
}

func waitWithTimeout(t *testing.T, manager *Manager) error {
	t.Helper()
	result := make(chan error, 1)
	go func() {
		result <- manager.Wait()
	}()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for managed process")
		return nil
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

func TestGuacdHelperProcess(t *testing.T) {
	if os.Getenv(helperEnvironmentKey) != "1" {
		return
	}
	mode, address, ok := helperArguments(os.Args)
	if !ok {
		fmt.Fprintln(os.Stderr, "missing helper arguments")
		os.Exit(2)
	}
	if err := runHelper(mode, address); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

func helperArguments(arguments []string) (string, string, bool) {
	for index, argument := range arguments {
		if argument == "--" && len(arguments) > index+2 {
			return arguments[index+1], arguments[index+2], true
		}
	}
	return "", "", false
}

func runHelper(mode, address string) error {
	switch mode {
	case "exit":
		os.Exit(23)
	case "output-then-exit":
		fmt.Fprintln(os.Stdout, "helper stdout")
		fmt.Fprintln(os.Stderr, "helper stderr")
		os.Exit(23)
	case "partial-output-then-exit":
		fmt.Fprint(os.Stdout, "partial helper stdout")
		fmt.Fprint(os.Stderr, "partial helper stderr")
		os.Exit(23)
	case "sleep":
		time.Sleep(time.Hour)
		return nil
	}

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()
	if mode == "serve" {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return acceptErr
			}
			_ = connection.Close()
		}
	}

	connection, err := listener.Accept()
	if err != nil {
		return err
	}
	switch mode {
	case "serve-then-exit":
		_ = connection.Close()
		os.Exit(23)
		return nil
	case "report":
		workingDirectory, err := os.Getwd()
		if err != nil {
			return err
		}
		for {
			_, _ = fmt.Fprintf(
				connection,
				"%s\n%s\n",
				os.Getenv("GUACD_HELPER_VALUE"),
				workingDirectory,
			)
			_ = connection.Close()
			connection, err = listener.Accept()
			if err != nil {
				return err
			}
		}
	default:
		_ = connection.Close()
		return fmt.Errorf("unknown helper mode %q", mode)
	}
}
