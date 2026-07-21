package admin

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"jianmen/internal/pkg/apiresp"
	"jianmen/internal/server/dbproxy"
	"jianmen/internal/service"
)

type databaseTLSPreflightPayload struct {
	InstanceID    string  `json:"instance_id"`
	Protocol      string  `json:"protocol"`
	Address       string  `json:"address"`
	Port          int     `json:"port"`
	TLSMode       string  `json:"tls_mode"`
	TLSServerName string  `json:"tls_server_name"`
	TLSCAPEM      *string `json:"tls_ca_pem"`
	ClearTLSCA    bool    `json:"clear_tls_ca"`
}

type databaseTLSPreflightProber struct{}

func (databaseTLSPreflightProber) ProbeTLS(ctx context.Context, instance service.DatabaseInstanceRecord) error {
	return dbproxy.ProbeUpstreamTLS(ctx, databaseInstanceRecordToModel(instance))
}

func (s *Server) handleDatabaseTLSPreflight(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		s.writeErrorText(w, r, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	var payload databaseTLSPreflightPayload
	if err := decoder.Decode(&payload); err != nil {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid database TLS preflight request")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		s.writeErrorText(w, r, http.StatusBadRequest, "invalid database TLS preflight request")
		return
	}

	started := time.Now()
	actorID := userIDFromRequest(r)
	err := s.databaseTLSPreflight.Probe(r.Context(), actorID, service.DatabaseTLSPreflightInput{
		InstanceID:    payload.InstanceID,
		Protocol:      payload.Protocol,
		Address:       payload.Address,
		Port:          payload.Port,
		TLSMode:       payload.TLSMode,
		TLSServerName: payload.TLSServerName,
		TLSCAPEM:      payload.TLSCAPEM,
		ClearTLSCA:    payload.ClearTLSCA,
	})
	if err != nil && !errors.Is(err, service.ErrDatabaseTLSPreflightFailed) {
		s.writeDatabaseManagementError(w, r, err)
		return
	}
	response := map[string]any{
		"ok":         err == nil,
		"latency_ms": time.Since(started).Milliseconds(),
	}
	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}
	if err != nil {
		failure := classifyDatabaseTLSPreflightError(err)
		response["stage"] = failure.Stage
		response["code"] = failure.Code
		response["message"] = failure.Message
		response["error"] = failure.Message
		logger.Warn(
			"database TLS preflight failed",
			"request_id", apiresp.RequestID(r.Context()),
			"actor_id", actorID,
			"instance_id", payload.InstanceID,
			"address", payload.Address,
			"protocol", payload.Protocol,
			"code", failure.Code,
		)
	} else {
		logger.Info(
			"database TLS preflight succeeded",
			"request_id", apiresp.RequestID(r.Context()),
			"actor_id", actorID,
			"instance_id", payload.InstanceID,
			"address", payload.Address,
			"protocol", payload.Protocol,
		)
	}
	s.writeJSON(w, r, http.StatusOK, response)
}

type databaseTLSPreflightFailure struct {
	Stage, Code, Message string
}

func classifyDatabaseTLSPreflightError(err error) databaseTLSPreflightFailure {
	if errors.Is(err, dbproxy.ErrUpstreamTLSUnsupported) {
		return databaseTLSPreflightFailure{"capability", "tls_unsupported", "远程数据库未启用 SSL/TLS，请先在数据库服务端启用 TLS"}
	}
	if errors.Is(err, context.Canceled) {
		return databaseTLSPreflightFailure{"connect", "cancelled", "TLS 检测已取消"}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return databaseTLSPreflightFailure{"connect", "tcp_timeout", "连接远程数据库超时，请检查地址、端口和网络"}
	}
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) {
		return databaseTLSPreflightFailure{"dns", "dns_failed", "无法解析远程数据库地址"}
	}
	var hostnameError x509.HostnameError
	if errors.As(err, &hostnameError) {
		return databaseTLSPreflightFailure{"certificate", "hostname_mismatch", "证书主机名与远程数据库地址不匹配"}
	}
	var authorityError x509.UnknownAuthorityError
	if errors.As(err, &authorityError) {
		return databaseTLSPreflightFailure{"certificate", "ca_untrusted", "证书不受信任，请使用受系统信任的证书或配置正确的自定义 CA"}
	}
	var certificateError x509.CertificateInvalidError
	if errors.As(err, &certificateError) {
		if certificateError.Cert != nil && time.Now().Before(certificateError.Cert.NotBefore) {
			return databaseTLSPreflightFailure{"certificate", "certificate_not_yet_valid", "远程数据库证书尚未生效"}
		}
		if certificateError.Cert != nil && time.Now().After(certificateError.Cert.NotAfter) {
			return databaseTLSPreflightFailure{"certificate", "certificate_expired", "远程数据库证书已过期"}
		}
		return databaseTLSPreflightFailure{"certificate", "certificate_invalid", "远程数据库证书验证失败"}
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return databaseTLSPreflightFailure{"connect", "tcp_refused", "远程数据库拒绝连接，请检查地址、端口和监听配置"}
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return databaseTLSPreflightFailure{"connect", "tcp_timeout", "连接远程数据库超时，请检查地址、端口和网络"}
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "mysql greeting") || strings.Contains(message, "postgresql ssl response") {
		return databaseTLSPreflightFailure{"protocol", "protocol_mismatch", "远程端口返回了非预期的数据库协议响应"}
	}
	return databaseTLSPreflightFailure{"handshake", "tls_handshake_failed", "TLS 握手失败，请确认远程数据库 TLS 端口和证书配置"}
}
