package rbac

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
)

const (
	ActionDBConnect         = "db:connect"
	ActionSessionConnect    = "session:connect"
	ActionSFTPRead          = "sftp:read"
	ActionSFTPWrite         = "sftp:write"
	ActionAuditView         = "audit:view"
	ActionDBAuditView       = "db:audit:view"
	ActionHostCreate        = "host:create"
	ActionHostUpdate        = "host:update"
	ActionHostDelete        = "host:delete"
	ActionHostView          = "host:view"
	ActionTargetCreate      = "target:create"
	ActionTargetUpdate      = "target:update"
	ActionTargetDelete      = "target:delete"
	ActionTargetView        = "target:view"
	ActionDBProxyCreate     = "dbproxy:create"
	ActionDBProxyUpdate     = "dbproxy:update"
	ActionDBProxyDelete     = "dbproxy:delete"
	ActionDBProxyView       = "dbproxy:view"
	ActionRBACManage        = "rbac:manage"
	ActionSessionView       = "session:view"
	ActionDashboardView     = "dashboard:view"
)

func DatabaseAccountResourceID(uniqueName string) string {
	return stableResourceID("dbacct", uniqueName)
}

func stableResourceID(prefix string, parts ...string) string {
	rawParts := append([]string{prefix}, parts...)
	raw := strings.Join(rawParts, "-")
	value := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		default:
			return '-'
		}
	}, raw)
	value = strings.Trim(value, "-")
	if value == "" {
		value = prefix
	}
	if len(value) <= 64 {
		return value
	}
	sum := sha1.Sum([]byte(raw))
	suffix := hex.EncodeToString(sum[:])[:12]
	headLen := 64 - 1 - len(suffix)
	head := strings.Trim(value[:headLen], "-")
	if head == "" {
		return suffix
	}
	return head + "-" + suffix
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
