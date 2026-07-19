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
	adminPlatformAccountRepository
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
	hostTargets         adminHostTargetRepository
	databases           adminDatabaseRepository
	applications        adminApplicationRepository
	containers          adminContainerRepository
	platformAccounts    adminPlatformAccountRepository
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
	ListAIAccessTokens(context.Context, string) ([]model.AIAccessToken, error)
	AuthenticateAIAccessToken(context.Context, string, time.Time) (model.AIAccessToken, error)
	RevokeAIAccessToken(context.Context, string, string, time.Time) error
}

type adminHostTargetRepository interface {
	Hosts() []store.HostView
	Host(string) (store.HostView, error)
	AddHost(store.HostRecord) (store.HostView, error)
	UpdateHost(string, store.HostRecord) (store.HostView, error)
	DeleteHost(string) error
	Targets() []store.TargetView
	Target(string) (store.TargetView, error)
	TargetConfig(string) (store.TargetConfig, error)
	AddTarget(config.Target) (store.TargetView, error)
	UpdateTarget(string, config.Target) (store.TargetView, error)
	DeleteTarget(string) error
	DefaultTarget(context.Context, model.User) (store.TargetConfig, error)
}

type adminDatabaseRepository interface {
	DatabaseInstances() []store.DatabaseInstanceView
	DatabaseInstance(string) (store.DatabaseInstanceView, error)
	AddDatabaseInstance(store.DatabaseInstanceInput) (store.DatabaseInstanceView, error)
	UpdateDatabaseInstance(string, store.DatabaseInstanceInput) (store.DatabaseInstanceView, error)
	DeleteDatabaseInstance(string) error
	DatabaseAccounts() ([]store.DatabaseAccountView, error)
	DatabaseAccount(string) (store.DatabaseAccountView, error)
	AddDatabaseAccount(string, string, string, string, string, *time.Time) (store.DatabaseAccountView, error)
	UpdateDatabaseAccount(string, string, string, string, string, *time.Time, string) (store.DatabaseAccountView, error)
	DeleteDatabaseAccount(string) error
}

type adminApplicationRepository interface {
	Applications() []store.ApplicationView
	Application(string) (store.ApplicationView, error)
	AddApplication(store.ApplicationInput) (store.ApplicationView, error)
	UpdateApplication(string, store.ApplicationInput) (store.ApplicationView, error)
	DeleteApplication(string) error
}

type adminContainerRepository interface {
	ListContainerEndpoints(context.Context, store.ContainerEndpointListParams) ([]store.ContainerEndpointView, int64, error)
	ContainerEndpoint(string) (store.ContainerEndpointView, error)
	AddContainerEndpoint(store.ContainerEndpointInput) (store.ContainerEndpointView, error)
	UpdateContainerEndpoint(string, store.ContainerEndpointInput) (store.ContainerEndpointView, error)
	DeleteContainerEndpoint(string) error
}

type adminPlatformAccountRepository interface {
	PlatformAccounts(store.PlatformAccountListParams) ([]store.PlatformAccountView, int64, error)
	PlatformAccount(string) (store.PlatformAccountView, error)
	AddPlatformAccount(model.PlatformAccount) (store.PlatformAccountView, error)
	UpdatePlatformAccount(string, model.PlatformAccount) (store.PlatformAccountView, error)
	DeletePlatformAccount(string) error
	GetPlatformAccountPassword(string) (string, error)
}

type adminUserSessionCreationRepository interface {
	service.UserSessionCreationRepository
}

type adminAuditRepository interface {
	CreateAuditSession(*model.AuditSession) error
	EndAuditSession(string) error
	GetAuditSession(string) (*model.AuditSession, error)
	ListAuditSessions(store.AuditListParams) ([]store.AuditSessionView, int64, error)
	UpdateAuditProtocol(string, string) error
	CreateAuditSSHCommand(*model.AuditSSHCommand) error
	ListAuditSSHCommands(string, store.PageOpts) ([]model.AuditSSHCommand, int64, error)
	CreateAuditSFTPEvent(*model.AuditSFTPEvent) error
	ListAuditSFTPEvents(string, store.PageOpts) ([]model.AuditSFTPEvent, int64, error)
	ListAuditDBQueryEvents(string) ([]model.AuditDBQuery, error)
	CreateAuditEvent(*model.AuditEvent) error
	ListAuditEvents(store.AuditEventListParams) ([]model.AuditEvent, int64, error)
	CreateLoginAuditLog(*model.LoginAuditLog) error
	ListLoginAuditLogs(store.LoginAuditListParams) ([]model.LoginAuditLog, int64, error)
}

type adminConnectionPasswordRepository interface {
	CreateConnectionPassword(context.Context, model.ConnectionPassword) error
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
