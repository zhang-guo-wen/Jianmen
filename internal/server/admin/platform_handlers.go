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

// platformAccountPathParts extracts the ID and optional child segment from
// /api/platform-accounts/{id}[/{child}].
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
	switch {
	case errors.Is(err, store.ErrPlatformAccountNotFound):
		apiresp.WriteError(w, http.StatusNotFound, apiresp.CodeNotFound, err.Error(), nil, apiresp.RequestID(r.Context()))
	case errors.Is(err, store.ErrPlatformShareNotFound):
		apiresp.WriteError(w, http.StatusNotFound, apiresp.CodeNotFound, err.Error(), nil, apiresp.RequestID(r.Context()))
	default:
		apiresp.WriteError(w, http.StatusBadRequest, apiresp.CodeValidation, err.Error(), nil, apiresp.RequestID(r.Context()))
	}
}

// handlePlatformAccounts handles GET (list) and POST (create) on /api/platform-accounts.
func (s *Server) handlePlatformAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionPlatformAccountView) {
			s.forbidden(w, r)
			return
		}
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
	userID := userIDFromRequest(r)
	isAdmin := s.isSuperAdmin(userID)

	var roleIDs []string
	if s.db != nil && !isAdmin {
		var roles []model.UserRole
		s.db.Where("user_id = ?", userID).Find(&roles)
		for _, ur := range roles {
			roleIDs = append(roleIDs, ur.RoleID)
		}
	}

	params := store.PlatformAccountListParams{
		Search:     r.URL.Query().Get("q"),
		OwnerID:    r.URL.Query().Get("owner_id"),
		Visibility: r.URL.Query().Get("visibility"),
		Platform:   r.URL.Query().Get("platform"),
		Category:   r.URL.Query().Get("category"),
		Page:       positiveIntRequestQuery(r, "page", 1),
		PageSize:   positiveIntRequestQuery(r, "page_size", defaultPageSize),
		UserID:     userID,
		RoleIDs:    roleIDs,
		IsAdmin:    isAdmin,
	}

	views, total, err := s.store.PlatformAccounts(params)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusOK, pageResponse{
		Items:    views,
		Total:    int(total),
		Page:     params.Page,
		PageSize: params.PageSize,
	})
}

func (s *Server) handleCreatePlatformAccount(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var payload struct {
		Name         string  `json:"name"`
		PlatformName string  `json:"platform_name"`
		URL          string  `json:"url"`
		Category     string  `json:"category"`
		Group        string  `json:"group"`
		Username     string  `json:"username"`
		Password     string  `json:"password"`
		TOTPSecret   string  `json:"totp_secret"`
		Remark       string  `json:"remark"`
		Visibility   string  `json:"visibility"`
		ExpiresAt    *string `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}

	if payload.Username == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "username is required")
		return
	}
	if payload.PlatformName == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "platform_name is required")
		return
	}

	userID := userIDFromRequest(r)

	acc := model.PlatformAccount{
		Name:         payload.Name,
		PlatformName: payload.PlatformName,
		URL:          payload.URL,
		Category:     payload.Category,
		GroupName:    payload.Group,
		Username:     payload.Username,
		Password:     model.NewEncryptedField(payload.Password),
		TOTPSecret:   model.NewEncryptedField(payload.TOTPSecret),
		Remark:       payload.Remark,
		OwnerID:      userID,
		Visibility:   payload.Visibility,
	}
	if payload.ExpiresAt != nil && *payload.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, *payload.ExpiresAt); err == nil {
			acc.ExpiresAt = &t
		}
	}

	view, err := s.store.AddPlatformAccount(acc)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusCreated, view)
}

// handlePlatformAccount handles GET/PUT/DELETE on /api/platform-accounts/{id}
// and sub-routes like /api/platform-accounts/{id}/password, /api/platform-accounts/{id}/shares.
func (s *Server) handlePlatformAccount(w http.ResponseWriter, r *http.Request) {
	id, child, ok := platformAccountPathParts(r.URL.Path)
	if !ok {
		s.writeErrorText(w, r, http.StatusNotFound, "not found")
		return
	}

	// Sub-routes
	switch child {
	case "password":
		s.handlePlatformAccountPassword(w, r, id)
		return
	case "shares":
		s.handlePlatformAccountShares(w, r, id)
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionPlatformAccountView) {
			s.forbidden(w, r)
			return
		}
		view, err := s.store.PlatformAccount(id)
		if err != nil {
			writePlatformStoreError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, view)
	case http.MethodPut:
		if !s.requirePermission(r, rbac.ActionPlatformAccountUpdate) {
			s.forbidden(w, r)
			return
		}
		s.handleUpdatePlatformAccount(w, r, id)
	case http.MethodDelete:
		if !s.requirePermission(r, rbac.ActionPlatformAccountDelete) {
			s.forbidden(w, r)
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
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var payload struct {
		Name         string  `json:"name"`
		PlatformName string  `json:"platform_name"`
		URL          string  `json:"url"`
		Category     string  `json:"category"`
		Group        string  `json:"group"`
		Username     string  `json:"username"`
		Password     string  `json:"password"`
		TOTPSecret   string  `json:"totp_secret"`
		Remark       string  `json:"remark"`
		Visibility   string  `json:"visibility"`
		Status       string  `json:"status"`
		ExpiresAt    *string `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}

	acc := model.PlatformAccount{
		Name:         payload.Name,
		PlatformName: payload.PlatformName,
		URL:          payload.URL,
		Category:     payload.Category,
		GroupName:    payload.Group,
		Username:     payload.Username,
		Password:     model.NewEncryptedField(payload.Password),
		TOTPSecret:   model.NewEncryptedField(payload.TOTPSecret),
		Remark:       payload.Remark,
		Visibility:   payload.Visibility,
		Status:       payload.Status,
	}
	if payload.ExpiresAt != nil && *payload.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, *payload.ExpiresAt); err == nil {
			acc.ExpiresAt = &t
		}
	}

	view, err := s.store.UpdatePlatformAccount(id, acc)
	if err != nil {
		writePlatformStoreError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, view)
}

// handlePlatformAccountPassword handles GET on /api/platform-accounts/{id}/password.
func (s *Server) handlePlatformAccountPassword(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !s.requirePermission(r, rbac.ActionPlatformAccountUse) {
		s.forbidden(w, r)
		return
	}

	password, err := s.store.GetPlatformAccountPassword(id)
	if err != nil {
		writePlatformStoreError(w, r, err)
		return
	}

	s.writeJSON(w, r, http.StatusOK, map[string]string{"password": password})
}

// handlePlatformAccountShares handles GET/POST on /api/platform-accounts/{id}/shares
// and DELETE on /api/platform-accounts/{id}/shares/{sid}.
func (s *Server) handlePlatformAccountShares(w http.ResponseWriter, r *http.Request, accountID string) {
	// Check if there's a share ID in the path: /api/platform-accounts/{id}/shares/{sid}
	// The full path was already parsed, so we need to re-parse
	trimmed := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/platform-accounts/"), "/")
	parts := strings.Split(trimmed, "/")

	// DELETE /api/platform-accounts/{id}/shares/{sid}
	if len(parts) == 3 && parts[1] == "shares" && r.Method == http.MethodDelete {
		shareID := parts[2]
		if !s.requirePermission(r, rbac.ActionPlatformAccountUpdate) {
			s.forbidden(w, r)
			return
		}
		if err := s.store.DeletePlatformAccountShare(accountID, shareID); err != nil {
			writePlatformStoreError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !s.requirePermission(r, rbac.ActionPlatformAccountView) {
			s.forbidden(w, r)
			return
		}
		shares, err := s.store.PlatformAccountShares(accountID)
		if err != nil {
			writePlatformStoreError(w, r, err)
			return
		}
		s.writeJSON(w, r, http.StatusOK, shares)
	case http.MethodPost:
		if !s.requirePermission(r, rbac.ActionPlatformAccountUpdate) {
			s.forbidden(w, r)
			return
		}
		s.handleCreatePlatformAccountShare(w, r, accountID)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleCreatePlatformAccountShare(w http.ResponseWriter, r *http.Request, accountID string) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var payload struct {
		UserID      string  `json:"user_id"`
		RoleID      string  `json:"role_id"`
		AccessLevel string  `json:"access_level"`
		ExpiresAt   *string `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}

	if payload.UserID == "" && payload.RoleID == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "user_id or role_id is required")
		return
	}

	share := model.PlatformAccountShare{
		PlatformAccountID: accountID,
		UserID:            payload.UserID,
		RoleID:            payload.RoleID,
		AccessLevel:       payload.AccessLevel,
	}
	if payload.ExpiresAt != nil && *payload.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, *payload.ExpiresAt); err == nil {
			share.ExpiresAt = &t
		}
	}

	view, err := s.store.AddPlatformAccountShare(share)
	if err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusCreated, view)
}
