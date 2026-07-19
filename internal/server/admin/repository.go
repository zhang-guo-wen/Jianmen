package admin

import (
	"context"
	"errors"
	"reflect"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/service"
	"jianmen/internal/store"
)

var errAdminStoreRequired = errors.New("admin store is required")

// adminRepository is the compile-time boundary accepted by New. Its method set
// is composed from resource-scoped interfaces owned by the Admin consumer and
// existing service repositories; Server retains the dependencies by domain.
type adminRepository interface {
	adminAIAccessTokenRepository
	adminHostTargetRepository
	adminDatabaseRepository
	adminApplicationRepository
	adminContainerRepository
	service.PlatformAccountRepository
	adminUserSessionCreationRepository
	adminAuditRepository
	adminConnectionPasswordRepository
	adminUserPreferenceRepository
	resourceAccessRepository
	service.TemporaryAccessRepository
	service.UserRepository
	service.UserGroupRepository
	service.RoleManagementRepository
}

// adminDependencies keeps the server coupled to resource-scoped repositories
// instead of the application-wide repository aggregate.
type adminDependencies struct {
	aiTokens            adminAIAccessTokenRepository
	aiResources         service.AIResourceRepository
	hostTargets         adminHostTargetRepository
	databases           adminDatabaseRepository
	applications        adminApplicationRepository
	containers          adminContainerRepository
	platformAccounts    service.PlatformAccountRepository
	userSessionCreation adminUserSessionCreationRepository
	audit               adminAuditRepository
	connectionPassword  adminConnectionPasswordRepository
	preferences         adminUserPreferenceRepository
	resourceAccess      resourceAccessRepository
	temporaryAccess     service.TemporaryAccessRepository
	users               service.UserRepository
	userGroups          service.UserGroupRepository
	roles               service.RoleManagementRepository
}

type adminAIAccessTokenRepository interface {
	service.AIAccessTokenRepository
}

type adminHostTargetRepository interface {
	Hosts(context.Context) ([]store.HostView, error)
	Host(context.Context, string) (store.HostView, error)
	AddHost(context.Context, store.HostRecord) (store.HostView, error)
	UpdateHost(context.Context, string, store.HostRecord) (store.HostView, error)
	DeleteHost(context.Context, string) error
	Targets(context.Context) ([]store.TargetView, error)
	Target(context.Context, string) (store.TargetView, error)
	TargetConfig(context.Context, string) (store.TargetConfig, error)
	AddTarget(context.Context, config.Target) (store.TargetView, error)
	UpdateTarget(context.Context, string, config.Target) (store.TargetView, error)
	DeleteTarget(context.Context, string) error
	DefaultTarget(context.Context, model.User) (store.TargetConfig, error)
}

type adminDatabaseRepository interface {
	DatabaseInstances(context.Context) []store.DatabaseInstanceView
	ListDatabaseInstances(context.Context) ([]store.DatabaseInstanceView, error)
	DatabaseInstance(context.Context, string) (store.DatabaseInstanceView, error)
	AddDatabaseInstance(context.Context, store.DatabaseInstanceInput) (store.DatabaseInstanceView, error)
	UpdateDatabaseInstance(context.Context, string, store.DatabaseInstanceInput) (store.DatabaseInstanceView, error)
	DeleteDatabaseInstance(context.Context, string) error
	DatabaseAccounts(context.Context) ([]store.DatabaseAccountView, error)
	ListDatabaseAccountsByInstance(context.Context, string) ([]store.DatabaseAccountView, error)
	DatabaseAccount(context.Context, string) (store.DatabaseAccountView, error)
	AddDatabaseAccount(context.Context, string, string, string, string, string, *time.Time) (store.DatabaseAccountView, error)
	UpdateDatabaseAccount(context.Context, string, string, string, string, string, *time.Time, string) (store.DatabaseAccountView, error)
	DeleteDatabaseAccount(context.Context, string) error
	CreateDatabaseInstanceWithCreatorGrant(context.Context, store.DatabaseInstanceInput, string) (store.DatabaseInstanceView, error)
	DatabaseAccountProbeMetadata(context.Context, string) (store.DatabaseAccountProbeMetadata, error)
	DatabaseAccountProbePassword(context.Context, string) (string, error)
	DatabaseInstanceForProbe(context.Context, string) (model.DatabaseInstance, error)
}

type adminApplicationRepository interface {
	service.ApplicationRepository
}

type adminContainerRepository interface {
	service.ContainerManagementRepository
}

type adminUserSessionCreationRepository interface {
	service.UserSessionCreationRepository
}

type adminAuditRepository interface {
	CreateAuditSession(context.Context, *model.AuditSession) error
	EndAuditSession(context.Context, string) error
	GetAuditSession(context.Context, string) (*model.AuditSession, error)
	ListAuditSessions(context.Context, store.AuditListParams) ([]store.AuditSessionView, int64, error)
	UpdateAuditProtocol(context.Context, string, string) error
	CreateAuditSSHCommand(context.Context, *model.AuditSSHCommand) error
	ListAuditSSHCommands(context.Context, string, store.PageOpts) ([]model.AuditSSHCommand, int64, error)
	CreateAuditSFTPEvent(context.Context, *model.AuditSFTPEvent) error
	ListAuditSFTPEvents(context.Context, string, store.PageOpts) ([]model.AuditSFTPEvent, int64, error)
	ListAuditDBQueryPreviews(context.Context, string, store.AuditDBQueryPreviewParams) ([]store.AuditDBQueryPreview, int64, error)
	CreateAuditEvent(context.Context, *model.AuditEvent) error
	ListAuditEvents(context.Context, store.AuditEventListParams) ([]model.AuditEvent, int64, error)
	CreateLoginAuditLog(context.Context, *model.LoginAuditLog) error
	ListLoginAuditLogs(context.Context, store.LoginAuditListParams) ([]model.LoginAuditLog, int64, error)
}

type adminConnectionPasswordRepository interface {
	service.ConnectionPasswordRepository
}

type adminUserPreferenceRepository interface {
	UserPreference(context.Context, string) (model.UserPreference, error)
	SaveUserPreference(context.Context, model.UserPreference) (model.UserPreference, error)
}

func resolveAdminDependencies(repository adminRepository) (adminDependencies, error) {
	if isNilAdminRepository(repository) {
		return adminDependencies{}, errAdminStoreRequired
	}
	return adminDependencies{
		aiTokens:            repository,
		aiResources:         aiResourceRepositoryAdapter{hostTargets: repository, databases: repository},
		hostTargets:         repository,
		databases:           repository,
		applications:        repository,
		containers:          repository,
		platformAccounts:    repository,
		userSessionCreation: repository,
		audit:               repository,
		connectionPassword:  repository,
		preferences:         repository,
		resourceAccess:      repository,
		temporaryAccess:     repository,
		users:               repository,
		userGroups:          repository,
		roles:               repository,
	}, nil
}

func isNilAdminRepository(repository adminRepository) bool {
	if repository == nil {
		return true
	}
	value := reflect.ValueOf(repository)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
