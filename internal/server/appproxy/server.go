package appproxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/config"
	"jianmen/internal/model"
	rbaccheck "jianmen/internal/rbac"
)

type Server struct {
	cfg           config.ApplicationGatewayConfig
	db            *gorm.DB
	checker       *rbaccheck.Checker
	superAdminIDs map[string]bool
	logger        *slog.Logger

	mu      sync.Mutex
	proxies map[int]*proxyEntry
}

type proxyEntry struct {
	app    model.Application
	server *http.Server
	proxy  *httputil.ReverseProxy
}

func New(cfg config.ApplicationGatewayConfig, db *gorm.DB, superAdminIDs map[string]bool, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		cfg:           cfg,
		db:            db,
		checker:       rbaccheck.NewChecker(db),
		superAdminIDs: superAdminIDs,
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

func (s *Server) UpdateProxy(app model.Application) error {
	s.RemoveProxy(app.ListenPort)
	return s.AddProxy(app)
}

func (s *Server) startProxy(app model.Application) error {
	target := fmt.Sprintf("%s://%s:%d", app.InternalScheme, app.InternalHost, app.InternalPort)
	targetURL, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("parse target %q: %w", target, err)
	}

	rp := httputil.NewSingleHostReverseProxy(targetURL)
	handler := s.authMiddleware(s.rbacMiddleware(app, rp))

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

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("jianmen_token")
		if err != nil {
			auth := r.Header.Get("Authorization")
			token := strings.TrimPrefix(auth, "Bearer ")
			if token != "" && token != auth {
				if !s.validateToken(token) {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
			} else {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		} else if !s.validateToken(cookie.Value) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) rbacMiddleware(app model.Application, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := s.getUserID(r)
		if userID == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if s.superAdminIDs[userID] {
			next.ServeHTTP(w, r)
			return
		}
		allowed, err := s.checker.HasPermission(userID, rbaccheck.ActionAppConnect, model.ResourceTypeApplication, app.ID)
		if err != nil || !allowed {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) validateToken(token string) bool {
	var user model.User
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])
	err := s.db.Where("token_hash = ? AND status = ?", tokenHash, "active").First(&user).Error
	return err == nil
}

func (s *Server) getUserID(r *http.Request) string {
	cookie, err := r.Cookie("jianmen_token")
	if err != nil {
		auth := r.Header.Get("Authorization")
		token := strings.TrimPrefix(auth, "Bearer ")
		if token == "" || token == auth {
			return ""
		}
		return s.userIDForToken(token)
	}
	return s.userIDForToken(cookie.Value)
}

func (s *Server) userIDForToken(token string) string {
	var user model.User
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])
	if err := s.db.Where("token_hash = ? AND status = ?", tokenHash, "active").First(&user).Error; err != nil {
		return ""
	}
	return user.ID
}

func (s *Server) shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for port, entry := range s.proxies {
		_ = entry.server.Close()
		delete(s.proxies, port)
	}
}
