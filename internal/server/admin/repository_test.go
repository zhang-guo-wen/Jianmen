package admin

import (
	"testing"

	"jianmen/internal/service"
)

func applyTestAdminDependencies(t *testing.T, server *Server, repository any) {
	t.Helper()
	dependencies, err := resolveAdminDependencies(repository)
	if err != nil {
		t.Fatalf("resolve admin dependencies: %v", err)
	}
	server.aiTokens = dependencies.aiTokens
	server.hostTargets = dependencies.hostTargets
	server.databases = dependencies.databases
	server.applications = dependencies.applications
	server.containers = dependencies.containers
	server.platformAccounts = dependencies.platformAccounts
	server.userSessions = dependencies.userSessions
	server.audit = dependencies.audit
	server.connectionPassword = dependencies.connectionPassword
	server.preferences = dependencies.preferences
	if server.resourceAccess == nil {
		server.resourceAccess = dependencies.resourceAccess
	}
}

func applyTestAdminServices(t *testing.T, server *Server, repository any) {
	t.Helper()
	dependencies, err := resolveAdminDependencies(repository)
	if err != nil {
		t.Fatalf("resolve admin dependencies: %v", err)
	}
	if server.temporaryAccess == nil {
		server.temporaryAccess, err = service.NewTemporaryAccessService(dependencies.temporaryAccess)
		if err != nil {
			t.Fatalf("new temporary access service: %v", err)
		}
	}
	if server.userManagement == nil {
		server.userManagement, err = service.NewUserService(dependencies.users)
		if err != nil {
			t.Fatalf("new user service: %v", err)
		}
	}
	if server.userGroups == nil {
		server.userGroups, err = service.NewUserGroupService(dependencies.userGroups)
		if err != nil {
			t.Fatalf("new user group service: %v", err)
		}
	}
	if server.roleManagement == nil {
		server.roleManagement, err = newRoleManagementService(dependencies.roles)
		if err != nil {
			t.Fatalf("new role service: %v", err)
		}
	}
}
