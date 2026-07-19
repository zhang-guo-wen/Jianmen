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

func TestChannelFilterRejectsControlInstructionsInWrongDirection(t *testing.T) {
	tests := []struct {
		name        string
		direction   string
		instruction rdpproxy.Instruction
	}{
		{
			name:      "put from remote",
			direction: "remote_to_browser",
			instruction: rdpproxy.Instruction{
				Opcode: "put",
				Args: []string{
					"101", "1",
					"application/octet-stream", "upload.bin",
				},
			},
		},
		{
			name:      "get from remote",
			direction: "remote_to_browser",
			instruction: rdpproxy.Instruction{
				Opcode: "get",
				Args:   []string{"101", "download.bin"},
			},
		},
		{
			name:      "filesystem from browser",
			direction: "browser_to_remote",
			instruction: rdpproxy.Instruction{
				Opcode: "filesystem",
				Args:   []string{"101", "drive"},
			},
		},
		{
			name:      "body from browser",
			direction: "browser_to_remote",
			instruction: rdpproxy.Instruction{
				Opcode: "body",
				Args: []string{
					"101", "1",
					"application/octet-stream", "download.bin",
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			audit := &channelAuditStub{}
			filter := newChannelFilter(
				"audit-session-1",
				test.direction,
				service.WebRDPChannelPolicy{
					FileUpload:   true,
					FileDownload: true,
					DriveMapping: true,
				},
				audit,
			)
			allowed, err := filter.Allow(
				context.Background(),
				test.instruction,
			)
			if err != nil {
				t.Fatalf("filter instruction: %v", err)
			}
			if allowed {
				t.Fatal("instruction was allowed in an invalid direction")
			}
			if len(audit.events) != 1 ||
				audit.events[0].Outcome != "denied" ||
				!strings.Contains(audit.events[0].Reason, "only valid") {
				t.Fatalf("audit event = %#v, want invalid-direction denial", audit.events)
			}
		})
	}
}

func TestChannelFilterRejectsMalformedStreamInstructions(t *testing.T) {
	tests := []rdpproxy.Instruction{
		{Opcode: "blob"},
		{Opcode: "blob", Args: []string{"stream-1"}},
		{Opcode: "blob", Args: []string{" ", "YQ=="}},
		{Opcode: "blob", Args: []string{"stream-1", "YQ==", "extra"}},
		{Opcode: "end"},
		{Opcode: "end", Args: []string{" "}},
		{Opcode: "end", Args: []string{"stream-1", "extra"}},
		{Opcode: "clipboard", Args: []string{"stream-1"}},
		{Opcode: "file", Args: []string{"stream-1", "application/octet-stream"}},
		{
			Opcode: "put",
			Args:   []string{"object-1", "stream-1", "application/octet-stream"},
		},
		{Opcode: "get", Args: []string{"object-1"}},
		{
			Opcode: "body",
			Args:   []string{"object-1", "stream-1", "application/octet-stream"},
		},
		{Opcode: "filesystem", Args: []string{"object-1"}},
	}

	for _, instruction := range tests {
		t.Run(
			instruction.Opcode+"/"+strings.Join(instruction.Args, "_"),
			func(t *testing.T) {
				filter := newChannelFilter(
					"audit-session-1",
					"browser_to_remote",
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
					instruction,
				)
				if allowed || err == nil {
					t.Fatalf(
						"malformed instruction allowed = %v, err = %v; want fail closed",
						allowed,
						err,
					)
				}
			},
		)
	}
}

func TestChannelFilterAppliesDownloadPolicyToBodyStream(t *testing.T) {
	ctx := context.Background()
	body := rdpproxy.Instruction{
		Opcode: "body",
		Args: []string{
			"101", "17",
			"application/octet-stream", "secret-download.bin",
		},
	}

	t.Run("denied", func(t *testing.T) {
		audit := &channelAuditStub{}
		filter := newChannelFilter(
			"audit-session-1",
			"remote_to_browser",
			service.WebRDPChannelPolicy{},
			audit,
		)
		requireChannelInstruction(t, filter, ctx, body, false)
		requireChannelInstruction(t, filter, ctx, rdpproxy.Instruction{
			Opcode: "blob",
			Args:   []string{"17", "c2VjcmV0"},
		}, false)
		requireChannelInstruction(t, filter, ctx, rdpproxy.Instruction{
			Opcode: "end",
			Args:   []string{"17"},
		}, false)
		assertChannelAudit(
			t,
			audit,
			"file",
			"remote_to_browser",
			"body",
			"denied",
		)
	})

	t.Run("allowed and counted", func(t *testing.T) {
		audit := &channelAuditStub{}
		filter := newChannelFilter(
			"audit-session-1",
			"remote_to_browser",
			service.WebRDPChannelPolicy{FileDownload: true},
			audit,
		)
		requireChannelInstruction(t, filter, ctx, body, true)
		requireChannelInstruction(t, filter, ctx, rdpproxy.Instruction{
			Opcode: "blob",
			Args:   []string{"17", "YWJjZA=="},
		}, true)
		requireChannelInstruction(t, filter, ctx, rdpproxy.Instruction{
			Opcode: "end",
			Args:   []string{"17"},
		}, true)

		if len(audit.events) != 2 ||
			audit.events[1].Operation != "end" ||
			audit.events[1].Bytes != 4 {
			t.Fatalf("body stream audit events = %#v", audit.events)
		}
		encoded, err := json.Marshal(audit.events)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"secret-download.bin", "YWJjZA=="} {
			if strings.Contains(string(encoded), secret) {
				t.Fatalf("body stream audit leaked %q: %s", secret, encoded)
			}
		}
	})
}

func TestGuacdErrorInstructionFailsAuditWithoutPersistingMessage(t *testing.T) {
	instruction := rdpproxy.Instruction{
		Opcode: "error",
		Args:   []string{"authentication failed for secret-user", "769"},
	}
	err := guacdInstructionError(instruction)
	var protocolErr *guacdProtocolError
	if !errors.As(err, &protocolErr) || protocolErr.status != 769 {
		t.Fatalf("guacdInstructionError() = %v, want status 769", err)
	}
	if strings.Contains(err.Error(), "secret-user") {
		t.Fatalf("guacd error leaked arbitrary upstream message: %v", err)
	}

	outcome, code, message := relayOutcome(err, context.Background())
	if outcome != model.AuditOutcomeFailed ||
		code != "guacd_error" ||
		!strings.Contains(message, "769") {
		t.Fatalf(
			"relay outcome = (%q, %q, %q), want failed guacd error",
			outcome,
			code,
			message,
		)
	}

	for _, malformed := range []rdpproxy.Instruction{
		{Opcode: "error"},
		{Opcode: "error", Args: []string{"message"}},
		{Opcode: "error", Args: []string{"message", "not-a-code"}},
		{Opcode: "error", Args: []string{"message", "-1"}},
	} {
		if malformedErr := guacdInstructionError(malformed); malformedErr == nil {
			t.Fatalf("malformed guacd error was accepted: %#v", malformed)
		}
	}
}
