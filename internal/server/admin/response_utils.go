package admin

import (
	"bufio"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jianmen/internal/pkg/apiresp"
)

func safeReplayDir(root, id string) (string, bool) {
	if id == "" || strings.ContainsAny(id, `/\.`) {
		return "", false
	}
	return filepath.Join(root, id), true
}

func (s *Server) writeJSONFile(w http.ResponseWriter, r *http.Request, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.writeErrorText(w, r, http.StatusNotFound, "not found")
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, apiresp.CodeInternal, err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(raw)
}

func (s *Server) writeTextFile(w http.ResponseWriter, r *http.Request, path, contentType string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.writeErrorText(w, r, http.StatusNotFound, "not found")
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, apiresp.CodeInternal, err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(raw)
}

func (s *Server) writeJSONLines(w http.ResponseWriter, r *http.Request, path string, limit int) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.writeJSON(w, r, http.StatusOK, []any{})
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, apiresp.CodeInternal, err.Error(), nil)
		return
	}
	defer file.Close()

	items := make([]map[string]any, 0)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		if limit > 0 && len(items) >= limit {
			break
		}
		var item map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &item); err == nil {
			items = append(items, item)
		}
	}
	if err := scanner.Err(); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, apiresp.CodeInternal, err.Error(), nil)
		return
	}
	s.writeJSON(w, r, http.StatusOK, items)
}

func readJSON(path string, dst any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

// writeJSON 写统一格式成功响应
func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, status int, value any) {
	reqID := apiresp.RequestID(r.Context())
	apiresp.Write(w, status, value, reqID)
}

// writeError 写统一格式错误响应
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, errCode string, message string, details any) {
	reqID := apiresp.RequestID(r.Context())
	apiresp.WriteError(w, status, errCode, message, details, reqID)
}

// writeErrorText 便捷方法：根据 HTTP 状态码自动映射 errCode
func (s *Server) writeErrorText(w http.ResponseWriter, r *http.Request, status int, message string) {
	var errCode string
	switch status {
	case http.StatusBadRequest:
		errCode = apiresp.CodeValidation
	case http.StatusUnauthorized:
		errCode = apiresp.CodeUnauthorized
	case http.StatusForbidden:
		errCode = apiresp.CodeForbidden
	case http.StatusNotFound:
		errCode = apiresp.CodeNotFound
	case http.StatusMethodNotAllowed:
		errCode = apiresp.CodeMethodNotAllowed
	case http.StatusConflict:
		errCode = apiresp.CodeConflict
	case http.StatusInternalServerError:
		errCode = apiresp.CodeInternal
	case http.StatusServiceUnavailable:
		errCode = apiresp.CodeServiceUnavailable
	case http.StatusTooManyRequests:
		errCode = apiresp.CodeTooManyRequests
	case http.StatusPreconditionFailed:
		errCode = apiresp.CodePreconditionFailed
	case http.StatusBadGateway:
		errCode = apiresp.CodeBadGateway
	default:
		errCode = apiresp.CodeInternal
	}
	s.writeError(w, r, status, errCode, message, nil)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func logRequests(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		logger.Debug("admin request", "method", r.Method, "path", r.URL.Path, "elapsed", time.Since(started))
	})
}

func withCORS(allowedOrigins []string, next http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			allowed[origin] = struct{}{}
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if _, ok := allowed[origin]; ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func friendlySSHError(err error) string {
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "unable to authenticate") || strings.Contains(lower, "no supported methods"):
		return "认证失败，请检查用户名和密码/私钥是否正确"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "i/o timeout"):
		return "连接超时，请检查主机地址和端口是否可达"
	case strings.Contains(lower, "connection refused"):
		return "连接被拒绝，请检查主机地址和端口是否正确，以及 SSH 服务是否已启动"
	case strings.Contains(lower, "no route to host") || strings.Contains(lower, "no such host"):
		return "无法访问主机，请检查主机地址和网络连接"
	case strings.Contains(lower, "host key") && strings.Contains(lower, "mismatch"):
		return "主机密钥不匹配，可能目标主机已变更或存在中间人攻击"
	default:
		return msg
	}
}
