package admin

import (
	"errors"
	"net/http"

	"jianmen/internal/service"
	"jianmen/internal/sshhost"
)

const (
	codeSSHHostKeyChanged             = "SSH_HOST_KEY_CHANGED"
	codeSSHHostIdentityUnavailable    = "SSH_HOST_KEY_UNAVAILABLE"
	codeSSHHostIdentityRefreshFailure = "SSH_HOST_IDENTITY_REFRESH_FAILED"
)

func (s *Server) writeSSHHostIdentityError(w http.ResponseWriter, r *http.Request, err error) bool {
	var changed *sshhost.KeyChangedError
	if errors.As(err, &changed) {
		status := "unchanged"
		if changed.HostDisabled {
			status = "disabled"
		}
		s.writeError(w, r, http.StatusConflict, codeSSHHostKeyChanged, "SSH 主机密钥已变更，连接已拒绝", map[string]any{
			"host_id":         changed.HostID,
			"old_fingerprint": changed.OldFingerprint,
			"new_fingerprint": changed.NewFingerprint,
			"host_disabled":   changed.HostDisabled,
			"host_status":     status,
		})
		return true
	}

	var transportUnavailable *sshhost.IdentityUnavailableError
	if errors.As(err, &transportUnavailable) {
		s.writeSSHHostIdentityUnavailable(w, r, transportUnavailable.HostID)
		return true
	}
	var serviceUnavailable *service.HostIdentityUnavailableError
	if errors.As(err, &serviceUnavailable) {
		s.writeSSHHostIdentityUnavailable(w, r, serviceUnavailable.HostID)
		return true
	}

	var refresh *service.HostIdentityRefreshError
	if errors.As(err, &refresh) {
		hostStatus := refresh.HostStatus
		if hostStatus == "" {
			hostStatus = "disabled"
		}
		identityStatus := refresh.IdentityStatus
		if identityStatus == "" {
			identityStatus = "unavailable"
		}
		s.writeError(w, r, http.StatusConflict, codeSSHHostIdentityRefreshFailure, "无法采集 SSH 主机身份，主机状态未变更", map[string]any{
			"host_id":         refresh.HostID,
			"identity_status": identityStatus,
			"host_status":     hostStatus,
		})
		return true
	}
	return false
}

func (s *Server) writeSSHHostIdentityUnavailable(w http.ResponseWriter, r *http.Request, hostID string) {
	s.writeError(w, r, http.StatusPreconditionFailed, codeSSHHostIdentityUnavailable, "SSH 主机身份尚未采集，连接已拒绝", map[string]any{
		"host_id":         hostID,
		"identity_status": "unavailable",
	})
}
