package recording

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type CommandRecorder struct {
	mu              sync.Mutex
	file            *os.File
	startedAt       time.Time
	seq             int64
	line            []rune
	current         *commandEvent
	sensitivePrompt bool
	redactor        AuditRedactor
	auditSink       AuditSink
	sessionID       string
	onFatal         func(error)
}

type commandEvent struct {
	Seq        int64  `json:"seq"`
	OffsetMs   int64  `json:"offset_ms"`
	Command    string `json:"command"`
	Preview    string `json:"preview"`
	Confidence string `json:"confidence"`
	StartedAt  int64  `json:"started_at"`
	EndedAt    int64  `json:"ended_at"`
}

func NewCommandRecorder(
	file *os.File,
	startedAt time.Time,
	redactor AuditRedactor,
	sink AuditSink,
	sessionID string,
	onFatal func(error),
) *CommandRecorder {
	return &CommandRecorder{
		file:      file,
		startedAt: startedAt,
		line:      make([]rune, 0, 128),
		redactor:  redactor,
		auditSink: sink,
		sessionID: sessionID,
		onFatal:   onFatal,
	}
}

func (r *CommandRecorder) MarkSensitivePrompt() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.sensitivePrompt = true
	r.mu.Unlock()
}

func (r *CommandRecorder) ObserveInput(data []byte) {
	if r == nil || len(data) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, ch := range string(data) {
		switch ch {
		case '\r', '\n':
			r.submitLineLocked()
		case '\b', 0x7f:
			if len(r.line) > 0 {
				r.line = r.line[:len(r.line)-1]
			}
		case '\t':
			r.line = append(r.line, ch)
		default:
			if ch >= 0x20 && ch != 0x1b {
				r.line = append(r.line, ch)
			}
		}
	}
}

func (r *CommandRecorder) ObserveOutput(data []byte) {
	if r == nil || len(data) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if isSensitivePrompt(data) {
		r.sensitivePrompt = true
	}
	if r.current == nil {
		return
	}
	const maxPreview = 4096
	if len(r.current.Preview) >= maxPreview {
		return
	}
	remaining := maxPreview - len(r.current.Preview)
	text := string(data)
	if len(text) > remaining {
		text = text[:remaining]
	}
	r.current.Preview += text
}

func (r *CommandRecorder) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	flushErr := r.flushCurrentLocked()
	if r.file == nil {
		return flushErr
	}
	closeErr := r.file.Close()
	r.file = nil
	return errors.Join(flushErr, closeErr)
}

func (r *CommandRecorder) submitLineLocked() {
	command := strings.TrimSpace(strings.ReplaceAll(string(r.line), "\t", " "))
	r.line = r.line[:0]
	if command == "" {
		return
	}
	if r.sensitivePrompt {
		r.sensitivePrompt = false
		return
	}
	command = r.redactor.Redact("command", command)
	if err := r.flushCurrentLocked(); err != nil {
		if r.onFatal != nil {
			r.onFatal(fmt.Errorf("flush command audit event: %w", err))
		}
		return
	}

	r.seq++
	now := time.Now().UTC()
	r.current = &commandEvent{
		Seq:        r.seq,
		OffsetMs:   int64(now.Sub(r.startedAt) / time.Millisecond),
		Command:    command,
		Confidence: "partial",
		StartedAt:  now.UnixMilli(),
	}
}

func (r *CommandRecorder) flushCurrentLocked() error {
	if r.current == nil {
		return nil
	}
	if r.file == nil {
		return errors.New("command audit file is unavailable")
	}
	now := time.Now().UTC()
	r.current.EndedAt = now.UnixMilli()
	r.current.Preview = strings.TrimSpace(stripControlPreview(r.current.Preview))
	r.current.Preview = r.redactor.Redact("output", r.current.Preview)
	raw, err := json.Marshal(r.current)
	if err != nil {
		return err
	}
	if _, err := r.file.Write(append(raw, '\n')); err != nil {
		return err
	}
	event := r.current
	r.current = nil
	if r.auditSink != nil {
		if err := r.auditSink.WriteCommand(r.sessionID, time.UnixMilli(event.StartedAt), event.Command); err != nil {
			return fmt.Errorf("write database command audit event: %w", err)
		}
	}
	return nil
}

func stripControlPreview(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		switch {
		case ch == '\x1b':
			// Skip ANSI escape sequence. CSI sequences end with a letter (A-Z, a-z).
			// OSC sequences end with BEL (\a) or ST (\x1b\\).
			if i+1 < len(runes) && runes[i+1] == '[' {
				i += 2 // skip ESC [
				for i < len(runes) {
					c := runes[i]
					if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
						break // terminator found
					}
					i++
				}
			} else if i+1 < len(runes) && runes[i+1] == ']' {
				i += 2 // skip ESC ]
				for i < len(runes) {
					if runes[i] == '\a' || (runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '\\') {
						if runes[i] == '\x1b' {
							i++
						}
						break
					}
					i++
				}
			}
			// For other ESC sequences, just skip the ESC and continue
		case ch == '\r':
			b.WriteRune('\n')
		case ch == '\t', ch == '\n':
			b.WriteRune(ch)
		default:
			if ch >= 0x20 && ch != 0x7f {
				b.WriteRune(ch)
			}
		}
	}
	return strings.TrimSpace(b.String())
}
