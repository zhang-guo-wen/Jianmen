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
)

func safeReplayDir(root, id string) (string, bool) {
	if id == "" || strings.ContainsAny(id, `/\.`) {
		return "", false
	}
	return filepath.Join(root, id), true
}

func writeJSONFile(w http.ResponseWriter, path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeErrorText(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(raw)
}

func writeTextFile(w http.ResponseWriter, path, contentType string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeErrorText(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(raw)
}

func writeJSONLines(w http.ResponseWriter, path string, limit int) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		writeError(w, http.StatusInternalServerError, err)
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
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func readJSON(path string, dst any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeErrorText(w, status, err.Error())
}

func writeErrorText(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
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
