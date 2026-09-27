package webrdp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/proxy/rdpproxy"
	"jianmen/internal/service"
)

type channelAuditStub struct {
	events []*model.AuditRDPChannelEvent
	err    error
	failAt int
	calls  int
}

func (s *channelAuditStub) CreateAuditRDPChannelEvent(
	_ context.Context,
	event *model.AuditRDPChannelEvent,
) error {
	s.calls++
	if s.err != nil && (s.failAt == 0 || s.calls == s.failAt) {
		return s.err
	}
	copy := *event
	s.events = append(s.events, &copy)
	return nil
}

func TestChannelFilterEnforcesEveryOptionalPolicy(t *testing.T) {
	tests := []struct {
		name      string
		direction string
		opcode    string
		channel   string
		enable    func(*service.WebRDPChannelPolicy)
	}{
		{
			name: "clipboard read", direction: "remote_to_browser",
			opcode: "clipboard", channel: "clipboard",
			enable: func(policy *service.WebRDPChannelPolicy) { policy.ClipboardRead = true },
		},
		{
			name: "clipboard write", direction: "browser_to_remote",
			opcode: "clipboard", channel: "clipboard",
			enable: func(policy *service.WebRDPChannelPolicy) { policy.ClipboardWrite = true },
		},
		{
			name: "file upload", direction: "browser_to_remote",
			opcode: "file", channel: "file",
			enable: func(policy *service.WebRDPChannelPolicy) { policy.FileUpload = true },
		},
		{
			name: "file download", direction: "remote_to_browser",
			opcode: "file", channel: "file",
			enable: func(policy *service.WebRDPChannelPolicy) { policy.FileDownload = true },
		},
		{
			name: "drive mapping", direction: "remote_to_browser",
			opcode: "filesystem", channel: "drive",
			enable: func(policy *service.WebRDPChannelPolicy) { policy.DriveMapping = true },
		},
	}

	for _, test := range tests {
		t.Run(test.name+"/denied", func(t *testing.T) {
			audit := &channelAuditStub{}
			filter := newChannelFilter(
				"audit-session-1",
				test.direction,
				service.WebRDPChannelPolicy{},
				audit,
			)
			allowed, err := filter.Allow(
				context.Background(),
				validChannelInstruction(test.opcode),
			)
			if err != nil {
				t.Fatalf("filter instruction: %v", err)
			}
			if allowed {
				t.Fatal("channel instruction was allowed with zero-value policy")
			}
			assertChannelAudit(t, audit, test.channel, test.direction, test.opcode, "denied")
		})

		t.Run(test.name+"/allowed", func(t *testing.T) {
			policy := service.WebRDPChannelPolicy{}
			test.enable(&policy)
			audit := &channelAuditStub{}
			filter := newChannelFilter("audit-session-1", test.direction, policy, audit)
			allowed, err := filter.Allow(
				context.Background(),
				validChannelInstruction(test.opcode),
			)
			if err != nil {
				t.Fatalf("filter instruction: %v", err)
			}
			if !allowed {
				t.Fatal("channel instruction was denied despite effective policy")
			}
			assertChannelAudit(t, audit, test.channel, test.direction, test.opcode, "allowed")
		})
	}
}

func TestDeniedChannelBlocksFollowingBlobStreamUntilEnd(t *testing.T) {
	audit := &channelAuditStub{}
	filter := newChannelFilter(
		"audit-session-1",
		"browser_to_remote",
		service.WebRDPChannelPolicy{},
		audit,
	)
	ctx := context.Background()

	allowed, err := filter.Allow(ctx, rdpproxy.Instruction{
		Opcode: "file",
		Args:   []string{"42", "payroll.xlsx", "application/vnd.ms-excel"},
	})
	if err != nil || allowed {
		t.Fatalf("file starter allowed = %v, err = %v; want denied", allowed, err)
	}
	for _, instruction := range []rdpproxy.Instruction{
		{Opcode: "blob", Args: []string{"42", "base64-secret-file-content"}},
		{Opcode: "end", Args: []string{"42"}},
	} {
		allowed, err = filter.Allow(ctx, instruction)
		if err != nil {
			t.Fatalf("filter %s: %v", instruction.Opcode, err)
		}
		if allowed {
			t.Fatalf("%s for denied stream was allowed", instruction.Opcode)
		}
	}

	allowed, err = filter.Allow(ctx, rdpproxy.Instruction{
		Opcode: "blob",
		Args:   []string{"42", "new-unrelated-stream"},
	})
	if err != nil || !allowed {
		t.Fatalf("stream block survived end instruction: allowed = %v, err = %v", allowed, err)
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want only channel metadata event", len(audit.events))
	}
	serialized, err := json.Marshal(audit.events)
	if err != nil {
		t.Fatalf("marshal events: %v", err)
	}
	for _, secret := range []string{"payroll.xlsx", "base64-secret-file-content"} {
		if strings.Contains(string(serialized), secret) {
			t.Fatalf("channel audit leaked content %q: %s", secret, serialized)
		}
	}
}

func TestChannelFilterTracksInterleavedAllowedStreamBytes(t *testing.T) {
	audit := &channelAuditStub{}
	filter := newChannelFilter(
		"audit-session-1",
		"browser_to_remote",
		service.WebRDPChannelPolicy{
			ClipboardWrite: true,
			FileUpload:     true,
		},
		audit,
	)
	ctx := context.Background()

	for _, instruction := range []rdpproxy.Instruction{
		{
			Opcode: "clipboard",
			Args:   []string{"7", "text/plain"},
		},
		{
			Opcode: "file",
			Args:   []string{"8", "application/octet-stream", "report.bin"},
		},
		{
			Opcode: "put",
			Args:   []string{"101", "9", "application/octet-stream", "upload.bin"},
		},
	} {
		requireChannelInstruction(t, filter, ctx, instruction, true)
	}

	// Blob payloads are independently base64-encoded. Interleave streams and
	// cover both padding variants plus an unpadded four-character group.
	for _, instruction := range []rdpproxy.Instruction{
		{Opcode: "blob", Args: []string{"7", "YQ=="}},
		{Opcode: "blob", Args: []string{"8", "YWI="}},
		{Opcode: "blob", Args: []string{"9", "YWJj"}},
		{Opcode: "blob", Args: []string{"7", "YWJj"}},
		{Opcode: "blob", Args: []string{"9", "YWJjZA=="}},
	} {
		requireChannelInstruction(t, filter, ctx, instruction, true)
	}

	// The first put argument is an object ID, not a stream ID.
	requireChannelInstruction(t, filter, ctx, rdpproxy.Instruction{
		Opcode: "blob",
		Args:   []string{"101", "YQ=="},
	}, true)
	requireChannelInstruction(t, filter, ctx, rdpproxy.Instruction{
		Opcode: "end",
		Args:   []string{"101"},
	}, true)

	for _, instruction := range []rdpproxy.Instruction{
		{Opcode: "end", Args: []string{"9"}},
		{Opcode: "end", Args: []string{"7"}},
		{Opcode: "end", Args: []string{"8"}},
	} {
		requireChannelInstruction(t, filter, ctx, instruction, true)
	}

	if len(audit.events) != 6 {
		t.Fatalf("audit events = %d, want 3 starts and 3 ends", len(audit.events))
	}
	expectedEnds := []struct {
		channel string
		bytes   int64
	}{
		{channel: "file", bytes: 7},
		{channel: "clipboard", bytes: 4},
		{channel: "file", bytes: 2},
	}
	for index, expected := range expectedEnds {
		event := audit.events[index+3]
		if event.Operation != "end" ||
			event.Channel != expected.channel ||
			event.Bytes != expected.bytes ||
			event.Outcome != "allowed" {
			t.Fatalf("end event %d = %#v, want channel %q and %d bytes",
				index, event, expected.channel, expected.bytes)
		}
	}
	serialized, err := json.Marshal(audit.events)
	if err != nil {
		t.Fatalf("marshal events: %v", err)
	}
	for _, payload := range []string{"YQ==", "YWI=", "YWJj", "YWJjZA=="} {
		if strings.Contains(string(serialized), payload) {
			t.Fatalf("channel audit leaked blob payload %q: %s", payload, serialized)
		}
	}
}

func TestDeniedPutBlocksStreamIDInsteadOfObjectID(t *testing.T) {
	audit := &channelAuditStub{}
	filter := newChannelFilter(
		"audit-session-1",
		"browser_to_remote",
		service.WebRDPChannelPolicy{},
		audit,
	)
	ctx := context.Background()

	requireChannelInstruction(t, filter, ctx, rdpproxy.Instruction{
		Opcode: "put",
		Args:   []string{"42", "99", "application/octet-stream", "secret.bin"},
	}, false)

	// The put stream ID is its second argument. Its full blob/end sequence must
	// be blocked, while the unrelated object ID must not poison another stream.
	requireChannelInstruction(t, filter, ctx, rdpproxy.Instruction{
		Opcode: "blob",
		Args:   []string{"42", "YQ=="},
	}, true)
	requireChannelInstruction(t, filter, ctx, rdpproxy.Instruction{
		Opcode: "end",
		Args:   []string{"42"},
	}, true)
	requireChannelInstruction(t, filter, ctx, rdpproxy.Instruction{
		Opcode: "blob",
		Args:   []string{"99", "YQ=="},
	}, false)
	requireChannelInstruction(t, filter, ctx, rdpproxy.Instruction{
		Opcode: "end",
		Args:   []string{"99"},
	}, false)

	// End releases the denied stream ID for later, unrelated streams.
	requireChannelInstruction(t, filter, ctx, rdpproxy.Instruction{
		Opcode: "blob",
		Args:   []string{"99", "YQ=="},
	}, true)
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want only denied put metadata", len(audit.events))
	}
	assertChannelAudit(t, audit, "file", "browser_to_remote", "put", "denied")
}

func TestChannelAuditFailureStopsFiltering(t *testing.T) {
	auditErr := errors.New("audit repository unavailable")
	filter := newChannelFilter(
		"audit-session-1",
		"browser_to_remote",
		service.WebRDPChannelPolicy{ClipboardWrite: true},
		&channelAuditStub{err: auditErr},
	)

	allowed, err := filter.Allow(context.Background(), rdpproxy.Instruction{
		Opcode: "clipboard",
		Args:   []string{"7", "text/plain"},
	})

	if allowed {
		t.Fatal("instruction was allowed after audit write failed")
	}
	if !errors.Is(err, auditErr) {
		t.Fatalf("filter error = %v, want wrapped audit error", err)
	}
}

func TestChannelEndAuditFailureIsFailClosed(t *testing.T) {
	auditErr := errors.New("audit repository unavailable")
	audit := &channelAuditStub{err: auditErr, failAt: 2}
	filter := newChannelFilter(
		"audit-session-1",
		"browser_to_remote",
		service.WebRDPChannelPolicy{ClipboardWrite: true},
		audit,
	)
	ctx := context.Background()

	requireChannelInstruction(t, filter, ctx, rdpproxy.Instruction{
		Opcode: "clipboard",
		Args:   []string{"7", "text/plain"},
	}, true)
	requireChannelInstruction(t, filter, ctx, rdpproxy.Instruction{
		Opcode: "blob",
		Args:   []string{"7", "YQ=="},
	}, true)
	allowed, err := filter.Allow(ctx, rdpproxy.Instruction{
		Opcode: "end",
		Args:   []string{"7"},
	})
	if allowed {
		t.Fatal("end instruction was allowed after audit write failed")
	}
	if !errors.Is(err, auditErr) {
		t.Fatalf("filter error = %v, want wrapped audit error", err)
	}
	if len(audit.events) != 1 {
		t.Fatalf("persisted audit events = %d, want only stream start", len(audit.events))
	}
}

func TestTrackedStreamRejectsMalformedBase64(t *testing.T) {
	filter := newChannelFilter(
		"audit-session-1",
		"browser_to_remote",
		service.WebRDPChannelPolicy{FileUpload: true},
		&channelAuditStub{},
	)
	ctx := context.Background()
	requireChannelInstruction(t, filter, ctx, rdpproxy.Instruction{
		Opcode: "file",
		Args:   []string{"7", "application/octet-stream", "upload.bin"},
	}, true)

	allowed, err := filter.Allow(ctx, rdpproxy.Instruction{
		Opcode: "blob",
		Args:   []string{"7", "not-base64!"},
	})
	if allowed || err == nil {
		t.Fatalf("malformed blob allowed = %v, err = %v; want fail closed", allowed, err)
	}
}

func requireChannelInstruction(
	t *testing.T,
	filter *channelFilter,
	ctx context.Context,
	instruction rdpproxy.Instruction,
	wantAllowed bool,
) {
	t.Helper()
	allowed, err := filter.Allow(ctx, instruction)
	if err != nil {
		t.Fatalf("filter %s %#v: %v", instruction.Opcode, instruction.Args, err)
	}
	if allowed != wantAllowed {
		t.Fatalf(
			"filter %s %#v allowed = %v, want %v",
			instruction.Opcode,
			instruction.Args,
			allowed,
			wantAllowed,
		)
	}
}

func assertChannelAudit(
	t *testing.T,
	audit *channelAuditStub,
	channel string,
	direction string,
	operation string,
	outcome string,
) {
	t.Helper()
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(audit.events))
	}
	event := audit.events[0]
	if event.AuditSessionID != "audit-session-1" ||
		event.Channel != channel ||
		event.Direction != direction ||
		event.Operation != operation ||
		event.Outcome != outcome {
		t.Fatalf("audit event = %#v", event)
	}
	serialized, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal audit event: %v", err)
	}
	if strings.Contains(string(serialized), "sensitive-channel-content") {
		t.Fatalf("channel audit leaked instruction content: %s", serialized)
	}
}

func validChannelInstruction(opcode string) rdpproxy.Instruction {
	switch opcode {
	case "clipboard":
		return rdpproxy.Instruction{
			Opcode: opcode,
			Args:   []string{"1", "text/plain"},
		}
	case "file":
		return rdpproxy.Instruction{
			Opcode: opcode,
			Args: []string{
				"1",
				"application/octet-stream",
				"sensitive-channel-content",
			},
		}
	case "filesystem":
		return rdpproxy.Instruction{
			Opcode: opcode,
			Args:   []string{"101", "sensitive-channel-content"},
		}
	default:
		panic("unsupported channel instruction: " + opcode)
	}
}
