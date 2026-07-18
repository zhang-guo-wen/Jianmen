package store

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/dbtls"
	"jianmen/internal/model"
	"jianmen/internal/util"
)

// -- db instances (DB-backed) --

func (s *DBStore) DatabaseInstances() []DatabaseInstanceView {
	var instances []model.DatabaseInstance
	if err := s.db.Order("name ASC").Find(&instances).Error; err != nil {
		return nil
	}
	counts, err := s.databaseAccountCounts(databaseInstanceIDs(instances))
	if err != nil {
		return nil
	}
	views := make([]DatabaseInstanceView, 0, len(instances))
	for _, inst := range instances {
		views = append(views, s.databaseInstanceView(inst, counts[inst.ID]))
	}
	return views
}

func (s *DBStore) DatabaseInstance(id string) (DatabaseInstanceView, error) {
	id = strings.TrimSpace(id)
	var inst model.DatabaseInstance
	if err := s.db.First(&inst, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DatabaseInstanceView{}, fmt.Errorf("%w: %q", ErrDBInstanceNotFound, id)
		}
		return DatabaseInstanceView{}, err
	}
	count, err := s.databaseAccountCount(inst.ID)
	if err != nil {
		return DatabaseInstanceView{}, err
	}
	return s.databaseInstanceView(inst, count), nil
}

func normalizeDBProtocol(protocol string) (string, error) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol == "" || protocol == "pg" || protocol == "postgresql" {
		protocol = "postgres"
	}
	if protocol != "mysql" && protocol != "postgres" && protocol != "redis" {
		return "", fmt.Errorf("unsupported database protocol %q", protocol)
	}
	return protocol, nil
}

func (s *DBStore) AddDatabaseInstance(input DatabaseInstanceInput) (DatabaseInstanceView, error) {
	instance, err := normalizeDatabaseInstanceInput(input, "")
	if err != nil {
		return DatabaseInstanceView{}, err
	}
	inst := instance
	if inst.Name == "" {
		inst.Name = inst.Address
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&inst).Error; err != nil {
			return err
		}
		if err := ensureResourceGroup(tx, inst.GroupName); err != nil {
			return err
		}
		return s.syncResourceTx(tx, model.ResourceTypeDatabaseInstance, inst.ID, databaseInstanceResourceName(inst), "")
	}); err != nil {
		return DatabaseInstanceView{}, err
	}
	return s.databaseInstanceView(inst, 0), nil
}

func (s *DBStore) UpdateDatabaseInstance(id string, input DatabaseInstanceInput) (DatabaseInstanceView, error) {
	id = strings.TrimSpace(id)
	var inst model.DatabaseInstance
	if err := s.db.First(&inst, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DatabaseInstanceView{}, fmt.Errorf("%w: %q", ErrDBInstanceNotFound, id)
		}
		return DatabaseInstanceView{}, err
	}
	updated, err := normalizeDatabaseInstanceInput(input, inst.TLSCAPEM)
	if err != nil {
		return DatabaseInstanceView{}, err
	}
	inst.Name = updated.Name
	inst.Protocol = updated.Protocol
	inst.Address = updated.Address
	inst.Port = updated.Port
	inst.TLSMode = updated.TLSMode
	inst.TLSServerName = updated.TLSServerName
	inst.TLSCAPEM = updated.TLSCAPEM
	inst.GroupName = updated.GroupName
	inst.Remark = updated.Remark
	inst.Status = updated.Status
	if inst.Name == "" {
		inst.Name = inst.Address
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&inst).Error; err != nil {
			return err
		}
		if err := ensureResourceGroup(tx, inst.GroupName); err != nil {
			return err
		}
		return s.syncResourceTx(tx, model.ResourceTypeDatabaseInstance, inst.ID, databaseInstanceResourceName(inst), "")
	}); err != nil {
		return DatabaseInstanceView{}, err
	}
	count, err := s.databaseAccountCount(inst.ID)
	if err != nil {
		return DatabaseInstanceView{}, err
	}
	return s.databaseInstanceView(inst, count), nil
}

func (s *DBStore) DeleteDatabaseInstance(id string) error {
	id = strings.TrimSpace(id)
	return s.db.Transaction(func(tx *gorm.DB) error {
		var inst model.DatabaseInstance
		if err := tx.First(&inst, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %q", ErrDBInstanceNotFound, id)
			}
			return err
		}
		var accounts []model.DatabaseAccount
		if err := tx.Where("instance_id = ?", id).Find(&accounts).Error; err != nil {
			return err
		}
		for _, account := range accounts {
			if err := s.deleteResourceTx(tx, model.ResourceTypeDatabaseAccount, account.ID); err != nil {
				return err
			}
		}
		if err := tx.Where("instance_id = ?", id).Delete(&model.DatabaseAccount{}).Error; err != nil {
			return err
		}
		if err := s.deleteResourceTx(tx, model.ResourceTypeDatabaseInstance, inst.ID); err != nil {
			return err
		}
		return tx.Delete(&inst).Error
	})
}

func (s *DBStore) InstanceAccounts(instanceID string) ([]DatabaseAccountView, error) {
	var accounts []model.DatabaseAccount
	if err := s.db.Where("instance_id = ?", instanceID).Order("username ASC").Find(&accounts).Error; err != nil {
		return nil, err
	}
	views := make([]DatabaseAccountView, 0, len(accounts))
	for _, acct := range accounts {
		views = append(views, s.databaseAccountView(acct))
	}
	return views, nil
}

func (s *DBStore) DatabaseAccounts() ([]DatabaseAccountView, error) {
	var accounts []model.DatabaseAccount
	if err := s.db.Order("username ASC").Find(&accounts).Error; err != nil {
		return nil, err
	}
	views := make([]DatabaseAccountView, 0, len(accounts))
	for _, account := range accounts {
		views = append(views, s.databaseAccountView(account))
	}
	return views, nil
}

func (s *DBStore) databaseInstanceView(inst model.DatabaseInstance, accountCount int) DatabaseInstanceView {
	return DatabaseInstanceView{
		ID:            inst.ID,
		Name:          inst.Name,
		Protocol:      inst.Protocol,
		Address:       inst.Address,
		Port:          inst.Port,
		TLSMode:       inst.TLSMode,
		TLSServerName: inst.TLSServerName,
		HasTLSCA:      strings.TrimSpace(inst.TLSCAPEM) != "",
		Group:         inst.GroupName,
		Remark:        inst.Remark,
		Status:        inst.Status,
		AccountCount:  accountCount,
		CreatedAt:     inst.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     inst.UpdatedAt.Format(time.RFC3339),
	}
}

func normalizeDatabaseInstanceInput(input DatabaseInstanceInput, existingCAPEM string) (model.DatabaseInstance, error) {
	protocol, err := normalizeDBProtocol(input.Protocol)
	if err != nil {
		return model.DatabaseInstance{}, err
	}
	address := strings.TrimSpace(input.Address)
	if address == "" {
		return model.DatabaseInstance{}, errors.New("address is required")
	}
	tlsMode, err := dbtls.NormalizeMode(input.TLSMode)
	if err != nil {
		return model.DatabaseInstance{}, err
	}
	tlsCAPEM := existingCAPEM
	if input.TLSCAPEM != nil {
		tlsCAPEM = strings.TrimSpace(*input.TLSCAPEM)
	}
	if input.ClearTLSCA || tlsMode == dbtls.ModeDisable {
		tlsCAPEM = ""
	}
	if tlsMode != dbtls.ModeDisable {
		if _, err := dbtls.ClientConfig(dbtls.Config{
			Mode:       tlsMode,
			ServerName: input.TLSServerName,
			CAPEM:      tlsCAPEM,
		}, net.JoinHostPort(address, strconv.Itoa(defaultDatabasePort(protocol, input.Port)))); err != nil {
			return model.DatabaseInstance{}, fmt.Errorf("validate upstream TLS: %w", err)
		}
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "active"
	}
	return model.DatabaseInstance{
		Name:          strings.TrimSpace(input.Name),
		Protocol:      protocol,
		Address:       address,
		Port:          input.Port,
		TLSMode:       tlsMode,
		TLSServerName: strings.TrimSpace(input.TLSServerName),
		TLSCAPEM:      tlsCAPEM,
		GroupName:     strings.TrimSpace(input.Group),
		Remark:        strings.TrimSpace(input.Remark),
		Status:        status,
	}, nil
}

func defaultDatabasePort(protocol string, port int) int {
	if port > 0 {
		return port
	}
	if protocol == "postgres" {
		return 5432
	}
	if protocol == "redis" {
		return 6379
	}
	return 3306
}

func (s *DBStore) databaseAccountCount(instanceID string) (int, error) {
	var count int64
	if err := s.db.Model(&model.DatabaseAccount{}).Where("instance_id = ?", instanceID).Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (s *DBStore) databaseAccountCounts(ids []string) (map[string]int, error) {
	counts := make(map[string]int, len(ids))
	if len(ids) == 0 {
		return counts, nil
	}
	var rows []struct {
		InstanceID string
		Count      int64
	}
	if err := s.db.Model(&model.DatabaseAccount{}).
		Select("instance_id, COUNT(*) AS count").
		Where("instance_id IN ?", ids).
		Group("instance_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.InstanceID] = int(row.Count)
	}
	return counts, nil
}

func databaseInstanceIDs(instances []model.DatabaseInstance) []string {
	ids := make([]string, 0, len(instances))
	for _, inst := range instances {
		if inst.ID != "" {
			ids = append(ids, inst.ID)
		}
	}
	return ids
}

func (s *DBStore) DatabaseAccount(id string) (DatabaseAccountView, error) {
	id = strings.TrimSpace(id)
	var acct model.DatabaseAccount
	if err := s.db.First(&acct, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DatabaseAccountView{}, fmt.Errorf("%w: %q", ErrDBAccountNotFound, id)
		}
		return DatabaseAccountView{}, err
	}
	return s.databaseAccountView(acct), nil
}

func (s *DBStore) AddDatabaseAccount(instanceID, username, password, group, remark string, expiresAt *time.Time) (DatabaseAccountView, error) {
	instanceID = strings.TrimSpace(instanceID)
	username = strings.TrimSpace(username)
	if password == "" {
		return DatabaseAccountView{}, errors.New("password is required")
	}
	// Verify instance exists
	var inst model.DatabaseInstance
	if err := s.db.First(&inst, "id = ?", instanceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DatabaseAccountView{}, fmt.Errorf("%w: %q", ErrDBInstanceNotFound, instanceID)
		}
		return DatabaseAccountView{}, err
	}
	if username != "" {
		if err := s.ensureDatabaseAccountUsernameAvailable(instanceID, username, ""); err != nil {
			return DatabaseAccountView{}, err
		}
	}
	uniqueName, err := s.generateUniqueName()
	if err != nil {
		return DatabaseAccountView{}, err
	}
	// 閸掑棝鍘ょ挧鍕爱ID
	seq, err := s.nextDBResourceSeq()
	if err != nil {
		return DatabaseAccountView{}, err
	}
	acct := model.DatabaseAccount{
		InstanceID:  instanceID,
		UniqueName:  uniqueName,
		Username:    username,
		Password:    model.NewEncryptedField(password),
		GroupName:   strings.TrimSpace(group),
		Remark:      strings.TrimSpace(remark),
		ExpiresAt:   expiresAt,
		ResourceSeq: seq,
		ResourceID:  util.ResourceIDFromSeq(util.PrefixDatabase, seq),
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&acct).Error; err != nil {
			return err
		}
		if err := ensureAccountGroup(tx, strings.TrimSpace(group)); err != nil {
			return err
		}
		return s.syncResourceTx(tx, model.ResourceTypeDatabaseAccount, acct.ID, databaseAccountResourceName(acct), acct.InstanceID)
	}); err != nil {
		return DatabaseAccountView{}, err
	}
	return s.databaseAccountView(acct), nil
}

func (s *DBStore) UpdateDatabaseAccount(id, username, password, group, remark string, expiresAt *time.Time, status string) (DatabaseAccountView, error) {
	id = strings.TrimSpace(id)
	username = strings.TrimSpace(username)
	var acct model.DatabaseAccount
	if err := s.db.First(&acct, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DatabaseAccountView{}, fmt.Errorf("%w: %q", ErrDBAccountNotFound, id)
		}
		return DatabaseAccountView{}, err
	}
	if acct.Managed && ((username != "" && username != acct.Username) || password != "") {
		return DatabaseAccountView{}, errors.New("managed database account identity is immutable")
	}
	if username != "" {
		if err := s.ensureDatabaseAccountUsernameAvailable(acct.InstanceID, username, acct.ID); err != nil {
			return DatabaseAccountView{}, err
		}
		acct.Username = username
	}
	if password != "" {
		acct.Password = model.NewEncryptedField(password)
	}
	acct.GroupName = strings.TrimSpace(group)
	acct.Remark = strings.TrimSpace(remark)
	acct.ExpiresAt = expiresAt
	acct.Status = status
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&acct).Error; err != nil {
			return err
		}
		if err := ensureAccountGroup(tx, strings.TrimSpace(group)); err != nil {
			return err
		}
		return s.syncResourceTx(tx, model.ResourceTypeDatabaseAccount, acct.ID, databaseAccountResourceName(acct), acct.InstanceID)
	}); err != nil {
		return DatabaseAccountView{}, err
	}
	return s.databaseAccountView(acct), nil
}

func (s *DBStore) ensureDatabaseAccountUsernameAvailable(instanceID, username, exceptID string) error {
	var count int64
	q := s.db.Model(&model.DatabaseAccount{}).
		Where("instance_id = ? AND username = ?", instanceID, username)
	if exceptID != "" {
		q = q.Where("id <> ?", exceptID)
	}
	if err := q.Count(&count).Error; err != nil {
		return fmt.Errorf("check database account duplicate: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("database account %q already exists on instance %q", username, instanceID)
	}
	return nil
}

func (s *DBStore) DeleteDatabaseAccount(id string) error {
	id = strings.TrimSpace(id)
	return s.db.Transaction(func(tx *gorm.DB) error {
		var account model.DatabaseAccount
		if err := tx.First(&account, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %q", ErrDBAccountNotFound, id)
			}
			return err
		}
		if account.Managed {
			return errors.New("managed database account requires deprovisioning")
		}
		if err := s.deleteResourceTx(tx, model.ResourceTypeDatabaseAccount, account.ID); err != nil {
			return err
		}
		return tx.Delete(&account).Error
	})
}

func (s *DBStore) DatabaseAccountByUniqueName(uniqueName string) (*model.DatabaseAccount, error) {
	uniqueName = strings.TrimSpace(uniqueName)
	var acct model.DatabaseAccount
	if err := s.db.Preload("Instance").First(&acct, "unique_name = ?", uniqueName).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: %q", ErrDBAccountNotFound, uniqueName)
		}
		return nil, err
	}
	return &acct, nil
}

func (s *DBStore) databaseAccountView(acct model.DatabaseAccount) DatabaseAccountView {
	return DatabaseAccountView{
		ID:          acct.ID,
		InstanceID:  acct.InstanceID,
		UniqueName:  acct.UniqueName,
		Username:    acct.Username,
		Group:       acct.GroupName,
		Remark:      acct.Remark,
		ExpiresAt:   acct.ExpiresAt,
		Status:      acct.Status,
		ResourceID:  acct.ResourceID,
		ResourceSeq: acct.ResourceSeq,
		CreatedAt:   acct.CreatedAt.Format(time.RFC3339),
		UpdatedAt:   acct.UpdatedAt.Format(time.RFC3339),
	}
}

func (s *DBStore) generateUniqueName() (string, error) {
	for i := 0; i < 10; i++ {
		name := "db-" + model.NewID()[:12]
		var count int64
		s.db.Model(&model.DatabaseAccount{}).Where("unique_name = ?", name).Count(&count)
		if count == 0 {
			return name, nil
		}
	}
	return "", errors.New("failed to generate unique database account name after 10 attempts")
}
