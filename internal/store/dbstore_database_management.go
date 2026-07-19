package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"jianmen/internal/model"
)

// ListDatabaseInstances is the error-preserving management read path. The
// older compatibility list intentionally collapses storage errors, which is
// unsafe for an authorization-filtered administration response.
func (s *DBStore) ListDatabaseInstances(ctx context.Context) ([]DatabaseInstanceView, error) {
	query := s.db.WithContext(ctx)
	var instances []model.DatabaseInstance
	if err := query.Order("name ASC").Find(&instances).Error; err != nil {
		return nil, err
	}
	counts, err := s.databaseAccountCounts(query, databaseInstanceIDs(instances))
	if err != nil {
		return nil, err
	}
	views := make([]DatabaseInstanceView, 0, len(instances))
	for _, instance := range instances {
		views = append(views, s.databaseInstanceView(instance, counts[instance.ID]))
	}
	return views, nil
}

// CreateDatabaseInstanceWithCreatorGrant keeps the resource and its creator
// grant in one metadata transaction. A failed grant can never leave a usable
// orphan instance behind.
func (s *DBStore) CreateDatabaseInstanceWithCreatorGrant(ctx context.Context, input DatabaseInstanceInput, creatorID string) (DatabaseInstanceView, error) {
	instance, err := normalizeDatabaseInstanceInput(input, "")
	if err != nil {
		return DatabaseInstanceView{}, err
	}
	if instance.Name == "" {
		instance.Name = instance.Address
	}
	creatorID = strings.TrimSpace(creatorID)
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&instance).Error; err != nil {
			return err
		}
		if err := ensureResourceGroup(tx, instance.GroupName); err != nil {
			return err
		}
		if err := s.syncResourceTx(tx, model.ResourceTypeDatabaseInstance, instance.ID, databaseInstanceResourceName(instance), ""); err != nil {
			return err
		}
		if creatorID == "" {
			return nil
		}
		var count int64
		if err := tx.Model(&model.User{}).Where("id = ?", creatorID).Count(&count).Error; err != nil {
			return fmt.Errorf("check database instance creator: %w", err)
		}
		if count == 0 {
			return errors.New("database instance creator not found")
		}
		grant := model.ResourceGrant{PrincipalType: "user", PrincipalID: creatorID, ResourceType: model.ResourceTypeDatabaseInstance, ResourceID: instance.ID, Effect: model.PermissionEffectAllow}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "principal_type"}, {Name: "principal_id"}, {Name: "resource_type"}, {Name: "resource_id"}, {Name: "effect"}}, DoNothing: true}).Create(&grant).Error
	}); err != nil {
		return DatabaseInstanceView{}, err
	}
	return s.databaseInstanceView(instance, 0), nil
}

func (s *DBStore) DatabaseAccountProbeMetadata(ctx context.Context, id string) (DatabaseAccountProbeMetadata, error) {
	var account model.DatabaseAccount
	if err := s.db.WithContext(ctx).Preload("Instance").First(&account, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DatabaseAccountProbeMetadata{}, fmt.Errorf("%w: %q", ErrDBAccountNotFound, id)
		}
		return DatabaseAccountProbeMetadata{}, err
	}
	return DatabaseAccountProbeMetadata{ID: account.ID, InstanceID: account.InstanceID, Username: account.Username, Status: account.Status, ExpiresAt: account.ExpiresAt, Instance: account.Instance}, nil
}

func (s *DBStore) DatabaseAccountProbePassword(ctx context.Context, id string) (string, error) {
	var account model.DatabaseAccount
	if err := s.db.WithContext(ctx).Select("id", "password").First(&account, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", fmt.Errorf("%w: %q", ErrDBAccountNotFound, id)
		}
		return "", err
	}
	return account.Password.GetPlaintext(), nil
}

func (s *DBStore) DatabaseInstanceForProbe(ctx context.Context, id string) (model.DatabaseInstance, error) {
	var instance model.DatabaseInstance
	if err := s.db.WithContext(ctx).First(&instance, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.DatabaseInstance{}, fmt.Errorf("%w: %q", ErrDBInstanceNotFound, id)
		}
		return model.DatabaseInstance{}, err
	}
	return instance, nil
}
