package webrdp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"jianmen/internal/model"
	"jianmen/internal/objectstore"
	"jianmen/internal/online"
	"jianmen/internal/proxy/rdpproxy"
	"jianmen/internal/service"
	"jianmen/internal/store"
)

const (
	Path             = "/api/web-rdp"
	TicketPath       = "/api/web-rdp/tickets"
	maxTicketBody    = 64 << 10
	maxRDPDimension  = 8192
	defaultRDPWidth  = 1280
	defaultRDPHeight = 720
	defaultRDPDPI    = 96
)

type Config struct {
	Enabled        bool
	ConnectTimeout time.Duration
}

type AuthenticatedSubject struct {
	UserID   string
	Username string
}

type BrowserTickets interface {
	CreateScopedWebSocketTicket(
		ctx context.Context,
		subject service.BrowserSessionSubject,
		purpose string,
		targetID string,
		connectionID string,
	) (string, error)
	ConsumeScopedWebSocketTicket(
		ctx context.Context,
		secret string,
		purpose string,
		targetID string,
	) (service.WebSocketTicketSubject, bool, error)
}

type IdentityFinder interface {
	FindIdentitySubject(ctx context.Context, userID string) (service.IdentitySubject, bool, error)
}

type AuditRepository interface {
	service.RDPAuditRepository
	CreateAuditRDPChannelEvent(ctx context.Context, event *model.AuditRDPChannelEvent) error
	GetAuditSession(ctx context.Context, id string) (*model.AuditSession, error)
	ListAuditSessions(ctx context.Context, params store.AuditListParams) ([]store.AuditSessionView, int64, error)
	AuditArtifactBySession(ctx context.Context, sessionID, kind string) (model.AuditArtifact, error)
}

type Authorizer interface {
	AuthorizeConnection(
		ctx context.Context,
		userID string,
		actions []string,
		resourceType string,
		resourceID string,
	) (bool, error)
}

type Handler struct {
	config     Config
	tickets    BrowserTickets
	identity   IdentityFinder
	control    *service.WebRDPService
	recording  *service.RDPRecordingService
	connector  *rdpproxy.Connector
	audit      AuditRepository
	objects    objectstore.Store
	authorizer Authorizer
	online     *online.Registry
	logger     *slog.Logger
	upgrader   websocket.Upgrader
}

func New(
	config Config,
	tickets BrowserTickets,
	identity IdentityFinder,
	control *service.WebRDPService,
	recording *service.RDPRecordingService,
	connector *rdpproxy.Connector,
	audit AuditRepository,
	objects objectstore.Store,
	authorizer Authorizer,
	onlineSessions *online.Registry,
	logger *slog.Logger,
) (*Handler, error) {
	switch {
	case tickets == nil || identity == nil || control == nil || recording == nil:
		return nil, errors.New("Web RDP control dependencies are required")
	case connector == nil || audit == nil || objects == nil || authorizer == nil:
		return nil, errors.New("Web RDP proxy dependencies are required")
	case onlineSessions == nil || logger == nil:
		return nil, errors.New("Web RDP runtime dependencies are required")
	}
	return &Handler{
		config: config, tickets: tickets, identity: identity, control: control,
		recording: recording, connector: connector, audit: audit, objects: objects,
		authorizer: authorizer, online: onlineSessions, logger: logger,
		upgrader: websocket.Upgrader{CheckOrigin: sameOriginOrNoOrigin},
	}, nil
}

func (h *Handler) CreateTicket(
	w http.ResponseWriter,
	r *http.Request,
	subject AuthenticatedSubject,
	browser service.BrowserSessionSubject,
) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !h.config.Enabled {
		writeError(w, http.StatusServiceUnavailable, "Web RDP is disabled")
		return
	}
	var input struct {
		TargetID string `json:"target_id"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
		DPI      int    `json:"dpi"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxTicketBody)).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(input.TargetID) == "" ||
		!validDisplay(input.Width, input.Height, input.DPI) {
		writeError(w, http.StatusBadRequest, "invalid RDP target or display dimensions")
		return
	}
	plan, err := h.control.Plan(r.Context(), subject.UserID, input.TargetID)
	if err != nil {
		h.recordDenied(
			r,
			subject,
			browser,
			input.TargetID,
			"",
			err,
		)
		writeControlError(w, err, service.WebRDPPlan{})
		return
	}
	connection, err := h.control.Authorize(r.Context(), subject.UserID, input.TargetID)
	if err != nil {
		h.recordDenied(
			r,
			subject,
			browser,
			input.TargetID,
			"",
			err,
		)
		writeControlError(w, err, plan)
		return
	}
	connectionID := model.NewID()
	ticket, err := h.tickets.CreateScopedWebSocketTicket(
		r.Context(), browser, service.WebSocketPurposeRDP, input.TargetID, connectionID,
	)
	if err != nil {
		h.logger.Error("create Web RDP ticket", "target_id", input.TargetID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create Web RDP ticket")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"ticket": ticket, "target_id": connection.Plan.TargetID,
		"effective_policy":  connection.Plan.EffectivePolicy,
		"approval_id":       connection.Plan.AccessRequestID,
		"access_expires_at": connection.Plan.AccessExpiresAt,
	})
}

func (h *Handler) recordDenied(
	r *http.Request,
	subject AuthenticatedSubject,
	browser service.BrowserSessionSubject,
	targetID string,
	connectionID string,
	authorizationErr error,
) {
	if h == nil || h.recording == nil || r == nil {
		return
	}
	failureCode := ""
	failureMessage := ""
	switch {
	case errors.Is(authorizationErr, service.ErrWebRDPApprovalRequired):
		failureCode = "approval_required"
		failureMessage = "RDP approval is required"
	case errors.Is(authorizationErr, service.ErrWebRDPNotAuthorized):
		failureCode = "rbac_denied"
		failureMessage = "RDP access is not authorized"
	default:
		return
	}
	err := h.recording.RecordDenied(r.Context(), service.DeniedRDPAuditInput{
		ID: connectionID, UserSessionID: browser.SessionID,
		UserID: subject.UserID, Username: subject.Username,
		TargetID: targetID, ClientIP: clientIP(r),
		FailureCode: failureCode, FailureMessage: failureMessage,
	})
	if err != nil && h.logger != nil {
		h.logger.Error(
			"record denied Web RDP attempt",
			"target_id",
			targetID,
			"failure_code",
			failureCode,
			"error",
			err,
		)
	}
}

// TestConnection verifies that guacd accepts the server-owned RDP connection
// configuration. A successful guacd ready handshake does not prove that the
// target Windows host or account has authenticated; only a real proxied
// session can establish that with the stock guacd protocol.
func (h *Handler) TestConnection(ctx context.Context, target store.TargetConfig) error {
	if h == nil || !h.config.Enabled {
		return errors.New("Web RDP is disabled")
	}
	session, _, err := h.connector.Connect(ctx, rdpproxy.ConnectRequest{
		Hostname: target.Host, Port: target.Port,
		Username: target.Username, Password: target.Password, Domain: target.Domain,
		Security: target.RDPSecurity, IgnoreCertificate: target.RDPIgnoreCertificate,
		CertificateFingerprint: target.RDPCertFingerprints,
		Width:                  1024, Height: 768, DPI: 96,
		ImageMIMETypes: []string{"image/png"},
	})
	if err != nil {
		return err
	}
	return session.Close()
}

func displayOptions(r *http.Request) (int, int, int, error) {
	width, err := positiveQuery(r, "width", defaultRDPWidth)
	if err != nil {
		return 0, 0, 0, err
	}
	height, err := positiveQuery(r, "height", defaultRDPHeight)
	if err != nil {
		return 0, 0, 0, err
	}
	dpi, err := positiveQuery(r, "dpi", defaultRDPDPI)
	if err != nil || !validDisplay(width, height, dpi) {
		return 0, 0, 0, errors.New("invalid RDP display dimensions")
	}
	return width, height, dpi, nil
}

func positiveQuery(r *http.Request, key string, fallback int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, errors.New("invalid " + key)
	}
	return value, nil
}

func validDisplay(width, height, dpi int) bool {
	return width > 0 && width <= maxRDPDimension &&
		height > 0 && height <= maxRDPDimension &&
		dpi >= 48 && dpi <= 480
}

func writeControlError(w http.ResponseWriter, err error, plan service.WebRDPPlan) {
	switch {
	case errors.Is(err, service.ErrWebRDPApprovalRequired):
		writeAPIError(w, http.StatusConflict, "RDP_APPROVAL_REQUIRED", "RDP approval is required", map[string]any{
			"approval_required": true,
			"target_id":         plan.TargetID,
			"required_actions":  plan.RequiredActions,
		})
	case errors.Is(err, service.ErrWebRDPNotAuthorized):
		writeError(w, http.StatusForbidden, "RDP access is not authorized")
	case errors.Is(err, service.ErrWebRDPUnavailable):
		writeError(w, http.StatusNotFound, "RDP target is unavailable")
	default:
		writeError(w, http.StatusInternalServerError, "failed to authorize RDP access")
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeAPIError(w, status, "WEB_RDP_ERROR", message, nil)
}

func writeAPIError(w http.ResponseWriter, status int, code, message string, details any) {
	body := map[string]any{
		"code": status,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
	if details != nil {
		body["error"].(map[string]any)["details"] = details
	}
	writeJSON(w, status, body)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
