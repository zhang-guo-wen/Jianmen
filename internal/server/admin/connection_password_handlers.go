package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
)

const connectionPasswordTTL = 30 * time.Minute

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

	resourceType, actions, err := s.connectionPasswordTarget(request.TargetID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.writeErrorText(w, r, http.StatusNotFound, "target account not found or disabled")
			return
		}
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to look up target")
		return
	}
	allowed, err := s.authorizeAnyConnection(r.Context(), userID, actions, resourceType, request.TargetID)
	if err != nil || !allowed {
		s.forbidden(w, r)
		return
	}

	now := time.Now().UTC()
	issued, err := service.IssueConnectionPassword(now, connectionPasswordTTL)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to generate connection password")
		return
	}
	credential := model.ConnectionPassword{
		UserID:          userID,
		ResourceType:    resourceType,
		ResourceID:      request.TargetID,
		SecretHash:      issued.Hash,
		MySQLNativeHash: issued.MySQLNativeHash,
		ExpiresAt:       issued.ExpiresAt,
	}
	if err := s.store.CreateConnectionPassword(r.Context(), credential); err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to save connection password")
		return
	}
	s.writeJSON(w, r, http.StatusCreated, map[string]any{
		"password":           issued.Plaintext,
		"expires_at":         issued.ExpiresAt.Format(time.RFC3339),
		"expires_in_seconds": int(connectionPasswordTTL.Seconds()),
		"reusable":           true,
	})
}

func (s *Server) connectionPasswordTarget(targetID string) (string, []string, error) {
	var hostAccount model.HostAccount
	if err := s.db.Preload("Host").Where("id = ? AND status = ?", targetID, "active").First(&hostAccount).Error; err == nil {
		if hostAccount.Host.Status == "disabled" {
			return "", nil, gorm.ErrRecordNotFound
		}
		return model.ResourceTypeHostAccount, []string{rbac.ActionSessionConnect, rbac.ActionSFTPConnect}, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil, err
	}

	var databaseAccount model.DatabaseAccount
	if err := s.db.Preload("Instance").Where("id = ? AND status = ?", targetID, "active").First(&databaseAccount).Error; err != nil {
		return "", nil, err
	}
	if databaseAccount.Instance.Status == "disabled" {
		return "", nil, gorm.ErrRecordNotFound
	}
	return model.ResourceTypeDatabaseAccount, []string{rbac.ActionDBConnect}, nil
}
