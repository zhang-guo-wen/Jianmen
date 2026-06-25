package store

import (
	"context"

	"golang.org/x/crypto/ssh"

	"jianmen/internal/access"
	"jianmen/internal/config"
	"jianmen/internal/model"
)

// StaticAdapter wraps access.StaticStore to implement Store.
type StaticAdapter struct {
	inner *access.StaticStore
}

func NewStaticAdapter(cfg *config.Config) (*StaticAdapter, error) {
	inner, err := access.NewStaticStore(cfg)
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

// -- db proxies --

func (a *StaticAdapter) DatabaseProxies() []DatabaseProxyView {
	raw := a.inner.DatabaseProxies()
	out := make([]DatabaseProxyView, len(raw))
	for i := range raw {
		out[i] = DatabaseProxyView{
			Name: raw[i].Name, Enabled: raw[i].Enabled, Protocol: raw[i].Protocol,
			ListenAddr: raw[i].ListenAddr, UpstreamAddr: raw[i].UpstreamAddr,
			Remark: raw[i].Remark, AccountCount: raw[i].AccountCount,
			AllowedUsersEnforced: raw[i].AllowedUsersEnforced,
			AllowedUsers: raw[i].AllowedUsers, QueryPolicy: raw[i].QueryPolicy, Static: raw[i].Static,
		}
	}
	return out
}
func (a *StaticAdapter) DatabaseProxyConfigs() []config.DatabaseProxyConfig {
	return a.inner.DatabaseProxyConfigs()
}
func (a *StaticAdapter) DatabaseProxy(name string) (DatabaseProxyView, error) {
	p, err := a.inner.DatabaseProxy(name)
	if err != nil {
		return DatabaseProxyView{}, err
	}
	return DatabaseProxyView{
		Name: p.Name, Enabled: p.Enabled, Protocol: p.Protocol,
		ListenAddr: p.ListenAddr, UpstreamAddr: p.UpstreamAddr,
		Remark: p.Remark, AccountCount: p.AccountCount,
		AllowedUsersEnforced: p.AllowedUsersEnforced,
		AllowedUsers: p.AllowedUsers, QueryPolicy: p.QueryPolicy, Static: p.Static,
	}, nil
}
func (a *StaticAdapter) AddDatabaseProxy(proxy config.DatabaseProxyConfig) (DatabaseProxyView, error) {
	p, err := a.inner.AddDatabaseProxy(proxy)
	if err != nil {
		return DatabaseProxyView{}, err
	}
	return DatabaseProxyView{
		Name: p.Name, Enabled: p.Enabled, Protocol: p.Protocol,
		ListenAddr: p.ListenAddr, UpstreamAddr: p.UpstreamAddr,
		Remark: p.Remark, AccountCount: p.AccountCount,
		AllowedUsersEnforced: p.AllowedUsersEnforced,
		AllowedUsers: p.AllowedUsers, QueryPolicy: p.QueryPolicy, Static: p.Static,
	}, nil
}
func (a *StaticAdapter) UpdateDatabaseProxy(name string, proxy config.DatabaseProxyConfig) (DatabaseProxyView, error) {
	p, err := a.inner.UpdateDatabaseProxy(name, proxy)
	if err != nil {
		return DatabaseProxyView{}, err
	}
	return DatabaseProxyView{
		Name: p.Name, Enabled: p.Enabled, Protocol: p.Protocol,
		ListenAddr: p.ListenAddr, UpstreamAddr: p.UpstreamAddr,
		Remark: p.Remark, AccountCount: p.AccountCount,
		AllowedUsersEnforced: p.AllowedUsersEnforced,
		AllowedUsers: p.AllowedUsers, QueryPolicy: p.QueryPolicy, Static: p.Static,
	}, nil
}
func (a *StaticAdapter) DeleteDatabaseProxy(name string) error { return a.inner.DeleteDatabaseProxy(name) }

func (a *StaticAdapter) DatabaseAccounts(proxyName string) ([]DatabaseAccountView, error) {
	raw, err := a.inner.DatabaseAccounts(proxyName)
	if err != nil {
		return nil, err
	}
	out := make([]DatabaseAccountView, len(raw))
	for i := range raw {
		out[i] = DatabaseAccountView{
			Username: raw[i].Username, Database: raw[i].Database, Remark: raw[i].Remark,
			Disabled: raw[i].Disabled, ResourceType: raw[i].ResourceType, ResourceID: raw[i].ResourceID, Static: raw[i].Static,
		}
	}
	return out, nil
}
func (a *StaticAdapter) AddDatabaseAccount(proxyName string, account config.DatabaseAccountConfig) (DatabaseAccountView, error) {
	v, err := a.inner.AddDatabaseAccount(proxyName, account)
	if err != nil {
		return DatabaseAccountView{}, err
	}
	return DatabaseAccountView{
		Username: v.Username, Database: v.Database, Remark: v.Remark,
		Disabled: v.Disabled, ResourceType: v.ResourceType, ResourceID: v.ResourceID, Static: v.Static,
	}, nil
}
func (a *StaticAdapter) UpdateDatabaseAccount(proxyName, username string, account config.DatabaseAccountConfig) (DatabaseAccountView, error) {
	v, err := a.inner.UpdateDatabaseAccount(proxyName, username, account)
	if err != nil {
		return DatabaseAccountView{}, err
	}
	return DatabaseAccountView{
		Username: v.Username, Database: v.Database, Remark: v.Remark,
		Disabled: v.Disabled, ResourceType: v.ResourceType, ResourceID: v.ResourceID, Static: v.Static,
	}, nil
}
func (a *StaticAdapter) DeleteDatabaseAccount(proxyName, username string) error {
	return a.inner.DeleteDatabaseAccount(proxyName, username)
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
