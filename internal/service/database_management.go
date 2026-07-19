package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

var (
	ErrDatabaseManagementForbidden = errors.New("database management access forbidden")
	ErrDatabaseManagementDisabled  = errors.New("database resource is disabled")
	ErrDatabaseManagementExpired   = errors.New("database account is expired")
	ErrDatabaseManagementInvalid   = errors.New("invalid database management request")
)

// DatabaseManagementRepository is intentionally owned by the service. It is
// limited to database instance/account persistence and never exposes GORM.
type DatabaseManagementRepository interface {
	DatabaseInstances(context.Context) ([]DatabaseInstance, error)
	DatabaseInstance(context.Context, string) (DatabaseInstance, error)
	CreateDatabaseInstanceWithCreatorGrant(context.Context, DatabaseInstanceInput, string) (DatabaseInstance, error)
	UpdateDatabaseInstance(context.Context, string, DatabaseInstanceInput) (DatabaseInstance, error)
	DeleteDatabaseInstance(context.Context, string) error
	DatabaseAccounts(context.Context) ([]DatabaseAccount, error)
	DatabaseAccount(context.Context, string) (DatabaseAccount, error)
	ListDatabaseAccountsByInstance(context.Context, string) ([]DatabaseAccount, error)
	AddDatabaseAccount(context.Context, string, string, string, string, string, *time.Time) (DatabaseAccount, error)
	UpdateDatabaseAccount(context.Context, string, string, string, string, string, *time.Time, string) (DatabaseAccount, error)
	DeleteDatabaseAccount(context.Context, string) error
	DatabaseAccountProbeMetadata(context.Context, string) (DatabaseAccountProbeMetadata, error)
	DatabaseAccountProbePassword(context.Context, string) (string, error)
	DatabaseInstanceForProbe(context.Context, string) (DatabaseInstanceRecord, error)
}

type DatabaseInstance struct {
	ID, Name, Protocol, Address, TLSMode, TLSServerName, Group, Remark, Status string
	Port, AccountCount                                                         int
	HasTLSCA, CanManage                                                        bool
	CreatedAt, UpdatedAt                                                       string
}
type DatabaseInstanceInput struct {
	Name, Protocol, Address, TLSMode, TLSServerName, Group, Remark, Status string
	Port                                                                   int
	TLSCAPEM                                                               *string
	ClearTLSCA                                                             bool
}
type DatabaseAccount struct {
	ID, InstanceID, UniqueName, Username, Group, Remark, Status, ResourceID string
	ExpiresAt                                                               *time.Time
	ResourceSeq                                                             int
	CreatedAt, UpdatedAt                                                    string
	CanManage                                                               bool
}
type DatabaseInstanceRecord struct {
	ID, Name, Protocol, Address, TLSMode, TLSServerName, TLSCAPEM, GroupName, Remark, Status string
	Port                                                                                     int
}
type DatabaseAccountProbeMetadata struct {
	ID, InstanceID, Username, Status string
	ExpiresAt                        *time.Time
	Instance                         DatabaseInstanceRecord
}

type DatabaseManagementAuthorizer interface {
	AuthorizeConnection(context.Context, string, []string, string, string) (bool, error)
	AuthorizeBatch(context.Context, string, []AuthorizationRequest) ([]AuthorizationDecision, error)
}

type DatabaseAccountDeprovisioner interface {
	Deprovision(context.Context, string) error
}

type DatabaseManagementService struct {
	repository    DatabaseManagementRepository
	authorizer    DatabaseManagementAuthorizer
	deprovisioner DatabaseAccountDeprovisioner
	now           func() time.Time
}

func NewDatabaseManagementService(repository DatabaseManagementRepository, authorizer DatabaseManagementAuthorizer, deprovisioner DatabaseAccountDeprovisioner) (*DatabaseManagementService, error) {
	if repository == nil {
		return nil, errors.New("database management repository is required")
	}
	if authorizer == nil {
		return nil, errors.New("database management authorizer is required")
	}
	if deprovisioner == nil {
		return nil, errors.New("database account deprovisioner is required")
	}
	return &DatabaseManagementService{repository: repository, authorizer: authorizer, deprovisioner: deprovisioner, now: time.Now}, nil
}

func (s *DatabaseManagementService) ListInstances(ctx context.Context, actorID string, connectable bool) ([]DatabaseInstance, error) {
	action := rbac.ActionDBProxyView
	if connectable {
		action = rbac.ActionDBConnect
	}
	now := s.now().UTC()
	instances, err := s.repository.DatabaseInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("list database instances: %w", err)
	}
	accounts, err := s.repository.DatabaseAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list database accounts: %w", err)
	}
	instanceVisible, err := s.batch(ctx, actorID, []string{action}, model.ResourceTypeDatabaseInstance, databaseInstanceIDs(instances))
	if err != nil {
		return nil, err
	}
	instanceManage, err := s.batch(ctx, actorID, []string{rbac.ActionDBProxyUpdate, rbac.ActionDBProxyDelete}, model.ResourceTypeDatabaseInstance, databaseInstanceIDs(instances))
	if err != nil {
		return nil, err
	}
	accountVisible, err := s.batch(ctx, actorID, []string{action}, model.ResourceTypeDatabaseAccount, databaseAccountIDs(accounts))
	if err != nil {
		return nil, err
	}
	instancesByID := databaseInstancesByID(instances)
	accountCount := map[string]int{}
	for i, account := range accounts {
		instance, found := instancesByID[account.InstanceID]
		if accountVisible[i] && (!connectable || found && databaseAccountConnectable(account, instance, now)) {
			accountCount[account.InstanceID]++
		}
	}
	result := make([]DatabaseInstance, 0, len(instances))
	for i, instance := range instances {
		if connectable && !databaseInstanceActive(instance) {
			continue
		}
		if instanceVisible[i] {
			instance.CanManage = instanceManage[i]
			if connectable {
				instance.AccountCount = accountCount[instance.ID]
			}
			result = append(result, instance)
			continue
		}
		if accountCount[instance.ID] != 0 {
			instance.AccountCount = accountCount[instance.ID]
			result = append(result, instance)
		}
	}
	return result, nil
}

func (s *DatabaseManagementService) ListAccounts(ctx context.Context, actorID string, instanceID string, connectable bool) ([]DatabaseAccount, error) {
	action := rbac.ActionDBProxyView
	if connectable {
		action = rbac.ActionDBConnect
	}
	var (
		accounts []DatabaseAccount
		err      error
	)
	if strings.TrimSpace(instanceID) == "" {
		accounts, err = s.repository.DatabaseAccounts(ctx)
	} else {
		accounts, err = s.repository.ListDatabaseAccountsByInstance(ctx, instanceID)
	}
	if err != nil {
		return nil, fmt.Errorf("list database accounts: %w", err)
	}
	var instances []DatabaseInstance
	if connectable {
		instances, err = s.repository.DatabaseInstances(ctx)
		if err != nil {
			return nil, fmt.Errorf("list database instances: %w", err)
		}
	}
	visible, err := s.batch(ctx, actorID, []string{action}, model.ResourceTypeDatabaseAccount, databaseAccountIDs(accounts))
	if err != nil {
		return nil, err
	}
	manageable, err := s.batch(ctx, actorID, []string{rbac.ActionDBProxyUpdate, rbac.ActionDBProxyDelete}, model.ResourceTypeDatabaseAccount, databaseAccountIDs(accounts))
	if err != nil {
		return nil, err
	}
	result := make([]DatabaseAccount, 0, len(accounts))
	now := s.now().UTC()
	instancesByID := databaseInstancesByID(instances)
	for i, account := range accounts {
		if !visible[i] {
			continue
		}
		if connectable {
			instance, found := instancesByID[account.InstanceID]
			if !found || !databaseAccountConnectable(account, instance, now) {
				continue
			}
		}
		account.CanManage = manageable[i]
		result = append(result, account)
	}
	return result, nil
}

func (s *DatabaseManagementService) GetInstance(ctx context.Context, actorID, id string) (DatabaseInstance, error) {
	if err := s.authorizeInstanceOrAccount(ctx, actorID, rbac.ActionDBProxyView, id); err != nil {
		return DatabaseInstance{}, err
	}
	view, err := s.repository.DatabaseInstance(ctx, id)
	if err != nil {
		return DatabaseInstance{}, fmt.Errorf("get database instance: %w", err)
	}
	return view, nil
}

func (s *DatabaseManagementService) GetAccount(ctx context.Context, actorID, id string) (DatabaseAccount, error) {
	if err := s.authorize(ctx, actorID, []string{rbac.ActionDBProxyView}, model.ResourceTypeDatabaseAccount, id); err != nil {
		return DatabaseAccount{}, err
	}
	view, err := s.repository.DatabaseAccount(ctx, id)
	if err != nil {
		return DatabaseAccount{}, fmt.Errorf("get database account: %w", err)
	}
	return view, nil
}

func (s *DatabaseManagementService) CreateInstance(ctx context.Context, actorID string, superAdmin bool, input DatabaseInstanceInput) (DatabaseInstance, error) {
	if err := s.authorize(ctx, actorID, []string{rbac.ActionDBProxyCreate}, "", ""); err != nil {
		return DatabaseInstance{}, err
	}
	creatorID := ""
	if !superAdmin {
		creatorID = actorID
	}
	view, err := s.repository.CreateDatabaseInstanceWithCreatorGrant(ctx, input, creatorID)
	if err != nil {
		return DatabaseInstance{}, fmt.Errorf("create database instance: %w", err)
	}
	return view, nil
}

func (s *DatabaseManagementService) UpdateInstance(ctx context.Context, actorID, id string, input DatabaseInstanceInput) (DatabaseInstance, error) {
	if err := s.authorize(ctx, actorID, []string{rbac.ActionDBProxyUpdate}, model.ResourceTypeDatabaseInstance, id); err != nil {
		return DatabaseInstance{}, err
	}
	view, err := s.repository.UpdateDatabaseInstance(ctx, id, input)
	if err != nil {
		return DatabaseInstance{}, fmt.Errorf("update database instance: %w", err)
	}
	return view, nil
}

func (s *DatabaseManagementService) DeleteInstance(ctx context.Context, actorID, id string) error {
	if err := s.authorize(ctx, actorID, []string{rbac.ActionDBProxyDelete}, model.ResourceTypeDatabaseInstance, id); err != nil {
		return err
	}
	if err := s.repository.DeleteDatabaseInstance(ctx, id); err != nil {
		return fmt.Errorf("delete database instance: %w", err)
	}
	return nil
}

func (s *DatabaseManagementService) CreateAccount(ctx context.Context, actorID, instanceID, username, password, group, remark string, expiresAt *time.Time) (DatabaseAccount, error) {
	if strings.TrimSpace(password) == "" {
		return DatabaseAccount{}, fmt.Errorf("%w: password is required", ErrDatabaseManagementInvalid)
	}
	if err := s.authorize(ctx, actorID, []string{rbac.ActionDBProxyCreate}, model.ResourceTypeDatabaseInstance, instanceID); err != nil {
		return DatabaseAccount{}, err
	}
	view, err := s.repository.AddDatabaseAccount(ctx, instanceID, username, password, group, remark, expiresAt)
	if err != nil {
		return DatabaseAccount{}, fmt.Errorf("create database account: %w", err)
	}
	return view, nil
}

func (s *DatabaseManagementService) UpdateAccount(ctx context.Context, actorID, id, username, password, group, remark string, expiresAt *time.Time, status string) (DatabaseAccount, error) {
	if err := s.authorize(ctx, actorID, []string{rbac.ActionDBProxyUpdate}, model.ResourceTypeDatabaseAccount, id); err != nil {
		return DatabaseAccount{}, err
	}
	view, err := s.repository.UpdateDatabaseAccount(ctx, id, username, password, group, remark, expiresAt, status)
	if err != nil {
		return DatabaseAccount{}, fmt.Errorf("update database account: %w", err)
	}
	return view, nil
}

func (s *DatabaseManagementService) DeleteAccount(ctx context.Context, actorID, id string) error {
	if err := s.authorize(ctx, actorID, []string{rbac.ActionDBProxyDelete}, model.ResourceTypeDatabaseAccount, id); err != nil {
		return err
	}
	if err := s.deprovisioner.Deprovision(ctx, id); err == nil {
		return nil
	} else if !errors.Is(err, ErrDatabaseAccountNotManaged) {
		return fmt.Errorf("deprovision database account: %w", err)
	}
	if err := s.repository.DeleteDatabaseAccount(ctx, id); err != nil {
		return fmt.Errorf("delete database account: %w", err)
	}
	return nil
}

type DatabaseProbeTarget struct {
	Instance           DatabaseInstanceRecord
	Username, Password string
}

func (s *DatabaseManagementService) SavedAccountProbe(ctx context.Context, actorID, id string) (DatabaseProbeTarget, error) {
	metadata, err := s.repository.DatabaseAccountProbeMetadata(ctx, id)
	if err != nil {
		return DatabaseProbeTarget{}, fmt.Errorf("load database account: %w", err)
	}
	if err := s.authorize(ctx, actorID, []string{rbac.ActionDBConnect}, model.ResourceTypeDatabaseAccount, metadata.ID); err != nil {
		return DatabaseProbeTarget{}, err
	}
	if err := s.validateConnectable(metadata.Status, metadata.ExpiresAt, metadata.Instance.Status); err != nil {
		return DatabaseProbeTarget{}, err
	}
	password, err := s.repository.DatabaseAccountProbePassword(ctx, metadata.ID)
	if err != nil {
		return DatabaseProbeTarget{}, fmt.Errorf("load database account password: %w", err)
	}
	return DatabaseProbeTarget{Instance: metadata.Instance, Username: metadata.Username, Password: password}, nil
}

func (s *DatabaseManagementService) PayloadProbe(ctx context.Context, actorID, instanceID, username, password string) (DatabaseProbeTarget, error) {
	if strings.TrimSpace(instanceID) == "" || password == "" {
		return DatabaseProbeTarget{}, fmt.Errorf("%w: instance_id and password are required", ErrDatabaseManagementInvalid)
	}
	if err := s.authorize(ctx, actorID, []string{rbac.ActionDBProxyCreate}, "", ""); err != nil {
		return DatabaseProbeTarget{}, err
	}
	if err := s.authorize(ctx, actorID, []string{rbac.ActionDBProxyCreate}, model.ResourceTypeDatabaseInstance, instanceID); err != nil {
		return DatabaseProbeTarget{}, err
	}
	instance, err := s.repository.DatabaseInstanceForProbe(ctx, instanceID)
	if err != nil {
		return DatabaseProbeTarget{}, fmt.Errorf("load database instance: %w", err)
	}
	if err := s.validateConnectable("active", nil, instance.Status); err != nil {
		return DatabaseProbeTarget{}, err
	}
	return DatabaseProbeTarget{Instance: instance, Username: strings.TrimSpace(username), Password: password}, nil
}

func (s *DatabaseManagementService) validateConnectable(accountStatus string, expiresAt *time.Time, instanceStatus string) error {
	if strings.EqualFold(strings.TrimSpace(accountStatus), "disabled") || strings.EqualFold(strings.TrimSpace(instanceStatus), "disabled") {
		return ErrDatabaseManagementDisabled
	}
	if expiresAt != nil && !expiresAt.After(s.now().UTC()) {
		return ErrDatabaseManagementExpired
	}
	return nil
}

func databaseInstanceActive(instance DatabaseInstance) bool {
	return strings.EqualFold(strings.TrimSpace(instance.Status), "active")
}

func databaseAccountConnectable(account DatabaseAccount, instance DatabaseInstance, now time.Time) bool {
	if !strings.EqualFold(strings.TrimSpace(account.Status), "active") || !databaseInstanceActive(instance) {
		return false
	}
	return account.ExpiresAt == nil || account.ExpiresAt.After(now)
}

func databaseInstancesByID(instances []DatabaseInstance) map[string]DatabaseInstance {
	result := make(map[string]DatabaseInstance, len(instances))
	for _, instance := range instances {
		result[instance.ID] = instance
	}
	return result
}

func (s *DatabaseManagementService) authorizeInstanceOrAccount(ctx context.Context, actorID, action, instanceID string) error {
	if err := s.authorize(ctx, actorID, []string{action}, model.ResourceTypeDatabaseInstance, instanceID); err == nil {
		return nil
	} else if !errors.Is(err, ErrDatabaseManagementForbidden) {
		return err
	}
	accounts, err := s.repository.ListDatabaseAccountsByInstance(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("list database instance accounts: %w", err)
	}
	for _, account := range accounts {
		if err := s.authorize(ctx, actorID, []string{action}, model.ResourceTypeDatabaseAccount, account.ID); err == nil {
			return nil
		} else if !errors.Is(err, ErrDatabaseManagementForbidden) {
			return err
		}
	}
	return ErrDatabaseManagementForbidden
}

func (s *DatabaseManagementService) authorize(ctx context.Context, actorID string, actions []string, resourceType, resourceID string) error {
	allowed, err := s.authorizer.AuthorizeConnection(ctx, strings.TrimSpace(actorID), actions, resourceType, resourceID)
	if err != nil {
		return fmt.Errorf("authorize database resource: %w", err)
	}
	if !allowed {
		return ErrDatabaseManagementForbidden
	}
	return nil
}

func (s *DatabaseManagementService) batch(ctx context.Context, actorID string, actions []string, resourceType string, ids []string) ([]bool, error) {
	requests := make([]AuthorizationRequest, len(ids))
	for i, id := range ids {
		requests[i] = AuthorizationRequest{Actions: actions, ResourceType: resourceType, ResourceID: id}
	}
	decisions, err := s.authorizer.AuthorizeBatch(ctx, strings.TrimSpace(actorID), requests)
	if err != nil {
		return nil, fmt.Errorf("authorize database resources: %w", err)
	}
	if len(decisions) != len(ids) {
		return nil, errors.New("database authorization decision count mismatch")
	}
	allowed := make([]bool, len(ids))
	for i := range decisions {
		allowed[i] = decisions[i].Allowed
	}
	return allowed, nil
}

func databaseInstanceIDs(values []DatabaseInstance) []string {
	ids := make([]string, len(values))
	for i := range values {
		ids[i] = values[i].ID
	}
	return ids
}
func databaseAccountIDs(values []DatabaseAccount) []string {
	ids := make([]string, len(values))
	for i := range values {
		ids[i] = values[i].ID
	}
	return ids
}
