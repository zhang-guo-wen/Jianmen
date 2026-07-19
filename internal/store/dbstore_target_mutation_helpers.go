package store

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"jianmen/internal/model"
)

func (s *DBStore) lockTargetHost(tx *gorm.DB, hostID, address, protocol string, port int) (model.Host, bool, error) {
	if tx == nil {
		return model.Host{}, false, errors.New("load target host: nil database")
	}
	var host model.Host
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&host, "id = ?", hostID).Error
	if err == nil {
		return host, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Host{}, false, err
	}
	name := address
	if port != 0 && port != 22 {
		name = fmt.Sprintf("%s:%d", address, port)
	}
	if name == "" {
		name = hostID
	}
	return model.Host{
		ID:       hostID,
		Name:     name,
		Address:  address,
		Port:     port,
		Protocol: normalizedHostProtocol(protocol),
	}, true, nil
}

func (s *DBStore) ensureHost(tx *gorm.DB, host *model.Host, create bool) error {
	if tx == nil {
		return errors.New("ensure host: nil database")
	}
	if host == nil {
		return errors.New("ensure host: nil host")
	}
	if create {
		if err := tx.Create(host).Error; err != nil {
			return err
		}
	}
	return s.syncResourceTx(tx, model.ResourceTypeHost, host.ID, hostResourceName(*host), "")
}

func (s *DBStore) ensureHostAccountUsernameAvailable(tx *gorm.DB, hostID, username, exceptID string) error {
	if tx == nil {
		return errors.New("check host account duplicate: nil database")
	}
	var count int64
	q := tx.Model(&model.HostAccount{}).
		Where("host_id = ? AND username = ?", hostID, username)
	if exceptID != "" {
		q = q.Where("id <> ?", exceptID)
	}
	if err := q.Count(&count).Error; err != nil {
		return fmt.Errorf("check host account duplicate: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("host account %q already exists on host %q", username, hostID)
	}
	return nil
}
