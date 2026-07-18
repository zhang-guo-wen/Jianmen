package appproxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
)

type Server struct {
	cfg           config.ApplicationGatewayConfig
	adminCfg      config.AdminConfig
	db            *gorm.DB
	authenticator tokenAuthenticator
	sessions      browserSessionAuthenticator
	authorizer    connectionAuthorizer
	logger        *slog.Logger

	mu      sync.Mutex
	proxies map[int]*proxyEntry
}

type tokenAuthenticator interface {
	FindIdentitySubject(context.Context, string) (service.IdentitySubject, bool, error)
}

type browserSessionAuthenticator interface {
	Authenticate(context.Context, string) (service.BrowserSessionSubject, bool, error)
}

type connectionAuthorizer interface {
	Authorize(context.Context, service.AuthorizationRequest) (service.AuthorizationDecision, error)
}

type authenticatedUserIDContextKey struct{}

type proxyEntry struct {
	app    model.Application
	server *http.Server
	proxy  *httputil.ReverseProxy
}

const browserSessionCookieName = "jianmen_session"

func New(
	cfg config.ApplicationGatewayConfig,
	adminCfg config.AdminConfig,
	db *gorm.DB,
	authenticator tokenAuthenticator,
	sessions browserSessionAuthenticator,
	authorizer connectionAuthorizer,
	logger *slog.Logger,
) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		cfg:           cfg,
		adminCfg:      adminCfg,
		db:            db,
		authenticator: authenticator,
		sessions:      sessions,
		authorizer:    authorizer,
		logger:        logger,
		proxies:       make(map[int]*proxyEntry),
	}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	if !s.cfg.Enabled {
		s.logger.Info("application gateway disabled")
		return nil
	}

	var apps []model.Application
	if err := s.db.Where("status = ?", "active").Find(&apps).Error; err != nil {
		return fmt.Errorf("load applications: %w", err)
	}

	for _, app := range apps {
		if err := s.startProxy(app); err != nil {
			s.logger.Error("failed to start app proxy", "name", app.Name, "port", app.ListenPort, "error", err)
		}
	}

	s.logger.Info("application gateway started", "port_range", fmt.Sprintf("%d-%d", s.cfg.PortStart, s.cfg.PortEnd))
	<-ctx.Done()
	s.shutdown()
	return nil
}

func (s *Server) AddProxy(app model.Application) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.proxies[app.ListenPort]; exists {
		return fmt.Errorf("port %d already in use", app.ListenPort)
	}
	return s.startProxy(app)
}

func (s *Server) RemoveProxy(listenPort int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.proxies[listenPort]; ok {
		_ = entry.server.Close()
		delete(s.proxies, listenPort)
		s.logger.Info("stopped app proxy", "port", listenPort)
	}
}

func (s *Server) UpdateProxy(previousListenPort int, app model.Application) error {
	s.RemoveProxy(previousListenPort)
	return s.AddProxy(app)
}

func (s *Server) startProxy(app model.Application) error {
	target := fmt.Sprintf("%s://%s", app.InternalScheme, net.JoinHostPort(app.InternalHost, fmt.Sprintf("%d", app.InternalPort)))
	targetURL, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("parse target %q: %w", target, err)
	}

	rp := httputil.NewSingleHostReverseProxy(targetURL)
	protectManagementSession(rp)
	rp.ErrorHandler = s.proxyErrorHandler(app)
	handler := s.authMiddleware(app, s.rbacMiddleware(app, s.entryRedirectMiddleware(app, rp)))

	addr := fmt.Sprintf(":%d", app.ListenPort)
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		s.logger.Info("starting app proxy", "name", app.Name, "port", app.ListenPort, "target", target)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("app proxy stopped", "name", app.Name, "port", app.ListenPort, "error", err)
		}
	}()

	s.proxies[app.ListenPort] = &proxyEntry{app: app, server: srv, proxy: rp}
	return nil
}

func (s *Server) entryRedirectMiddleware(app model.Application, next http.Handler) http.Handler {
	entryPath := strings.TrimSpace(app.EntryPath)
	if entryPath == "" {
		entryPath = "/"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPageNavigation(r) && r.URL.Path == "/" && r.URL.RawQuery == "" && entryPath != "/" {
			http.Redirect(w, r, entryPath, http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authMiddleware(app model.Application, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(browserSessionCookieName)
		if err != nil || strings.TrimSpace(cookie.Value) == "" || s.sessions == nil || s.authenticator == nil {
			s.writeUnauthorizedForApp(w, r, app)
			return
		}
		session, found, err := s.sessions.Authenticate(r.Context(), cookie.Value)
		if err != nil || !found {
			s.writeUnauthorizedForApp(w, r, app)
			return
		}
		subject, found, err := s.authenticator.FindIdentitySubject(r.Context(), session.UserID)
		userID := strings.TrimSpace(subject.ID)
		if err != nil || !found || userID == "" {
			s.writeUnauthorizedForApp(w, r, app)
			return
		}
		removeRequestCookie(r, browserSessionCookieName)
		ctx := context.WithValue(r.Context(), authenticatedUserIDContextKey{}, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func protectManagementSession(proxy *httputil.ReverseProxy) {
	if proxy == nil {
		return
	}
	previous := proxy.ModifyResponse
	proxy.ModifyResponse = func(response *http.Response) error {
		if previous != nil {
			if err := previous(response); err != nil {
				return err
			}
		}
		values := response.Header.Values("Set-Cookie")
		response.Header.Del("Set-Cookie")
		for _, value := range values {
			if setCookieName(value) != browserSessionCookieName {
				response.Header.Add("Set-Cookie", value)
			}
		}
		return nil
	}
}

func removeRequestCookie(r *http.Request, name string) {
	cookies := r.Cookies()
	r.Header.Del("Cookie")
	for _, cookie := range cookies {
		if cookie.Name != name {
			r.AddCookie(cookie)
		}
	}
}

func setCookieName(value string) string {
	pair, _, _ := strings.Cut(value, ";")
	name, _, found := strings.Cut(strings.TrimSpace(pair), "=")
	if !found {
		return ""
	}
	return strings.TrimSpace(name)
}

func (s *Server) rbacMiddleware(app model.Application, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := authenticatedUserID(r.Context())
		if userID == "" {
			s.writeUnauthorizedForApp(w, r, app)
			return
		}
		if err := s.authorizeApp(r.Context(), userID, app.ID); err != nil {
			s.writeForbidden(w, r, app)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authorizeApp(ctx context.Context, userID, appID string) error {
	if s.authorizer == nil {
		return errors.New("application authorization unavailable")
	}
	decision, err := s.authorizer.Authorize(ctx, service.AuthorizationRequest{
		UserID:       userID,
		Actions:      []string{rbac.ActionAppConnect},
		ResourceType: model.ResourceTypeApplication,
		ResourceID:   appID,
	})
	if err != nil {
		return fmt.Errorf("authorize application: %w", err)
	}
	if !decision.Allowed {
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = "denied"
		}
		return fmt.Errorf("application authorization denied: %s", reason)
	}
	return nil
}

func authenticatedUserID(ctx context.Context) string {
	userID, _ := ctx.Value(authenticatedUserIDContextKey{}).(string)
	return strings.TrimSpace(userID)
}

func (s *Server) shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for port, entry := range s.proxies {
		_ = entry.server.Close()
		delete(s.proxies, port)
	}
}
