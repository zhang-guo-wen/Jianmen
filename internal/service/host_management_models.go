package service

import (
	"fmt"
	"strings"
	"time"
)

type HostManagementHostRecord struct {
	ID, Name, Group, Address, Protocol, Remark, Status string
	Port                                               int
}

type HostManagementHostView struct {
	ID, Name, Group, Address, Protocol, Remark, Status string
	Port, AccountCount                                 int
	CreatedAt, UpdatedAt                               string
	CanManage                                          bool
}

type HostManagementTargetView struct {
	ID, HostID, ResourceType, ResourceID, HostResourceID, Name, Group, Remark, ExpiresAt, Status, Host, Protocol, Username, Domain                                    string
	ResourceSeq                                                                                                                                                       int
	Port                                                                                                                                                              int
	AuthMethods                                                                                                                                                       []string
	InsecureIgnoreHostKey, RDPIgnoreCertificate, RDPApprovalRequired, RDPClipboardRead, RDPClipboardWrite, RDPFileUpload, RDPFileDownload, RDPDriveMapping, CanManage bool
	HostKeyFingerprint, KnownHostsPath, RDPSecurity, RDPCertFingerprints                                                                                              string
}

type HostManagementTargetConfig struct {
	ID, Name, HostName, Host, Protocol, Username, Domain, Password, PrivateKeyPath, PrivateKeyPEM, Passphrase                                                        string
	Port                                                                                                                                                             int
	InsecureIgnoreHostKey, RDPIgnoreCertificate, RDPApprovalRequired, RDPClipboardRead, RDPClipboardWrite, RDPFileUpload, RDPFileDownload, RDPDriveMapping, Disabled bool
	HostKeyFingerprint, KnownHostsPath, RDPSecurity, RDPCertFingerprints, ExpiresAt, HostID                                                                          string
}

func (t HostManagementTargetConfig) Addr() string {
	if strings.TrimSpace(t.Host) == "" || t.Port <= 0 {
		return ""
	}
	host := strings.TrimSpace(t.Host)
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("%s:%d", host, t.Port)
}

func (t HostManagementTargetConfig) Expired(now time.Time) bool {
	if strings.TrimSpace(t.ExpiresAt) == "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, t.ExpiresAt)
	return err == nil && !now.Before(expiresAt)
}
