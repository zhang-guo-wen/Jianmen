package admin

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
	"jianmen/internal/store"
	"jianmen/internal/util"
)

type aiResource struct {
	ID           string   `json:"id"`
	Type         string   `json:"type"`
	Name         string   `json:"name"`
	Group        string   `json:"group,omitempty"`
	Remark       string   `json:"remark,omitempty"`
	Address      string   `json:"address"`
	Port         int      `json:"port"`
	Protocol     string   `json:"protocol,omitempty"`
	Username     string   `json:"username"`
	ResourceID   string   `json:"resource_id,omitempty"`
	ResourceSeq  int      `json:"resource_seq,omitempty"`
	Status       string   `json:"status"`
	ExpiresAt    string   `json:"expires_at,omitempty"`
	Capabilities []string `json:"capabilities"`
}

type aiResourceDetail struct {
	Resource aiResource        `json:"resource"`
	Bastion  map[string]any    `json:"bastion"`
	Usage    map[string]string `json:"usage"`
}

func (s *Server) handleAIResources(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/ai/resources"), "/")
	if path == "" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		s.listAIResources(w, r)
		return
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 || len(parts) > 3 {
		s.writeErrorText(w, r, http.StatusNotFound, "resource endpoint not found")
		return
	}
	resourceType, resourceID := parts[0], parts[1]
	if len(parts) == 2 && r.Method == http.MethodGet {
		s.getAIResource(w, r, resourceType, resourceID)
		return
	}
	if len(parts) == 3 && parts[2] == "credentials" && r.Method == http.MethodPost {
		s.issueAIResourceCredential(w, r, resourceType, resourceID)
		return
	}
	if len(parts) == 3 && parts[2] == "session" && r.Method == http.MethodPost {
		s.issueAIResourceSession(w, r, resourceType, resourceID)
		return
	}
	s.writeErrorText(w, r, http.StatusNotFound, "resource endpoint not found")
}

func (s *Server) listAIResources(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromRequest(r)
	resources := make([]aiResource, 0)
	targets := s.store.Targets()
	for _, target := range targets {
		allowed, authErr := s.authorizeAnyConnection(userID, []string{rbac.ActionSessionConnect, rbac.ActionSFTPConnect}, model.ResourceTypeHostAccount, target.ID)
		if authErr != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, authErr.Error())
			return
		}
		if allowed && aiResourceStatusActive(target.Status) && !aiExpiryPassed(target.ExpiresAt) {
			resources = append(resources, hostAIResource(target))
		}
	}
	accounts, err := s.store.DatabaseAccounts()
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	for _, account := range accounts {
		allowed, authErr := s.authorizeAnyConnection(userID, []string{rbac.ActionDBConnect}, model.ResourceTypeDatabaseAccount, account.ID)
		if authErr != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, authErr.Error())
			return
		}
		if allowed && aiResourceStatusActive(account.Status) && (account.ExpiresAt == nil || time.Now().UTC().Before(*account.ExpiresAt)) {
			instance, instanceErr := s.store.DatabaseInstance(account.InstanceID)
			if instanceErr != nil || !aiResourceStatusActive(instance.Status) {
				continue
			}
			resources = append(resources, databaseAIResource(account, instance))
		}
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"items": resources, "total": len(resources)})
}

func (s *Server) getAIResource(w http.ResponseWriter, r *http.Request, resourceType, resourceID string) {
	resource, err := s.loadAIResource(resourceType, resourceID)
	if err != nil {
		s.writeAIResourceError(w, r, err)
		return
	}
	actions := aiResourceActions(resourceType)
	allowed, authErr := s.authorizeAnyConnection(userIDFromRequest(r), actions, resourceType, resourceID)
	if authErr != nil || !allowed {
		s.forbidden(w, r)
		return
	}
	s.writeJSON(w, r, http.StatusOK, aiResourceDetail{
		Resource: resource,
		Bastion:  s.aiBastionInfo(r),
		Usage: map[string]string{
			"session":     "POST /api/ai/resources/" + url.PathEscape(resourceType) + "/" + url.PathEscape(resourceID) + "/session",
			"credentials": "POST /api/ai/resources/" + url.PathEscape(resourceType) + "/" + url.PathEscape(resourceID) + "/credentials",
		},
	})
}

func (s *Server) issueAIResourceCredential(w http.ResponseWriter, r *http.Request, resourceType, resourceID string) {
	if resourceType != model.ResourceTypeHostAccount && resourceType != model.ResourceTypeDatabaseAccount {
		s.writeErrorText(w, r, http.StatusNotFound, "unsupported resource type")
		return
	}
	if _, err := s.loadAIResource(resourceType, resourceID); err != nil {
		s.writeAIResourceError(w, r, err)
		return
	}
	allowed, err := s.authorizeAnyConnection(userIDFromRequest(r), aiResourceActions(resourceType), resourceType, resourceID)
	if err != nil || !allowed {
		s.forbidden(w, r)
		return
	}
	issued, err := service.IssueConnectionPassword(time.Now().UTC(), 30*time.Minute)
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.store.CreateConnectionPassword(r.Context(), model.ConnectionPassword{
		UserID: userIDFromRequest(r), ResourceType: resourceType, ResourceID: resourceID,
		SecretHash: issued.Hash, MySQLNativeHash: issued.MySQLNativeHash, ExpiresAt: issued.ExpiresAt,
	}); err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, r, http.StatusCreated, map[string]any{
		"resource_id": resourceID, "password": issued.Plaintext,
		"expires_at": issued.ExpiresAt, "expires_in_seconds": int((30 * time.Minute).Seconds()),
	})
}

func (s *Server) issueAIResourceSession(w http.ResponseWriter, r *http.Request, resourceType, resourceID string) {
	resource, err := s.loadAIResource(resourceType, resourceID)
	if err != nil {
		s.writeAIResourceError(w, r, err)
		return
	}
	allowed, authErr := s.authorizeAnyConnection(userIDFromRequest(r), aiResourceActions(resourceType), resourceType, resourceID)
	if authErr != nil || !allowed {
		s.forbidden(w, r)
		return
	}
	sessions, err := s.store.UserSessions(userIDFromRequest(r))
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	var session *store.SessionView
	for i := range sessions {
		if sessions[i].Type == "permanent" && sessions[i].Status == "active" {
			session = &sessions[i]
			break
		}
	}
	if session == nil {
		created, createErr := s.store.CreateUserSession(model.UserSession{UserID: userIDFromRequest(r), Type: "permanent", Status: "active"})
		if createErr != nil {
			s.writeErrorText(w, r, http.StatusInternalServerError, createErr.Error())
			return
		}
		session = &store.SessionView{SessionID: created.SessionID, SessionSeq: created.SessionSeq, Type: created.Type, Status: created.Status}
	}
	prefix := util.PrefixHost
	if resourceType == model.ResourceTypeDatabaseAccount {
		prefix = util.PrefixDatabase
		if strings.EqualFold(resource.Protocol, "redis") {
			prefix = util.PrefixRedis
		}
	}
	compactUsername := prefix + resource.ResourceID + session.SessionID
	s.writeJSON(w, r, http.StatusCreated, map[string]any{
		"resource_id": resourceID, "compact_username": compactUsername,
		"session_id": session.SessionID, "session_seq": session.SessionSeq,
		"bastion": s.aiBastionInfo(r),
	})
}

func (s *Server) loadAIResource(resourceType, resourceID string) (aiResource, error) {
	if resourceType == model.ResourceTypeHostAccount {
		target, err := s.store.Target(resourceID)
		if err != nil {
			return aiResource{}, err
		}
		if !aiResourceStatusActive(target.Status) || aiExpiryPassed(target.ExpiresAt) {
			return aiResource{}, gorm.ErrRecordNotFound
		}
		return hostAIResource(target), nil
	}
	if resourceType == model.ResourceTypeDatabaseAccount {
		account, err := s.store.DatabaseAccount(resourceID)
		if err != nil {
			return aiResource{}, err
		}
		if !aiResourceStatusActive(account.Status) || (account.ExpiresAt != nil && time.Now().UTC().After(*account.ExpiresAt)) {
			return aiResource{}, gorm.ErrRecordNotFound
		}
		instance, err := s.store.DatabaseInstance(account.InstanceID)
		if err != nil || !aiResourceStatusActive(instance.Status) {
			return aiResource{}, gorm.ErrRecordNotFound
		}
		return databaseAIResource(account, instance), nil
	}
	return aiResource{}, fmt.Errorf("unsupported resource type")
}

func (s *Server) writeAIResourceError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, store.ErrTargetNotFound) || errors.Is(err, store.ErrDBAccountNotFound) || errors.Is(err, store.ErrDBInstanceNotFound) {
		s.writeErrorText(w, r, http.StatusNotFound, "resource not found or disabled")
		return
	}
	s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
}

func hostAIResource(target store.TargetView) aiResource {
	return aiResource{ID: target.ID, Type: model.ResourceTypeHostAccount, Name: target.Name, Group: target.Group, Remark: target.Remark, Address: target.Host, Port: target.Port, Username: target.Username, ResourceID: target.ResourceID, ResourceSeq: target.ResourceSeq, Status: target.Status, ExpiresAt: target.ExpiresAt, Capabilities: []string{"ssh", "sftp", "temporary_password"}}
}

func databaseAIResource(account store.DatabaseAccountView, instance store.DatabaseInstanceView) aiResource {
	return aiResource{ID: account.ID, Type: model.ResourceTypeDatabaseAccount, Name: account.UniqueName, Group: account.Group, Remark: account.Remark, Address: instance.Address, Port: instance.Port, Protocol: instance.Protocol, Username: account.Username, ResourceID: account.ResourceID, ResourceSeq: account.ResourceSeq, Status: account.Status, ExpiresAt: formatTimePtr(account.ExpiresAt), Capabilities: []string{"database", "temporary_password"}}
}

func aiResourceActions(resourceType string) []string {
	if resourceType == model.ResourceTypeDatabaseAccount {
		return []string{rbac.ActionDBConnect}
	}
	return []string{rbac.ActionSessionConnect, rbac.ActionSFTPConnect}
}

func (s *Server) aiBastionInfo(r *http.Request) map[string]any {
	requestHostName := requestHostname(r)
	host, port := parseListenAddr(s.cfg.ListenAddr)
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = requestHostName
	}
	databaseHost, databasePort := parseListenAddr(s.cfg.DatabaseGateway.ListenAddr)
	if databaseHost == "" || databaseHost == "0.0.0.0" || databaseHost == "::" || databaseHost == "[::]" {
		databaseHost = requestHostName
	}
	return map[string]any{
		"host": host, "ssh_port": port,
		"database_gateway_host": databaseHost, "database_gateway_port": databasePort,
	}
}

func (s *Server) aiBaseURL(r *http.Request) string {
	for _, candidate := range []string{r.Header.Get("Origin"), r.Header.Get("Referer")} {
		if parsed, err := url.Parse(strings.TrimSpace(candidate)); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" {
			return strings.TrimRight(parsed.Scheme+"://"+parsed.Host, "/")
		}
	}
	if configured := strings.TrimRight(strings.TrimSpace(s.cfg.Admin.PublicURL), "/"); configured != "" {
		return configured
	}
	scheme := "http"
	if r.Header.Get("X-Forwarded-Proto") == "https" || r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + requestHost(r)
}

func requestHost(r *http.Request) string {
	if host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); host != "" {
		return host
	}
	return r.Host
}

func requestHostname(r *http.Request) string {
	host := requestHost(r)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		return strings.Trim(parsed, "[]")
	}
	return strings.Trim(host, "[]")
}

func aiTokenTTLs(request aiTokenRequest) (time.Duration, time.Duration, error) {
	accessTTL, refreshTTL := aiAccessTokenDefaultTTL, aiRefreshTokenDefaultTTL
	if request.AccessTTLSeconds > 0 {
		accessTTL = time.Duration(request.AccessTTLSeconds) * time.Second
	}
	if request.RefreshTTLSeconds > 0 {
		refreshTTL = time.Duration(request.RefreshTTLSeconds) * time.Second
	}
	if accessTTL < aiAccessTokenMinTTL || accessTTL > aiAccessTokenMaxTTL {
		return 0, 0, fmt.Errorf("access TTL must be between 5 minutes and 24 hours")
	}
	if refreshTTL < aiRefreshTokenMinTTL || refreshTTL > aiRefreshTokenMaxTTL {
		return 0, 0, fmt.Errorf("refresh TTL must be between 1 day and 90 days")
	}
	if refreshTTL <= accessTTL {
		return 0, 0, fmt.Errorf("refresh TTL must be longer than access TTL")
	}
	return accessTTL, refreshTTL, nil
}

func aiResourceStatusActive(status string) bool {
	return !strings.EqualFold(strings.TrimSpace(status), "disabled")
}

func aiExpiryPassed(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && !time.Now().UTC().Before(expiresAt)
}

func formatTimePtr(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func contextWithString(ctx context.Context, key contextKey, value string) context.Context {
	return context.WithValue(ctx, key, value)
}
