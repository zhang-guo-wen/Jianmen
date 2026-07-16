package appproxy

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/model"
)

func TestWriteUnauthorizedRedirectsPageNavigationToLogin(t *testing.T) {
	s := &Server{adminCfg: config.AdminConfig{
		ListenAddr: "127.0.0.1:47100",
		PublicURL:  "http://127.0.0.1:47101",
	}}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:47110/nacos/?namespace=dev", nil)
	req.Header.Set("Accept", "text/html")
	recorder := httptest.NewRecorder()

	s.writeUnauthorized(recorder, req)

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusFound)
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	if got, want := location.Scheme+"://"+location.Host+location.Path, "http://127.0.0.1:47101/login"; got != want {
		t.Fatalf("login URL = %q, want %q", got, want)
	}
	if got, want := location.Query().Get("redirect"), "http://127.0.0.1:47110/nacos/?namespace=dev"; got != want {
		t.Fatalf("return URL = %q, want %q", got, want)
	}
}

func TestWriteUnauthorizedKeepsAPIResponseAs401(t *testing.T) {
	s := &Server{adminCfg: config.AdminConfig{ListenAddr: "127.0.0.1:47100"}}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:47110/api/data", nil)
	req.Header.Set("Accept", "application/json")
	recorder := httptest.NewRecorder()

	s.writeUnauthorized(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if location := recorder.Header().Get("Location"); location != "" {
		t.Fatalf("unexpected redirect location %q", location)
	}
}

func TestLoginRedirectDerivesAdminPortFromRequestHost(t *testing.T) {
	s := &Server{adminCfg: config.AdminConfig{ListenAddr: "0.0.0.0:47100"}}
	req := httptest.NewRequest(http.MethodGet, "http://gateway.example.com:47110/nacos/", nil)

	location, err := url.Parse(s.loginRedirectURL(req))
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	if got, want := location.Scheme+"://"+location.Host+location.Path, "http://gateway.example.com:47100/login"; got != want {
		t.Fatalf("login URL = %q, want %q", got, want)
	}
}

func TestLoginRedirectHonorsForwardedHTTPS(t *testing.T) {
	s := &Server{adminCfg: config.AdminConfig{
		ListenAddr: "0.0.0.0:47100",
		PublicURL:  "https://gateway.example.com",
	}}
	req := httptest.NewRequest(http.MethodGet, "http://gateway.example.com:47110/nacos/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")

	location, err := url.Parse(s.loginRedirectURL(req))
	if err != nil {
		t.Fatalf("parse redirect location: %v", err)
	}
	if got, want := location.Query().Get("redirect"), "https://gateway.example.com:47110/nacos/"; got != want {
		t.Fatalf("return URL = %q, want %q", got, want)
	}
}

func TestWriteForbiddenRendersFriendlyEscapedPage(t *testing.T) {
	s := &Server{adminCfg: config.AdminConfig{
		ListenAddr: "127.0.0.1:47100",
		PublicURL:  "http://127.0.0.1:47101",
	}}
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:47110/", nil)
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	recorder := httptest.NewRecorder()

	s.writeForbidden(recorder, req, model.Application{Name: `<script>alert("x")</script>`})

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "<script>") {
		t.Fatalf("response contains unescaped application name: %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("response does not contain escaped application name: %s", body)
	}
}

func TestProxyErrorHandlerRendersHTMLOnlyForNavigation(t *testing.T) {
	s := &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	handler := s.proxyErrorHandler(model.Application{Name: "console"})

	pageReq := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:47110/", nil)
	pageReq.Header.Set("Accept", "text/html")
	pageRecorder := httptest.NewRecorder()
	handler(pageRecorder, pageReq, errors.New("dial failed"))
	if pageRecorder.Code != http.StatusBadGateway {
		t.Fatalf("page status = %d, want %d", pageRecorder.Code, http.StatusBadGateway)
	}
	if contentType := pageRecorder.Header().Get("Content-Type"); !strings.Contains(contentType, "text/html") {
		t.Fatalf("page content type = %q", contentType)
	}

	apiReq := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:47110/api", nil)
	apiReq.Header.Set("Accept", "application/json")
	apiRecorder := httptest.NewRecorder()
	handler(apiRecorder, apiReq, errors.New("dial failed"))
	if apiRecorder.Code != http.StatusBadGateway {
		t.Fatalf("API status = %d, want %d", apiRecorder.Code, http.StatusBadGateway)
	}
	if contentType := apiRecorder.Header().Get("Content-Type"); strings.Contains(contentType, "text/html") {
		t.Fatalf("API content type = %q, want non-HTML", contentType)
	}
}
