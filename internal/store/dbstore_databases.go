package store

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"jianmen/internal/dbtls"
	"jianmen/internal/model"
	"jianmen/internal/util"
)

// -- db instances (DB-backed) --

func (s *DBStore) DatabaseInstances(ctx context.Context) []DatabaseInstanceView {
	query := s.db.WithContext(ctx)
	var instances []model.DatabaseInstance
	if err := query.Order("name ASC").Find(&instances).Error; err != nil {
		return nil
	}
	counts, err := s.databaseAccountCounts(query, databaseInstanceIDs(instances))
	if err != nil {
		return nil
	}
	views := make([]DatabaseInstanceView, 0, len(instances))
	for _, inst := range instances {
		views = append(views, s.databaseInstanceView(inst, counts[inst.ID]))
	}
	return views
}

func (s *DBStore) DatabaseInstance(ctx context.Context, id string) (DatabaseInstanceView, error) {
	id = strings.TrimSpace(id)
	var inst model.DatabaseInstance
	query := s.db.WithContext(ctx)
	if err := query.First(&inst, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DatabaseInstanceView{}, fmt.Errorf("%w: %q", ErrDBInstanceNotFound, id)
		}
		return DatabaseInstanceView{}, err
	}
	count, err := s.databaseAccountCount(query, inst.ID)
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

func (s *DBStore) AddDatabaseInstance(ctx context.Context, input DatabaseInstanceInput) (DatabaseInstanceView, error) {
	instance, err := normalizeDatabaseInstanceInput(input, "")
	if err != nil {
		return DatabaseInstanceView{}, err
	}
	inst := instance
	if inst.Name == "" {
		inst.Name = inst.Address
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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

func (s *DBStore) ListDatabaseAccountsByInstance(ctx context.Context, instanceID string) ([]DatabaseAccountView, error) {
	var accounts []model.DatabaseAccount
	if err := s.db.WithContext(ctx).Where("instance_id = ?", instanceID).Order("username ASC").Find(&accounts).Error; err != nil {
		return nil, err
	}
	views := make([]DatabaseAccountView, 0, len(accounts))
	for _, acct := range accounts {
		views = append(views, s.databaseAccountView(acct))
	}
	return views, nil
}

func (s *DBStore) DatabaseAccounts(ctx context.Context) ([]DatabaseAccountView, error) {
	var accounts []model.DatabaseAccount
	if err := s.db.WithContext(ctx).Order("username ASC").Find(&accounts).Error; err != nil {
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

func (s *DBStore) databaseAccountCount(db *gorm.DB, instanceID string) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("database handle required")
	}
	var count int64
	if err := db.Model(&model.DatabaseAccount{}).Scopes(ActiveScope).Where("instance_id = ?", instanceID).Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (s *DBStore) databaseAccountCounts(db *gorm.DB, ids []string) (map[string]int, error) {
	if db == nil {
		return nil, fmt.Errorf("database handle required")
	}
	counts := make(map[string]int, len(ids))
	if len(ids) == 0 {
		return counts, nil
	}
	var rows []struct {
		InstanceID string
		Count      int64
	}
	if err := db.Model(&model.DatabaseAccount{}).Scopes(ActiveScope).
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

func (s *DBStore) DatabaseAccount(ctx context.Context, id string) (DatabaseAccountView, error) {
	id = strings.TrimSpace(id)
	var acct model.DatabaseAccount
	if err := s.db.WithContext(ctx).First(&acct, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DatabaseAccountView{}, fmt.Errorf("%w: %q", ErrDBAccountNotFound, id)
		}
		return DatabaseAccountView{}, err
	}
	return s.databaseAccountView(acct), nil
}

func (s *DBStore) AddDatabaseAccount(
	ctx context.Context,
	instanceID, username, password, group, remark string,
	expiresAt *time.Time,
) (DatabaseAccountView, error) {
	instanceID = strings.TrimSpace(instanceID)
	username = strings.TrimSpace(username)
	if password == "" {
		return DatabaseAccountView{}, errors.New("password is required")
	}

	var account model.DatabaseAccount
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var instance model.DatabaseInstance
		if err := tx.First(&instance, "id = ?", instanceID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %q", ErrDBInstanceNotFound, instanceID)
			}
			return err
		}
		if username != "" {
			if err := s.ensureDatabaseAccountUsernameAvailable(tx, instanceID, username, ""); err != nil {
				return err
			}
		}
		uniqueName, err := s.generateUniqueName(tx)
		if err != nil {
			return err
		}
		// 閸掑棝鍘ょ挧鍕爱ID
		seq, err := s.nextDBResourceSeq(tx)
		if err != nil {
			return err
		}
		account = model.DatabaseAccount{
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
		if err := tx.Create(&account).Error; err != nil {
			return err
		}
		if err := ensureAccountGroup(tx, strings.TrimSpace(group)); err != nil {
			return err
		}
		return s.syncResourceTx(tx, model.ResourceTypeDatabaseAccount, account.ID, databaseAccountResourceName(account), account.InstanceID)
	}); err != nil {
		return DatabaseAccountView{}, err
	}
	return s.databaseAccountView(account), nil
}

func (s *DBStore) UpdateDatabaseAccount(
	ctx context.Context,
	id, username, password, group, remark string,
	expiresAt *time.Time,
	status string,
) (DatabaseAccountView, error) {
	id = strings.TrimSpace(id)
	username = strings.TrimSpace(username)
	var locator model.DatabaseAccount
	if err := s.db.WithContext(ctx).Select("id", "instance_id").First(&locator, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DatabaseAccountView{}, fmt.Errorf("%w: %q", ErrDBAccountNotFound, id)
		}
		return DatabaseAccountView{}, err
	}
	var acct model.DatabaseAccount
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockProvisioningInstance(tx, locator.InstanceID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&acct, "id = ? AND instance_id = ?", id, locator.InstanceID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %q", ErrDBAccountNotFound, id)
			}
			return err
		}
		changesIdentity := username != "" && username != acct.Username
		changesPassword := password != ""
		changesStatus := status != "" && status != "active"
		if changesIdentity || changesPassword || changesStatus {
			if err := protectReferencedAdministrator(tx, acct.ID); err != nil {
				return err
			}
		}
		if acct.Managed && (changesIdentity || changesPassword) {
			return errors.New("managed database account identity is immutable")
		}
		if username != "" {
			if err := s.ensureDatabaseAccountUsernameAvailable(tx, acct.InstanceID, username, acct.ID); err != nil {
				return err
			}
			acct.Username = username
		}
		if password != "" {
			acct.Password = model.NewEncryptedField(password)
		}
		acct.GroupName = strings.TrimSpace(group)
		acct.Remark = strings.TrimSpace(remark)
		acct.ExpiresAt = expiresAt
		if status != "" {
			acct.Status = status
		}
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

func (s *DBStore) ensureDatabaseAccountUsernameAvailable(db *gorm.DB, instanceID, username, exceptID string) error {
	if db == nil {
		return fmt.Errorf("database handle required")
	}
	var count int64
	q := db.Model(&model.DatabaseAccount{}).Scopes(ActiveScope).
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

func (s *DBStore) DeleteDatabaseAccount(ctx context.Context, id string) error {
	id = strings.TrimSpace(id)
	var locator model.DatabaseAccount
	if err := s.db.WithContext(ctx).Select("id", "instance_id").First(&locator, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: %q", ErrDBAccountNotFound, id)
		}
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account model.DatabaseAccount
		if _, err := lockProvisioningInstance(tx, locator.InstanceID); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, "id = ? AND instance_id = ?", id, locator.InstanceID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %q", ErrDBAccountNotFound, id)
			}
			return err
		}
		if account.Managed {
			return errors.New("managed database account requires deprovisioning")
		}
		if err := protectReferencedAdministrator(tx, account.ID); err != nil {
			return err
		}
		if err := s.deleteResourceTx(tx, model.ResourceTypeDatabaseAccount, account.ID); err != nil {
			return err
		}
		return SoftDelete(ctx, tx, "database_accounts", id)
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

func (s *DBStore) generateUniqueName(db *gorm.DB) (string, error) {
	if db == nil {
		return "", fmt.Errorf("database handle required")
	}
	for i := 0; i < 10; i++ {
		name := "db-" + model.NewID()[:12]
		var count int64
		if err := db.Model(&model.DatabaseAccount{}).Scopes(ActiveScope).Where("unique_name = ?", name).Count(&count).Error; err != nil {
			return "", fmt.Errorf("check database account unique name: %w", err)
		}
		if count == 0 {
			return name, nil
		}
	}
	return "", errors.New("failed to generate unique database account name after 10 attempts")
}
