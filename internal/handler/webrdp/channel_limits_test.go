package webrdp

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"jianmen/internal/proxy/rdpproxy"
	"jianmen/internal/service"
)

func TestChannelFilterRejectsNonCanonicalOrOversizedIDs(t *testing.T) {
	tests := []struct {
		name        string
		direction   string
		instruction rdpproxy.Instruction
	}{
		{
			name:      "clipboard leading zero",
			direction: "browser_to_remote",
			instruction: rdpproxy.Instruction{
				Opcode: "clipboard",
				Args:   []string{"01", "text/plain"},
			},
		},
		{
			name:      "file negative stream",
			direction: "browser_to_remote",
			instruction: rdpproxy.Instruction{
				Opcode: "file",
				Args:   []string{"-1", "application/octet-stream", "x"},
			},
		},
		{
			name:      "put nonnumeric object",
			direction: "browser_to_remote",
			instruction: rdpproxy.Instruction{
				Opcode: "put",
				Args:   []string{"object", "1", "application/octet-stream", "x"},
			},
		},
		{
			name:      "body oversized stream",
			direction: "remote_to_browser",
			instruction: rdpproxy.Instruction{
				Opcode: "body",
				Args:   []string{"1", "2147483648", "application/octet-stream", "x"},
			},
		},
		{
			name:      "filesystem signed object",
			direction: "remote_to_browser",
			instruction: rdpproxy.Instruction{
				Opcode: "filesystem",
				Args:   []string{"+1", "drive"},
			},
		},
		{
			name:      "blob oversized identifier",
			direction: "browser_to_remote",
			instruction: rdpproxy.Instruction{
				Opcode: "blob",
				Args:   []string{strings.Repeat("1", 1024), "YQ=="},
			},
		},
		{
			name:      "end whitespace",
			direction: "browser_to_remote",
			instruction: rdpproxy.Instruction{
				Opcode: "end",
				Args:   []string{" "},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filter := newChannelFilter(
				"audit-session-1",
				test.direction,
				service.WebRDPChannelPolicy{
					ClipboardRead:  true,
					ClipboardWrite: true,
					FileUpload:     true,
					FileDownload:   true,
					DriveMapping:   true,
				},
				&channelAuditStub{},
			)
			allowed, err := filter.Allow(
				context.Background(),
				test.instruction,
			)
			if allowed || err == nil ||
				!strings.Contains(err.Error(), "canonical") {
				t.Fatalf(
					"Allow() = (%v, %v), want canonical ID error",
					allowed,
					err,
				)
			}
		})
	}
}

func TestChannelFilterLimitsTrackedStreamsAcrossPolicies(t *testing.T) {
	ctx := context.Background()
	audit := &channelAuditStub{}
	filter := newChannelFilter(
		"audit-session-1",
		"browser_to_remote",
		service.WebRDPChannelPolicy{ClipboardWrite: true},
		audit,
	)

	for index := 0; index < maxTrackedChannelStreams; index++ {
		streamID := strconv.Itoa(index)
		instruction := rdpproxy.Instruction{
			Opcode: "file",
			Args: []string{
				streamID,
				"application/octet-stream",
				"x",
			},
		}
		wantAllowed := false
		if index%2 == 0 {
			instruction = rdpproxy.Instruction{
				Opcode: "clipboard",
				Args:   []string{streamID, "text/plain"},
			}
			wantAllowed = true
		}
		requireChannelInstruction(
			t,
			filter,
			ctx,
			instruction,
			wantAllowed,
		)
	}

	allowed, err := filter.Allow(ctx, rdpproxy.Instruction{
		Opcode: "clipboard",
		Args:   []string{strconv.Itoa(maxTrackedChannelStreams), "text/plain"},
	})
	if allowed || err == nil || !strings.Contains(err.Error(), "stream limit") {
		t.Fatalf("Allow() = (%v, %v), want stream limit error", allowed, err)
	}
	if got := len(filter.blocked) + len(filter.streams); got != maxTrackedChannelStreams {
		t.Fatalf("tracked streams = %d, want %d", got, maxTrackedChannelStreams)
	}
	if len(audit.events) != maxTrackedChannelStreams {
		t.Fatalf("audit events = %d, want %d", len(audit.events), maxTrackedChannelStreams)
	}
}

func TestChannelFilterLimitsAuditEvents(t *testing.T) {
	ctx := context.Background()
	audit := &channelAuditStub{}
	filter := newChannelFilter(
		"audit-session-1",
		"browser_to_remote",
		service.WebRDPChannelPolicy{ClipboardWrite: true},
		audit,
	)
	filter.auditEvents = maxChannelAuditEvents

	allowed, err := filter.Allow(ctx, rdpproxy.Instruction{
		Opcode: "clipboard",
		Args:   []string{"1", "text/plain"},
	})
	if allowed || err == nil || !strings.Contains(err.Error(), "audit event limit") {
		t.Fatalf("Allow() = (%v, %v), want audit event limit error", allowed, err)
	}
	if len(audit.events) != 0 || len(filter.streams) != 0 {
		t.Fatalf(
			"limit failure mutated state: events=%d streams=%d",
			len(audit.events),
			len(filter.streams),
		)
	}
}
