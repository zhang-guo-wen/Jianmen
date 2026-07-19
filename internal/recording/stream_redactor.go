package recording

import (
	"bytes"
	"errors"
	"strings"
	"sync"
)

const maxAuditStreamBufferBytes = 1024 * 1024

var errAuditStreamBufferLimit = errors.New("audit stream redaction buffer limit exceeded")

// auditStreamRedactor waits for logical line boundaries before applying the
// stateless policy. It also keeps complete PEM private-key blocks together so
// secrets cannot bypass redaction by crossing network-frame boundaries.
type auditStreamRedactor struct {
	mu              sync.Mutex
	kind            string
	redactor        AuditRedactor
	pending         []byte
	privateKeyBlock strings.Builder
	inPrivateKey    bool
}

type auditStreamPrefixPolicy interface {
	SafeStreamPrefix(kind, value string) int
}

func newAuditStreamRedactor(kind string, redactor AuditRedactor) *auditStreamRedactor {
	return &auditStreamRedactor{kind: kind, redactor: redactor}
}

func (s *auditStreamRedactor) Write(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.pending)+s.privateKeyBlock.Len()+len(data) > maxAuditStreamBufferBytes {
		return nil, errAuditStreamBufferLimit
	}
	s.pending = append(s.pending, data...)

	var output strings.Builder
	for {
		lineEnd := bytes.IndexAny(s.pending, "\r\n")
		if lineEnd < 0 {
			break
		}
		delimiterEnd := lineEnd + 1
		if s.pending[lineEnd] == '\r' &&
			delimiterEnd < len(s.pending) &&
			s.pending[delimiterEnd] == '\n' {
			delimiterEnd++
		}
		line := string(s.pending[:delimiterEnd])
		s.pending = s.pending[delimiterEnd:]
		s.writeLine(&output, line)
	}
	if !s.inPrivateKey && len(s.pending) > 0 {
		safePrefix := 0
		if policy, ok := s.redactor.(auditStreamPrefixPolicy); ok {
			safePrefix = policy.SafeStreamPrefix(s.kind, string(s.pending))
			if safePrefix < 0 {
				safePrefix = 0
			} else if safePrefix > len(s.pending) {
				safePrefix = len(s.pending)
			}
		}
		if safePrefix > 0 {
			output.WriteString(s.redactor.Redact(s.kind, string(s.pending[:safePrefix])))
			s.pending = s.pending[safePrefix:]
		}
	}
	return []byte(output.String()), nil
}

func (s *auditStreamRedactor) Flush() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	var output strings.Builder
	if len(s.pending) > 0 {
		s.writeLine(&output, string(s.pending))
		s.pending = nil
	}
	if s.inPrivateKey {
		output.WriteString(s.redactor.Redact(s.kind, s.privateKeyBlock.String()))
		s.privateKeyBlock.Reset()
		s.inPrivateKey = false
	}
	return []byte(output.String())
}

func (s *auditStreamRedactor) writeLine(output *strings.Builder, line string) {
	if s.inPrivateKey {
		s.privateKeyBlock.WriteString(line)
		if containsPrivateKeyMarker(line, "-----END ") {
			output.WriteString(s.redactor.Redact(s.kind, s.privateKeyBlock.String()))
			s.privateKeyBlock.Reset()
			s.inPrivateKey = false
		}
		return
	}
	if containsPrivateKeyMarker(line, "-----BEGIN ") &&
		!containsPrivateKeyMarker(line, "-----END ") {
		s.inPrivateKey = true
		s.privateKeyBlock.WriteString(line)
		return
	}
	output.WriteString(s.redactor.Redact(s.kind, line))
}

func containsPrivateKeyMarker(line, prefix string) bool {
	upper := strings.ToUpper(line)
	return strings.Contains(upper, prefix) && strings.Contains(upper, "PRIVATE KEY-----")
}
