package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"jianmen/internal/service"
)

type connectionPasswordRequest struct {
	TargetID string `json:"target_id"`
}

func (s *Server) handleConnectionPasswords(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := userIDFromRequest(r)
	if userID == "" {
		s.writeErrorText(w, r, http.StatusUnauthorized, "user not authenticated")
		return
	}
	var request connectionPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	request.TargetID = strings.TrimSpace(request.TargetID)
	if request.TargetID == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "target_id is required")
		return
	}

	if s.connectionPassword == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "connection password service unavailable")
		return
	}
	issued, err := s.connectionPassword.Issue(
		r.Context(),
		service.ConnectionPasswordIssueRequest{
			UserID:   userID,
			TargetID: request.TargetID,
		},
	)
	if err != nil {
		s.writeConnectionPasswordServiceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, map[string]any{
		"password":           issued.Password,
		"expires_at":         issued.ExpiresAt.Format(time.RFC3339),
		"expires_in_seconds": issued.ExpiresInSeconds,
		"reusable":           issued.Reusable,
	})
}

func (s *Server) writeConnectionPasswordServiceError(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, service.ErrInvalidConnectionPasswordRequest):
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid connection password request")
	case errors.Is(err, service.ErrConnectionPasswordTargetNotFound):
		s.writeErrorText(w, r, http.StatusNotFound, "target account not found or disabled")
	case errors.Is(err, service.ErrConnectionPasswordForbidden),
		errors.Is(err, service.ErrConnectionPasswordAuthorization):
		s.forbidden(w, r)
	case errors.Is(err, service.ErrConnectionPasswordTargetLookup):
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to look up target")
	case errors.Is(err, service.ErrConnectionPasswordGeneration):
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to generate connection password")
	case errors.Is(err, service.ErrConnectionPasswordPersistence):
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to save connection password")
	default:
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to issue connection password")
	}
}
