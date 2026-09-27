package systemsettings

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"jianmen/internal/pkg/apiresp"
	"jianmen/internal/service"
)

const maxRequestBody = 64 << 10

type Subject struct {
	UserID   string
	Username string
}

type SettingsService interface {
	GetState(ctx context.Context) (service.SystemSettingsState, error)
	Update(
		ctx context.Context,
		input service.SystemSettingsUpdate,
	) (service.SystemSettingsState, error)
	ListRevisions(ctx context.Context, limit int) ([]service.SystemSettingsRevision, error)
}

type DiagnosticsService interface {
	Infrastructure() service.SystemSettingsRuntimeInfrastructure
	TestGuacd(ctx context.Context) service.SystemSettingsDiagnosticResult
	TestObjectStorage(ctx context.Context) service.SystemSettingsDiagnosticResult
}

type Handler struct {
	settings    SettingsService
	diagnostics DiagnosticsService
}

func New(settings SettingsService, diagnostics DiagnosticsService) (*Handler, error) {
	if settings == nil || diagnostics == nil {
		return nil, errors.New("system settings handler dependencies are required")
	}
	return &Handler{settings: settings, diagnostics: diagnostics}, nil
}

func (h *Handler) Collection(w http.ResponseWriter, r *http.Request, subject Subject) {
	switch r.Method {
	case http.MethodGet:
		h.get(w, r)
	case http.MethodPut:
		h.update(w, r, subject)
	default:
		w.Header().Set("Allow", "GET, PUT")
		writeError(w, r, http.StatusMethodNotAllowed, apiresp.CodeMethodNotAllowed, "method not allowed")
	}
}

func (h *Handler) Revisions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, r, http.StatusMethodNotAllowed, apiresp.CodeMethodNotAllowed, "method not allowed")
		return
	}
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(w, r, http.StatusBadRequest, apiresp.CodeValidation, "limit must be between 1 and 100")
			return
		}
		limit = parsed
	}
	revisions, err := h.settings.ListRevisions(r.Context(), limit)
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	items := make([]revisionResponse, 0, len(revisions))
	for _, revision := range revisions {
		items = append(items, mapRevision(revision))
	}
	writeJSON(w, r, http.StatusOK, revisionListResponse{Items: items})
}

func (h *Handler) Diagnostic(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, r, http.StatusMethodNotAllowed, apiresp.CodeMethodNotAllowed, "method not allowed")
		return
	}
	var result service.SystemSettingsDiagnosticResult
	switch name {
	case "guacd":
		result = h.diagnostics.TestGuacd(r.Context())
	case "object-storage":
		result = h.diagnostics.TestObjectStorage(r.Context())
	default:
		writeError(w, r, http.StatusNotFound, apiresp.CodeNotFound, "diagnostic not found")
		return
	}
	if !result.OK {
		apiresp.WriteError(
			w,
			http.StatusBadGateway,
			apiresp.CodeBadGateway,
			result.Message,
			map[string]any{"ok": false, "latency_ms": result.LatencyMS},
			apiresp.RequestID(r.Context()),
		)
		return
	}
	writeJSON(w, r, http.StatusOK, diagnosticResponse{
		OK: result.OK, Message: result.Message, LatencyMS: result.LatencyMS,
	})
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	state, err := h.settings.GetState(r.Context())
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, h.mapState(state))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, subject Subject) {
	var request updateRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, r, http.StatusBadRequest, apiresp.CodeValidation, "invalid system settings request")
		return
	}
	if request.Settings == nil || request.ExpectedRevision == nil {
		writeError(w, r, http.StatusBadRequest, apiresp.CodeValidation, "settings and expected_revision are required")
		return
	}
	settings, err := request.Settings.toService()
	if err != nil {
		writeError(w, r, http.StatusBadRequest, apiresp.CodeValidation, err.Error())
		return
	}
	state, err := h.settings.Update(r.Context(), service.SystemSettingsUpdate{
		Settings:         settings,
		ExpectedRevision: *request.ExpectedRevision,
		ConfirmRisk:      request.ConfirmRisk,
		Actor: service.SystemSettingsActor{
			ID: subject.UserID, Username: subject.Username,
		},
	})
	if err != nil {
		writeServiceError(w, r, err)
		return
	}
	writeJSON(w, r, http.StatusOK, h.mapState(state))
}

func (h *Handler) mapState(state service.SystemSettingsState) stateResponse {
	return stateResponse{
		Desired:           mapValues(state.Desired),
		Effective:         mapValues(state.Effective),
		Revision:          state.Revision,
		EffectiveRevision: state.EffectiveRevision,
		PendingRestart:    state.PendingRestart,
		UpdatedBy:         state.UpdatedBy,
		UpdatedAt:         timePointer(state.UpdatedAt),
		AppliedAt:         state.AppliedAt,
		Infrastructure:    mapInfrastructure(h.diagnostics.Infrastructure()),
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidSystemSettings):
		writeError(w, r, http.StatusBadRequest, apiresp.CodeValidation, err.Error())
	case errors.Is(err, service.ErrSystemSettingsRiskConfirmationRequired):
		writeError(w, r, http.StatusPreconditionFailed, apiresp.CodePreconditionFailed, err.Error())
	case errors.Is(err, service.ErrSystemSettingsRevisionConflict):
		writeError(w, r, http.StatusConflict, apiresp.CodeConflict, err.Error())
	case errors.Is(err, service.ErrSystemSettingsNotBootstrapped):
		writeError(w, r, http.StatusServiceUnavailable, apiresp.CodeServiceUnavailable, err.Error())
	default:
		writeError(w, r, http.StatusInternalServerError, apiresp.CodeInternal, "system settings operation failed")
	}
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, value any) {
	apiresp.Write(w, status, value, apiresp.RequestID(r.Context()))
}

func writeError(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	code string,
	message string,
) {
	apiresp.WriteError(w, status, code, message, nil, apiresp.RequestID(r.Context()))
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}
