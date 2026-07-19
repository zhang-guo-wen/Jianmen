package service

import "testing"

func TestAuditPolicySafeStreamPrefixReleasesOrdinaryOutput(t *testing.T) {
	policy := NewAuditPolicy(30, true)
	value := "JIANMEN_SSH_OK"
	if got := policy.SafeStreamPrefix("output", value); got != len(value) {
		t.Fatalf("SafeStreamPrefix ordinary output = %d, want %d", got, len(value))
	}
}

func TestAuditPolicySafeStreamPrefixBuffersSplitSensitiveSyntax(t *testing.T) {
	policy := NewAuditPolicy(30, true)
	cases := []struct {
		name  string
		value string
		want  int
	}{
		{name: "partial key", value: "safe tok", want: len("safe ")},
		{name: "quoted partial key", value: `safe "tok`, want: len("safe ")},
		{name: "partial flag", value: "safe --tok", want: len("safe ")},
		{name: "partial bearer", value: "safe Bear", want: len("safe ")},
		{name: "partial PEM marker", value: "safe\n-----BE", want: len("safe\n")},
		{name: "open PEM marker", value: "safe\n-----BEGIN CUSTOM", want: len("safe\n")},
		{name: "detected assignment", value: "token=secret-", want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := policy.SafeStreamPrefix("output", tc.value); got != tc.want {
				t.Fatalf("SafeStreamPrefix(%q) = %d, want %d", tc.value, got, tc.want)
			}
		})
	}
}

func TestAuditPolicySafeStreamPrefixDoesNotHoldOrdinaryKeySuffix(t *testing.T) {
	policy := NewAuditPolicy(30, true)
	for _, value := range []string{"status", "mytoken", "ordinary-output"} {
		if got := policy.SafeStreamPrefix("output", value); got != len(value) {
			t.Fatalf("SafeStreamPrefix(%q) = %d, want %d", value, got, len(value))
		}
	}
}
