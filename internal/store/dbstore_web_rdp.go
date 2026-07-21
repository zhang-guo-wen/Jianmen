package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

func (s *DBStore) WebRDPTarget(ctx context.Context, targetID string) (service.WebRDPTarget, error) {
	var account model.HostAccount
	err := s.db.WithContext(ctx).
		Preload("Host").
		First(&account, "id = ?", strings.TrimSpace(targetID)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return service.WebRDPTarget{}, fmt.Errorf("%w: %q", ErrTargetNotFound, targetID)
	}
	if err != nil {
		return service.WebRDPTarget{}, fmt.Errorf("load RDP target: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(account.Host.Protocol), "rdp") {
		return service.WebRDPTarget{}, errors.New("target is not an RDP host account")
	}
	return service.WebRDPTarget{
		ID: account.ID, HostID: account.HostID, HostName: account.Host.Name,
		Protocol: account.Host.Protocol,
		Address:  account.Host.Address, Port: account.Host.Port,
		Username: account.Username, Domain: account.Domain,
		Password:               account.Password.GetPlaintext(),
		Security:               normalizedRDPSecurity(account.RDPSecurity),
		IgnoreCertificate:      account.RDPIgnoreCertificate,
		CertificateFingerprint: strings.TrimSpace(account.RDPCertFingerprints),
		ClipboardRead:          account.RDPClipboardRead,
		ClipboardWrite:         account.RDPClipboardWrite,
		FileUpload:             account.RDPFileUpload,
		FileDownload:           account.RDPFileDownload,
		DriveMapping:           account.RDPDriveMapping,
		Disabled:               account.Status == "disabled" || account.Host.Status == "disabled",
		ExpiresAt:              account.ExpiresAt,
	}, nil
}
