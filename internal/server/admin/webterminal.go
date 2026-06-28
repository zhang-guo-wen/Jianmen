package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"

	"jianmen/internal/model"
	"jianmen/internal/recording"
	"jianmen/internal/store"
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
	if target.Disabled {
		writeErrorText(w, http.StatusForbidden, "target is disabled or unavailable")
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

	recorder := s.newWebTerminalRecorder(r, target)
	if recorder != nil {
		defer recorder.Close()
	}

	if err := serveWebTerminalSSHSession(r.Context(), conn, targetClient, opts, recorder, s.logger); err != nil && r.Context().Err() == nil {
		writeWebTerminalClose(conn, err)
		s.logger.Warn("web terminal session ended with error", "target", target.ID, "error", err)
	}
}

func (s *Server) authenticateWebTerminal(r *http.Request) bool {
	// WebTerminal 使用与 Admin API 相同的 per-user token 认证
	auth := r.Header.Get("Authorization")
	token := strings.TrimPrefix(auth, "Bearer ")
	if token == "" || token == auth {
		// 也支持 query string 传 token
		query := r.URL.Query()
		token = query.Get("token")
		if token == "" {
			token = query.Get("access_token")
		}
	}
	if token == "" {
		return false
	}
	if s.db == nil {
		return true // 无 DB 时允许通过
	}
	hash := sha256.Sum256([]byte(token))
	hashStr := hex.EncodeToString(hash[:])
	var user model.User
	return s.db.Where("token_hash = ?", hashStr).First(&user).Error == nil
}

func (s *Server) resolveWebTerminalTarget(ctx context.Context, targetID string) (store.TargetConfig, error) {
	user := model.User{
		Username:          "admin-web-terminal",
		RequestedTargetID: targetID,
	}
	target, err := s.store.DefaultTarget(ctx, user)
	if err != nil {
		return store.TargetConfig{}, err
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

func dialWebTerminalTarget(target store.TargetConfig) (*ssh.Client, error) {
	clientConfig, err := store.ClientConfigForTarget(target)
	if err != nil {
		return nil, err
	}
	if clientConfig.Timeout == 0 {
		clientConfig.Timeout = 10 * time.Second
	}
	return ssh.Dial("tcp", target.Addr(), clientConfig)
}

func (s *Server) newWebTerminalRecorder(r *http.Request, target store.TargetConfig) *recording.SessionRecorder {
	if s == nil || s.cfg == nil || !s.cfg.Recording.Enabled {
		return nil
	}
	user := model.User{
		Username:          "admin-web-terminal",
		RequestedTargetID: target.ID,
	}
	session := model.NewSession(user, target.ID, firstNonEmpty(target.Name, target.ID, target.Addr()), webTerminalClientIP(r))
	session.Protocol = "ssh"
	session.ProtocolSubtype = "web-terminal"
	session.HostID = target.HostID
	session.AccountID = target.ID
	session.AccountUsername = target.Username
	session.HostIP = target.Host
	session.ConnIP = target.Host
	session.ConnPort = target.Port

	recorder, err := recording.NewSessionRecorder(
		s.cfg.ReplayDir,
		session,
		s.cfg.Recording.RecordInput,
		s.cfg.Recording.RecordCommands,
		s.logger,
	)
	if err != nil {
		s.logger.Warn("failed to initialize web terminal recorder", "target", target.ID, "error", err)
		return nil
	}
	s.logger.Info("web terminal recording started",
		"session", session.ID,
		"target", target.Name,
		"client", session.ClientIP,
	)
	return recorder
}

func webTerminalClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type webTerminalResult struct {
	source string
	err    error
}

func serveWebTerminalSSHSession(ctx context.Context, conn *websocket.Conn, targetClient *ssh.Client, opts webTerminalOptions, recorder *recording.SessionRecorder, logger *slog.Logger) error {
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
	if recorder != nil {
		recorder.RecordResize("", opts.Columns, opts.Rows)
	}
	if err := session.Shell(); err != nil {
		return err
	}

	writer := &webTerminalWriter{conn: conn}
	resultCh := make(chan webTerminalResult, 4)
	outputDone := make(chan struct{})
	var outputWG sync.WaitGroup
	outputWG.Add(2)
	go func() {
		defer outputWG.Done()
		copyWebTerminalOutput("stdout", stdout, writer, recorder, resultCh)
	}()
	go func() {
		defer outputWG.Done()
		copyWebTerminalOutput("stderr", stderr, writer, recorder, resultCh)
	}()
	go func() {
		outputWG.Wait()
		close(outputDone)
	}()
	go readWebTerminalInput(conn, stdin, session, recorder, resultCh)
	go func() {
		resultCh <- webTerminalResult{source: "session", err: session.Wait()}
	}()

	outputDoneCh := (<-chan struct{})(outputDone)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case result := <-resultCh:
		if result.err != nil && !isExpectedWebTerminalError(result.err) {
			return result.err
		}
		if result.source == "session" {
			return waitWebTerminalOutputDrain(ctx, outputDoneCh)
		}
		return nil
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

type webTerminalOutputWriter interface {
	writeBinary([]byte) error
}

func copyWebTerminalOutput(source string, src io.Reader, writer webTerminalOutputWriter, recorder *recording.SessionRecorder, resultCh chan<- webTerminalResult) {
	buf := make([]byte, 32*1024)
	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			payload := append([]byte(nil), buf[:n]...)
			if recorder != nil {
				recorder.RecordOutput(payload)
			}
			if err := writer.writeBinary(payload); err != nil {
				resultCh <- webTerminalResult{source: source, err: err}
				return
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return
			}
			resultCh <- webTerminalResult{source: source, err: readErr}
			return
		}
	}
}

func readWebTerminalInput(conn *websocket.Conn, stdin io.WriteCloser, session *ssh.Session, recorder *recording.SessionRecorder, resultCh chan<- webTerminalResult) {
	defer stdin.Close()
	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			resultCh <- webTerminalResult{source: "input", err: normalizeWebTerminalReadError(err)}
			return
		}

		switch messageType {
		case websocket.TextMessage:
			resize, ok, err := parseWebTerminalResizeMessage(payload)
			if err != nil {
				resultCh <- webTerminalResult{source: "input", err: err}
				return
			}
			if ok {
				if err := session.WindowChange(resize.Rows, resize.Columns); err != nil {
					resultCh <- webTerminalResult{source: "input", err: err}
					return
				}
				if recorder != nil {
					recorder.RecordResize("", resize.Columns, resize.Rows)
				}
				continue
			}
			if recorder != nil {
				recorder.RecordInput(payload)
			}
			if _, err := stdin.Write(payload); err != nil {
				resultCh <- webTerminalResult{source: "input", err: err}
				return
			}
		case websocket.BinaryMessage:
			if recorder != nil {
				recorder.RecordInput(payload)
			}
			if _, err := stdin.Write(payload); err != nil {
				resultCh <- webTerminalResult{source: "input", err: err}
				return
			}
		}
	}
}

func waitWebTerminalOutputDrain(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(2 * time.Second):
		return nil
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
