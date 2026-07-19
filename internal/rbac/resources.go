package rbac

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
)

const (
	ActionDBConnect         = "db:connect"
	ActionSessionConnect    = "session:connect"
	ActionSFTPConnect       = "sftp:connect"
	ActionRDPConnect        = "rdp:connect"
	ActionRDPClipboardRead  = "rdp:clipboard:read"
	ActionRDPClipboardWrite = "rdp:clipboard:write"
	ActionRDPFileUpload     = "rdp:file:upload"
	ActionRDPFileDownload   = "rdp:file:download"
	ActionRDPDriveMap       = "rdp:drive:map"
	ActionRDPRecordingView  = "rdp:recording:view"
	ActionRDPApprovalManage = "rdp:approval:manage"
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
	ActionSessionDisconnect = "session:disconnect"
	ActionAppCreate         = "application:create"
	ActionAppUpdate         = "application:update"
	ActionAppDelete         = "application:delete"
	ActionAppView           = "application:view"
	ActionAppConnect        = "app:connect"
	ActionAIManage          = "ai:access:manage"
	ActionContainerCreate   = "container:create"
	ActionContainerUpdate   = "container:update"
	ActionContainerDelete   = "container:delete"
	ActionContainerView     = "container:view"
	ActionContainerConnect  = "container:connect"

	ActionPlatformAccountCreate = "platform_account:create"
	ActionPlatformAccountUpdate = "platform_account:update"
	ActionPlatformAccountDelete = "platform_account:delete"
	ActionPlatformAccountView   = "platform_account:view"
	ActionPlatformAccountUse    = "platform_account:use"
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
