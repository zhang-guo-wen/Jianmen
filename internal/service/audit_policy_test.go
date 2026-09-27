package service

import (
	"strings"
	"testing"
	"time"
)

func TestAuditPolicyRedactMasksCommonCredentials(t *testing.T) {
	policy := NewAuditPolicy(30, true)

	for _, input := range []string{
		"password=hunter2", "PASSWD : hunter2", "pwd = hunter2", "secret=hunter2",
		"token=hunter2", "api_key=hunter2", "access_token=hunter2", "refresh_token=hunter2",
		"client_secret=hunter2", "passphrase=hunter2", "private_key=hunter2",
		"PGPASSWORD=hunter2", "MYSQL_PWD=hunter2",
		"secret_key=hunter2", "secret_access_key=hunter2", "aws_secret_access_key=hunter2",
		"aws_session_token=hunter2", "azure_client_secret=hunter2", "service_account_key=hunter2",
		"Bearer hunter2", "Authorization: Bearer hunter2", "authorization = Basic hunter2",
	} {
		redacted := policy.Redact("event", input)
		if strings.Contains(strings.ToLower(redacted), "hunter2") || !strings.Contains(redacted, "[REDACTED]") {
			t.Fatalf("Redact(%q) = %q, want credential masked", input, redacted)
		}
	}
}

func TestAuditPolicyRedactMasksStructuredInputs(t *testing.T) {
	policy := NewAuditPolicy(30, true)
	cases := []struct {
		name, kind, input, want string
	}{
		{"url query", "url", "https://example.test/connect?token=hunter2&safe=yes", "https://example.test/connect?token=[REDACTED]&safe=yes"},
		{"url userinfo", "url", "https://alice:hunter2@example.test/connect?safe=yes", "https://alice:[REDACTED]@example.test/connect?safe=yes"},
		{"json", "json", `{"password": "hunter2", "name": "alice"}`, `{"password": "[REDACTED]", "name": "alice"}`},
		{"json escaped key", "json", `{"pass\u0077ord":"hunter2","name":"alice"}`, `{"pass\u0077ord":"[REDACTED]","name":"alice"}`},
		{"shell export", "shell", "export PASSWORD=hunter2", "export PASSWORD=[REDACTED]"},
		{"shell flag", "shell", "tool --access-token hunter2 --name alice", "tool --access-token [REDACTED] --name alice"},
		{"quoted shell flag", "shell", "tool --passphrase='hunter2' --name alice", "tool --passphrase='[REDACTED]' --name alice"},
		{"quoted bearer", "event", `Bearer "hunter2"`, `Bearer "[REDACTED]"`},
		{"quoted authorization bearer", "event", `Authorization: Bearer "hunter2"`, `Authorization: Bearer "[REDACTED]"`},
		{"sql literals", "sql", "SELECT * FROM users WHERE name = 'alice' AND password = 'hunter2'", "SELECT * FROM users WHERE name = '[REDACTED]' AND password = '[REDACTED]'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := policy.Redact(tc.kind, tc.input); got != tc.want {
				t.Fatalf("Redact(%q, %q) = %q, want %q", tc.kind, tc.input, got, tc.want)
			}
		})
	}
}

func TestAuditPolicyRedactMasksPEMPrivateKeys(t *testing.T) {
	policy := NewAuditPolicy(30, true)
	input := "before\n-----BEGIN OPENSSH PRIVATE KEY-----\nprivate-material\n-----END OPENSSH PRIVATE KEY-----\nafter"
	if got := policy.Redact("event", input); got != "before\n[REDACTED PRIVATE KEY]\nafter" {
		t.Fatalf("Redact private key = %q", got)
	}
}

func TestAuditPolicyRedactPEMIsLinearAcrossManyBlocks(t *testing.T) {
	policy := NewAuditPolicy(30, true)
	const blocks = 80
	var input strings.Builder
	for i := 0; i < blocks; i++ {
		input.WriteString("prefix\n-----BEGIN PRIVATE KEY-----\n")
		input.WriteString(strings.Repeat("private-material\n", 32))
		input.WriteString("-----END PRIVATE KEY-----\nsuffix\n")
	}
	raw := input.String()
	got := policy.Redact("event", raw)
	if strings.Contains(got, "private-material") || strings.Count(got, "[REDACTED PRIVATE KEY]") != blocks {
		t.Fatalf("multi-block PEM redaction incomplete")
	}
	if allocs := testing.AllocsPerRun(3, func() {
		_ = redactPrivateKeys(raw)
	}); allocs > 30 {
		t.Fatalf("PEM redaction allocated %.0f objects, want at most 30", allocs)
	}
}

func TestAuditPolicySQLStillAppliesCommonRedaction(t *testing.T) {
	policy := NewAuditPolicy(30, true)
	input := "SELECT 'safe'; password=hunter2\n-----BEGIN PRIVATE KEY-----\nprivate-material\n-----END PRIVATE KEY-----"
	want := "SELECT '[REDACTED]'; password=[REDACTED]\n[REDACTED PRIVATE KEY]"
	if got := policy.Redact("sql", input); got != want {
		t.Fatalf("Redact SQL = %q, want %q", got, want)
	}
}

func TestAuditPolicySQLMasksPostgresDollarQuotedStrings(t *testing.T) {
	policy := NewAuditPolicy(30, true)
	input := "SELECT $$alpha$$, $tag$beta$tag$, 'gamma'; token=hunter2"
	want := "SELECT $$[REDACTED]$$, $tag$[REDACTED]$tag$, '[REDACTED]'; token=[REDACTED]"
	if got := policy.Redact("sql", input); got != want {
		t.Fatalf("Redact dollar-quoted SQL = %q, want %q", got, want)
	}
}

func TestAuditPolicyInputRecordingDefaultsToDenied(t *testing.T) {
	policy := NewAuditPolicy(30, false)
	for _, kind := range []string{"input", "terminal_input", "stdin", "keystroke"} {
		if got := policy.Redact(kind, "ordinary user input"); got != "[REDACTED]" {
			t.Fatalf("Redact(%q) = %q, want fully redacted", kind, got)
		}
	}
	if got := policy.RedactInput("ordinary user input"); got != "[REDACTED]" {
		t.Fatalf("RedactInput = %q, want fully redacted", got)
	}
	if got := policy.Redact("input_typo", "ordinary user input"); got != "ordinary user input" {
		t.Fatalf("unknown kind = %q, want unchanged", got)
	}
	enabled := NewAuditPolicy(30, true)
	if got := enabled.Redact("input", "ordinary user input"); got != "ordinary user input" {
		t.Fatalf("enabled input recording = %q, want original", got)
	}
	if got := enabled.RedactInput("password=hunter2"); strings.Contains(got, "hunter2") {
		t.Fatalf("enabled RedactInput leaked credential: %q", got)
	}
}

func TestAuditPolicyLeavesSafeTextUnchanged(t *testing.T) {
	policy := NewAuditPolicy(30, true)
	input := "user alice connected to host web-01"
	if got := policy.Redact("event", input); got != input {
		t.Fatalf("Redact safe text = %q, want %q", got, input)
	}
}

func TestAuditPolicyRetentionCutoffUsesSafeDefaultAndExactBoundary(t *testing.T) {
	now := time.Date(2026, 7, 18, 12, 34, 56, 123456789, time.FixedZone("CST", 8*3600))
	if got, want := NewAuditPolicy(7, false).RetentionCutoff(now), now.AddDate(0, 0, -7); !got.Equal(want) {
		t.Fatalf("seven day cutoff = %s, want %s", got, want)
	}
	if got, want := NewAuditPolicy(0, false).RetentionCutoff(now), now.AddDate(0, 0, -30); !got.Equal(want) {
		t.Fatalf("default cutoff = %s, want %s", got, want)
	}
	if got, want := NewAuditPolicy(3651, false).RetentionCutoff(now), now.AddDate(0, 0, -3650); !got.Equal(want) {
		t.Fatalf("maximum cutoff = %s, want %s", got, want)
	}
}

func TestAuditPolicyRedactHandlesLargeInputWithoutExpansion(t *testing.T) {
	policy := NewAuditPolicy(30, true)
	input := strings.Repeat("safe-data ", 50_000) + "token=hunter2"
	got := policy.Redact("event", input)
	if strings.Contains(got, "hunter2") || len(got) > len(input)+32 {
		t.Fatalf("redaction leaked or expanded input: output length %d, input length %d", len(got), len(input))
	}
}
