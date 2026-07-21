package webrdp

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"jianmen/internal/model"
	"jianmen/internal/online"
	"jianmen/internal/proxy/rdpproxy"
	"jianmen/internal/service"
)

func (h *Handler) Connect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.config.Enabled {
		writeError(w, http.StatusServiceUnavailable, "Web RDP is disabled")
		return
	}
	if !sameOriginOrNoOrigin(r) {
		writeError(w, http.StatusForbidden, "Web RDP origin is not allowed")
		return
	}
	if strings.TrimSpace(r.Header.Get("Authorization")) != "" ||
		r.URL.Query().Get("token") != "" ||
		r.URL.Query().Get("access_token") != "" {
		writeError(w, http.StatusUnauthorized, "legacy credentials are not accepted")
		return
	}
	targetID := strings.TrimSpace(r.URL.Query().Get("target_id"))
	ticketSecret := strings.TrimSpace(r.URL.Query().Get("ticket"))
	if targetID == "" || ticketSecret == "" {
		writeError(w, http.StatusUnauthorized, "missing or invalid Web RDP ticket")
		return
	}
	width, height, dpi, err := displayOptions(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ticket, found, err := h.tickets.ConsumeScopedWebSocketTicket(
		r.Context(), ticketSecret, service.WebSocketPurposeRDP, targetID,
	)
	if err != nil {
		h.logger.Error("consume Web RDP ticket", "target_id", targetID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to consume Web RDP ticket")
		return
	}
	if !found || strings.TrimSpace(ticket.ConnectionID) == "" {
		writeError(w, http.StatusUnauthorized, "missing or invalid Web RDP ticket")
		return
	}
	identity, found, err := h.identity.FindIdentitySubject(r.Context(), ticket.UserID)
	if err != nil || !found {
		writeError(w, http.StatusUnauthorized, "invalid Web RDP session identity")
		return
	}
	connection, err := h.control.Authorize(r.Context(), identity.ID, targetID)
	if err != nil {
		h.recordDenied(
			r,
			AuthenticatedSubject{
				UserID:   identity.ID,
				Username: identity.Username,
			},
			ticket.BrowserSessionSubject,
			targetID,
			ticket.ConnectionID,
			err,
		)
		writeControlError(w, err)
		return
	}
	sessionCtx, cancelSession, deadlineReason := webRDPSessionContext(
		r.Context(),
		connection,
	)
	defer cancelSession()
	auditHandle, err := h.recording.Begin(sessionCtx, service.BeginRDPAuditInput{
		ID: ticket.ConnectionID, UserSessionID: ticket.SessionID,
		UserID: identity.ID, Username: identity.Username,
		Target: connection.Target, ClientIP: clientIP(r),
		Policy: connection.Plan.EffectivePolicy,
	})
	if err != nil {
		h.logger.Error("start Web RDP audit", "target_id", targetID, "error", err)
		writeError(w, http.StatusServiceUnavailable, "RDP audit recording is unavailable")
		return
	}
	recording := rdpproxy.Recording{}
	if auditHandle.Artifact != nil {
		recording = rdpproxy.Recording{
			Path: auditHandle.GuacdPath, Name: auditHandle.RecordingName,
			CreatePath: true, IncludeKeys: false,
		}
	}

	proxySession, _, err := h.connector.Connect(sessionCtx, rdpproxy.ConnectRequest{
		Hostname: connection.Target.Address, Port: connection.Target.Port,
		Username: connection.Target.Username, Password: connection.Target.Password,
		Domain: connection.Target.Domain, Security: connection.Target.Security,
		IgnoreCertificate:      connection.Target.IgnoreCertificate,
		CertificateFingerprint: connection.Target.CertificateFingerprint,
		Width:                  width, Height: height, DPI: dpi,
		ClientName: "Jianmen Web RDP", Timezone: "Asia/Shanghai",
		ImageMIMETypes: []string{"image/png", "image/jpeg"},
		ChannelPolicy: rdpproxy.ChannelPolicy{
			ClipboardRead:  connection.Plan.EffectivePolicy.ClipboardRead,
			ClipboardWrite: connection.Plan.EffectivePolicy.ClipboardWrite,
			FileUpload:     connection.Plan.EffectivePolicy.FileUpload,
			FileDownload:   connection.Plan.EffectivePolicy.FileDownload,
			DriveMapping:   connection.Plan.EffectivePolicy.DriveMapping,
		},
		DrivePath: auditHandle.GuacdDrivePath, DriveName: "Jianmen",
		CreateDrivePath: connection.Plan.EffectivePolicy.DriveMapping,
		Recording:       recording,
	})
	if err != nil {
		outcome, code, message := preRelayOutcome(
			err,
			sessionCtx,
			deadlineReason,
			"guacd_connect_failed",
		)
		h.finishAudit(auditHandle, outcome, code, message)
		writeError(w, http.StatusBadGateway, "failed to establish downstream RDP connection")
		return
	}

	websocketConn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		_ = proxySession.Close()
		h.finishAudit(auditHandle, model.AuditOutcomeFailed, "websocket_upgrade_failed", err.Error())
		h.logger.Warn("upgrade Web RDP websocket", "target_id", targetID, "error", err)
		return
	}
	if err := h.recording.Activate(sessionCtx, auditHandle); err != nil {
		_ = websocketConn.Close()
		_ = proxySession.Close()
		outcome, code, message := preRelayOutcome(
			err,
			sessionCtx,
			deadlineReason,
			"audit_activate_failed",
		)
		h.finishAudit(auditHandle, outcome, code, message)
		return
	}

	unregister := h.online.Register(online.Session{
		ID: auditHandle.Session.ID, AuditSessionID: auditHandle.Session.ID,
		ResourceType: model.ResourceTypeHostAccount, ResourceID: connection.Target.ID,
		AccountID: connection.Target.ID, Instance: connection.Target.HostName,
		Protocol: "rdp", ProtocolSubtype: "web-rdp",
		Account: connection.Target.Username, Operator: identity.Username,
		StartedAt: auditHandle.Session.StartedAt, HasReplay: auditHandle.Artifact != nil,
	}, func() {
		_ = websocketConn.Close()
		_ = proxySession.Close()
	})

	relayErr := h.relay(
		sessionCtx, websocketConn, proxySession, auditHandle.Session.ID,
		connection.Plan.EffectivePolicy,
	)
	unregister()
	_ = websocketConn.Close()
	_ = proxySession.Close()

	outcome, failureCode, failureMessage := relayOutcomeWithDeadline(
		relayErr,
		sessionCtx,
		deadlineReason,
	)
	h.finishAudit(auditHandle, outcome, failureCode, failureMessage)
	if relayErr != nil && !expectedRelayError(relayErr) {
		h.logger.Warn("Web RDP session ended", "session_id", auditHandle.Session.ID, "error", relayErr)
	}
}

func webRDPSessionContext(
	parent context.Context,
	connection service.WebRDPConnection,
) (context.Context, context.CancelFunc, string) {
	var deadline time.Time
	reason := ""
	choose := func(candidate *time.Time, candidateReason string) {
		if candidate == nil || candidate.IsZero() {
			return
		}
		value := candidate.UTC()
		if deadline.IsZero() || value.Before(deadline) {
			deadline = value
			reason = candidateReason
		}
	}
	choose(connection.Target.ExpiresAt, "account_expired")
	if deadline.IsZero() {
		ctx, cancel := context.WithCancel(parent)
		return ctx, cancel, ""
	}
	ctx, cancel := context.WithDeadline(parent, deadline)
	return ctx, cancel, reason
}

func (h *Handler) finishAudit(
	handle *service.RDPAuditHandle,
	outcome string,
	failureCode string,
	failureMessage string,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if err := h.recording.Finish(ctx, handle, outcome, failureCode, failureMessage); err != nil {
		h.logger.Error("finalize Web RDP audit", "session_id", handle.Session.ID, "error", err)
	}
}

func relayOutcome(err error, ctx context.Context) (string, string, string) {
	var guacdErr *guacdProtocolError
	switch {
	case err == nil || expectedRelayError(err):
		return model.AuditOutcomeSucceeded, "", ""
	case errors.As(err, &guacdErr):
		return model.AuditOutcomeFailed, "guacd_error", guacdErr.Error()
	case errors.Is(err, context.Canceled) || ctx.Err() != nil:
		return model.AuditOutcomeTerminated, "session_cancelled", ""
	default:
		return model.AuditOutcomeFailed, "relay_failed", err.Error()
	}
}

func relayOutcomeWithDeadline(
	err error,
	ctx context.Context,
	deadlineReason string,
) (string, string, string) {
	if deadlineReason != "" &&
		errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return model.AuditOutcomeTerminated, deadlineReason, ""
	}
	return relayOutcome(err, ctx)
}

func preRelayOutcome(
	err error,
	ctx context.Context,
	deadlineReason string,
	failureCode string,
) (string, string, string) {
	if deadlineReason != "" &&
		errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return model.AuditOutcomeTerminated, deadlineReason, ""
	}
	return model.AuditOutcomeFailed, failureCode, err.Error()
}

func expectedRelayError(err error) bool {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	if websocket.IsCloseError(
		err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseNoStatusReceived,
	) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "use of closed network connection") ||
		strings.Contains(message, "connection reset by peer")
}

func clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func writeWebSocketClose(conn *websocket.Conn, err error) {
	if conn == nil || err == nil {
		return
	}
	_ = conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "RDP proxy session ended"),
		time.Now().Add(time.Second),
	)
}
