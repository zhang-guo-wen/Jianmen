package admin

import (
	"context"
	"errors"
	"fmt"
	"net"
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
	listener, err := net.Listen("tcp", s.cfg.Admin.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen for admin server: %w", err)
	}
	return s.serveAdmin(ctx, listener)
}

func (s *Server) serveAdmin(ctx context.Context, listener net.Listener) error {
	// http.Server.Serve closes the listener after it starts serving, but
	// ServeTLS can fail while loading the certificate before Serve takes
	// ownership. Keep the lifecycle explicit so every startup error releases
	// the bound port.
	defer listener.Close()

	server := &http.Server{
		Addr:              listener.Addr().String(),
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	stopped := make(chan struct{})
	defer close(stopped)
	go func() {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdownCtx)
		case <-stopped:
		}
	}()

	certFile := strings.TrimSpace(s.cfg.Admin.TLS.CertFile)
	keyFile := strings.TrimSpace(s.cfg.Admin.TLS.KeyFile)
	if certFile != "" && keyFile != "" {
		s.logger.Info("admin server listening", "addr", listener.Addr().String(), "tls", true)
		err := server.ServeTLS(listener, certFile, keyFile)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}

	if s.cfg.Admin.TLS.AllowInsecureHTTP {
		s.logger.Warn("admin server is using explicitly allowed insecure HTTP", "addr", listener.Addr().String())
	}
	s.logger.Info("admin server listening", "addr", listener.Addr().String(), "tls", false)
	err := server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
