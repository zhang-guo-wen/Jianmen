package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"

	"jianmen/internal/access"
	"jianmen/internal/config"
	"jianmen/internal/model"
)

const (
	webTerminalPath        = "/api/web-terminal"
	defaultTerminalTerm    = "xterm-256color"
	defaultTerminalColumns = 80
	defaultTerminalRows    = 24
	maxTerminalDimension   = 1000
)

var webTerminalUpgrader = websocket.Upgrader{
	CheckOrigin: sameOriginOrNoOrigin,
}

type webTerminalOptions struct {
	TargetID string
	Term     string
	Columns  int
	Rows     int
}

type webTerminalResize struct {
	Columns int
	Rows    int
}

func (s *Server) handleWebTerminal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.authenticateWebTerminal(r) {
		writeErrorText(w, http.StatusUnauthorized, "missing or invalid bearer token")
		return
	}

	opts, err := webTerminalOptionsFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	target, err := s.resolveWebTerminalTarget(r.Context(), opts.TargetID)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	targetClient, err := dialWebTerminalTarget(target)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}

	conn, err := webTerminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		_ = targetClient.Close()
		s.logger.Warn("web terminal websocket upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	if err := serveWebTerminalSSHSession(r.Context(), conn, targetClient, opts, s.logger); err != nil && r.Context().Err() == nil {
		writeWebTerminalClose(conn, err)
		s.logger.Warn("web terminal session ended with error", "target", target.ID, "error", err)
	}
}

func (s *Server) authenticateWebTerminal(r *http.Request) bool {
	token := s.cfg.Admin.Token
	if token == "" {
		return true
	}
	if r.Header.Get("Authorization") == "Bearer "+token {
		return true
	}
	query := r.URL.Query()
	return query.Get("token") == token || query.Get("access_token") == token
}

func (s *Server) resolveWebTerminalTarget(ctx context.Context, targetID string) (config.Target, error) {
	user := model.User{
		Username:          "admin-web-terminal",
		RequestedTargetID: targetID,
	}
	target, err := s.store.DefaultTarget(ctx, user)
	if err != nil {
		return config.Target{}, err
	}
	return target, nil
}

func webTerminalOptionsFromRequest(r *http.Request) (webTerminalOptions, error) {
	query := r.URL.Query()
	columns, err := positiveIntQuery(query, "cols", defaultTerminalColumns)
	if err != nil {
		return webTerminalOptions{}, err
	}
	rows, err := positiveIntQuery(query, "rows", defaultTerminalRows)
	if err != nil {
		return webTerminalOptions{}, err
	}
	term := strings.TrimSpace(query.Get("term"))
	if term == "" {
		term = defaultTerminalTerm
	}
	return webTerminalOptions{
		TargetID: firstNonEmpty(strings.TrimSpace(query.Get("target_id")), strings.TrimSpace(query.Get("target"))),
		Term:     term,
		Columns:  columns,
		Rows:     rows,
	}, nil
}

func positiveIntQuery(query url.Values, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(query.Get(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value > maxTerminalDimension {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return value, nil
}

func dialWebTerminalTarget(target config.Target) (*ssh.Client, error) {
	clientConfig, err := access.ClientConfigForTarget(target)
	if err != nil {
		return nil, err
	}
	if clientConfig.Timeout == 0 {
		clientConfig.Timeout = 10 * time.Second
	}
	return ssh.Dial("tcp", target.Addr(), clientConfig)
}

func serveWebTerminalSSHSession(ctx context.Context, conn *websocket.Conn, targetClient *ssh.Client, opts webTerminalOptions, logger *slog.Logger) error {
	defer targetClient.Close()
	if logger == nil {
		logger = slog.Default()
	}

	session, err := targetClient.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return err
	}

	if err := session.RequestPty(opts.Term, opts.Rows, opts.Columns, ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		return err
	}
	if err := session.Shell(); err != nil {
		return err
	}

	writer := &webTerminalWriter{conn: conn}
	errCh := make(chan error, 4)
	go copyWebTerminalOutput(stdout, writer, errCh)
	go copyWebTerminalOutput(stderr, writer, errCh)
	go readWebTerminalInput(conn, stdin, session, errCh)
	go func() {
		errCh <- session.Wait()
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		if err == nil || isExpectedWebTerminalError(err) {
			return nil
		}
		return err
	}
}

type webTerminalWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (w *webTerminalWriter) writeBinary(payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteMessage(websocket.BinaryMessage, payload)
}

func copyWebTerminalOutput(src io.Reader, writer *webTerminalWriter, errCh chan<- error) {
	buf := make([]byte, 32*1024)
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			payload := append([]byte(nil), buf[:n]...)
			if err := writer.writeBinary(payload); err != nil {
				errCh <- err
				return
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				errCh <- nil
				return
			}
			errCh <- readErr
			return
		}
	}
}

func readWebTerminalInput(conn *websocket.Conn, stdin io.WriteCloser, session *ssh.Session, errCh chan<- error) {
	defer stdin.Close()
	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			errCh <- normalizeWebTerminalReadError(err)
			return
		}

		switch messageType {
		case websocket.TextMessage:
			resize, ok, err := parseWebTerminalResizeMessage(payload)
			if err != nil {
				errCh <- err
				return
			}
			if ok {
				if err := session.WindowChange(resize.Rows, resize.Columns); err != nil {
					errCh <- err
					return
				}
				continue
			}
			if _, err := stdin.Write(payload); err != nil {
				errCh <- err
				return
			}
		case websocket.BinaryMessage:
			if _, err := stdin.Write(payload); err != nil {
				errCh <- err
				return
			}
		}
	}
}

func parseWebTerminalResizeMessage(payload []byte) (webTerminalResize, bool, error) {
	var msg struct {
		Type string `json:"type"`
		Cols int    `json:"cols"`
		Rows int    `json:"rows"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		return webTerminalResize{}, false, nil
	}
	if msg.Type != "resize" {
		return webTerminalResize{}, false, nil
	}
	if msg.Cols <= 0 || msg.Rows <= 0 || msg.Cols > maxTerminalDimension || msg.Rows > maxTerminalDimension {
		return webTerminalResize{}, false, fmt.Errorf("invalid resize dimensions")
	}
	return webTerminalResize{Columns: msg.Cols, Rows: msg.Rows}, true, nil
}

func normalizeWebTerminalReadError(err error) error {
	if err == nil {
		return nil
	}
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
		return nil
	}
	return err
}

func isExpectedWebTerminalError(err error) bool {
	if err == nil || errors.Is(err, io.EOF) {
		return true
	}
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived) {
		return true
	}
	return strings.Contains(err.Error(), "use of closed network connection")
}

func writeWebTerminalClose(conn *websocket.Conn, err error) {
	message := err.Error()
	if len(message) > 120 {
		message = message[:120]
	}
	_ = conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseInternalServerErr, message),
		time.Now().Add(time.Second),
	)
}

func sameOriginOrNoOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}
