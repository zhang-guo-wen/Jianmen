package store

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
)

// -- hosts --

func (s *DBStore) hostView(m model.Host, accountCount ...int) HostView {
	status := m.Status
	if status == "" {
		status = "active"
	}
	count := 0
	if len(accountCount) > 0 {
		count = accountCount[0]
	} else {
		var total int64
		_ = s.db.Model(&model.HostAccount{}).Where("host_id = ?", m.ID).Count(&total).Error
		count = int(total)
	}
	return HostView{
		ID: m.ID, Name: m.Name, Group: m.GroupName, Address: m.Address,
		Port: m.Port, Remark: m.Remark, Status: status,
		AccountCount: count,
		CreatedAt:    m.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    m.UpdatedAt.Format(time.RFC3339),
	}
}

func (s *DBStore) Hosts() []HostView {
	var hosts []model.Host
	if err := s.db.Order("created_at DESC").Find(&hosts).Error; err != nil {
		return nil
	}
	counts, err := s.hostAccountCounts(hostIDs(hosts))
	if err != nil {
		return nil
	}
	out := make([]HostView, len(hosts))
	for i := range hosts {
		out[i] = s.hostView(hosts[i], counts[hosts[i].ID])
	}
	return out
}

func (s *DBStore) Host(id string) (HostView, error) {
	var m model.Host
	if err := s.db.First(&m, "id = ?", id).Error; err != nil {
		return HostView{}, fmt.Errorf("%w: %q", ErrHostNotFound, id)
	}
	return s.hostView(m), nil
}

func (s *DBStore) hostAccountCounts(ids []string) (map[string]int, error) {
	counts := make(map[string]int, len(ids))
	if len(ids) == 0 {
		return counts, nil
	}
	var rows []struct {
		HostID string
		Count  int64
	}
	if err := s.db.Model(&model.HostAccount{}).
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

func (s *DBStore) AddHost(host HostRecord) (HostView, error) {
	normalized := normalizeHostRecord(host)
	m := model.Host{
		ID:        normalized.ID,
		Name:      normalized.Name,
		Address:   normalized.Address,
		Port:      normalized.Port,
		GroupName: normalized.Group,
		Remark:    normalized.Remark,
	}
	if normalized.Status == "disabled" {
		m.Status = "disabled"
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&m).Error; err != nil {
			return err
		}
		if err := ensureResourceGroup(tx, normalized.Group); err != nil {
			return err
		}
		return s.syncResourceTx(tx, model.ResourceTypeHost, m.ID, hostResourceName(m), "")
	}); err != nil {
		return HostView{}, fmt.Errorf("create host: %w", err)
	}
	return s.hostView(m), nil
}

func (s *DBStore) UpdateHost(id string, host HostRecord) (HostView, error) {
	var m model.Host
	if err := s.db.First(&m, "id = ?", id).Error; err != nil {
		return HostView{}, fmt.Errorf("%w: %q", ErrHostNotFound, id)
	}
	normalized := normalizeHostRecord(host)
	m.Name = normalized.Name
	m.Address = normalized.Address
	m.Port = normalized.Port
	m.GroupName = normalized.Group
	m.Remark = normalized.Remark
	m.Status = "active"
	if normalized.Status == "disabled" {
		m.Status = "disabled"
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&m).Error; err != nil {
			return err
		}
		if err := ensureResourceGroup(tx, normalized.Group); err != nil {
			return err
		}
		return s.syncResourceTx(tx, model.ResourceTypeHost, m.ID, hostResourceName(m), "")
	}); err != nil {
		return HostView{}, fmt.Errorf("update host: %w", err)
	}
	return s.hostView(m), nil
}

func (s *DBStore) DeleteHost(id string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
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
	h.Remark = strings.TrimSpace(h.Remark)
	if h.Port == 0 {
		h.Port = 22
	}
	if h.ID == "" {
		h.ID = fmt.Sprintf("%s-%d", strings.ToLower(h.Address), h.Port)
	}
	if h.Name == "" {
		h.Name = formatHostAddress(h.Address, h.Port)
	}
	return h
}
