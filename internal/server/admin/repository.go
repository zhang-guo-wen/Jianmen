package admin

import (
	"context"
	"errors"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/service"
	"jianmen/internal/store"
)

var (
	errAdminStoreRequired                  = errors.New("admin store is required")
	errAdminAIAccessTokensUnsupported      = errors.New("admin store does not support AI access tokens")
	errAdminHostTargetsUnsupported         = errors.New("admin store does not support host targets")
	errAdminDatabasesUnsupported           = errors.New("admin store does not support databases")
	errAdminApplicationsUnsupported        = errors.New("admin store does not support applications")
	errAdminContainersUnsupported          = errors.New("admin store does not support containers")
	errAdminPlatformAccountsUnsupported    = errors.New("admin store does not support platform accounts")
	errAdminUserSessionsUnsupported        = errors.New("admin store does not support user sessions")
	errAdminAuditUnsupported               = errors.New("admin store does not support audit")
	errAdminConnectionPasswordsUnsupported = errors.New("admin store does not support connection passwords")
	errAdminUserPreferencesUnsupported     = errors.New("admin store does not support user preferences")
	errAdminResourceAccessUnsupported      = errors.New("admin store does not support resource access")
	errAdminTemporaryAccessUnsupported     = errors.New("admin store does not support temporary access")
	errAdminUsersUnsupported               = errors.New("admin store does not support user management")
	errAdminUserGroupsUnsupported          = errors.New("admin store does not support user group management")
	errAdminRolesUnsupported               = errors.New("admin store does not support role management")
)

// adminDependencies keeps the server coupled to resource-scoped repositories
// instead of the application-wide repository aggregate.
type adminDependencies struct {
	aiTokens           adminAIAccessTokenRepository
	hostTargets        adminHostTargetRepository
	databases          adminDatabaseRepository
	applications       adminApplicationRepository
	containers         adminContainerRepository
	platformAccounts   adminPlatformAccountRepository
	userSessions       adminUserSessionRepository
	audit              adminAuditRepository
	connectionPassword adminConnectionPasswordRepository
	preferences        adminUserPreferenceRepository
	resourceAccess     resourceAccessRepository
	temporaryAccess    service.TemporaryAccessRepository
	users              service.UserRepository
	userGroups         service.UserGroupRepository
	roles              service.RoleManagementRepository
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

type adminUserSessionRepository interface {
	UserSessions(string) ([]store.SessionView, error)
	CreateUserSession(model.UserSession) (*model.UserSession, error)
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

func resolveAdminDependencies(repository any) (adminDependencies, error) {
	if repository == nil {
		return adminDependencies{}, errAdminStoreRequired
	}
	dependencies := adminDependencies{}
	var ok bool
	if dependencies.aiTokens, ok = repository.(adminAIAccessTokenRepository); !ok {
		return adminDependencies{}, errAdminAIAccessTokensUnsupported
	}
	if dependencies.hostTargets, ok = repository.(adminHostTargetRepository); !ok {
		return adminDependencies{}, errAdminHostTargetsUnsupported
	}
	if dependencies.databases, ok = repository.(adminDatabaseRepository); !ok {
		return adminDependencies{}, errAdminDatabasesUnsupported
	}
	if dependencies.applications, ok = repository.(adminApplicationRepository); !ok {
		return adminDependencies{}, errAdminApplicationsUnsupported
	}
	if dependencies.containers, ok = repository.(adminContainerRepository); !ok {
		return adminDependencies{}, errAdminContainersUnsupported
	}
	if dependencies.platformAccounts, ok = repository.(adminPlatformAccountRepository); !ok {
		return adminDependencies{}, errAdminPlatformAccountsUnsupported
	}
	if dependencies.userSessions, ok = repository.(adminUserSessionRepository); !ok {
		return adminDependencies{}, errAdminUserSessionsUnsupported
	}
	if dependencies.audit, ok = repository.(adminAuditRepository); !ok {
		return adminDependencies{}, errAdminAuditUnsupported
	}
	if dependencies.connectionPassword, ok = repository.(adminConnectionPasswordRepository); !ok {
		return adminDependencies{}, errAdminConnectionPasswordsUnsupported
	}
	if dependencies.preferences, ok = repository.(adminUserPreferenceRepository); !ok {
		return adminDependencies{}, errAdminUserPreferencesUnsupported
	}
	if dependencies.resourceAccess, ok = repository.(resourceAccessRepository); !ok {
		return adminDependencies{}, errAdminResourceAccessUnsupported
	}
	if dependencies.temporaryAccess, ok = repository.(service.TemporaryAccessRepository); !ok {
		return adminDependencies{}, errAdminTemporaryAccessUnsupported
	}
	if dependencies.users, ok = repository.(service.UserRepository); !ok {
		return adminDependencies{}, errAdminUsersUnsupported
	}
	if dependencies.userGroups, ok = repository.(service.UserGroupRepository); !ok {
		return adminDependencies{}, errAdminUserGroupsUnsupported
	}
	if dependencies.roles, ok = repository.(service.RoleManagementRepository); !ok {
		return adminDependencies{}, errAdminRolesUnsupported
	}
	return dependencies, nil
}
