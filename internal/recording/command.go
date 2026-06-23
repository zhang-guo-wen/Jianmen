package recording

import (
	"encoding/json"
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

func NewCommandRecorder(file *os.File, startedAt time.Time) *CommandRecorder {
	return &CommandRecorder{
		file:      file,
		startedAt: startedAt,
		line:      make([]rune, 0, 128),
	}
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
	if err := r.flushCurrentLocked(); err != nil {
		return err
	}
	if r.file == nil {
		return nil
	}
	err := r.file.Close()
	r.file = nil
	return err
}

func (r *CommandRecorder) submitLineLocked() {
	command := strings.TrimSpace(string(r.line))
	r.line = r.line[:0]
	if command == "" {
		return
	}
	if r.sensitivePrompt {
		r.sensitivePrompt = false
		return
	}
	_ = r.flushCurrentLocked()

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
	if r.current == nil || r.file == nil {
		return nil
	}
	now := time.Now().UTC()
	r.current.EndedAt = now.UnixMilli()
	r.current.Preview = strings.TrimSpace(stripControlPreview(r.current.Preview))
	raw, err := json.Marshal(r.current)
	if err != nil {
		return err
	}
	if _, err := r.file.Write(append(raw, '\n')); err != nil {
		return err
	}
	r.current = nil
	return nil
}

func stripControlPreview(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, ch := range s {
		switch ch {
		case '\r':
			b.WriteRune('\n')
		case '\t', '\n':
			b.WriteRune(ch)
		default:
			if ch >= 0x20 && ch != 0x7f {
				b.WriteRune(ch)
			}
		}
	}
	return b.String()
}
