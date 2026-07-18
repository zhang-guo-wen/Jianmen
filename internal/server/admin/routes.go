package admin

import (
	"net/http"

	"jianmen/internal/frontend"
	"jianmen/internal/rbac"
)

// muxHandle 注册路由并包裹 requestIDMiddleware。
func (s *Server) muxHandle(mux *http.ServeMux, pattern string, handler http.HandlerFunc) {
	mux.HandleFunc(pattern, requestIDMiddleware(handler))
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	frontendHandler, err := frontend.Handler()
	if err != nil || s.cfg.Admin.Dev {
		s.muxHandle(mux, "/", s.handleIndex)
	} else {
		mux.Handle("/", frontendHandler)
	}

	s.muxHandle(mux, "/api/init/status", s.handleInitStatus)
	s.muxHandle(mux, "/api/init/setup", s.handleInitSetup)
	s.muxHandle(mux, "/api/init/encryption-key", s.withAuthAndUser(s.handleInitEncryptionKey))
	s.muxHandle(mux, "/api/login/challenge", s.handleLoginCaptchaChallenge)
	s.muxHandle(mux, "/api/login", s.handleLogin)
	s.muxHandle(mux, "/api/logout", s.withAuthAndUser(s.handleLogout))
	s.muxHandle(mux, "/api/ai/docs", s.handleAIDocs)
	s.muxHandle(mux, "/api/ai/auth/refresh", s.handleAIRefresh)
	s.muxHandle(mux, "/api/ai/tokens", s.withAuthAndUser(s.withAnyPermission([]string{rbac.ActionRBACManage, rbac.ActionAIManage}, s.handleAITokens)))
	s.muxHandle(mux, "/api/ai/tokens/", s.withAuthAndUser(s.withAnyPermission([]string{rbac.ActionRBACManage, rbac.ActionAIManage}, s.handleAIToken)))
	s.muxHandle(mux, "/api/ai/resources", s.withAIToken(s.handleAIResources))
	s.muxHandle(mux, "/api/ai/resources/", s.withAIToken(s.handleAIResources))
	s.muxHandle(mux, "/api/health", s.withAuthAndUser(s.handleHealth))
	s.muxHandle(mux, "/api/users", s.withAuthAndUser(s.handleUsers))
	s.muxHandle(mux, "/api/users/", s.withAuthAndUser(s.handleUser))
	s.muxHandle(mux, "/api/hosts", s.withAuthAndUser(s.handleHosts))
	s.muxHandle(mux, "/api/hosts/", s.withAuthAndUser(s.handleHost))
	s.muxHandle(mux, "/api/targets", s.withAuthAndUser(s.handleTargets))
	s.muxHandle(mux, "/api/targets/test-connection", s.withAuthAndUser(s.handleTestConnection))
	s.muxHandle(mux, "/api/targets/", s.withAuthAndUser(s.handleTarget))
	s.muxHandle(mux, "/api/web-terminal/tickets", s.withAuthAndUser(s.handleWebTerminalTicket))
	s.muxHandle(mux, webTerminalPath, s.handleWebTerminal)
	s.muxHandle(mux, "/api/sessions", s.withAuthAndUser(s.handleSessions))
	s.muxHandle(mux, "/api/sessions/", s.withAuthAndUser(s.handleSessionArtifact))
	s.muxHandle(mux, "/api/online-sessions", s.withAuthAndUser(s.handleOnlineSessions))
	s.muxHandle(mux, "/api/online-sessions/", s.withAuthAndUser(s.handleOnlineSession))
	s.muxHandle(mux, "/api/user-sessions", s.withAuthAndUser(s.handleUserSessions))
	s.muxHandle(mux, "/api/connection-passwords", s.withAuthAndUser(s.handleConnectionPasswords))

	s.muxHandle(mux, "/api/db/gateway", s.withAuthAndUser(s.handleDBGateway))
	s.muxHandle(mux, "/api/db/instances", s.withAuthAndUser(s.handleDBInstances))
	s.muxHandle(mux, "/api/db/instances/", s.withAuthAndUser(s.handleDBInstance))
	s.muxHandle(mux, "/api/db/accounts/test", s.withAuthAndUser(s.handleTestDBConnection))
	s.muxHandle(mux, "/api/db/accounts/test/", s.withAuthAndUser(s.handleTestDBConnection))
	s.muxHandle(mux, "/api/db/accounts", s.withAuthAndUser(s.handleDBAccounts))
	s.muxHandle(mux, "/api/db/accounts/", s.withAuthAndUser(s.handleDBAccount))
	s.muxHandle(mux, "/api/db/connections", s.withAuthAndUser(s.handleDBConnections))
	s.muxHandle(mux, "/api/db/connections/", s.withAuthAndUser(s.handleDBConnectionArtifact))

	s.muxHandle(mux, "/api/rbac/roles", s.withAuthAndUser(s.withPermission(rbac.ActionRBACManage, s.handleRBACRoles)))
	s.muxHandle(mux, "/api/rbac/roles/", s.withAuthAndUser(s.withPermission(rbac.ActionRBACManage, s.handleRBACRole)))
	s.muxHandle(mux, "/api/rbac/catalog", s.withAuthAndUser(s.withPermission(rbac.ActionRBACManage, s.handleRBACCatalog)))
	s.muxHandle(mux, "/api/rbac/permissions", s.withAuthAndUser(s.withPermission(rbac.ActionRBACManage, s.handleRBACPermissions)))
	s.muxHandle(mux, "/api/rbac/permissions/", s.withAuthAndUser(s.withPermission(rbac.ActionRBACManage, s.handleRBACPermission)))
	s.muxHandle(mux, "/api/rbac/user-roles", s.withAuthAndUser(s.withPermission(rbac.ActionRBACManage, s.handleRBACUserRoles)))
	s.muxHandle(mux, "/api/rbac/user-roles/", s.withAuthAndUser(s.withPermission(rbac.ActionRBACManage, s.handleRBACUserRole)))
	s.muxHandle(mux, "/api/rbac/role-permissions", s.withAuthAndUser(s.withPermission(rbac.ActionRBACManage, s.handleRBACRolePermissions)))
	s.muxHandle(mux, "/api/rbac/role-permissions/", s.withAuthAndUser(s.withPermission(rbac.ActionRBACManage, s.handleRBACRolePermission)))
	s.muxHandle(mux, "/api/rbac/effective", s.withAuthAndUser(s.withPermission(rbac.ActionRBACManage, s.handleRBACEffective)))

	s.muxHandle(mux, "/api/audit/ssh", s.withAuthAndUser(s.handleAuditSSH))
	s.muxHandle(mux, "/api/audit/db", s.withAuthAndUser(s.handleAuditDB))
	s.muxHandle(mux, "/api/audit/operations", s.withAuthAndUser(s.handleAuditOperations))
	s.muxHandle(mux, "/api/audit/logins", s.withAuthAndUser(s.handleAuditLogins))
	s.muxHandle(mux, "/api/audit/", s.withAuthAndUser(s.handleAuditArtifact))
	s.muxHandle(mux, "/api/me", s.withAuthAndUser(s.handleMe))
	s.muxHandle(mux, "/api/me/access-context", s.withAuthAndUser(s.handleMeAccessContext))
	s.muxHandle(mux, "/api/me/preferences", s.withAuthAndUser(s.handleMePreferences))
	s.muxHandle(mux, "/api/me/permissions", s.withAuthAndUser(s.handleMePermissions))
	s.muxHandle(mux, "/api/me/menus", s.withAuthAndUser(s.handleMeMenus))
	s.muxHandle(mux, "/api/applications", s.withAuthAndUser(s.handleApplications))
	s.muxHandle(mux, "/api/applications/", s.withAuthAndUser(s.handleApplication))
	s.muxHandle(mux, "/api/containers/test", s.withAuthAndUser(s.handleContainerConnectionTest))
	s.muxHandle(mux, "/api/containers/endpoints", s.withAuthAndUser(s.handleContainerEndpoints))
	s.muxHandle(mux, "/api/containers/endpoints/", s.withAuthAndUser(s.handleContainerEndpoint))
	s.muxHandle(mux, "/api/platform-accounts", s.withAuthAndUser(s.handlePlatformAccounts))
	s.muxHandle(mux, "/api/platform-accounts/", s.withAuthAndUser(s.handlePlatformAccount))
	s.muxHandle(mux, "/api/user-groups", s.withAuthAndUser(s.handleUserGroups))
	s.muxHandle(mux, "/api/user-groups/", s.withAuthAndUser(s.handleUserGroupOrMembers))
	s.muxHandle(mux, "/api/resource-groups", s.withAuthAndUser(s.handleResourceGroups))
	s.muxHandle(mux, "/api/resource-groups/", s.withAuthAndUser(s.handleResourceGroups))
	s.muxHandle(mux, "/api/temporary-accounts", s.withAuthAndUser(s.handleTemporaryAccounts))
	s.muxHandle(mux, "/api/temporary-accounts/", s.withAuthAndUser(s.handleTemporaryAccount))
	s.muxHandle(mux, "/api/resource-grants", s.withAuthAndUser(s.handleResourceGrants))
	s.muxHandle(mux, "/api/resource-grants/check", s.withAuthAndUser(s.handleResourceGrantCheck))
	s.muxHandle(mux, "/api/resource-grants/", s.withAuthAndUser(s.handleResourceGrant))

	return logRequests(s.logger, withCORS(s.cfg.Admin.CORSAllowedOrigins, mux))
}
