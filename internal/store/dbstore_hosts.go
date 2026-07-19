package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"jianmen/internal/model"
)

// -- hosts --

func (s *DBStore) hostView(ctx context.Context, m model.Host, accountCount ...int) HostView {
	status := m.Status
	if status == "" {
		status = "active"
	}
	count := 0
	if len(accountCount) > 0 {
		count = accountCount[0]
	}
	return HostView{
		ID: m.ID, Name: m.Name, Group: m.GroupName, Address: m.Address,
		Port: m.Port, Protocol: normalizedHostProtocol(m.Protocol), Remark: m.Remark, Status: status,
		AccountCount: count,
		CreatedAt:    m.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    m.UpdatedAt.Format(time.RFC3339),
	}
}

func (s *DBStore) Hosts(ctx context.Context) ([]HostView, error) {
	var hosts []model.Host
	if err := s.db.WithContext(ctx).Order("created_at DESC").Find(&hosts).Error; err != nil {
		return nil, err
	}
	counts, err := s.hostAccountCounts(ctx, hostIDs(hosts))
	if err != nil {
		return nil, err
	}
	out := make([]HostView, len(hosts))
	for i := range hosts {
		out[i] = s.hostView(ctx, hosts[i], counts[hosts[i].ID])
	}
	return out, nil
}

func (s *DBStore) Host(ctx context.Context, id string) (HostView, error) {
	var m model.Host
	if err := s.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return HostView{}, err
		}
		return HostView{}, fmt.Errorf("%w: %q", ErrHostNotFound, id)
	}
	counts, err := s.hostAccountCounts(ctx, []string{m.ID})
	if err != nil {
		return HostView{}, err
	}
	return s.hostView(ctx, m, counts[m.ID]), nil
}

func (s *DBStore) hostAccountCounts(ctx context.Context, ids []string) (map[string]int, error) {
	counts := make(map[string]int, len(ids))
	if len(ids) == 0 {
		return counts, nil
	}
	var rows []struct {
		HostID string
		Count  int64
	}
	if err := s.db.WithContext(ctx).Model(&model.HostAccount{}).
		Select("host_id, COUNT(*) AS count").
		Where("host_id IN ?", ids).
		Group("host_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.HostID] = int(row.Count)
	}
	return counts, nil
}

func hostIDs(hosts []model.Host) []string {
	ids := make([]string, 0, len(hosts))
	for _, host := range hosts {
		if host.ID != "" {
			ids = append(ids, host.ID)
		}
	}
	return ids
}

func (s *DBStore) AddHost(ctx context.Context, host HostRecord) (HostView, error) {
	return s.createHost(ctx, host, "")
}

// CreateManagedHost atomically creates the host, its resource row, and the
// non-super-administrator creator grant when creatorID is provided.
func (s *DBStore) CreateManagedHost(ctx context.Context, host HostRecord, creatorID string) (HostView, error) {
	return s.createHost(ctx, host, strings.TrimSpace(creatorID))
}

func (s *DBStore) createHost(ctx context.Context, host HostRecord, creatorID string) (HostView, error) {
	if err := validateHostProtocol(host.Protocol); err != nil {
		return HostView{}, err
	}
	normalized := normalizeHostRecord(host)
	m := model.Host{
		ID:        normalized.ID,
		Name:      normalized.Name,
		Address:   normalized.Address,
		Port:      normalized.Port,
		Protocol:  normalized.Protocol,
		GroupName: normalized.Group,
		Remark:    normalized.Remark,
	}
	if normalized.Status == "disabled" {
		m.Status = "disabled"
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&m).Error; err != nil {
			return err
		}
		if err := ensureResourceGroup(tx, normalized.Group); err != nil {
			return err
		}
		if err := s.syncResourceTx(tx, model.ResourceTypeHost, m.ID, hostResourceName(m), ""); err != nil {
			return err
		}
		if creatorID == "" {
			return nil
		}
		var creatorCount int64
		if err := tx.Model(&model.User{}).Where("id = ?", creatorID).Count(&creatorCount).Error; err != nil {
			return fmt.Errorf("check host creator: %w", err)
		}
		if creatorCount == 0 {
			return fmt.Errorf("host creator not found: %q", creatorID)
		}
		grant := model.ResourceGrant{
			PrincipalType: "user",
			PrincipalID:   creatorID,
			ResourceType:  model.ResourceTypeHost,
			ResourceID:    m.ID,
			Effect:        model.PermissionEffectAllow,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "principal_type"},
				{Name: "principal_id"},
				{Name: "resource_type"},
				{Name: "resource_id"},
				{Name: "effect"},
			},
			DoNothing: true,
		}).Create(&grant).Error; err != nil {
			return fmt.Errorf("create host creator grant: %w", err)
		}
		return nil
	}); err != nil {
		return HostView{}, fmt.Errorf("create host: %w", err)
	}
	return s.hostView(ctx, m), nil
}

func (s *DBStore) UpdateHost(ctx context.Context, id string, host HostRecord) (HostView, error) {
	if err := validateHostProtocol(host.Protocol); err != nil {
		return HostView{}, err
	}
	normalized := normalizeHostRecord(host)
	var (
		m                           model.Host
		protocolChangeValidationErr error
	)
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&m, "id = ?", id).Error; err != nil {
			return fmt.Errorf("%w: %q", ErrHostNotFound, id)
		}
		if normalizedHostProtocol(m.Protocol) != normalized.Protocol {
			var accountCount int64
			if err := tx.Model(&model.HostAccount{}).Where("host_id = ?", m.ID).Count(&accountCount).Error; err != nil {
				return fmt.Errorf("count host accounts: %w", err)
			}
			if accountCount != 0 {
				protocolChangeValidationErr = errors.New("host protocol cannot change while accounts exist")
				return protocolChangeValidationErr
			}
		}
		m.Name = normalized.Name
		m.Address = normalized.Address
		m.Port = normalized.Port
		m.Protocol = normalized.Protocol
		m.GroupName = normalized.Group
		m.Remark = normalized.Remark
		m.Status = "active"
		if normalized.Status == "disabled" {
			m.Status = "disabled"
		}
		if err := tx.Save(&m).Error; err != nil {
			return err
		}
		if err := ensureResourceGroup(tx, normalized.Group); err != nil {
			return err
		}
		return s.syncResourceTx(tx, model.ResourceTypeHost, m.ID, hostResourceName(m), "")
	}); err != nil {
		if errors.Is(err, ErrHostNotFound) || errors.Is(err, protocolChangeValidationErr) {
			return HostView{}, err
		}
		return HostView{}, fmt.Errorf("update host: %w", err)
	}
	return s.hostView(ctx, m), nil
}

func (s *DBStore) DeleteHost(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var host model.Host
		if err := tx.First(&host, "id = ?", id).Error; err != nil {
			return fmt.Errorf("%w: %q", ErrHostNotFound, id)
		}
		var accounts []model.HostAccount
		if err := tx.Where("host_id = ?", id).Find(&accounts).Error; err != nil {
			return err
		}
		for _, account := range accounts {
			if err := s.deleteResourceTx(tx, model.ResourceTypeHostAccount, account.ID); err != nil {
				return err
			}
		}
		if err := tx.Where("host_id = ?", id).Delete(&model.HostAccount{}).Error; err != nil {
			return err
		}
		if err := s.deleteResourceTx(tx, model.ResourceTypeHost, host.ID); err != nil {
			return err
		}
		return tx.Delete(&host).Error
	})
}

func normalizeHostRecord(h HostRecord) HostRecord {
	h.ID = strings.TrimSpace(h.ID)
	h.Name = strings.TrimSpace(h.Name)
	h.Group = strings.TrimSpace(h.Group)
	h.Address = strings.TrimSpace(h.Address)
	h.Protocol = normalizedHostProtocol(h.Protocol)
	h.Remark = strings.TrimSpace(h.Remark)
	if h.Port == 0 {
		h.Port = defaultHostPort(h.Protocol)
	}
	if h.ID == "" {
		h.ID = fmt.Sprintf("%s-%d", strings.ToLower(h.Address), h.Port)
	}
	if h.Name == "" {
		h.Name = formatHostAddress(h.Address, h.Port)
	}
	return h
}

func normalizedHostProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "rdp":
		return "rdp"
	default:
		return "ssh"
	}
}

func validateHostProtocol(protocol string) error {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "", "ssh", "rdp":
		return nil
	default:
		return fmt.Errorf("host protocol %q is not supported", protocol)
	}
}

func defaultHostPort(protocol string) int {
	if normalizedHostProtocol(protocol) == "rdp" {
		return 3389
	}
	return 22
}
