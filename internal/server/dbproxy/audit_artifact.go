package dbproxy

import (
	"errors"
	"fmt"
	"os"

	"jianmen/internal/auditartifact"
	"jianmen/internal/databaseaudit"
)

var errDatabaseAuditArtifactWrite = errors.New("database audit artifact write failed")

const (
	databaseAuditDataFileName   = "query-data.bin"
	databaseAuditWriterBuffer   = auditartifact.DefaultBufferBytes
	databaseAuditArtifactSQL    = "sql"
	databaseAuditArtifactParams = "parameters"
)

type databaseAuditArtifact struct {
	offset, bytes int64
	redacted      bool
}

func (a databaseAuditArtifact) available() bool { return a.offset >= 0 && a.bytes > 0 }

type databaseAuditCapture interface {
	Write([]byte) error
	Complete() (databaseAuditArtifact, error)
	Abort()
}
type databaseAuditArtifactSink interface {
	BeginDatabaseAuditCapture(kind string) (databaseAuditCapture, error)
}

type discardDatabaseAuditCapture struct{}

func (discardDatabaseAuditCapture) Write([]byte) error { return nil }
func (discardDatabaseAuditCapture) Complete() (databaseAuditArtifact, error) {
	return databaseAuditArtifact{offset: -1}, nil
}
func (discardDatabaseAuditCapture) Abort() {}

type databaseAuditArtifactFile struct {
	writer  *auditartifact.Writer
	redact  bool
	initErr error
}

func newDatabaseAuditArtifactFile(file *os.File, redact bool) *databaseAuditArtifactFile {
	writer, err := auditartifact.NewWriter(file, databaseAuditWriterBuffer)
	return &databaseAuditArtifactFile{writer: writer, redact: redact, initErr: err}
}

func (f *databaseAuditArtifactFile) BeginDatabaseAuditCapture(kind string) (databaseAuditCapture, error) {
	if f == nil {
		return nil, errors.New("database audit artifact file is unavailable")
	}
	if f.initErr != nil {
		return nil, fmt.Errorf("database audit artifact writer is unusable: %w", f.initErr)
	}
	if kind != databaseAuditArtifactSQL && kind != databaseAuditArtifactParams {
		return nil, fmt.Errorf("unsupported database audit artifact kind %q", kind)
	}
	var processor auditartifact.Processor
	if f.redact && kind == databaseAuditArtifactSQL {
		processor = databaseaudit.NewSQLProcessor(true, 0)
	}
	if f.redact && kind == databaseAuditArtifactParams {
		processor = databaseaudit.NewParameterProcessor(true)
	}
	capture, err := f.writer.Begin(processor)
	if err != nil {
		return nil, err
	}
	return &databaseAuditFileCapture{capture: capture, redacted: f.redact}, nil
}
func (f *databaseAuditArtifactFile) close() error {
	if f == nil {
		return nil
	}
	if f.writer == nil {
		return f.initErr
	}
	return errors.Join(f.initErr, f.writer.Close())
}

type databaseAuditFileCapture struct {
	capture  *auditartifact.Capture
	redacted bool
}

func (c *databaseAuditFileCapture) Write(data []byte) error {
	if c == nil || c.capture == nil {
		return errors.New("database audit artifact capture is unavailable")
	}
	return c.capture.Write(data)
}
func (c *databaseAuditFileCapture) Complete() (databaseAuditArtifact, error) {
	if c == nil || c.capture == nil {
		return databaseAuditArtifact{}, errors.New("database audit artifact capture is unavailable")
	}
	section, err := c.capture.Complete()
	if err != nil {
		return databaseAuditArtifact{}, err
	}
	return databaseAuditArtifact{offset: section.Offset, bytes: section.Bytes, redacted: c.redacted}, nil
}
func (c *databaseAuditFileCapture) Abort() {
	if c != nil && c.capture != nil {
		_ = c.capture.Abort()
	}
}

func captureDatabaseAuditBytes(sink querySink, kind string, data []byte) (databaseAuditArtifact, error) {
	artifactSink, ok := sink.(databaseAuditArtifactSink)
	if !ok {
		return databaseAuditArtifact{offset: -1}, nil
	}
	capture, err := artifactSink.BeginDatabaseAuditCapture(kind)
	if err != nil {
		return databaseAuditArtifact{}, err
	}
	if err := capture.Write(data); err != nil {
		capture.Abort()
		return databaseAuditArtifact{}, err
	}
	return capture.Complete()
}
