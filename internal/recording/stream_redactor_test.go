package recording

import (
	"errors"
	"strings"
	"testing"
)

func TestAuditStreamRedactorJoinsNetworkFramesBeforeRedaction(t *testing.T) {
	stream := newAuditStreamRedactor("output", maskingAuditRedactor{})
	first, err := stream.Write([]byte("token=secret-"))
	if err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if len(first) != 0 {
		t.Fatalf("first output = %q, want buffered", first)
	}
	second, err := stream.Write([]byte("value\n"))
	if err != nil {
		t.Fatalf("second Write: %v", err)
	}
	if strings.Contains(string(second), "secret-value") || !strings.Contains(string(second), "[MASKED]") {
		t.Fatalf("redacted output = %q", second)
	}
}

func TestAuditStreamRedactorKeepsPrivateKeyBlockTogether(t *testing.T) {
	stream := newAuditStreamRedactor("output", privateKeyTestRedactor{})
	chunks := []string{
		"-----BE",
		"GIN OPENSSH PRIVATE KEY-----\nsecret-body\n",
		"-----END OPENSSH PRIVATE KEY-----\n",
	}
	var output strings.Builder
	for _, chunk := range chunks {
		redacted, err := stream.Write([]byte(chunk))
		if err != nil {
			t.Fatalf("Write(%q): %v", chunk, err)
		}
		output.Write(redacted)
	}
	if strings.Contains(output.String(), "secret-body") || output.String() != "[PRIVATE KEY]\n" {
		t.Fatalf("private key output = %q", output.String())
	}
}

func TestAuditStreamRedactorFailsClosedAtBufferLimit(t *testing.T) {
	stream := newAuditStreamRedactor("output", passthroughAuditRedactor{})
	_, err := stream.Write([]byte(strings.Repeat("x", maxAuditStreamBufferBytes+1)))
	if !errors.Is(err, errAuditStreamBufferLimit) {
		t.Fatalf("Write error = %v", err)
	}
	if output := stream.Flush(); len(output) != 0 {
		t.Fatalf("buffer-limit flush leaked data: %q", output)
	}
}

type privateKeyTestRedactor struct{}

func (privateKeyTestRedactor) Redact(_ string, value string) string {
	if strings.Contains(value, "PRIVATE KEY") {
		return "[PRIVATE KEY]\n"
	}
	return value
}
