package webrdp

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"jianmen/internal/model"
	"jianmen/internal/proxy/rdpproxy"
	"jianmen/internal/service"
)

const maxWebSocketInstruction = 8 << 20

type relayResult struct {
	source string
	err    error
}

func (h *Handler) relay(
	ctx context.Context,
	websocketConn *websocket.Conn,
	proxySession io.ReadWriteCloser,
	auditSessionID string,
	policy service.WebRDPChannelPolicy,
) error {
	websocketConn.SetReadLimit(maxWebSocketInstruction)
	results := make(chan relayResult, 2)
	inputFilter := newChannelFilter(
		auditSessionID, "browser_to_remote", policy, h.audit,
	)
	outputFilter := newChannelFilter(
		auditSessionID, "remote_to_browser", policy, h.audit,
	)

	go func() {
		results <- relayResult{
			source: "browser",
			err:    relayBrowserToGuacd(ctx, websocketConn, proxySession, inputFilter),
		}
	}()
	go func() {
		results <- relayResult{
			source: "guacd",
			err:    relayGuacdToBrowser(ctx, proxySession, websocketConn, outputFilter),
		}
	}()

	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = proxySession.Close()
			_ = websocketConn.Close()
		})
	}
	select {
	case <-ctx.Done():
		closeBoth()
		return ctx.Err()
	case result := <-results:
		if result.source == "browser" && expectedRelayError(result.err) {
			timer := time.NewTimer(50 * time.Millisecond)
			select {
			case guacdResult := <-results:
				if guacdResult.err != nil &&
					!expectedRelayError(guacdResult.err) {
					result = guacdResult
				}
			case <-ctx.Done():
				timer.Stop()
				closeBoth()
				return ctx.Err()
			case <-timer.C:
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		if result.err != nil && !expectedRelayError(result.err) {
			writeWebSocketClose(websocketConn, result.err)
		}
		closeBoth()
		return result.err
	}
}

func relayBrowserToGuacd(
	ctx context.Context,
	conn *websocket.Conn,
	target io.Writer,
	filter *channelFilter,
) error {
	encoder := rdpproxy.NewEncoder(target)
	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
			continue
		}
		decoder := rdpproxy.NewDecoder(bytes.NewReader(payload))
		for {
			instruction, decodeErr := decoder.Decode()
			if errors.Is(decodeErr, io.EOF) {
				break
			}
			if decodeErr != nil {
				return fmt.Errorf("decode browser Guacamole instruction: %w", decodeErr)
			}
			allowed, filterErr := filter.Allow(ctx, instruction)
			if filterErr != nil {
				return filterErr
			}
			if allowed {
				if err := encoder.Encode(instruction); err != nil {
					return err
				}
			}
		}
	}
}

func relayGuacdToBrowser(
	ctx context.Context,
	source io.Reader,
	conn *websocket.Conn,
	filter *channelFilter,
) error {
	decoder := rdpproxy.NewDecoder(source)
	for {
		instruction, err := decoder.Decode()
		if err != nil {
			return err
		}
		allowed, err := filter.Allow(ctx, instruction)
		if err != nil {
			return err
		}
		if !allowed {
			continue
		}
		var encoded bytes.Buffer
		if err := rdpproxy.NewEncoder(&encoded).Encode(instruction); err != nil {
			return err
		}
		if err := conn.WriteMessage(websocket.TextMessage, encoded.Bytes()); err != nil {
			return err
		}
		if err := guacdInstructionError(instruction); err != nil {
			return err
		}
	}
}

type channelEventWriter interface {
	CreateAuditRDPChannelEvent(ctx context.Context, event *model.AuditRDPChannelEvent) error
}

type channelFilter struct {
	sessionID   string
	direction   string
	policy      service.WebRDPChannelPolicy
	audit       channelEventWriter
	blocked     map[string]struct{}
	streams     map[string]channelStream
	auditEvents int
}

type channelStream struct {
	channel string
	bytes   int64
}

func newChannelFilter(
	sessionID string,
	direction string,
	policy service.WebRDPChannelPolicy,
	audit channelEventWriter,
) *channelFilter {
	return &channelFilter{
		sessionID: sessionID, direction: direction, policy: policy,
		audit: audit, blocked: make(map[string]struct{}),
		streams: make(map[string]channelStream),
	}
}

func (f *channelFilter) Allow(
	ctx context.Context,
	instruction rdpproxy.Instruction,
) (bool, error) {
	switch instruction.Opcode {
	case "blob":
		return f.allowBlob(instruction)
	case "end":
		return f.allowEnd(ctx, instruction)
	}

	channel, allowed, relevant, denialReason := f.channelDecision(
		instruction.Opcode,
	)
	if !relevant {
		return true, nil
	}
	if err := validateChannelInstruction(instruction); err != nil {
		return false, err
	}

	streamID, startsStream := channelStreamID(instruction)
	if startsStream {
		if _, blocked := f.blocked[streamID]; blocked {
			return false, fmt.Errorf(
				"RDP channel stream %q was reused before end",
				streamID,
			)
		}
		if _, active := f.streams[streamID]; active {
			return false, fmt.Errorf(
				"RDP channel stream %q was reused before end",
				streamID,
			)
		}
		if len(f.blocked)+len(f.streams) >= maxTrackedChannelStreams {
			return false, fmt.Errorf(
				"RDP channel stream limit of %d exceeded",
				maxTrackedChannelStreams,
			)
		}
	}

	outcome := "allowed"
	reason := ""
	if !allowed {
		outcome = "denied"
		reason = denialReason
		if reason == "" {
			reason = "channel disabled by effective policy"
		}
	}
	if err := f.writeAuditEvent(
		ctx,
		channel,
		instruction.Opcode,
		outcome,
		reason,
		0,
	); err != nil {
		return false, err
	}

	if startsStream {
		if allowed {
			f.streams[streamID] = channelStream{channel: channel}
		} else {
			f.blocked[streamID] = struct{}{}
		}
	}
	return allowed, nil
}

func (f *channelFilter) allowBlob(
	instruction rdpproxy.Instruction,
) (bool, error) {
	if len(instruction.Args) != 2 {
		return false, errors.New(
			"RDP blob instruction must contain a stream ID and data",
		)
	}
	streamID := firstArg(instruction)
	if err := validateGuacamoleIndex("blob stream", streamID); err != nil {
		return false, err
	}
	if _, blocked := f.blocked[streamID]; blocked {
		return false, nil
	}
	stream, tracked := f.streams[streamID]
	if !tracked {
		return true, nil
	}
	size, err := decodedBlobSize(instruction.Args[1])
	if err != nil {
		return false, fmt.Errorf(
			"decode RDP blob for stream %q: %w",
			streamID,
			err,
		)
	}
	total := stream.bytes + size
	if total < stream.bytes {
		return false, fmt.Errorf(
			"RDP channel stream %q byte count overflow",
			streamID,
		)
	}
	stream.bytes = total
	f.streams[streamID] = stream
	return true, nil
}

func (f *channelFilter) allowEnd(
	ctx context.Context,
	instruction rdpproxy.Instruction,
) (bool, error) {
	if len(instruction.Args) != 1 {
		return false, errors.New(
			"RDP end instruction must contain exactly one stream ID",
		)
	}
	streamID := firstArg(instruction)
	if err := validateGuacamoleIndex("end stream", streamID); err != nil {
		return false, err
	}
	if _, blocked := f.blocked[streamID]; blocked {
		delete(f.blocked, streamID)
		return false, nil
	}
	stream, tracked := f.streams[streamID]
	if !tracked {
		return true, nil
	}
	if err := f.writeAuditEvent(
		ctx,
		stream.channel,
		instruction.Opcode,
		"allowed",
		"",
		stream.bytes,
	); err != nil {
		return false, err
	}
	delete(f.streams, streamID)
	return true, nil
}

func (f *channelFilter) writeAuditEvent(
	ctx context.Context,
	channel string,
	operation string,
	outcome string,
	reason string,
	bytes int64,
) error {
	if f.auditEvents >= maxChannelAuditEvents {
		return fmt.Errorf(
			"RDP channel audit event limit of %d exceeded",
			maxChannelAuditEvents,
		)
	}
	event := &model.AuditRDPChannelEvent{
		AuditSessionID: f.sessionID,
		Timestamp:      time.Now().UTC(),
		Channel:        channel,
		Direction:      f.direction,
		Operation:      operation,
		Bytes:          bytes,
		Outcome:        outcome,
		Reason:         reason,
	}
	if err := f.audit.CreateAuditRDPChannelEvent(ctx, event); err != nil {
		return fmt.Errorf("record RDP channel event: %w", err)
	}
	f.auditEvents++
	return nil
}

func (f *channelFilter) channelDecision(
	opcode string,
) (string, bool, bool, string) {
	browserToRemote := f.direction == "browser_to_remote"
	switch opcode {
	case "clipboard":
		if browserToRemote {
			return "clipboard", f.policy.ClipboardWrite, true, ""
		}
		return "clipboard", f.policy.ClipboardRead, true, ""
	case "file":
		if browserToRemote {
			return "file", f.policy.FileUpload, true, ""
		}
		return "file", f.policy.FileDownload, true, ""
	case "put":
		if !browserToRemote {
			return "file", false, true,
				"put is only valid from browser to remote"
		}
		return "file", f.policy.FileUpload, true, ""
	case "get":
		if !browserToRemote {
			return "file", false, true,
				"get is only valid from browser to remote"
		}
		return "file", f.policy.FileDownload, true, ""
	case "body":
		if browserToRemote {
			return "file", false, true,
				"body is only valid from remote to browser"
		}
		return "file", f.policy.FileDownload, true, ""
	case "filesystem":
		if browserToRemote {
			return "drive", false, true,
				"filesystem is only valid from remote to browser"
		}
		return "drive", f.policy.DriveMapping, true, ""
	default:
		return "", true, false, ""
	}
}

func channelStreamID(instruction rdpproxy.Instruction) (string, bool) {
	switch instruction.Opcode {
	case "clipboard", "file":
		return firstArg(instruction), true
	case "put", "body":
		if len(instruction.Args) < 2 {
			return "", true
		}
		return instruction.Args[1], true
	default:
		return "", false
	}
}

func decodedBlobSize(encoded string) (int64, error) {
	decoder := base64.NewDecoder(
		base64.StdEncoding.Strict(),
		strings.NewReader(encoded),
	)
	size, err := io.Copy(io.Discard, decoder)
	if err != nil {
		return 0, err
	}
	return size, nil
}

func firstArg(instruction rdpproxy.Instruction) string {
	if len(instruction.Args) == 0 {
		return ""
	}
	return instruction.Args[0]
}
