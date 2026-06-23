package admin

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"jianmen/internal/config"
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
