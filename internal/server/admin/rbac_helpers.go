package admin

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"jianmen/internal/pkg/apiresp"
	"jianmen/internal/service"
)

func decodeRBACJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		slog.Default().Warn("decode RBAC request failed", "error", err)
		apiresp.WriteError(w, http.StatusBadRequest, apiresp.CodeValidation, "invalid RBAC request", nil, apiresp.RequestID(r.Context()))
		return false
	}
	return true
}

func rbacIDFromPath(path, prefix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	id := strings.TrimSpace(strings.TrimPrefix(path, prefix))
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

func writeRBACServiceError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		apiresp.WriteError(w, http.StatusInternalServerError, apiresp.CodeInternal, "RBAC operation failed", nil, apiresp.RequestID(r.Context()))
		return
	}
	slog.Default().Warn("RBAC service operation failed", "error", err)

	switch {
	case errors.Is(err, service.ErrUserNotFound), errors.Is(err, service.ErrRoleNotFound), errors.Is(err, service.ErrPermissionNotFound), errors.Is(err, service.ErrRoleBindingNotFound):
		apiresp.WriteError(w, http.StatusNotFound, apiresp.CodeNotFound, "not found", nil, apiresp.RequestID(r.Context()))
	case errors.Is(err, service.ErrBuiltinRole):
		apiresp.WriteError(w, http.StatusConflict, apiresp.CodeConflict, "built-in role cannot be modified", nil, apiresp.RequestID(r.Context()))
	case errors.Is(err, service.ErrRoleConflict), errors.Is(err, service.ErrPermissionConflict), errors.Is(err, service.ErrRoleBindingConflict):
		apiresp.WriteError(w, http.StatusConflict, apiresp.CodeConflict, "RBAC resource already exists", nil, apiresp.RequestID(r.Context()))
	case errors.Is(err, service.ErrInvalidRole), errors.Is(err, service.ErrInvalidPermission):
		apiresp.WriteError(w, http.StatusBadRequest, apiresp.CodeValidation, "invalid RBAC request", nil, apiresp.RequestID(r.Context()))
	default:
		apiresp.WriteError(w, http.StatusInternalServerError, apiresp.CodeInternal, "RBAC operation failed", nil, apiresp.RequestID(r.Context()))
	}
}
