package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
)

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
	temporaryAccess, err := s.temporaryAccessService()
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "temporary access service is unavailable")
		return
	}
	result, err := temporaryAccess.List(r.Context(), q, page, pageSize, time.Now().UTC())
	if err != nil {
		s.writeTemporaryAccessError(w, r, err, http.StatusNotFound)
		return
	}
	views := make([]temporaryAccountView, 0, len(result.Items))
	for _, item := range result.Items {
		views = append(views, temporaryAccountViewFromDetails(item))
	}
	s.writeJSON(w, r, http.StatusOK, pageResponse{
		Items: views, Total: result.Total, Page: result.Page, PageSize: result.PageSize,
	})
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
	connection, err := s.issueTemporaryConnection(r, result.ConnectionTarget, result.Account.SessionID, result.ConnectionPassword, expiresAt)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "build temporary connection details")
		return
	}
	view := temporaryAccountViewFromDetails(result.Details)
	view.Connection = &connection
	s.writeJSON(w, r, http.StatusCreated, view)
}

func (s *Server) issueTemporaryConnection(r *http.Request, target service.TemporaryConnectionTarget, sessionID, password string, expiresAt time.Time) (temporaryConnectionView, error) {
	publicHost := requestHostnameFromPage(r, s.aiBaseURL(r))
	var port int
	switch target.Gateway {
	case service.TemporaryGatewaySSH:
		_, port = parseListenAddr(s.cfg.ListenAddr)
	case service.TemporaryGatewayDatabase:
		_, port = parseListenAddr(databaseGatewayListenerAddress(s.cfg.DatabaseGateway, target.Protocol))
	default:
		return temporaryConnectionView{}, fmt.Errorf("unsupported temporary gateway %q", target.Gateway)
	}
	return temporaryConnectionView{
		Address: net.JoinHostPort(publicHost, strconv.Itoa(port)), Host: publicHost, Port: port,
		Username: target.UsernamePrefix + target.CompactResourceID + sessionID, Password: password,
		Protocol: target.Protocol, ExpiresAt: expiresAt,
	}, nil
}

func databaseGatewayListenerAddress(gateway config.DatabaseGatewayConfig, protocol string) string {
	switch strings.ToLower(protocol) {
	case "postgres", "postgresql":
		return gateway.PostgreSQL.Address
	case "redis":
		return gateway.Redis.Address
	default:
		return gateway.MySQL.Address
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
	case errors.Is(err, service.ErrTemporaryAccessExpiry):
		s.writeErrorText(w, r, http.StatusBadRequest, "\u4e34\u65f6\u6388\u6743\u6709\u6548\u671f\u4e0d\u80fd\u8d85\u8fc7 7 \u5929\uff0c\u8bf7\u9009\u62e9 7 \u5929\u4ee5\u5185\u7684\u65f6\u95f4")
	case errors.Is(err, service.ErrInvalidTemporaryAccess):
		s.writeErrorText(w, r, http.StatusBadRequest, "temporary access request is invalid")
	case errors.Is(err, service.ErrTemporaryAccessInactive):
		s.writeErrorText(w, r, http.StatusConflict, "temporary account is not active")
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
	temporaryAccess, err := s.temporaryAccessService()
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "temporary access service is unavailable")
		return
	}
	details, err := temporaryAccess.Extend(r.Context(), id, expiresAt, now)
	if err != nil {
		s.writeTemporaryAccessError(w, r, err, http.StatusNotFound)
		return
	}
	s.writeJSON(w, r, http.StatusOK, temporaryAccountViewFromDetails(details))
}

func (s *Server) disableTemporaryAccount(w http.ResponseWriter, r *http.Request, id string) {
	now := time.Now().UTC()
	temporaryAccess, err := s.temporaryAccessService()
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, "temporary access service is unavailable")
		return
	}
	details, err := temporaryAccess.Disable(r.Context(), id, now)
	if err != nil {
		s.writeTemporaryAccessError(w, r, err, http.StatusNotFound)
		return
	}
	s.writeJSON(w, r, http.StatusOK, temporaryAccountViewFromDetails(details))
}

func temporaryAccountViewFromDetails(details service.TemporaryAccessDetails) temporaryAccountView {
	return temporaryAccountView{
		ID: details.ID, SessionID: details.SessionID, Type: details.Type,
		AuthorizedUserID: details.AuthorizedUserID, AuthorizedUser: details.AuthorizedUser,
		Status: details.Status, StartsAt: details.StartsAt, ExpiresAt: details.ExpiresAt,
		ResourceType: details.ResourceType, ResourceName: details.ResourceName, AccountName: details.AccountName,
		Remark: details.Remark, CreatedBy: details.CreatedBy, CreatedAt: details.CreatedAt,
	}
}
