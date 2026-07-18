package service

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAuditPolicyRedactJSONConsumesCompleteSensitiveValue(t *testing.T) {
	policy := NewAuditPolicy(30, true)
	cases := []struct {
		name, input, want string
	}{
		{
			name:  "array",
			input: `{"token":["first-secret","second-secret"],"name":"alice"}`,
			want:  `{"token":"[REDACTED]","name":"alice"}`,
		},
		{
			name:  "object",
			input: `{"client_secret":{"primary":"first-secret","fallback":["second-secret"]},"name":"alice"}`,
			want:  `{"client_secret":"[REDACTED]","name":"alice"}`,
		},
		{
			name:  "null",
			input: `{"private_key":null,"name":"alice"}`,
			want:  `{"private_key":"[REDACTED]","name":"alice"}`,
		},
		{
			name:  "nested sensitive key",
			input: `{"outer":{"token":["first-secret",{"value":"second-secret"}]},"name":"alice"}`,
			want:  `{"outer":{"token":"[REDACTED]"},"name":"alice"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := policy.Redact("json", tc.input); got != tc.want {
				t.Fatalf("Redact JSON = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAuditPolicyRedactJSONFailsClosedForMalformedSensitiveValue(t *testing.T) {
	policy := NewAuditPolicy(30, true)
	for _, input := range []string{
		`{"token":["first-secret","second-secret"`,
		`{"token":{"nested":"first-secret"`,
		`{"token":"first-secret`,
		`{"token":first-secret second-secret}`,
	} {
		got := policy.Redact("json", input)
		if strings.Contains(got, "first-secret") || strings.Contains(got, "second-secret") {
			t.Fatalf("malformed JSON leaked secret: %q", got)
		}
		if !strings.Contains(got, `"[REDACTED]"`) {
			t.Fatalf("malformed JSON did not emit legal redacted value: %q", got)
		}
	}
}

func TestAuditPolicySQLMasksBackslashEscapedQuotes(t *testing.T) {
	policy := NewAuditPolicy(30, true)
	for _, input := range []string{
		`SELECT 'first-secret\'second-secret', 1`,
		`SELECT E'first-secret\'second-secret', 1`,
	} {
		got := policy.Redact("sql", input)
		if strings.Contains(got, "first-secret") || strings.Contains(got, "second-secret") {
			t.Fatalf("escaped SQL literal leaked secret: %q", got)
		}
		if !strings.HasSuffix(got, ", 1") {
			t.Fatalf("escaped SQL literal destroyed trailing structure: %q", got)
		}
	}
}

func TestRedactAssignmentsAdvancesPastLongNonSensitiveKey(t *testing.T) {
	longKey := strings.Repeat("ordinary_key_part_", 16_384)
	input := longKey + "=public token=hunter2"
	want := longKey + "=public token=[REDACTED]"
	done := make(chan string, 1)
	go func() {
		done <- redactAssignments(input)
	}()

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case got := <-done:
		if got != want {
			t.Fatalf("long-key redaction mismatch")
		}
	case <-timer.C:
		t.Fatal("long non-sensitive key scan exceeded 2 seconds")
	}
}

func TestAuditPolicySQLFailsClosedForUnclosedDollarQuote(t *testing.T) {
	policy := NewAuditPolicy(30, true)
	if got, want := policy.Redact("sql", "SELECT $tag$first-secret"), "SELECT $tag$[REDACTED]"; got != want {
		t.Fatalf("unclosed dollar quote = %q, want %q", got, want)
	}
}

func TestAuditPolicySQLStopsAfterFirstUnclosedUniqueDollarTag(t *testing.T) {
	var input strings.Builder
	input.WriteString("SELECT ")
	for i := 0; i < 2_000; i++ {
		input.WriteString("$tag")
		input.WriteString(strconv.Itoa(i))
		input.WriteString("$secret-")
		input.WriteString(strconv.Itoa(i))
		input.WriteByte(' ')
	}

	got := NewAuditPolicy(30, true).Redact("sql", input.String())
	if want := "SELECT $tag0$[REDACTED]"; got != want {
		t.Fatalf("large unclosed dollar quote length = %d, want %q", len(got), want)
	}
}

func TestAuditPolicyAuthorizationPreservesFollowingSQLStructure(t *testing.T) {
	policy := NewAuditPolicy(30, true)
	cases := []struct {
		input, want string
	}{
		{
			input: "SELECT $$hunter2$$ /* Authorization: Basic second-secret */",
			want:  "SELECT $$[REDACTED]$$ /* Authorization: Basic [REDACTED] */",
		},
		{
			input: `SELECT 1 /* Authorization: Bearer "second-secret" */`,
			want:  `SELECT 1 /* Authorization: Bearer "[REDACTED]" */`,
		},
		{
			input: "SELECT 1 /* Authorization: Basic second-secret*/",
			want:  "SELECT 1 /* Authorization: Basic [REDACTED]*/",
		},
		{
			input: "SELECT 1 /* Authorization: opaque-secret*/",
			want:  "SELECT 1 /* Authorization: [REDACTED]*/",
		},
	}
	for _, tc := range cases {
		if got := policy.Redact("sql", tc.input); got != tc.want {
			t.Fatalf("authorization structure = %q, want %q", got, tc.want)
		}
	}
}
