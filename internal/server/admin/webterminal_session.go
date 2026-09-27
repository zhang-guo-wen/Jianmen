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
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"

	"jianmen/internal/recording"
)

type webTerminalResize struct {
	Columns int
	Rows    int
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
