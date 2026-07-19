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
	Hosts(context.Context) ([]store.HostView, error)
	Host(context.Context, string) (store.HostView, error)
	AddHost(context.Context, store.HostRecord) (store.HostView, error)
	UpdateHost(context.Context, string, store.HostRecord) (store.HostView, error)
	DeleteHost(context.Context, string) error
	Targets(context.Context) ([]store.TargetView, error)
	ListHostAccounts(context.Context, string) ([]store.TargetView, error)
	Target(context.Context, string) (store.TargetView, error)
	TargetConfig(context.Context, string) (store.TargetConfig, error)
	AddTarget(context.Context, config.Target) (store.TargetView, error)
	UpdateTarget(context.Context, string, config.Target) (store.TargetView, error)
	DeleteTarget(context.Context, string) error
	DefaultTarget(context.Context, model.User) (store.TargetConfig, error)
}

type hostManagementRepositoryAdapter struct{ repository adminHostTargetRepository }

func (a hostManagementRepositoryAdapter) Hosts(ctx context.Context) ([]service.HostManagementHostView, error) {
	views, err := a.repository.Hosts(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]service.HostManagementHostView, len(views))
	for index := range views {
		result[index] = hostManagementHostView(views[index])
	}
	return result, nil
}

func (a hostManagementRepositoryAdapter) Host(ctx context.Context, id string) (service.HostManagementHostView, error) {
	view, err := a.repository.Host(ctx, id)
	return hostManagementHostView(view), err
}

func (a hostManagementRepositoryAdapter) AddHost(ctx context.Context, record service.HostManagementHostRecord) (service.HostManagementHostView, error) {
	view, err := a.repository.AddHost(ctx, store.HostRecord{ID: record.ID, Name: record.Name, Group: record.Group, Address: record.Address, Port: record.Port, Protocol: record.Protocol, Remark: record.Remark, Status: record.Status})
	return hostManagementHostView(view), err
}

func (a hostManagementRepositoryAdapter) UpdateHost(ctx context.Context, id string, record service.HostManagementHostRecord) (service.HostManagementHostView, error) {
	view, err := a.repository.UpdateHost(ctx, id, store.HostRecord{ID: record.ID, Name: record.Name, Group: record.Group, Address: record.Address, Port: record.Port, Protocol: record.Protocol, Remark: record.Remark, Status: record.Status})
	return hostManagementHostView(view), err
}

func (a hostManagementRepositoryAdapter) DeleteHost(ctx context.Context, id string) error {
	return a.repository.DeleteHost(ctx, id)
}

func (a hostManagementRepositoryAdapter) Targets(ctx context.Context) ([]service.HostManagementTargetView, error) {
	views, err := a.repository.Targets(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]service.HostManagementTargetView, len(views))
	for index := range views {
		result[index] = hostManagementTargetView(views[index])
	}
	return result, nil
}

func (a hostManagementRepositoryAdapter) ListHostAccounts(ctx context.Context, hostID string) ([]service.HostManagementTargetView, error) {
	views, err := a.repository.ListHostAccounts(ctx, hostID)
	if err != nil {
		return nil, err
	}
	result := make([]service.HostManagementTargetView, len(views))
	for index := range views {
		result[index] = hostManagementTargetView(views[index])
	}
	return result, nil
}

func (a hostManagementRepositoryAdapter) Target(ctx context.Context, id string) (service.HostManagementTargetView, error) {
	view, err := a.repository.Target(ctx, id)
	return hostManagementTargetView(view), err
}

func (a hostManagementRepositoryAdapter) TargetConfig(ctx context.Context, id string) (service.HostManagementTargetConfig, error) {
	config, err := a.repository.TargetConfig(ctx, id)
	return hostManagementTargetConfig(config), err
}

func (a hostManagementRepositoryAdapter) AddTarget(ctx context.Context, target config.Target) (service.HostManagementTargetView, error) {
	view, err := a.repository.AddTarget(ctx, target)
	return hostManagementTargetView(view), err
}

func (a hostManagementRepositoryAdapter) UpdateTarget(ctx context.Context, id string, target config.Target) (service.HostManagementTargetView, error) {
	view, err := a.repository.UpdateTarget(ctx, id, target)
	return hostManagementTargetView(view), err
}

func (a hostManagementRepositoryAdapter) DeleteTarget(ctx context.Context, id string) error {
	return a.repository.DeleteTarget(ctx, id)
}

func hostManagementHostView(view store.HostView) service.HostManagementHostView {
	return service.HostManagementHostView{ID: view.ID, Name: view.Name, Group: view.Group, Address: view.Address, Port: view.Port, Protocol: view.Protocol, Remark: view.Remark, Status: view.Status, AccountCount: view.AccountCount, CreatedAt: view.CreatedAt, UpdatedAt: view.UpdatedAt, CanManage: view.CanManage}
}

func hostManagementTargetView(view store.TargetView) service.HostManagementTargetView {
	return service.HostManagementTargetView{ID: view.ID, HostID: view.HostID, ResourceType: view.ResourceType, ResourceID: view.ResourceID, ResourceSeq: view.ResourceSeq, HostResourceID: view.HostResourceID, Name: view.Name, Group: view.Group, Remark: view.Remark, ExpiresAt: view.ExpiresAt, Status: view.Status, Host: view.Host, Port: view.Port, Protocol: view.Protocol, Username: view.Username, Domain: view.Domain, AuthMethods: view.AuthMethods, InsecureIgnoreHostKey: view.InsecureIgnoreHostKey, HostKeyFingerprint: view.HostKeyFingerprint, KnownHostsPath: view.KnownHostsPath, RDPSecurity: view.RDPSecurity, RDPIgnoreCertificate: view.RDPIgnoreCertificate, RDPCertFingerprints: view.RDPCertFingerprints, RDPApprovalRequired: view.RDPApprovalRequired, RDPClipboardRead: view.RDPClipboardRead, RDPClipboardWrite: view.RDPClipboardWrite, RDPFileUpload: view.RDPFileUpload, RDPFileDownload: view.RDPFileDownload, RDPDriveMapping: view.RDPDriveMapping, CanManage: view.CanManage}
}

func hostManagementTargetConfig(config store.TargetConfig) service.HostManagementTargetConfig {
	return service.HostManagementTargetConfig{ID: config.ID, Name: config.Name, HostName: config.HostName, Host: config.Host, Port: config.Port, Protocol: config.Protocol, Username: config.Username, Domain: config.Domain, Password: config.Password, PrivateKeyPath: config.PrivateKeyPath, PrivateKeyPEM: config.PrivateKeyPEM, Passphrase: config.Passphrase, InsecureIgnoreHostKey: config.InsecureIgnoreHostKey, HostKeyFingerprint: config.HostKeyFingerprint, KnownHostsPath: config.KnownHostsPath, RDPSecurity: config.RDPSecurity, RDPIgnoreCertificate: config.RDPIgnoreCertificate, RDPCertFingerprints: config.RDPCertFingerprints, RDPApprovalRequired: config.RDPApprovalRequired, RDPClipboardRead: config.RDPClipboardRead, RDPClipboardWrite: config.RDPClipboardWrite, RDPFileUpload: config.RDPFileUpload, RDPFileDownload: config.RDPFileDownload, RDPDriveMapping: config.RDPDriveMapping, Disabled: config.Disabled, ExpiresAt: config.ExpiresAt, HostID: config.HostID}
}

type adminDatabaseRepository interface {
	DatabaseInstances(context.Context) []store.DatabaseInstanceView
	DatabaseInstance(context.Context, string) (store.DatabaseInstanceView, error)
	AddDatabaseInstance(context.Context, store.DatabaseInstanceInput) (store.DatabaseInstanceView, error)
	UpdateDatabaseInstance(context.Context, string, store.DatabaseInstanceInput) (store.DatabaseInstanceView, error)
	DeleteDatabaseInstance(context.Context, string) error
	DatabaseAccounts(context.Context) ([]store.DatabaseAccountView, error)
	DatabaseAccount(context.Context, string) (store.DatabaseAccountView, error)
	AddDatabaseAccount(context.Context, string, string, string, string, string, *time.Time) (store.DatabaseAccountView, error)
	UpdateDatabaseAccount(context.Context, string, string, string, string, string, *time.Time, string) (store.DatabaseAccountView, error)
	DeleteDatabaseAccount(context.Context, string) error
}

type adminApplicationRepository interface {
	Applications(context.Context) []store.ApplicationView
	Application(context.Context, string) (store.ApplicationView, error)
	AddApplication(context.Context, store.ApplicationInput) (store.ApplicationView, error)
	UpdateApplication(context.Context, string, store.ApplicationInput) (store.ApplicationView, error)
	DeleteApplication(context.Context, string) error
}

type adminContainerRepository interface {
	ListContainerEndpoints(context.Context, store.ContainerEndpointListParams) ([]store.ContainerEndpointView, int64, error)
	ContainerEndpoint(context.Context, string) (store.ContainerEndpointView, error)
	AddContainerEndpoint(context.Context, store.ContainerEndpointInput) (store.ContainerEndpointView, error)
	UpdateContainerEndpoint(context.Context, string, store.ContainerEndpointInput) (store.ContainerEndpointView, error)
	DeleteContainerEndpoint(context.Context, string) error
}

type adminPlatformAccountRepository interface {
	PlatformAccounts(context.Context, store.PlatformAccountListParams) ([]store.PlatformAccountView, int64, error)
	PlatformAccount(context.Context, string) (store.PlatformAccountView, error)
	AddPlatformAccount(context.Context, model.PlatformAccount) (store.PlatformAccountView, error)
	UpdatePlatformAccount(context.Context, string, model.PlatformAccount) (store.PlatformAccountView, error)
	DeletePlatformAccount(context.Context, string) error
	GetPlatformAccountPassword(context.Context, string) (string, error)
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
