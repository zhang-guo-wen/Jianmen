package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func (s *Server) ListenAndServe(ctx context.Context) error {
	if s == nil || s.cfg == nil {
		return errors.New("admin server config is required")
	}
	if err := s.cfg.Validate(); err != nil {
		return fmt.Errorf("invalid admin configuration: %w", err)
	}
	server := &http.Server{
		Addr:              s.cfg.Admin.ListenAddr,
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	certFile := strings.TrimSpace(s.cfg.Admin.TLS.CertFile)
	keyFile := strings.TrimSpace(s.cfg.Admin.TLS.KeyFile)
	if certFile != "" && keyFile != "" {
		s.logger.Info("admin server listening", "addr", s.cfg.Admin.ListenAddr, "tls", true)
		err := server.ListenAndServeTLS(certFile, keyFile)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}

	if s.cfg.Admin.TLS.AllowInsecureHTTP {
		s.logger.Warn("admin server is using explicitly allowed insecure HTTP", "addr", s.cfg.Admin.ListenAddr)
	}
	s.logger.Info("admin server listening", "addr", s.cfg.Admin.ListenAddr, "tls", false)
	err := server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
