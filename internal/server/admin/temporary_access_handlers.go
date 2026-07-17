package admin

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
	"jianmen/internal/util"
)

const maxTemporaryAuthorizationDuration = 7 * 24 * time.Hour

type temporaryAuthorizationRequest struct {
	AuthorizedUserID string     `json:"authorized_user_id"`
	ResourceType     string     `json:"resource_type"`
	ResourceID       string     `json:"resource_id"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	Remark           string     `json:"remark,omitempty"`
}

type temporaryAccountActionRequest struct {
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type temporaryConnectionView struct {
	Address   string    `json:"address"`
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	Username  string    `json:"username"`
	Password  string    `json:"password"`
	Protocol  string    `json:"protocol"`
	ExpiresAt time.Time `json:"expires_at"`
}

type temporaryAccountView struct {
	ID               string                   `json:"id"`
	SessionID        string                   `json:"session_id"`
	Type             string                   `json:"type"`
	AuthorizedUserID string                   `json:"authorized_user_id,omitempty"`
	AuthorizedUser   string                   `json:"authorized_user,omitempty"`
	Status           string                   `json:"status"`
	StartsAt         time.Time                `json:"starts_at"`
	ExpiresAt        *time.Time               `json:"expires_at,omitempty"`
	ResourceType     string                   `json:"resource_type,omitempty"`
	ResourceName     string                   `json:"resource_name,omitempty"`
	AccountName      string                   `json:"account_name,omitempty"`
	Remark           string                   `json:"remark,omitempty"`
	CreatedBy        string                   `json:"created_by,omitempty"`
	CreatedAt        time.Time                `json:"created_at"`
	Connection       *temporaryConnectionView `json:"connection,omitempty"`
}

func (s *Server) handleTemporaryAccounts(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.listTemporaryAccounts(w, r)
	case http.MethodPost:
		s.createTemporaryAuthorization(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleTemporaryAccount(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(r, rbac.ActionRBACManage) {
		s.forbidden(w, r)
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/temporary-accounts/"), "/")
	parts := strings.Split(id, "/")
	if len(parts) == 0 || parts[0] == "" {
		s.writeErrorText(w, r, http.StatusNotFound, "temporary account not found")
		return
	}
	if len(parts) == 2 && r.Method == http.MethodPost {
		switch parts[1] {
		case "extend":
			s.extendTemporaryAccount(w, r, parts[0])
		case "disable":
			s.disableTemporaryAccount(w, r, parts[0])
		default:
			s.writeErrorText(w, r, http.StatusNotFound, "temporary account action not found")
		}
		return
	}
	s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
}

func (s *Server) listTemporaryAccounts(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	page := positiveIntRequestQuery(r, "page", 1)
	pageSize := positiveIntRequestQuery(r, "page_size", defaultPageSize)
	if pageSize > 200 {
		pageSize = 200
	}
	tx := s.db.Model(&model.TemporaryAccount{})
	if q != "" {
		like := "%" + q + "%"
		tx = tx.Where("session_id LIKE ? OR username LIKE ? OR remark LIKE ?", like, like, like)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	var accounts []model.TemporaryAccount
	if err := tx.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&accounts).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	views := make([]temporaryAccountView, 0, len(accounts))
	for _, account := range accounts {
		if account.Status == "active" && account.ExpiresAt != nil && !account.ExpiresAt.After(time.Now().UTC()) {
			account.Status = "disabled"
			_ = s.db.Model(&model.TemporaryAccount{}).Where("id = ?", account.ID).Update("status", "disabled").Error
		}
		views = append(views, s.temporaryAccountView(account))
	}
	s.writeJSON(w, r, http.StatusOK, pageResponse{Items: views, Total: int(total), Page: page, PageSize: pageSize})
}

func (s *Server) createTemporaryAuthorization(w http.ResponseWriter, r *http.Request) {
	var req temporaryAuthorizationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	req.AuthorizedUserID = strings.TrimSpace(req.AuthorizedUserID)
	if req.AuthorizedUserID == "" {
		req.AuthorizedUserID = userIDFromRequest(r)
	}
	req.ResourceType = strings.TrimSpace(req.ResourceType)
	req.ResourceID = strings.TrimSpace(req.ResourceID)
	if req.AuthorizedUserID == "" || req.ResourceID == "" {
		s.writeErrorText(w, r, http.StatusBadRequest, "resource_id is required")
		return
	}
	if req.ResourceType != model.ResourceTypeHostAccount && req.ResourceType != model.ResourceTypeDatabaseAccount {
		s.writeErrorText(w, r, http.StatusBadRequest, "resource_type must be host_account or database_account")
		return
	}
	var user model.User
	if err := s.db.Where("id = ? AND status = ?", req.AuthorizedUserID, "active").First(&user).Error; err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "authorized user not found or disabled")
		return
	}
	if user.IsExpired(time.Now().UTC()) {
		s.writeErrorText(w, r, http.StatusBadRequest, "authorized user account expired")
		return
	}
	if !s.resourceExists(req.ResourceType, req.ResourceID) {
		s.writeErrorText(w, r, http.StatusBadRequest, "resource not found")
		return
	}
	now := time.Now().UTC()
	expiresAt := now.Add(24 * time.Hour)
	if req.ExpiresAt != nil {
		expiresAt = req.ExpiresAt.UTC()
	}
	if !expiresAt.After(now) || expiresAt.After(now.Add(maxTemporaryAuthorizationDuration)) {
		s.writeErrorText(w, r, http.StatusBadRequest, "\u4e34\u65f6\u6388\u6743\u6709\u6548\u671f\u4e0d\u80fd\u8d85\u8fc7 7 \u5929\uff0c\u8bf7\u9009\u62e9 7 \u5929\u4ee5\u5185\u7684\u65f6\u95f4")
		return
	}
	userSession, err := s.store.CreateUserSession(model.UserSession{
		UserID: req.AuthorizedUserID, Type: "temporary", Status: "active",
		ExpiresAt: &expiresAt, CreatedBy: userIDFromRequest(r),
	})
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "failed to create temporary session")
		return
	}
	account, err := s.createTemporaryAccount(model.TemporaryAccountTypeUser, req.AuthorizedUserID, &expiresAt, req.Remark, userIDFromRequest(r), userSession.SessionID)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	grant := model.TemporaryAccountGrant{
		TemporaryAccountID: account.ID, UserID: req.AuthorizedUserID,
		Action: rbac.ActionSessionConnect, ResourceType: req.ResourceType, ResourceID: req.ResourceID,
		StartsAt: &now, ExpiresAt: &expiresAt, CreatedBy: userIDFromRequest(r),
	}
	if err := s.db.Create(&grant).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	connection, err := s.issueTemporaryConnection(r, req.AuthorizedUserID, req.ResourceType, req.ResourceID, userSession.SessionID, expiresAt)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	view := s.temporaryAccountView(account)
	view.Connection = &connection
	s.writeJSON(w, r, http.StatusCreated, view)
}

func (s *Server) createTemporaryAccount(accountType, authorizedUserID string, expiresAt *time.Time, remark, createdBy, sessionID string) (model.TemporaryAccount, error) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = model.NewID()
	}
	usernameSuffix := sessionID
	if len(usernameSuffix) > 12 {
		usernameSuffix = usernameSuffix[:12]
	}
	account := model.TemporaryAccount{
		ID: model.NewID(), SessionID: sessionID, Type: accountType,
		Username: "tmp_" + usernameSuffix, AuthorizedUserID: authorizedUserID,
		Status: "active", StartsAt: time.Now().UTC(), ExpiresAt: expiresAt,
		Remark: strings.TrimSpace(remark), CreatedBy: createdBy,
	}
	if err := s.db.Create(&account).Error; err != nil {
		return model.TemporaryAccount{}, err
	}
	return account, nil
}

func (s *Server) issueTemporaryConnection(r *http.Request, userID, resourceType, resourceID, sessionID string, expiresAt time.Time) (temporaryConnectionView, error) {
	now := time.Now().UTC()
	issued, err := service.IssueConnectionPassword(now, expiresAt.Sub(now))
	if err != nil {
		return temporaryConnectionView{}, fmt.Errorf("issue temporary connection password: %w", err)
	}
	if err := s.store.CreateConnectionPassword(r.Context(), model.ConnectionPassword{
		UserID: userID, ResourceType: resourceType, ResourceID: resourceID,
		SecretHash: issued.Hash, MySQLNativeHash: issued.MySQLNativeHash, ExpiresAt: issued.ExpiresAt,
	}); err != nil {
		return temporaryConnectionView{}, fmt.Errorf("save temporary connection password: %w", err)
	}

	publicHost := requestHostnameFromPage(r, s.aiBaseURL(r))
	prefix, compactResourceID, protocol, port, err := s.temporaryConnectionTarget(resourceType, resourceID)
	if err != nil {
		return temporaryConnectionView{}, err
	}
	return temporaryConnectionView{
		Address: net.JoinHostPort(publicHost, strconv.Itoa(port)), Host: publicHost, Port: port,
		Username: prefix + compactResourceID + sessionID, Password: issued.Plaintext,
		Protocol: protocol, ExpiresAt: issued.ExpiresAt,
	}, nil
}

func (s *Server) temporaryConnectionTarget(resourceType, resourceID string) (string, string, string, int, error) {
	switch resourceType {
	case model.ResourceTypeHostAccount:
		var account model.HostAccount
		if err := s.db.First(&account, "id = ?", resourceID).Error; err != nil {
			return "", "", "", 0, err
		}
		_, port := parseListenAddr(s.cfg.ListenAddr)
		return util.PrefixHost, account.ResourceID, "ssh", port, nil
	case model.ResourceTypeDatabaseAccount:
		var account model.DatabaseAccount
		if err := s.db.Preload("Instance").First(&account, "id = ?", resourceID).Error; err != nil {
			return "", "", "", 0, err
		}
		prefix := util.PrefixDatabase
		if strings.EqualFold(account.Instance.Protocol, "redis") {
			prefix = util.PrefixRedis
		}
		_, port := parseListenAddr(s.cfg.DatabaseGateway.ListenAddr)
		return prefix, account.ResourceID, account.Instance.Protocol, port, nil
	default:
		return "", "", "", 0, fmt.Errorf("unsupported temporary resource type %q", resourceType)
	}
}

func requestHostnameFromPage(r *http.Request, baseURL string) string {
	if parsed, err := url.Parse(baseURL); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	return requestHostname(r)
}

func (s *Server) extendTemporaryAccount(w http.ResponseWriter, r *http.Request, id string) {
	var req temporaryAccountActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid request body")
		return
	}
	var account model.TemporaryAccount
	if err := s.db.First(&account, "id = ?", id).Error; err != nil {
		s.writeErrorText(w, r, http.StatusNotFound, "temporary account not found")
		return
	}
	now := time.Now().UTC()
	expiresAt := now.Add(24 * time.Hour)
	if req.ExpiresAt != nil {
		expiresAt = req.ExpiresAt.UTC()
	}
	if !expiresAt.After(now) || expiresAt.After(now.Add(maxTemporaryAuthorizationDuration)) {
		s.writeErrorText(w, r, http.StatusBadRequest, "\u4e34\u65f6\u6388\u6743\u6709\u6548\u671f\u4e0d\u80fd\u8d85\u8fc7 7 \u5929\uff0c\u8bf7\u9009\u62e9 7 \u5929\u4ee5\u5185\u7684\u65f6\u95f4")
		return
	}
	if err := s.db.Model(&account).Updates(map[string]any{"expires_at": expiresAt, "status": "active"}).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.db.Model(&model.TemporaryAccountGrant{}).Where("temporary_account_id = ?", account.ID).Updates(map[string]any{"expires_at": expiresAt, "revoked_at": nil}).Error
	_ = s.db.Model(&model.UserSession{}).Where("session_id = ?", account.SessionID).Updates(map[string]any{"expires_at": expiresAt, "status": "active"}).Error
	account.ExpiresAt = &expiresAt
	account.Status = "active"
	view := s.temporaryAccountView(account)
	var grant model.TemporaryAccountGrant
	if account.Type == model.TemporaryAccountTypeUser && s.db.Where("temporary_account_id = ? AND revoked_at IS NULL", account.ID).First(&grant).Error == nil {
		if connection, issueErr := s.issueTemporaryConnection(r, account.AuthorizedUserID, grant.ResourceType, grant.ResourceID, account.SessionID, expiresAt); issueErr == nil {
			view.Connection = &connection
		}
	}
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) disableTemporaryAccount(w http.ResponseWriter, r *http.Request, id string) {
	var account model.TemporaryAccount
	if err := s.db.First(&account, "id = ?", id).Error; err != nil {
		s.writeErrorText(w, r, http.StatusNotFound, "temporary account not found")
		return
	}
	now := time.Now().UTC()
	if err := s.db.Model(&account).Update("status", "disabled").Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.db.Model(&model.TemporaryAccountGrant{}).Where("temporary_account_id = ?", account.ID).Update("revoked_at", now).Error
	_ = s.db.Model(&model.UserSession{}).Where("session_id = ?", account.SessionID).Update("status", "disabled").Error
	_ = s.db.Model(&model.AIAccessToken{}).Where("temporary_account_id = ?", account.ID).Update("revoked_at", now).Error
	account.Status = "disabled"
	s.writeJSON(w, r, http.StatusOK, s.temporaryAccountView(account))
}

func (s *Server) temporaryAccountView(account model.TemporaryAccount) temporaryAccountView {
	view := temporaryAccountView{
		ID: account.ID, SessionID: account.SessionID, Type: account.Type,
		AuthorizedUserID: account.AuthorizedUserID, Status: account.Status,
		StartsAt: account.StartsAt, ExpiresAt: account.ExpiresAt, Remark: account.Remark,
		CreatedBy: account.CreatedBy, CreatedAt: account.CreatedAt,
	}
	if account.AuthorizedUserID != "" {
		var user model.User
		if s.db.First(&user, "id = ?", account.AuthorizedUserID).Error == nil {
			view.AuthorizedUser = user.DisplayName
			if view.AuthorizedUser == "" {
				view.AuthorizedUser = user.Username
			}
		}
	}
	var grant model.TemporaryAccountGrant
	if s.db.Where("temporary_account_id = ? AND revoked_at IS NULL", account.ID).Order("created_at DESC").First(&grant).Error == nil {
		view.ResourceType = grant.ResourceType
		view.ResourceName, view.AccountName = s.resourceDisplayNames(grant.ResourceType, grant.ResourceID)
	}
	return view
}

func (s *Server) resourceExists(resourceType, resourceID string) bool {
	var count int64
	switch resourceType {
	case model.ResourceTypeHostAccount:
		s.db.Model(&model.HostAccount{}).Where("id = ?", resourceID).Count(&count)
	case model.ResourceTypeDatabaseAccount:
		s.db.Model(&model.DatabaseAccount{}).Where("id = ?", resourceID).Count(&count)
	}
	return count > 0
}

func (s *Server) resourceDisplayNames(resourceType, resourceID string) (string, string) {
	switch resourceType {
	case model.ResourceTypeHostAccount:
		var account model.HostAccount
		if s.db.Preload("Host").First(&account, "id = ?", resourceID).Error == nil {
			return account.Host.Name, account.Name
		}
	case model.ResourceTypeDatabaseAccount:
		var account model.DatabaseAccount
		if s.db.Preload("Instance").First(&account, "id = ?", resourceID).Error == nil {
			return account.Instance.Name, account.UniqueName
		}
	}
	return "", ""
}
