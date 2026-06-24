package admin

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/recording"
)

func TestParseWebTerminalResizeMessage(t *testing.T) {
	resize, ok, err := parseWebTerminalResizeMessage([]byte(`{"type":"resize","cols":120,"rows":40}`))
	if err != nil {
		t.Fatalf("parse resize: %v", err)
	}
	if !ok {
		t.Fatal("expected resize message")
	}
	if resize.Columns != 120 || resize.Rows != 40 {
		t.Fatalf("resize = %+v, want cols=120 rows=40", resize)
	}
}

func TestParseWebTerminalResizeMessageIgnoresTerminalText(t *testing.T) {
	_, ok, err := parseWebTerminalResizeMessage([]byte("ls -la\n"))
	if err != nil {
		t.Fatalf("parse terminal text: %v", err)
	}
	if ok {
		t.Fatal("terminal input was parsed as resize")
	}
}

func TestHandleWebTerminalRejectsMissingToken(t *testing.T) {
	server := &Server{
		cfg: &config.Config{
			Admin: config.AdminConfig{Token: "secret"},
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	req := httptest.NewRequest(http.MethodGet, webTerminalPath+"?target_id=web01", nil)
	rec := httptest.NewRecorder()

	server.handleWebTerminal(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestCopyWebTerminalOutputRecordsTerminalCast(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	session := model.NewSession(model.User{Username: "web"}, "target-1", "target", "127.0.0.1")
	recorder, err := recording.NewSessionRecorder(t.TempDir(), session, false, false, logger)
	if err != nil {
		t.Fatalf("new recorder: %v", err)
	}

	writer := &bufferWebTerminalWriter{}
	resultCh := make(chan webTerminalResult, 1)
	copyWebTerminalOutput("stdout", strings.NewReader("remote output\n"), writer, recorder, resultCh)
	if err := recorder.Close(); err != nil {
		t.Fatalf("close recorder: %v", err)
	}

	if got := writer.String(); got != "remote output\n" {
		t.Fatalf("browser output = %q, want remote output", got)
	}
	raw, err := os.ReadFile(filepath.Join(recorder.Dir(), "terminal.cast"))
	if err != nil {
		t.Fatalf("read terminal.cast: %v", err)
	}
	if cast := string(raw); !strings.Contains(cast, `"o","remote output\n"`) {
		t.Fatalf("terminal.cast did not record stdout: %q", cast)
	}
}

type bufferWebTerminalWriter struct {
	bytes.Buffer
}

func (w *bufferWebTerminalWriter) writeBinary(payload []byte) error {
	_, err := w.Write(payload)
	return err
}
