package admin

import (
	"crypto/rand"
	"encoding/json"
	"math/big"
	"net/http"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
)

// handleDBDatabases handles GET /api/db/instances/{id}/databases
func (s *Server) handleDBDatabases(w http.ResponseWriter, r *http.Request, instanceID string) {
	if !s.requirePermission(r, rbac.ActionDBProxyView) {
		s.forbidden(w, r)
		return
	}

	adminAccountID := strings.TrimSpace(r.URL.Query().Get("admin_account_id"))
	if adminAccountID == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "admin_account_id is required")
		return
	}

	var acct model.DatabaseAccount
	if err := s.db.Preload("Instance").First(&acct, "id = ? AND instance_id = ?", adminAccountID, instanceID).Error; err != nil {
		s.writeErrorText(w, r, http.StatusNotFound, "admin account not found")
		return
	}
	if acct.Status != "active" {
		s.writeErrorText(w, r, http.StatusBadRequest, "admin account is not active")
		return
	}
	if acct.ExpiresAt != nil && time.Now().UTC().After(*acct.ExpiresAt) {
		s.writeErrorText(w, r, http.StatusBadRequest, "admin account has expired")
		return
	}
	if acct.Instance.Status != "active" {
		s.writeErrorText(w, r, http.StatusBadRequest, "database instance is disabled")
		return
	}
	if acct.Instance.Protocol != "mysql" {
		s.writeErrorText(w, r, http.StatusBadRequest, "only mysql instances support auto-provisioning")
		return
	}

	dbs, err := service.ListMySQLDatabases(acct.Instance, acct)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, r, http.StatusOK, map[string]any{"databases": dbs})
}

// handleDBProvisionAccount handles POST /api/db/instances/{id}/provision-account
func (s *Server) handleDBProvisionAccount(w http.ResponseWriter, r *http.Request, instanceID string) {
	if !s.requirePermission(r, rbac.ActionDBProxyCreate) {
		s.forbidden(w, r)
		return
	}

	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var payload struct {
		AdminAccountID string            `json:"admin_account_id"`
		NewUsername    string            `json:"new_username"`
		Password       string            `json:"password"`
		Host           string            `json:"host"`
		Grants         []service.DBGrant `json:"grants"`
		Group          string            `json:"group"`
		Remark         string            `json:"remark"`
		ExpiresAt      *time.Time        `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, err.Error())
		return
	}
	payload.AdminAccountID = strings.TrimSpace(payload.AdminAccountID)
	payload.NewUsername = strings.TrimSpace(payload.NewUsername)
	payload.Host = strings.TrimSpace(payload.Host)

	if payload.AdminAccountID == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "admin_account_id is required")
		return
	}
	if payload.Host == "" {
		payload.Host = "%"
	}

	// Generate password
	generatedPassword := payload.Password
	if generatedPassword == "" {
		generatedPassword = generateRandomPassword(20)
	}

	// Generate username
	newUsername := payload.NewUsername
	if newUsername == "" {
		newUsername = "u_" + generateRandomPassword(8)
	}

	// Load admin credentials (must belong to this instance)
	var acct model.DatabaseAccount
	if err := s.db.Preload("Instance").First(&acct, "id = ? AND instance_id = ?", payload.AdminAccountID, instanceID).Error; err != nil {
		s.writeErrorText(w, r, http.StatusNotFound, "admin account not found for this instance")
		return
	}
	if acct.Status != "active" {
		s.writeErrorText(w, r, http.StatusBadRequest, "admin account is not active")
		return
	}
	if acct.ExpiresAt != nil && time.Now().UTC().After(*acct.ExpiresAt) {
		s.writeErrorText(w, r, http.StatusBadRequest, "admin account has expired")
		return
	}
	if acct.Instance.Status != "active" {
		s.writeErrorText(w, r, http.StatusBadRequest, "database instance is disabled")
		return
	}
	if acct.Instance.Protocol != "mysql" {
		s.writeErrorText(w, r, http.StatusBadRequest, "only mysql protocol is supported for provisioning")
		return
	}

	// Execute CREATE USER + GRANT on target MySQL
	if err := service.ProvisionMySQLAccount(acct.Instance, acct, newUsername, generatedPassword, payload.Host, payload.Grants); err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	// Register in the bastion host
	view, err := s.store.AddDatabaseAccount(instanceID, newUsername, generatedPassword, payload.Group, payload.Remark, payload.ExpiresAt)
	if err != nil {
		writeDBStoreError(w, r, err)
		return
	}

	s.writeJSON(w, r, http.StatusCreated, map[string]any{
		"ok":                 true,
		"account":            view,
		"generated_password": generatedPassword,
	})
}

func generateRandomPassword(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!#$%&()*+,-./:;<=>?@[]^_"
	result := make([]byte, length)
	for i := range result {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		result[i] = chars[n.Int64()]
	}
	return string(result)
}
