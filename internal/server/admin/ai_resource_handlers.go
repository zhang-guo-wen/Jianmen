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

	"jianmen/internal/model"
	"jianmen/internal/service"
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
	resources, err := s.aiResources.List(r.Context(), userIDFromRequest(r))
	if err != nil {
		s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]aiResource, len(resources))
	for index, resource := range resources {
		items[index] = aiResourceResponse(resource)
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) getAIResource(w http.ResponseWriter, r *http.Request, resourceType, resourceID string) {
	resource, err := s.aiResources.Get(r.Context(), userIDFromRequest(r), resourceType, resourceID)
	if err != nil {
		s.writeAIResourceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, aiResourceDetail{
		Resource: aiResourceResponse(resource),
		Bastion:  s.aiBastionInfo(r, resource.Protocol),
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
	if s.connectionPassword == nil {
		s.writeErrorText(w, r, http.StatusServiceUnavailable, "connection password service unavailable")
		return
	}
	issued, err := s.connectionPassword.Issue(
		r.Context(),
		service.ConnectionPasswordIssueRequest{
			UserID:               userIDFromRequest(r),
			TargetID:             resourceID,
			ExpectedResourceType: resourceType,
		},
	)
	if err != nil {
		s.writeConnectionPasswordServiceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, map[string]any{
		"resource_id": resourceID, "password": issued.Password,
		"expires_at": issued.ExpiresAt, "expires_in_seconds": issued.ExpiresInSeconds,
	})
}

func (s *Server) issueAIResourceSession(w http.ResponseWriter, r *http.Request, resourceType, resourceID string) {
	result, err := s.aiResources.CreateSession(r.Context(), userIDFromRequest(r), resourceType, resourceID)
	if err != nil {
		s.writeAIResourceError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, map[string]any{
		"resource_id": resourceID, "compact_username": result.CompactUsername,
		"session_id": result.SessionID, "session_seq": result.SessionSeq,
		"bastion": s.aiBastionInfo(r, result.Resource.Protocol),
	})
}

func (s *Server) writeAIResourceError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, service.ErrAIResourceNotFound) {
		s.writeErrorText(w, r, http.StatusNotFound, "resource not found or disabled")
		return
	}
	s.writeErrorText(w, r, http.StatusInternalServerError, err.Error())
}

func aiResourceResponse(resource service.AIResource) aiResource {
	return aiResource{
		ID: resource.ID, Type: resource.Type, Name: resource.Name,
		Group: resource.Group, Remark: resource.Remark,
		Address: resource.Address, Port: resource.Port, Protocol: resource.Protocol,
		Username: resource.Username, ResourceID: resource.ResourceID, ResourceSeq: resource.ResourceSeq,
		Status: resource.Status, ExpiresAt: resource.ExpiresAt,
		Capabilities: append([]string(nil), resource.Capabilities...),
	}
}

func (s *Server) aiBastionInfo(r *http.Request, databaseProtocol string) map[string]any {
	requestHostName := requestHostname(r)
	host, port := parseListenAddr(s.cfg.ListenAddr)
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = requestHostName
	}
	databaseHost, databasePort := parseListenAddr(
		databaseGatewayListenerAddress(s.cfg.DatabaseGateway, databaseProtocol),
	)
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
		return 0, 0, fmt.Errorf("access TTL must be between 5 minutes and 48 hours")
	}
	if refreshTTL < aiRefreshTokenMinTTL || refreshTTL > aiRefreshTokenMaxTTL {
		return 0, 0, fmt.Errorf("refresh TTL must be between 1 day and 90 days")
	}
	if refreshTTL <= accessTTL {
		return 0, 0, fmt.Errorf("refresh TTL must be longer than access TTL")
	}
	return accessTTL, refreshTTL, nil
}

func contextWithString(ctx context.Context, key contextKey, value string) context.Context {
	return context.WithValue(ctx, key, value)
}
