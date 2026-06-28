package store

import (
	"context"
	"errors"
	"time"

	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"

	"jianmen/internal/access"
	"jianmen/internal/config"
	"jianmen/internal/model"
)

// StaticAdapter wraps access.StaticStore to implement Store.
type StaticAdapter struct {
	inner *access.StaticStore
}

func NewStaticAdapter(cfg *config.Config, db *gorm.DB) (*StaticAdapter, error) {
	inner, err := access.NewStaticStore(cfg, db)
	if err != nil {
		return nil, err
	}
	return &StaticAdapter{inner: inner}, nil
}

// -- auth --

func (a *StaticAdapter) Authenticate(ctx context.Context, username, password string) (model.User, error) {
	return a.inner.Authenticate(ctx, username, password)
}

func (a *StaticAdapter) AuthenticatePublicKey(ctx context.Context, username string, key ssh.PublicKey) (model.User, error) {
	return a.inner.AuthenticatePublicKey(ctx, username, key)
}

func (a *StaticAdapter) Users() []UserView {
	raw := a.inner.Users()
	out := make([]UserView, len(raw))
	for i := range raw {
		out[i] = UserView{ID: raw[i].ID, Username: raw[i].Username}
	}
	return out
}

// -- hosts --

func (a *StaticAdapter) Hosts() []HostView {
	raw := a.inner.Hosts()
	out := make([]HostView, len(raw))
	for i := range raw {
		out[i] = HostView{
			ID: raw[i].ID, Name: raw[i].Name, Group: raw[i].Group,
			Host: raw[i].Host, Port: raw[i].Port, Remark: raw[i].Remark,
			Disabled: raw[i].Disabled, Status: raw[i].Status,
			AccountCount: raw[i].AccountCount, Static: raw[i].Static,
		}
	}
	return out
}

func (a *StaticAdapter) Host(id string) (HostView, error) {
	h, err := a.inner.Host(id)
	if err != nil {
		return HostView{}, err
	}
	return HostView{
		ID: h.ID, Name: h.Name, Group: h.Group,
		Host: h.Host, Port: h.Port, Remark: h.Remark,
		Disabled: h.Disabled, Status: h.Status,
		AccountCount: h.AccountCount, Static: h.Static,
	}, nil
}

func (a *StaticAdapter) AddHost(host HostRecord) (HostView, error) {
	h, err := a.inner.AddHost(access.HostRecord{
		ID: host.ID, Name: host.Name, Group: host.Group,
		Host: host.Host, Port: host.Port, Remark: host.Remark, Disabled: host.Disabled,
	})
	if err != nil {
		return HostView{}, err
	}
	return HostView{
		ID: h.ID, Name: h.Name, Group: h.Group,
		Host: h.Host, Port: h.Port, Remark: h.Remark,
		Disabled: h.Disabled, Status: h.Status,
		AccountCount: h.AccountCount, Static: h.Static,
	}, nil
}

func (a *StaticAdapter) UpdateHost(id string, host HostRecord) (HostView, error) {
	h, err := a.inner.UpdateHost(id, access.HostRecord{
		ID: host.ID, Name: host.Name, Group: host.Group,
		Host: host.Host, Port: host.Port, Remark: host.Remark, Disabled: host.Disabled,
	})
	if err != nil {
		return HostView{}, err
	}
	return HostView{
		ID: h.ID, Name: h.Name, Group: h.Group,
		Host: h.Host, Port: h.Port, Remark: h.Remark,
		Disabled: h.Disabled, Status: h.Status,
		AccountCount: h.AccountCount, Static: h.Static,
	}, nil
}

func (a *StaticAdapter) DeleteHost(id string) error { return a.inner.DeleteHost(id) }

// -- targets / host accounts --

func (a *StaticAdapter) HostAccounts(hostID string) ([]TargetView, error) {
	raw, err := a.inner.HostAccounts(hostID)
	if err != nil {
		return nil, err
	}
	out := make([]TargetView, len(raw))
	for i := range raw {
		out[i] = adaptTargetView(raw[i])
	}
	return out, nil
}
func (a *StaticAdapter) Targets() []TargetView {
	raw := a.inner.Targets()
	out := make([]TargetView, len(raw))
	for i := range raw {
		out[i] = adaptTargetView(raw[i])
	}
	return out
}
func (a *StaticAdapter) Target(id string) (TargetView, error) {
	t, err := a.inner.Target(id)
	if err != nil {
		return TargetView{}, err
	}
	return adaptTargetView(t), nil
}
func (a *StaticAdapter) AddTarget(target config.Target) (TargetView, error) {
	t, err := a.inner.AddTarget(target)
	if err != nil {
		return TargetView{}, err
	}
	return adaptTargetView(t), nil
}
func (a *StaticAdapter) UpdateTarget(id string, target config.Target) (TargetView, error) {
	t, err := a.inner.UpdateTarget(id, target)
	if err != nil {
		return TargetView{}, err
	}
	return adaptTargetView(t), nil
}
func (a *StaticAdapter) DeleteTarget(id string) error { return a.inner.DeleteTarget(id) }

func adaptTargetView(t access.TargetView) TargetView {
	return TargetView{
		ID: t.ID, HostID: t.HostID, ResourceType: t.ResourceType,
		ResourceID: t.ResourceID, HostResourceID: t.HostResourceID,
		Name: t.Name, Group: t.Group, Remark: t.Remark,
		Disabled: t.Disabled, ExpiresAt: t.ExpiresAt, Status: t.Status,
		Host: t.Host, Port: t.Port, Username: t.Username,
		AuthMethods: t.AuthMethods, InsecureIgnoreHostKey: t.InsecureIgnoreHostKey,
		HostKeyFingerprint: t.HostKeyFingerprint, KnownHostsPath: t.KnownHostsPath, Static: t.Static,
	}
}

// -- db instances --

func (a *StaticAdapter) DatabaseInstances() []DatabaseInstanceView {
	raw := a.inner.DatabaseInstances()
	out := make([]DatabaseInstanceView, len(raw))
	for i := range raw {
		out[i] = adaptDatabaseInstanceView(raw[i])
	}
	return out
}
func (a *StaticAdapter) DatabaseInstance(id string) (DatabaseInstanceView, error) {
	v, err := a.inner.DatabaseInstance(id)
	if err != nil {
		return DatabaseInstanceView{}, err
	}
	return adaptDatabaseInstanceView(v), nil
}
func (a *StaticAdapter) AddDatabaseInstance(name, protocol, address, groupName, remark string) (DatabaseInstanceView, error) {
	v, err := a.inner.AddDatabaseInstance(name, protocol, address, groupName, remark)
	if err != nil {
		return DatabaseInstanceView{}, err
	}
	return adaptDatabaseInstanceView(v), nil
}
func (a *StaticAdapter) UpdateDatabaseInstance(id, name, protocol, address, groupName, remark string, disabled bool) (DatabaseInstanceView, error) {
	v, err := a.inner.UpdateDatabaseInstance(id, name, protocol, address, groupName, remark, disabled)
	if err != nil {
		return DatabaseInstanceView{}, err
	}
	return adaptDatabaseInstanceView(v), nil
}
func (a *StaticAdapter) DeleteDatabaseInstance(id string) error {
	return a.inner.DeleteDatabaseInstance(id)
}

// -- db accounts --

func (a *StaticAdapter) InstanceAccounts(instanceID string) ([]DatabaseAccountView, error) {
	raw, err := a.inner.DatabaseAccounts(instanceID)
	if err != nil {
		return nil, err
	}
	out := make([]DatabaseAccountView, len(raw))
	for i := range raw {
		out[i] = adaptDatabaseAccountView(raw[i])
	}
	return out, nil
}
func (a *StaticAdapter) DatabaseAccount(id string) (DatabaseAccountView, error) {
	v, err := a.inner.DatabaseAccount(id)
	if err != nil {
		return DatabaseAccountView{}, err
	}
	return adaptDatabaseAccountView(v), nil
}
func (a *StaticAdapter) AddDatabaseAccount(instanceID, upstreamUsername, upstreamPassword, groupName, remark string, expiresAt *time.Time) (DatabaseAccountView, error) {
	v, err := a.inner.AddDatabaseAccount(instanceID, upstreamUsername, upstreamPassword, groupName, remark, expiresAt)
	if err != nil {
		return DatabaseAccountView{}, err
	}
	return adaptDatabaseAccountView(v), nil
}
func (a *StaticAdapter) UpdateDatabaseAccount(id, upstreamUsername, upstreamPassword, groupName, remark string, expiresAt *time.Time, disabled bool) (DatabaseAccountView, error) {
	v, err := a.inner.UpdateDatabaseAccount(id, upstreamUsername, upstreamPassword, groupName, remark, expiresAt, disabled)
	if err != nil {
		return DatabaseAccountView{}, err
	}
	return adaptDatabaseAccountView(v), nil
}
func (a *StaticAdapter) DeleteDatabaseAccount(id string) error {
	return a.inner.DeleteDatabaseAccount(id)
}

func (a *StaticAdapter) DatabaseAccountByUniqueName(uniqueName string) (*model.DatabaseAccount, error) {
	return a.inner.DatabaseAccountByUniqueName(uniqueName)
}

func (a *StaticAdapter) AuthenticateDirect(ctx context.Context, username, password string) (model.User, error) {
	return a.inner.AuthenticateDirect(ctx, username, password)
}

// -- db view adapters --

func adaptDatabaseInstanceView(v access.DatabaseInstanceView) DatabaseInstanceView {
	return DatabaseInstanceView{
		ID:           v.ID,
		Name:         v.Name,
		Protocol:     v.Protocol,
		Address:      v.Address,
		GroupName:    v.GroupName,
		Remark:       v.Remark,
		Disabled:     v.Disabled,
		AccountCount: int(v.AccountCount),
		CreatedAt:    timeToStr(v.CreatedAt),
		UpdatedAt:    timeToStr(v.UpdatedAt),
	}
}

func adaptDatabaseAccountView(v access.DatabaseAccountView) DatabaseAccountView {
	return DatabaseAccountView{
		ID:               v.ID,
		InstanceID:       v.InstanceID,
		UniqueName:       v.UniqueName,
		UpstreamUsername: v.UpstreamUsername,
		GroupName:        v.GroupName,
		Remark:           v.Remark,
		ExpiresAt:        v.ExpiresAt,
		Disabled:         v.Disabled,
		ResourceID:       v.ResourceID,
		ResourceSeq:      v.ResourceSeq,
		CreatedAt:        timeToStr(v.CreatedAt),
		UpdatedAt:        timeToStr(v.UpdatedAt),
	}
}

func timeToStr(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func (a *StaticAdapter) DefaultTarget(ctx context.Context, user model.User) (TargetConfig, error) {
	t, err := a.inner.DefaultTarget(ctx, user)
	if err != nil {
		return TargetConfig{}, err
	}
	return TargetConfig{
		ID: t.ID, Name: t.Name, Host: t.Host, Port: t.Port, Username: t.Username,
		Password: t.Password, PrivateKeyPath: t.PrivateKeyPath, PrivateKeyPEM: t.PrivateKeyPEM,
		Passphrase: t.Passphrase, InsecureIgnoreHostKey: t.InsecureIgnoreHostKey,
		HostKeyFingerprint: t.HostKeyFingerprint, KnownHostsPath: t.KnownHostsPath,
		Disabled: t.Disabled, ExpiresAt: t.ExpiresAt, HostID: t.HostID,
	}, nil
}

// -- user sessions (not supported in static adapter) --

func (a *StaticAdapter) UserSessions(_ string) ([]SessionView, error) {
	return []SessionView{}, nil
}

func (a *StaticAdapter) CreateUserSession(_ model.UserSession) (*model.UserSession, error) {
	return nil, errors.New("user sessions: db-only feature")
}

func (a *StaticAdapter) DisableUserSession(_ string) error {
	return errors.New("user sessions: db-only feature")
}

func (a *StaticAdapter) EnableUserSession(_ string) error {
	return errors.New("user sessions: db-only feature")
}

func (a *StaticAdapter) UserSessionByID(_, _ string) (*model.UserSession, error) {
	return nil, errors.New("user sessions: db-only feature")
}
