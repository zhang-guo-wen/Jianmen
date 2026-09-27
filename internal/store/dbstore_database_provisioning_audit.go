package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

func (s *DBStore) DatabaseProvisioningAdmin(
	ctx context.Context,
	instanceID, accountID string,
) (model.DatabaseInstance, model.DatabaseAccount, error) {
	if ctx == nil {
		return model.DatabaseInstance{}, model.DatabaseAccount{},
			errors.New("load database provisioning administrator: nil context")
	}
	instanceID = strings.TrimSpace(instanceID)
	accountID = strings.TrimSpace(accountID)
	if instanceID == "" || accountID == "" {
		return model.DatabaseInstance{}, model.DatabaseAccount{},
			errors.New("database instance and administrator account are required")
	}
	var account model.DatabaseAccount
	err := s.db.WithContext(ctx).Scopes(activeDatabaseAccountScope).
		Preload("Instance", ActiveScope).
		First(&account, "database_accounts.id = ? AND database_accounts.instance_id = ?", accountID, instanceID).
		Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.DatabaseInstance{}, model.DatabaseAccount{},
			errors.New("database provisioning administrator is unavailable")
	}
	if err != nil {
		return model.DatabaseInstance{}, model.DatabaseAccount{},
			errors.New("load database provisioning administrator")
	}
	return account.Instance, account, nil
}

func (s *DBStore) BeginDatabaseProvisioningAudit(
	ctx context.Context,
	audit service.DatabaseProvisioningAudit,
) (string, error) {
	if ctx == nil {
		return "", errors.New("create database provisioning audit: nil context")
	}
	detail, err := json.Marshal(map[string]string{
		"instance_id": strings.TrimSpace(audit.InstanceID),
		"operation":   strings.TrimSpace(audit.Operation),
		"result":      "started",
	})
	if err != nil {
		return "", errors.New("encode database provisioning audit")
	}
	event := model.AuditEvent{
		ActorID:       strings.TrimSpace(audit.Actor.UserID),
		ActorUsername: strings.TrimSpace(audit.Actor.Username),
		Action:        "use_provisioning_credential",
		ResourceType:  model.ResourceTypeDatabaseAccount,
		ResourceID:    strings.TrimSpace(audit.AccountID),
		Detail:        string(detail),
		ClientIP:      strings.TrimSpace(audit.Actor.ClientIP),
	}
	if err := s.db.WithContext(ctx).Create(&event).Error; err != nil {
		return "", errors.New("create database provisioning audit")
	}
	return event.ID, nil
}

func (s *DBStore) CompleteDatabaseProvisioningAudit(
	ctx context.Context,
	auditID, result string,
) error {
	if ctx == nil {
		return errors.New("complete database provisioning audit: nil context")
	}
	result = strings.TrimSpace(result)
	if result != "success" && result != "failure" {
		return errors.New("complete database provisioning audit: invalid result")
	}
	var event model.AuditEvent
	if err := s.db.WithContext(ctx).First(
		&event,
		"id = ? AND action = ?",
		strings.TrimSpace(auditID),
		"use_provisioning_credential",
	).Error; err != nil {
		return errors.New("complete database provisioning audit")
	}
	var detail map[string]string
	if err := json.Unmarshal([]byte(event.Detail), &detail); err != nil {
		return errors.New("complete database provisioning audit")
	}
	detail["result"] = result
	encoded, err := json.Marshal(detail)
	if err != nil {
		return errors.New("complete database provisioning audit")
	}
	if err := s.db.WithContext(ctx).Model(&model.AuditEvent{}).
		Where("id = ?", event.ID).
		Update("detail", string(encoded)).Error; err != nil {
		return errors.New("complete database provisioning audit")
	}
	return nil
}
