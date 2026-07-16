package appproxy

import (
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"strings"

	"jianmen/internal/model"
)

func (s *Server) writeUnauthorized(w http.ResponseWriter, r *http.Request) {
	s.writeUnauthorizedForApp(w, r, model.Application{})
}

func (s *Server) writeUnauthorizedForApp(w http.ResponseWriter, r *http.Request, app model.Application) {
	w.Header().Set("Cache-Control", "no-store")
	if !isPageNavigation(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, s.loginRedirectURLForApp(r, app), http.StatusFound)
}

func (s *Server) writeForbidden(w http.ResponseWriter, r *http.Request, app model.Application) {
	if !isPageNavigation(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	s.writeStatusPage(w, http.StatusForbidden, "暂无访问权限",
		fmt.Sprintf("你已登录，但当前账号没有访问“%s”的权限。请联系管理员为你授权。", app.Name),
		"返回 Jianmen", s.adminHomeURL(r))
}

func (s *Server) proxyErrorHandler(app model.Application) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		s.logger.Error("application upstream request failed",
			"name", app.Name,
			"path", r.URL.Path,
			"error", err,
		)
		if !isPageNavigation(r) {
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		s.writeStatusPage(w, http.StatusBadGateway, "应用暂时无法访问",
			"Jianmen 无法连接到目标应用，请稍后重试或联系管理员检查上游地址和网络。",
			"重新加载", r.URL.RequestURI())
	}
}

func (s *Server) writeStatusPage(w http.ResponseWriter, status int, title, message, action, actionURL string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, statusPageHTML,
		html.EscapeString(title),
		html.EscapeString(title),
		html.EscapeString(message),
		html.EscapeString(actionURL),
		html.EscapeString(action),
	)
}

func isPageNavigation(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Mode")), "navigate") {
		return true
	}
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html")
}

func (s *Server) loginRedirectURL(r *http.Request) string {
	return s.loginRedirectURLForApp(r, model.Application{})
}

func (s *Server) loginRedirectURLForApp(r *http.Request, app model.Application) string {
	returnURL := requestPublicURL(r)
	if entryURL, err := url.Parse(strings.TrimSpace(app.EntryPath)); err == nil && entryURL.Fragment != "" && entryURL.Path == r.URL.Path && entryURL.RawQuery == r.URL.RawQuery {
		if parsedReturnURL, parseErr := url.Parse(returnURL); parseErr == nil {
			parsedReturnURL.Fragment = entryURL.Fragment
			returnURL = parsedReturnURL.String()
		}
	}

	loginURL := s.adminBaseURL(r)
	loginURL.Path = "/login"
	query := loginURL.Query()
	query.Set("redirect", returnURL)
	loginURL.RawQuery = query.Encode()
	return loginURL.String()
}

func (s *Server) adminHomeURL(r *http.Request) string {
	homeURL := s.adminBaseURL(r)
	homeURL.Path = "/"
	return homeURL.String()
}

func (s *Server) adminBaseURL(r *http.Request) *url.URL {
	if raw := strings.TrimSpace(s.adminCfg.PublicURL); raw != "" {
		parsed, err := url.Parse(raw)
		if err == nil && parsed.Scheme != "" && parsed.Host != "" {
			copyURL := *parsed
			copyURL.Path = strings.TrimRight(copyURL.Path, "/")
			copyURL.RawQuery = ""
			copyURL.Fragment = ""
			return &copyURL
		}
		s.logger.Warn("invalid admin public URL; deriving from request", "public_url", raw)
	}

	host := requestHostname(r)
	_, adminPort, err := net.SplitHostPort(s.adminCfg.ListenAddr)
	if err != nil || adminPort == "" {
		adminPort = "47100"
	}
	return &url.URL{Scheme: requestScheme(r), Host: net.JoinHostPort(host, adminPort)}
}

func requestPublicURL(r *http.Request) string {
	return (&url.URL{
		Scheme:   requestScheme(r),
		Host:     r.Host,
		Path:     r.URL.Path,
		RawPath:  r.URL.RawPath,
		RawQuery: r.URL.RawQuery,
	}).String()
}

func requestScheme(r *http.Request) string {
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded == "http" || forwarded == "https" {
		return forwarded
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func requestHostname(r *http.Request) string {
	host := r.Host
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		return parsedHost
	}
	return strings.Trim(host, "[]")
}

const statusPageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>%s - Jianmen</title>
  <style>
    :root { color-scheme: light; font-family: "Microsoft YaHei", "PingFang SC", sans-serif; }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; display: grid; place-items: center; padding: 24px; color: #172033; background: radial-gradient(circle at 18%% 16%%, #dbeafe 0, transparent 34%%), linear-gradient(145deg, #f8fafc, #eef2f7); }
    main { width: min(520px, 100%%); padding: 38px; border: 1px solid rgba(148, 163, 184, .28); border-radius: 18px; background: rgba(255,255,255,.9); box-shadow: 0 24px 70px rgba(15,23,42,.12); }
    .mark { display: inline-grid; place-items: center; width: 44px; height: 44px; margin-bottom: 22px; border-radius: 12px; color: #fff; background: #2563eb; font-size: 22px; font-weight: 700; }
    h1 { margin: 0 0 12px; font-size: 26px; letter-spacing: -.02em; }
    p { margin: 0 0 28px; color: #596579; line-height: 1.75; }
    a { display: inline-flex; min-height: 42px; align-items: center; justify-content: center; padding: 0 18px; border-radius: 10px; color: #fff; background: #172033; text-decoration: none; font-weight: 600; }
    a:hover { background: #28364e; }
  </style>
</head>
<body>
  <main>
    <div class="mark">J</div>
    <h1>%s</h1>
    <p>%s</p>
    <a href="%s">%s</a>
  </main>
</body>
</html>`
