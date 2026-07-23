package store

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"jianmen/internal/model"
)

var (
	// ErrReferencedDatabaseAdministrator prevents changing a credential that a
	// retained provisioning operation may still need for recovery.
	ErrReferencedDatabaseAdministrator = errSentinel("database administrator is referenced by provisioning operation")
	// ErrReferencedDatabaseInstance prevents partial deletion of an instance
	// whose provisioning lifecycle has not been fully removed.
	ErrReferencedDatabaseInstance = errSentinel("database instance is referenced by provisioning operation")
)

func lockProvisioningInstance(tx *gorm.DB, instanceID string) (model.DatabaseInstance, error) {
	var instance model.DatabaseInstance
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&instance, "id = ?", strings.TrimSpace(instanceID)).Error; err != nil {
		return model.DatabaseInstance{}, err
	}
	return instance, nil
}

func lockProvisioningAdministrator(tx *gorm.DB, instanceID, accountID string) (model.DatabaseAccount, error) {
	var account model.DatabaseAccount
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(
		&account, "id = ? AND instance_id = ?", strings.TrimSpace(accountID), strings.TrimSpace(instanceID),
	).Error
	if err != nil {
		return model.DatabaseAccount{}, err
	}
	return account, nil
}

func hasProvisioningOperationForAdministrator(tx *gorm.DB, accountID string) (bool, error) {
	var count int64
	if err := tx.Model(&model.DatabaseProvisioningOperation{}).Scopes(ActiveScope).
		Where("admin_account_id = ?", strings.TrimSpace(accountID)).Count(&count).Error; err != nil {
		return false, fmt.Errorf("check provisioning administrator references: %w", err)
	}
	return count != 0, nil
}

func hasProvisioningOperationForInstance(tx *gorm.DB, instanceID string) (bool, error) {
	var count int64
	if err := tx.Model(&model.DatabaseProvisioningOperation{}).Scopes(ActiveScope).
		Where("instance_id = ?", strings.TrimSpace(instanceID)).Count(&count).Error; err != nil {
		return false, fmt.Errorf("check provisioning instance references: %w", err)
	}
	return count != 0, nil
}

func protectReferencedAdministrator(tx *gorm.DB, accountID string) error {
	referenced, err := hasProvisioningOperationForAdministrator(tx, accountID)
	if err != nil {
		return err
	}
	if referenced {
		return ErrReferencedDatabaseAdministrator
	}
	return nil
}

func protectReferencedInstance(tx *gorm.DB, instanceID string) error {
	referenced, err := hasProvisioningOperationForInstance(tx, instanceID)
	if err != nil {
		return err
	}
	if referenced {
		return ErrReferencedDatabaseInstance
	}
	return nil
}

func databaseProvisioningInstanceProof(instance model.DatabaseInstance) string {
	payload, _ := json.Marshal(struct {
		Protocol, Address, TLSMode, TLSServerName, TLSCAPEM string
		Port                                                int
	}{
		Protocol: instance.Protocol, Address: instance.Address, Port: instance.Port,
		TLSMode: instance.TLSMode, TLSServerName: instance.TLSServerName, TLSCAPEM: instance.TLSCAPEM,
	})
	digest := sha256.Sum256(payload)
	return fmt.Sprintf("%x", digest)
}

func hasCriticalDatabaseInstanceChange(current, updated model.DatabaseInstance) bool {
	return current.Protocol != updated.Protocol || current.Address != updated.Address ||
		current.Port != updated.Port || current.TLSMode != updated.TLSMode ||
		current.TLSServerName != updated.TLSServerName || current.TLSCAPEM != updated.TLSCAPEM ||
		current.Status != updated.Status
}

func isRecordNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
