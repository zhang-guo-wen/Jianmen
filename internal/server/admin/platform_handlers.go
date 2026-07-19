package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/pkg/apiresp"
	"jianmen/internal/rbac"
	"jianmen/internal/store"
)

func platformAccountPathParts(path string) (id, child string, ok bool) {
	trimmed := strings.Trim(strings.TrimPrefix(path, "/api/platform-accounts/"), "/")
	if trimmed == "" {
		return "", "", false
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) == 1 {
		return parts[0], "", true
	}
	if len(parts) == 2 {
		return parts[0], parts[1], true
	}
	return "", "", false
}

func writePlatformStoreError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrPlatformAccountNotFound) {
		apiresp.WriteError(w, http.StatusNotFound, apiresp.CodeNotFound, err.Error(), nil, apiresp.RequestID(r.Context()))
		return
	}
	apiresp.WriteError(w, http.StatusBadRequest, apiresp.CodeValidation, err.Error(), nil, apiresp.RequestID(r.Context()))
}

func (s *Server) handlePlatformAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListPlatformAccounts(w, r)
	case http.MethodPost:
		if !s.requirePermission(r, rbac.ActionPlatformAccountCreate) {
			s.forbidden(w, r)
			return
		}
		s.handleCreatePlatformAccount(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleListPlatformAccounts(w http.ResponseWriter, r *http.Request) {
	if !s.requireAuthenticatedUser(w, r) {
		return
	}
	params := store.PlatformAccountListParams{
		Search:   r.URL.Query().Get("q"),
		Platform: r.URL.Query().Get("platform"),
		Unpaged:  true,
	}
	views, _, err := s.store.PlatformAccounts(params)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	views, err = s.visiblePlatformAccounts(r, views)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	resp := paginateSlice(views, r, func(store.PlatformAccountView, string) bool { return true })
	s.writeJSON(w, r, http.StatusOK, resp)
}

func (s *Server) handleCreatePlatformAccount(w http.ResponseWriter, r *http.Request) {
	payload, ok := s.decodePlatformAccountPayload(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(payload.Username) == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "username is required")
		return
	}
	view, err := s.store.AddPlatformAccount(platformAccountModel(payload, userIDFromRequest(r)))
	if err != nil {
		writePlatformStoreError(w, r, err)
		return
	}
	if err := s.grantCreatedResource(r, model.ResourceTypePlatformAccount, view.ID); err != nil {
		if cleanupErr := s.store.DeletePlatformAccount(view.ID); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusCreated, view)
}

func (s *Server) handlePlatformAccount(w http.ResponseWriter, r *http.Request) {
	id, child, ok := platformAccountPathParts(r.URL.Path)
	if !ok {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}
	if child == "password" {
		s.handlePlatformAccountPassword(w, r, id)
		return
	}
	if child != "" {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !s.requireResourceAction(w, r, rbac.ActionPlatformAccountView, model.ResourceTypePlatformAccount, id) {
			return
		}
		view, err := s.store.PlatformAccount(id)
		if err != nil {
			writePlatformStoreError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, view)
	case http.MethodPut:
		if !s.requireResourceAction(w, r, rbac.ActionPlatformAccountUpdate, model.ResourceTypePlatformAccount, id) {
			return
		}
		s.handleUpdatePlatformAccount(w, r, id)
	case http.MethodDelete:
		if !s.requireResourceAction(w, r, rbac.ActionPlatformAccountDelete, model.ResourceTypePlatformAccount, id) {
			return
		}
		if err := s.store.DeletePlatformAccount(id); err != nil {
			writePlatformStoreError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleUpdatePlatformAccount(w http.ResponseWriter, r *http.Request, id string) {
	payload, ok := s.decodePlatformAccountPayload(w, r)
	if !ok {
		return
	}
	view, err := s.store.UpdatePlatformAccount(id, platformAccountModel(payload, ""))
	if err != nil {
		writePlatformStoreError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) handlePlatformAccountPassword(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requireResourceAction(w, r, rbac.ActionPlatformAccountUse, model.ResourceTypePlatformAccount, id) {
		return
	}
	password, err := s.store.GetPlatformAccountPassword(id)
	if err != nil {
		writePlatformStoreError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]string{"password": password})
}

type platformAccountPayload struct {
	Name         string  `json:"name"`
	PlatformName string  `json:"platform_name"`
	URL          string  `json:"url"`
	Group        string  `json:"group"`
	Username     string  `json:"username"`
	Password     string  `json:"password"`
	Remark       string  `json:"remark"`
	Status       string  `json:"status"`
	ExpiresAt    *string `json:"expires_at"`
}

func (s *Server) decodePlatformAccountPayload(w http.ResponseWriter, r *http.Request) (platformAccountPayload, bool) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var payload platformAccountPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return platformAccountPayload{}, false
	}
	return payload, true
}

func platformAccountModel(payload platformAccountPayload, ownerID string) model.PlatformAccount {
	platformName := strings.TrimSpace(payload.PlatformName)
	if platformName == "" {
		platformName = strings.TrimSpace(payload.URL)
	}
	account := model.PlatformAccount{
		Name:         strings.TrimSpace(payload.Name),
		PlatformName: platformName,
		URL:          strings.TrimSpace(payload.URL),
		GroupName:    strings.TrimSpace(payload.Group),
		Username:     strings.TrimSpace(payload.Username),
		Password:     model.NewEncryptedField(payload.Password),
		Remark:       strings.TrimSpace(payload.Remark),
		OwnerID:      ownerID,
		Status:       strings.TrimSpace(payload.Status),
	}
	if payload.ExpiresAt != nil && *payload.ExpiresAt != "" {
		if expiresAt, err := time.Parse(time.RFC3339, *payload.ExpiresAt); err == nil {
			account.ExpiresAt = &expiresAt
		}
	}
	return account
}
