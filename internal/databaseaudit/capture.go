package databaseaudit

import (
	"bytes"
	"errors"
	"unicode/utf8"

	"jianmen/internal/auditartifact"
)

const (
	RedactedMarker      = "[REDACTED]"
	DefaultPreviewBytes = 64 * 1024
	maxDollarTagBytes   = 64 * 1024
)

type Policy struct {
	PreviewBytes     int
	RedactionEnabled bool
}
type SQLResult struct {
	Preview                    string
	OriginalBytes, StoredBytes int64
	Truncated, Redacted        bool
	Section                    auditartifact.Section
}

type SQLProcessor struct {
	redact        bool
	previewLimit  int
	preview       []byte
	originalBytes int64
	redactor      *SQLStreamRedactor
}

func NewSQLProcessor(redact bool, previewBytes int) *SQLProcessor {
	if previewBytes < 0 {
		previewBytes = 0
	}
	return &SQLProcessor{redact: redact, previewLimit: previewBytes}
}
func (p *SQLProcessor) Write(data []byte, emit func([]byte) error) error {
	if p == nil {
		return errors.New("database SQL processor is unavailable")
	}
	p.originalBytes += int64(len(data))
	if !p.redact {
		p.collect(data)
		return emit(data)
	}
	if p.redactor == nil {
		p.redactor = NewSQLStreamRedactor(func(output []byte) error { p.collect(output); return emit(output) })
	}
	return p.redactor.Write(data)
}
func (p *SQLProcessor) Flush(emit func([]byte) error) error {
	if p == nil || !p.redact || p.redactor == nil {
		return nil
	}
	return p.redactor.Flush()
}
func (p *SQLProcessor) collect(data []byte) {
	if p.previewLimit <= 0 || len(p.preview) >= p.previewLimit {
		return
	}
	remaining := p.previewLimit - len(p.preview)
	if remaining > len(data) {
		remaining = len(data)
	}
	p.preview = append(p.preview, data[:remaining]...)
}
func (p *SQLProcessor) Result(section auditartifact.Section) SQLResult {
	preview := append([]byte(nil), p.preview...)
	for len(preview) > 0 && !utf8.Valid(preview) {
		preview = preview[:len(preview)-1]
	}
	return SQLResult{Preview: string(preview), OriginalBytes: p.originalBytes, StoredBytes: section.Bytes, Truncated: int64(len(preview)) < section.Bytes, Redacted: p.redact, Section: section}
}
func CaptureSQL(writer *auditartifact.Writer, policy Policy, data []byte) (SQLResult, error) {
	if writer == nil {
		return SQLResult{}, errors.New("database audit writer is required")
	}
	limit := policy.PreviewBytes
	if limit <= 0 {
		limit = DefaultPreviewBytes
	}
	processor := NewSQLProcessor(policy.RedactionEnabled, limit)
	capture, err := writer.Begin(processor)
	if err != nil {
		return SQLResult{}, err
	}
	if err = capture.Write(data); err != nil {
		_ = capture.Abort()
		return SQLResult{}, err
	}
	section, err := capture.Complete()
	if err != nil {
		return SQLResult{}, err
	}
	return processor.Result(section), nil
}
func PreviewSQL(data []byte, policy Policy) SQLResult {
	limit := policy.PreviewBytes
	if limit <= 0 {
		limit = DefaultPreviewBytes
	}
	processor := NewSQLProcessor(policy.RedactionEnabled, limit)
	var stored bytes.Buffer
	_ = processor.Write(data, func(output []byte) error { _, err := stored.Write(output); return err })
	_ = processor.Flush(func(output []byte) error { _, err := stored.Write(output); return err })
	return processor.Result(auditartifact.Section{Bytes: int64(stored.Len())})
}
func RedactSQL(data []byte) string {
	return PreviewSQL(data, Policy{PreviewBytes: int(^uint(0) >> 1), RedactionEnabled: true}).Preview
}

type ParameterProcessor struct{ redact bool }

func NewParameterProcessor(redact bool) *ParameterProcessor {
	return &ParameterProcessor{redact: redact}
}
func (p *ParameterProcessor) Write(data []byte, emit func([]byte) error) error {
	if !p.redact {
		return emit(data)
	}
	return nil
}
func (p *ParameterProcessor) Flush(emit func([]byte) error) error {
	if p.redact {
		return emit([]byte("[PARAMETERS REDACTED]"))
	}
	return nil
}
func isIdentifierStart(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}
func isIdentifierPart(value byte) bool {
	return isIdentifierStart(value) || value >= '0' && value <= '9' || value == '$'
}
