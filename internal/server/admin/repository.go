package admin

import (
	"context"
	"errors"
	"reflect"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/service"
	"jianmen/internal/sshhost"
	"jianmen/internal/store"
)

var errAdminStoreRequired = errors.New("admin store is required")

// adminRepository is the compile-time boundary accepted by New. Its method set
// is composed from resource-scoped interfaces owned by the Admin consumer and
// existing service repositories; Server retains the dependencies by domain.
type adminRepository interface {
	service.AdminAuthRepository
	adminAIAccessTokenRepository
	adminHostTargetRepository
	adminDatabaseRepository
	adminApplicationRepository
	adminContainerRepository
	service.PlatformAccountRepository
	adminUserSessionCreationRepository
	adminAuditRepository
	adminConnectionPasswordRepository
	resourceAccessRepository
	service.TemporaryAccessRepository
	service.UserRepository
	service.UserGroupRepository
	service.RoleManagementRepository
	service.UserPreferenceRepository
}

// adminDependencies keeps the server coupled to resource-scoped repositories
// instead of the application-wide repository aggregate.
type adminDependencies struct {
	adminAuth           service.AdminAuthRepository
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
	resourceAccess      resourceAccessRepository
	temporaryAccess     service.TemporaryAccessRepository
	users               service.UserRepository
	userGroups          service.UserGroupRepository
	roles               service.RoleManagementRepository
	userPreferences     service.UserPreferenceRepository
}

type adminAIAccessTokenRepository interface {
	service.AIAccessTokenRepository
}

type adminHostTargetRepository interface {
	Hosts(context.Context) ([]store.HostView, error)
	Host(context.Context, string) (store.HostView, error)
	AddHost(context.Context, store.HostRecord) (store.HostView, error)
	CreateManagedHost(context.Context, store.HostRecord, string) (store.HostView, error)
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

type hostIdentityCollectorAdapter struct {
	collector *sshhost.Collector
}

func (a hostIdentityCollectorAdapter) Collect(ctx context.Context, address string, port int) (service.HostIdentity, error) {
	identity, err := a.collector.Collect(ctx, address, port)
	if err != nil {
		return service.HostIdentity{}, err
	}
	return service.HostIdentity{Fingerprint: identity.Fingerprint, KnownHosts: identity.KnownHosts}, nil
}

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
	view, err := a.repository.AddHost(ctx, store.HostRecord{ID: record.ID, Name: record.Name, Group: record.Group, Address: record.Address, Port: record.Port, Protocol: record.Protocol, Remark: record.Remark, Status: record.Status, HostKeyFingerprint: record.HostKeyFingerprint, KnownHosts: record.KnownHosts})
	return hostManagementHostView(view), err
}

func (a hostManagementRepositoryAdapter) CreateManagedHost(ctx context.Context, record service.HostManagementHostRecord, creatorID string) (service.HostManagementHostView, error) {
	view, err := a.repository.CreateManagedHost(ctx, store.HostRecord{ID: record.ID, Name: record.Name, Group: record.Group, Address: record.Address, Port: record.Port, Protocol: record.Protocol, Remark: record.Remark, Status: record.Status, HostKeyFingerprint: record.HostKeyFingerprint, KnownHosts: record.KnownHosts}, creatorID)
	return hostManagementHostView(view), err
}

func (a hostManagementRepositoryAdapter) UpdateHost(ctx context.Context, id string, record service.HostManagementHostRecord) (service.HostManagementHostView, error) {
	view, err := a.repository.UpdateHost(ctx, id, store.HostRecord{ID: record.ID, Name: record.Name, Group: record.Group, Address: record.Address, Port: record.Port, Protocol: record.Protocol, Remark: record.Remark, Status: record.Status, HostKeyFingerprint: record.HostKeyFingerprint, KnownHosts: record.KnownHosts})
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
	var changeHandler func(string, string, string) (bool, error)
	if view.HostKeyChangeHandler != nil {
		changeHandler = func(hostID, oldFingerprint, newFingerprint string) (bool, error) {
			return view.HostKeyChangeHandler(sshhost.Change{HostID: hostID, OldFingerprint: oldFingerprint, NewFingerprint: newFingerprint})
		}
	}
	return service.HostManagementHostView{ID: view.ID, Name: view.Name, Group: view.Group, Address: view.Address, Port: view.Port, Protocol: view.Protocol, Remark: view.Remark, Status: view.Status, HostKeyFingerprint: view.HostKeyFingerprint, KnownHosts: view.KnownHosts, IdentityStatus: view.IdentityStatus, HostKeyChangeHandler: changeHandler, AccountCount: view.AccountCount, CreatedAt: view.CreatedAt, UpdatedAt: view.UpdatedAt, CanManage: view.CanManage}
}

func hostManagementTargetView(view store.TargetView) service.HostManagementTargetView {
	return service.HostManagementTargetView{ID: view.ID, HostID: view.HostID, ResourceType: view.ResourceType, ResourceID: view.ResourceID, ResourceSeq: view.ResourceSeq, HostResourceID: view.HostResourceID, Name: view.Name, Group: view.Group, Remark: view.Remark, ExpiresAt: view.ExpiresAt, Status: view.Status, HostStatus: view.HostStatus, Host: view.Host, Port: view.Port, Protocol: view.Protocol, Username: view.Username, Domain: view.Domain, AuthMethods: view.AuthMethods, InsecureIgnoreHostKey: view.InsecureIgnoreHostKey, HostKeyFingerprint: view.HostKeyFingerprint, KnownHostsPath: view.KnownHostsPath, RDPSecurity: view.RDPSecurity, RDPIgnoreCertificate: view.RDPIgnoreCertificate, RDPCertFingerprints: view.RDPCertFingerprints, RDPClipboardRead: view.RDPClipboardRead, RDPClipboardWrite: view.RDPClipboardWrite, RDPFileUpload: view.RDPFileUpload, RDPFileDownload: view.RDPFileDownload, RDPDriveMapping: view.RDPDriveMapping, CanManage: view.CanManage}
}

func hostManagementTargetConfig(config store.TargetConfig) service.HostManagementTargetConfig {
	var changeHandler func(string, string, string) (bool, error)
	if config.HostKeyChangeHandler != nil {
		changeHandler = func(hostID, oldFingerprint, newFingerprint string) (bool, error) {
			return config.HostKeyChangeHandler(sshhost.Change{HostID: hostID, OldFingerprint: oldFingerprint, NewFingerprint: newFingerprint})
		}
	}
	return service.HostManagementTargetConfig{ID: config.ID, Name: config.Name, HostName: config.HostName, Host: config.Host, Port: config.Port, Protocol: config.Protocol, Username: config.Username, Domain: config.Domain, Password: config.Password, PrivateKeyPath: config.PrivateKeyPath, PrivateKeyPEM: config.PrivateKeyPEM, Passphrase: config.Passphrase, InsecureIgnoreHostKey: config.InsecureIgnoreHostKey, HostKeyFingerprint: config.HostKeyFingerprint, KnownHosts: config.KnownHosts, KnownHostsPath: config.KnownHostsPath, HostKeyChangeHandler: changeHandler, RDPSecurity: config.RDPSecurity, RDPIgnoreCertificate: config.RDPIgnoreCertificate, RDPCertFingerprints: config.RDPCertFingerprints, RDPClipboardRead: config.RDPClipboardRead, RDPClipboardWrite: config.RDPClipboardWrite, RDPFileUpload: config.RDPFileUpload, RDPFileDownload: config.RDPFileDownload, RDPDriveMapping: config.RDPDriveMapping, Disabled: config.Disabled, ExpiresAt: config.ExpiresAt, HostID: config.HostID}
}

func storeHostKeyChangeHandler(handler func(string, string, string) (bool, error)) sshhost.ChangeHandler {
	if handler == nil {
		return nil
	}
	return func(change sshhost.Change) (bool, error) {
		return handler(change.HostID, change.OldFingerprint, change.NewFingerprint)
	}
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
	GetAuditSessionAccessMetadata(context.Context, string) (store.AuditSessionAccessMetadata, error)
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

func resolveAdminDependencies(repository adminRepository) (adminDependencies, error) {
	if isNilAdminRepository(repository) {
		return adminDependencies{}, errAdminStoreRequired
	}
	return adminDependencies{
		adminAuth:           repository,
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
		resourceAccess:      repository,
		temporaryAccess:     repository,
		users:               repository,
		userGroups:          repository,
		roles:               repository,
		userPreferences:     repository,
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
