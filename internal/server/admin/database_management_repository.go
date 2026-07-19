package admin

import (
	"context"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/service"
	"jianmen/internal/store"
)

// databaseManagementRepositoryAdapter prevents the service from depending on
// store DTOs while preserving the existing admin composition root.
type databaseManagementRepositoryAdapter struct{ repository adminDatabaseRepository }

func (a databaseManagementRepositoryAdapter) DatabaseInstances(ctx context.Context) ([]service.DatabaseInstance, error) {
	values, err := a.repository.ListDatabaseInstances(ctx)
	result := make([]service.DatabaseInstance, len(values))
	for i := range values {
		result[i] = databaseInstanceFromStore(values[i])
	}
	return result, err
}
func (a databaseManagementRepositoryAdapter) DatabaseInstance(ctx context.Context, id string) (service.DatabaseInstance, error) {
	value, err := a.repository.DatabaseInstance(ctx, id)
	return databaseInstanceFromStore(value), err
}
func (a databaseManagementRepositoryAdapter) CreateDatabaseInstanceWithCreatorGrant(ctx context.Context, input service.DatabaseInstanceInput, creatorID string) (service.DatabaseInstance, error) {
	value, err := a.repository.CreateDatabaseInstanceWithCreatorGrant(ctx, databaseInstanceInputToStore(input), creatorID)
	return databaseInstanceFromStore(value), err
}
func (a databaseManagementRepositoryAdapter) UpdateDatabaseInstance(ctx context.Context, id string, input service.DatabaseInstanceInput) (service.DatabaseInstance, error) {
	value, err := a.repository.UpdateDatabaseInstance(ctx, id, databaseInstanceInputToStore(input))
	return databaseInstanceFromStore(value), err
}
func (a databaseManagementRepositoryAdapter) DeleteDatabaseInstance(ctx context.Context, id string) error {
	return a.repository.DeleteDatabaseInstance(ctx, id)
}
func (a databaseManagementRepositoryAdapter) DatabaseAccounts(ctx context.Context) ([]service.DatabaseAccount, error) {
	values, err := a.repository.DatabaseAccounts(ctx)
	result := make([]service.DatabaseAccount, len(values))
	for i := range values {
		result[i] = databaseAccountFromStore(values[i])
	}
	return result, err
}
func (a databaseManagementRepositoryAdapter) DatabaseAccount(ctx context.Context, id string) (service.DatabaseAccount, error) {
	value, err := a.repository.DatabaseAccount(ctx, id)
	return databaseAccountFromStore(value), err
}
func (a databaseManagementRepositoryAdapter) ListDatabaseAccountsByInstance(ctx context.Context, id string) ([]service.DatabaseAccount, error) {
	values, err := a.repository.ListDatabaseAccountsByInstance(ctx, id)
	result := make([]service.DatabaseAccount, len(values))
	for i := range values {
		result[i] = databaseAccountFromStore(values[i])
	}
	return result, err
}
func (a databaseManagementRepositoryAdapter) AddDatabaseAccount(ctx context.Context, instanceID, username, password, group, remark string, expiresAt *time.Time) (service.DatabaseAccount, error) {
	value, err := a.repository.AddDatabaseAccount(ctx, instanceID, username, password, group, remark, expiresAt)
	return databaseAccountFromStore(value), err
}
func (a databaseManagementRepositoryAdapter) UpdateDatabaseAccount(ctx context.Context, id, username, password, group, remark string, expiresAt *time.Time, status string) (service.DatabaseAccount, error) {
	value, err := a.repository.UpdateDatabaseAccount(ctx, id, username, password, group, remark, expiresAt, status)
	return databaseAccountFromStore(value), err
}
func (a databaseManagementRepositoryAdapter) DeleteDatabaseAccount(ctx context.Context, id string) error {
	return a.repository.DeleteDatabaseAccount(ctx, id)
}
func (a databaseManagementRepositoryAdapter) DatabaseAccountProbeMetadata(ctx context.Context, id string) (service.DatabaseAccountProbeMetadata, error) {
	value, err := a.repository.DatabaseAccountProbeMetadata(ctx, id)
	return service.DatabaseAccountProbeMetadata{ID: value.ID, InstanceID: value.InstanceID, Username: value.Username, Status: value.Status, ExpiresAt: value.ExpiresAt, Instance: databaseInstanceRecordFromModel(value.Instance)}, err
}
func (a databaseManagementRepositoryAdapter) DatabaseAccountProbePassword(ctx context.Context, id string) (string, error) {
	return a.repository.DatabaseAccountProbePassword(ctx, id)
}
func (a databaseManagementRepositoryAdapter) DatabaseInstanceForProbe(ctx context.Context, id string) (service.DatabaseInstanceRecord, error) {
	value, err := a.repository.DatabaseInstanceForProbe(ctx, id)
	return databaseInstanceRecordFromModel(value), err
}

func databaseInstanceFromStore(v store.DatabaseInstanceView) service.DatabaseInstance {
	return service.DatabaseInstance{ID: v.ID, Name: v.Name, Protocol: v.Protocol, Address: v.Address, Port: v.Port, TLSMode: v.TLSMode, TLSServerName: v.TLSServerName, HasTLSCA: v.HasTLSCA, Group: v.Group, Remark: v.Remark, Status: v.Status, AccountCount: v.AccountCount, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, CanManage: v.CanManage}
}
func databaseAccountFromStore(v store.DatabaseAccountView) service.DatabaseAccount {
	return service.DatabaseAccount{ID: v.ID, InstanceID: v.InstanceID, UniqueName: v.UniqueName, Username: v.Username, Group: v.Group, Remark: v.Remark, ExpiresAt: v.ExpiresAt, Status: v.Status, ResourceID: v.ResourceID, ResourceSeq: v.ResourceSeq, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, CanManage: v.CanManage}
}
func databaseInstanceInputToStore(v service.DatabaseInstanceInput) store.DatabaseInstanceInput {
	return store.DatabaseInstanceInput{Name: v.Name, Protocol: v.Protocol, Address: v.Address, Port: v.Port, TLSMode: v.TLSMode, TLSServerName: v.TLSServerName, TLSCAPEM: v.TLSCAPEM, ClearTLSCA: v.ClearTLSCA, Group: v.Group, Remark: v.Remark, Status: v.Status}
}
func databaseInstanceRecordFromModel(v model.DatabaseInstance) service.DatabaseInstanceRecord {
	return service.DatabaseInstanceRecord{ID: v.ID, Name: v.Name, Protocol: v.Protocol, Address: v.Address, Port: v.Port, TLSMode: v.TLSMode, TLSServerName: v.TLSServerName, TLSCAPEM: v.TLSCAPEM, GroupName: v.GroupName, Remark: v.Remark, Status: v.Status}
}
func databaseInstanceToStore(v service.DatabaseInstance) store.DatabaseInstanceView {
	return store.DatabaseInstanceView{ID: v.ID, Name: v.Name, Protocol: v.Protocol, Address: v.Address, Port: v.Port, TLSMode: v.TLSMode, TLSServerName: v.TLSServerName, HasTLSCA: v.HasTLSCA, Group: v.Group, Remark: v.Remark, Status: v.Status, AccountCount: v.AccountCount, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, CanManage: v.CanManage}
}
func databaseAccountToStore(v service.DatabaseAccount) store.DatabaseAccountView {
	return store.DatabaseAccountView{ID: v.ID, InstanceID: v.InstanceID, UniqueName: v.UniqueName, Username: v.Username, Group: v.Group, Remark: v.Remark, ExpiresAt: v.ExpiresAt, Status: v.Status, ResourceID: v.ResourceID, ResourceSeq: v.ResourceSeq, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, CanManage: v.CanManage}
}
func databaseInstanceRecordToModel(v service.DatabaseInstanceRecord) model.DatabaseInstance {
	return model.DatabaseInstance{ID: v.ID, Name: v.Name, Protocol: v.Protocol, Address: v.Address, Port: v.Port, TLSMode: v.TLSMode, TLSServerName: v.TLSServerName, TLSCAPEM: v.TLSCAPEM, GroupName: v.GroupName, Remark: v.Remark, Status: v.Status}
}
