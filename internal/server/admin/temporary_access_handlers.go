package admin

import (
	"context"
	"encoding/json"
	"errors"
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
	now := time.Now().UTC()
	expiresAt := now.Add(24 * time.Hour)
	if req.ExpiresAt != nil {
		expiresAt = req.ExpiresAt.UTC()
	}
	if !expiresAt.After(now) || expiresAt.After(now.Add(maxTemporaryAuthorizationDuration)) {
		s.writeErrorText(w, r, http.StatusBadRequest, "\u4e34\u65f6\u6388\u6743\u6709\u6548\u671f\u4e0d\u80fd\u8d85\u8fc7 7 \u5929\uff0c\u8bf7\u9009\u62e9 7 \u5929\u4ee5\u5185\u7684\u65f6\u95f4")
		return
	}
	temporaryAccess, err := s.temporaryAccessService()
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "temporary access service is unavailable")
		return
	}
	result, err := temporaryAccess.Create(r.Context(), service.CreateTemporaryAccessInput{
		AuthorizedUserID: req.AuthorizedUserID, ResourceType: req.ResourceType, ResourceID: req.ResourceID,
		ExpiresAt: expiresAt, Remark: req.Remark, CreatedBy: userIDFromRequest(r), Now: now,
	})
	if err != nil {
		s.writeTemporaryAccessError(w, r, err, http.StatusBadRequest)
		return
	}
	connection, err := s.issueTemporaryConnection(r, result.Grant.ResourceType, result.Grant.ResourceID, result.Account.SessionID, result.ConnectionPassword, expiresAt)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "build temporary connection details")
		return
	}
	view := s.temporaryAccountView(result.Account)
	view.Connection = &connection
	s.writeJSON(w, r, http.StatusCreated, view)
}

func (s *Server) createTemporaryAccount(accountType, authorizedUserID string, expiresAt *time.Time, remark, createdBy, sessionID string) (model.TemporaryAccount, error) {
	temporaryAccess, err := s.temporaryAccessService()
	if err != nil {
		return model.TemporaryAccount{}, err
	}
	return temporaryAccess.CreateAccount(context.Background(), service.CreateTemporaryAccountInput{
		AccountType: accountType, AuthorizedUserID: authorizedUserID, ExpiresAt: expiresAt,
		Remark: remark, CreatedBy: createdBy, SessionID: sessionID, Now: time.Now().UTC(),
	})
}

func (s *Server) issueTemporaryConnection(r *http.Request, resourceType, resourceID, sessionID, password string, expiresAt time.Time) (temporaryConnectionView, error) {
	publicHost := requestHostnameFromPage(r, s.aiBaseURL(r))
	prefix, compactResourceID, protocol, port, err := s.temporaryConnectionTarget(resourceType, resourceID)
	if err != nil {
		return temporaryConnectionView{}, err
	}
	return temporaryConnectionView{
		Address: net.JoinHostPort(publicHost, strconv.Itoa(port)), Host: publicHost, Port: port,
		Username: prefix + compactResourceID + sessionID, Password: password,
		Protocol: protocol, ExpiresAt: expiresAt,
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

func (s *Server) temporaryAccessService() (*service.TemporaryAccessService, error) {
	if s.temporaryAccess != nil {
		return s.temporaryAccess, nil
	}
	repository, ok := s.store.(service.TemporaryAccessRepository)
	if !ok {
		return nil, errors.New("temporary access repository is unavailable")
	}
	temporaryAccess, err := service.NewTemporaryAccessService(repository)
	if err != nil {
		return nil, err
	}
	s.temporaryAccess = temporaryAccess
	return temporaryAccess, nil
}

func (s *Server) writeTemporaryAccessError(w http.ResponseWriter, r *http.Request, err error, notFoundStatus int) {
	switch {
	case errors.Is(err, service.ErrInvalidTemporaryAccess):
		s.writeErrorText(w, r, http.StatusBadRequest, "temporary access request is invalid")
	case errors.Is(err, service.ErrTemporaryAccessNotFound):
		s.writeErrorText(w, r, notFoundStatus, "temporary account or dependent resource not found")
	default:
		s.writeErrorText(w, r, http.StatusInternalServerError, "temporary access operation failed")
	}
}

func (s *Server) extendTemporaryAccount(w http.ResponseWriter, r *http.Request, id string) {
	var req temporaryAccountActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid request body")
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
	temporaryAccess, err := s.temporaryAccessService()
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "temporary access service is unavailable")
		return
	}
	if err := temporaryAccess.Extend(r.Context(), id, expiresAt, now); err != nil {
		s.writeTemporaryAccessError(w, r, err, http.StatusNotFound)
		return
	}
	var account model.TemporaryAccount
	if err := s.db.First(&account, "id = ?", id).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "load extended temporary account")
		return
	}
	view := s.temporaryAccountView(account)
	s.writeJSON(w, r, http.StatusOK, view)
}

func (s *Server) disableTemporaryAccount(w http.ResponseWriter, r *http.Request, id string) {
	now := time.Now().UTC()
	temporaryAccess, err := s.temporaryAccessService()
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "temporary access service is unavailable")
		return
	}
	if err := temporaryAccess.Disable(r.Context(), id, now); err != nil {
		s.writeTemporaryAccessError(w, r, err, http.StatusNotFound)
		return
	}
	var account model.TemporaryAccount
	if err := s.db.First(&account, "id = ?", id).Error; err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "load disabled temporary account")
		return
	}
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
