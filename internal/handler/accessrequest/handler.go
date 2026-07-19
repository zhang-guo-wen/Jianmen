package accessrequest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
)

const maxRequestBody = 64 << 10

type Subject struct {
	UserID   string
	Username string
}

type RDPPlanner interface {
	Plan(ctx context.Context, userID, targetID string) (service.WebRDPPlan, error)
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
	access     *service.AccessRequestService
	planner    RDPPlanner
	authorizer Authorizer
}

func New(
	access *service.AccessRequestService,
	planner RDPPlanner,
	authorizer Authorizer,
) (*Handler, error) {
	if access == nil || planner == nil || authorizer == nil {
		return nil, errors.New("access request handler dependencies are required")
	}
	return &Handler{access: access, planner: planner, authorizer: authorizer}, nil
}

func (h *Handler) Collection(w http.ResponseWriter, r *http.Request, subject Subject) {
	switch r.Method {
	case http.MethodGet:
		h.list(w, r, subject)
	case http.MethodPost:
		h.create(w, r, subject)
	default:
		w.Header().Set("Allow", "GET, POST")
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) Item(w http.ResponseWriter, r *http.Request, subject Subject) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/access-requests/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		writeError(w, http.StatusNotFound, "access request not found")
		return
	}
	id := strings.TrimSpace(parts[0])
	if r.Method == http.MethodGet && len(parts) == 1 {
		h.get(w, r, subject, id)
		return
	}
	if r.Method != http.MethodPost || len(parts) != 2 {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	switch parts[1] {
	case "approve":
		h.decide(w, r, subject, id, true)
	case "reject":
		h.decide(w, r, subject, id, false)
	case "cancel":
		h.cancel(w, r, subject, id)
	default:
		writeError(w, http.StatusNotFound, "access request action not found")
	}
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, subject Subject) {
	var input struct {
		TargetID        string     `json:"target_id"`
		ResourceType    string     `json:"resource_type"`
		ResourceID      string     `json:"resource_id"`
		Protocol        string     `json:"protocol"`
		Actions         []string   `json:"actions"`
		Reason          string     `json:"reason"`
		AccessStartsAt  *time.Time `json:"access_starts_at"`
		AccessExpiresAt *time.Time `json:"access_expires_at"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	targetID := firstNonEmpty(input.TargetID, input.ResourceID)
	if input.ResourceType != "" && input.ResourceType != model.ResourceTypeHostAccount {
		writeError(w, http.StatusBadRequest, "resource_type must be host_account")
		return
	}
	if input.Protocol != "" && !strings.EqualFold(input.Protocol, "rdp") {
		writeError(w, http.StatusBadRequest, "protocol must be rdp")
		return
	}
	plan, err := h.planner.Plan(r.Context(), subject.UserID, targetID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	actions := plan.RequiredActions
	if len(input.Actions) != 0 {
		available := make(map[string]bool, len(plan.RequiredActions))
		for _, action := range plan.RequiredActions {
			available[action] = true
		}
		for _, action := range input.Actions {
			if !available[strings.TrimSpace(action)] {
				writeError(w, http.StatusForbidden, "requested RDP action is not authorized")
				return
			}
		}
		actions = input.Actions
	}
	created, err := h.access.Create(r.Context(), service.CreateAccessRequestInput{
		RequesterID: subject.UserID, ResourceType: model.ResourceTypeHostAccount,
		ResourceID: plan.TargetID, Protocol: "rdp", Actions: actions,
		Reason: input.Reason, AccessStartsAt: input.AccessStartsAt,
		AccessExpiresAt: input.AccessExpiresAt,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, subject Subject) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(firstNonEmpty(r.URL.Query().Get("size"), r.URL.Query().Get("page_size")))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	canReview, err := h.authorizer.AuthorizeConnection(
		r.Context(), subject.UserID, []string{rbac.ActionRDPApprovalManage}, "", "",
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "authorization failed")
		return
	}
	review := canReview && r.URL.Query().Get("requester_id") != subject.UserID
	params := service.AccessRequestListParams{
		RequesterID: subject.UserID, ResourceType: model.ResourceTypeHostAccount,
		Protocol: "rdp", ResourceID: strings.TrimSpace(r.URL.Query().Get("resource_id")),
		Status: strings.TrimSpace(r.URL.Query().Get("status")), Page: page, Size: size,
	}
	if review {
		params.RequesterID = ""
		params.Page = 1
		params.Size = 200
	}
	items, total, err := h.access.List(r.Context(), params)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if review {
		all := append([]service.AccessRequestView(nil), items...)
		for int64(params.Page*params.Size) < total {
			params.Page++
			next, _, listErr := h.access.List(r.Context(), params)
			if listErr != nil {
				writeServiceError(w, listErr)
				return
			}
			all = append(all, next...)
		}
		filtered := make([]service.AccessRequestView, 0, len(all))
		for _, item := range all {
			if item.RequesterID == subject.UserID {
				filtered = append(filtered, item)
				continue
			}
			allowed, authErr := h.canManage(r, subject.UserID, item.ResourceID)
			if authErr != nil {
				writeError(w, http.StatusInternalServerError, "authorization failed")
				return
			}
			if allowed {
				filtered = append(filtered, item)
			}
		}
		total = int64(len(filtered))
		start := (page - 1) * size
		if start > len(filtered) {
			start = len(filtered)
		}
		end := start + size
		if end > len(filtered) {
			end = len(filtered)
		}
		items = filtered[start:end]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "page": page, "page_size": size,
	})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, subject Subject, id string) {
	request, err := h.access.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if request.RequesterID != subject.UserID {
		allowed, authErr := h.canManage(r, subject.UserID, request.ResourceID)
		if authErr != nil || !allowed {
			writeError(w, http.StatusForbidden, "access request is not authorized")
			return
		}
	}
	writeJSON(w, http.StatusOK, request)
}

func (h *Handler) decide(
	w http.ResponseWriter,
	r *http.Request,
	subject Subject,
	id string,
	approved bool,
) {
	request, err := h.access.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	allowed, err := h.canManage(r, subject.UserID, request.ResourceID)
	if err != nil || !allowed {
		writeError(w, http.StatusForbidden, "access request approval is not authorized")
		return
	}
	var input struct {
		Remark string `json:"remark"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	decided, err := h.access.Decide(r.Context(), id, approved, subject.UserID, input.Remark)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, decided)
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request, subject Subject, id string) {
	cancelled, err := h.access.Cancel(r.Context(), id, subject.UserID)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cancelled)
}

func (h *Handler) canManage(r *http.Request, userID, resourceID string) (bool, error) {
	return h.authorizer.AuthorizeConnection(
		r.Context(), userID, []string{rbac.ActionRDPApprovalManage},
		model.ResourceTypeHostAccount, resourceID,
	)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, value any) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody)).Decode(value)
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrAccessRequestNotFound):
		writeError(w, http.StatusNotFound, "access request not found")
	case errors.Is(err, service.ErrAccessRequestConflict):
		writeError(w, http.StatusConflict, "access request is no longer pending")
	case errors.Is(err, service.ErrAccessRequestSelfDecision):
		writeError(w, http.StatusForbidden, "requesters cannot decide their own access requests")
	case errors.Is(err, service.ErrWebRDPNotAuthorized):
		writeError(w, http.StatusForbidden, "RDP access is not authorized")
	case errors.Is(err, service.ErrWebRDPUnavailable):
		writeError(w, http.StatusNotFound, "RDP target is unavailable")
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"code": status,
		"error": map[string]string{
			"code":    "ACCESS_REQUEST_ERROR",
			"message": message,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
