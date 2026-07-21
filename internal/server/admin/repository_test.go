package admin

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"jianmen/internal/config"
	"jianmen/internal/handler/sqlconsole"
	"jianmen/internal/handler/systemsettings"
	"jianmen/internal/handler/webrdp"
	"jianmen/internal/online"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
	"jianmen/internal/sshhost"
	"jianmen/internal/storage"
	"jianmen/internal/store"
)

var _ adminRepository = (*store.DBStore)(nil)

type repositoryTestAuthorization struct{}

type typedNilAuthorization struct{}

func (*typedNilAuthorization) AuthorizeConnection(context.Context, string, []string, string, string) (bool, error) {
	return false, nil
}
func (*typedNilAuthorization) AuthorizeBatch(context.Context, string, []service.AuthorizationRequest) ([]service.AuthorizationDecision, error) {
	return nil, nil
}

func (repositoryTestAuthorization) AuthorizeConnection(
	context.Context,
	string,
	[]string,
	string,
	string,
) (bool, error) {
	return false, nil
}

func (repositoryTestAuthorization) AuthorizeBatch(context.Context, string, []service.AuthorizationRequest) ([]service.AuthorizationDecision, error) {
	return nil, nil
}

type repositoryTestProvisioning struct{}

func (repositoryTestProvisioning) ListDatabases(
	context.Context,
	service.ListProvisioningDatabasesRequest,
) ([]string, error) {
	return nil, nil
}

func (repositoryTestProvisioning) Provision(
	context.Context,
	service.ProvisionDatabaseAccountRequest,
) (service.ProvisionDatabaseAccountResult, error) {
	return service.ProvisionDatabaseAccountResult{}, nil
}

func (repositoryTestProvisioning) Deprovision(context.Context, string) error {
	return nil
}

func TestNewAcceptsCompleteDBStoreRepository(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	repository := store.NewDBStore(db)
	identity, err := service.NewIdentityService(repository)
	if err != nil {
		t.Fatalf("new identity service: %v", err)
	}
	browserSessions, err := service.NewBrowserSessionService(repository)
	if err != nil {
		t.Fatalf("new browser session service: %v", err)
	}
	resourceGrants, err := service.NewResourceGrantService(repository, rbac.NewResourceGrantChecker(db))
	if err != nil {
		t.Fatalf("new resource grant service: %v", err)
	}
	resourceGroups, err := service.NewResourceGroupService(repository)
	if err != nil {
		t.Fatalf("new resource group service: %v", err)
	}
	var nilAuthorization *typedNilAuthorization
	if _, err := New(&config.Config{}, repository, db, identity, browserSessions, nilAuthorization, resourceGrants, resourceGroups, repositoryTestProvisioning{}, slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir(), nil, online.NewRegistry(), &webrdp.Handler{}, nil, nil); err == nil {
		t.Fatal("New accepted typed-nil authorization service")
	}
	sqlConsoleService, err := service.NewSQLConsoleService(
		repository,
		repositoryTestAuthorization{},
		service.NewDatabaseSQLConsoleExecutor(),
	)
	if err != nil {
		t.Fatalf("new SQL console service: %v", err)
	}
	sqlConsoleHandler, err := sqlconsole.New(sqlConsoleService)
	if err != nil {
		t.Fatalf("new SQL console handler: %v", err)
	}

	server, err := New(
		&config.Config{},
		repository,
		db,
		identity,
		browserSessions,
		repositoryTestAuthorization{},
		resourceGrants,
		resourceGroups,
		repositoryTestProvisioning{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		t.TempDir(),
		nil,
		online.NewRegistry(),
		&webrdp.Handler{},
		&systemsettings.Handler{},
		sqlConsoleHandler,
	)
	if err != nil {
		t.Fatalf("new admin server: %v", err)
	}
	if server.hostTargets != repository ||
		server.databases != repository ||
		server.audit != repository ||
		server.roleRepository != repository {
		t.Fatal("admin server did not retain DBStore through its resource-scoped boundaries")
	}
	if server.aiResources == nil {
		t.Fatal("admin server did not construct the AI resource service")
	}
}

func TestResolveAdminDependenciesRejectsTypedNil(t *testing.T) {
	var repository *store.DBStore

	dependencies, err := resolveAdminDependencies(repository)

	if !errors.Is(err, errAdminStoreRequired) {
		t.Fatalf("resolve typed-nil repository error = %v, want %v", err, errAdminStoreRequired)
	}
	if dependencies.aiResources != nil || dependencies.hostTargets != nil || dependencies.roles != nil {
		t.Fatal("typed-nil repository returned non-empty dependencies")
	}
}

func TestAdminRepositoryBoundaryStaysStaticallyComposedAndDomainSplit(t *testing.T) {
	repositoryType := reflect.TypeOf((*adminRepository)(nil)).Elem()
	if got := reflect.TypeOf(New).In(1); got != repositoryType {
		t.Fatalf("New repository parameter = %v, want %v", got, repositoryType)
	}

	serverType := reflect.TypeOf(Server{})
	if _, found := serverType.FieldByName("store"); found {
		t.Fatal("Server regained an application-wide store field")
	}
	expectedFields := map[string]reflect.Type{
		"aiAccessTokens":         reflect.TypeOf((*service.AIAccessTokenService)(nil)),
		"aiResources":            reflect.TypeOf((*service.AIResourceService)(nil)),
		"hostTargets":            reflect.TypeOf((*adminHostTargetRepository)(nil)).Elem(),
		"hostManagement":         reflect.TypeOf((*service.HostManagementService)(nil)),
		"databases":              reflect.TypeOf((*adminDatabaseRepository)(nil)).Elem(),
		"databaseManagement":     reflect.TypeOf((*service.DatabaseManagementService)(nil)),
		"databaseTLSPreflight":   reflect.TypeOf((*service.DatabaseTLSPreflightService)(nil)),
		"applicationService":     reflect.TypeOf((*service.ApplicationService)(nil)),
		"containerManagement":    reflect.TypeOf((*service.ContainerManagementService)(nil)),
		"platformAccountService": reflect.TypeOf((*service.PlatformAccountService)(nil)),
		"userSessionCreation":    reflect.TypeOf((*service.UserSessionCreationService)(nil)),
		"audit":                  reflect.TypeOf((*adminAuditRepository)(nil)).Elem(),
		"connectionPassword":     reflect.TypeOf((*service.ConnectionPasswordService)(nil)),
		"preferences":            reflect.TypeOf((*service.UserPreferenceService)(nil)),
	}
	for name, want := range expectedFields {
		field, found := serverType.FieldByName(name)
		if !found {
			t.Fatalf("Server resource field %q is missing", name)
		}
		if field.Type != want {
			t.Fatalf("Server field %q type = %v, want %v", name, field.Type, want)
		}
	}
	dependenciesType := reflect.TypeOf(adminDependencies{})
	aiResources, found := dependenciesType.FieldByName("aiResources")
	if !found || aiResources.Type != reflect.TypeOf((*service.AIResourceRepository)(nil)).Elem() {
		t.Fatalf("admin AI resource repository boundary = %v, found %t", aiResources.Type, found)
	}
	adapterType := reflect.TypeOf(aiResourceRepositoryAdapter{})
	adapterFields := map[string]reflect.Type{
		"hostTargets": reflect.TypeOf((*adminHostTargetRepository)(nil)).Elem(),
		"databases":   reflect.TypeOf((*adminDatabaseRepository)(nil)).Elem(),
	}
	for name, want := range adapterFields {
		field, found := adapterType.FieldByName(name)
		if !found || field.Type != want {
			t.Fatalf("AI resource adapter field %q = %v, found %t, want %v", name, field.Type, found, want)
		}
	}
	if _, found := adapterType.FieldByName("repository"); found {
		t.Fatal("AI resource adapter regained an application-wide repository field")
	}

	file, err := parser.ParseFile(token.NewFileSet(), "repository.go", nil, 0)
	if err != nil {
		t.Fatalf("parse repository boundary: %v", err)
	}
	wantEmbedded := map[string]bool{
		"service.AdminAuthRepository":        true,
		"adminAIAccessTokenRepository":       true,
		"adminHostTargetRepository":          true,
		"adminDatabaseRepository":            true,
		"adminApplicationRepository":         true,
		"adminContainerRepository":           true,
		"service.PlatformAccountRepository":  true,
		"adminUserSessionCreationRepository": true,
		"adminAuditRepository":               true,
		"adminConnectionPasswordRepository":  true,
		"service.UserPreferenceRepository":   true,
		"resourceAccessRepository":           true,
		"service.TemporaryAccessRepository":  true,
		"service.UserRepository":             true,
		"service.UserGroupRepository":        true,
		"service.RoleManagementRepository":   true,
	}
	gotEmbedded := adminRepositoryEmbeddings(t, file)
	if !reflect.DeepEqual(gotEmbedded, wantEmbedded) {
		t.Fatalf("adminRepository embeddings = %#v, want %#v", gotEmbedded, wantEmbedded)
	}
}

func TestAIResourceHandlerUsesServiceBoundary(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "ai_resource_handlers.go", nil, 0)
	if err != nil {
		t.Fatalf("parse AI resource handlers: %v", err)
	}
	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if selector.Sel.Name == "hostTargets" || selector.Sel.Name == "databases" {
			t.Errorf("AI resource handler directly accesses Server.%s", selector.Sel.Name)
		}
		return true
	})
}

func adminRepositoryEmbeddings(t *testing.T, file *ast.File) map[string]bool {
	t.Helper()
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, specification := range general.Specs {
			typeSpec, ok := specification.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != "adminRepository" {
				continue
			}
			boundary, ok := typeSpec.Type.(*ast.InterfaceType)
			if !ok {
				t.Fatal("adminRepository is not an interface")
			}
			embeddings := make(map[string]bool, len(boundary.Methods.List))
			for _, field := range boundary.Methods.List {
				if len(field.Names) != 0 {
					t.Fatal("adminRepository declares methods instead of composing resource interfaces")
				}
				switch embedded := field.Type.(type) {
				case *ast.Ident:
					embeddings[embedded.Name] = true
				case *ast.SelectorExpr:
					pkg, ok := embedded.X.(*ast.Ident)
					if !ok {
						t.Fatalf("unexpected embedded interface expression %T", embedded.X)
					}
					embeddings[pkg.Name+"."+embedded.Sel.Name] = true
				default:
					t.Fatalf("unexpected embedded interface type %T", field.Type)
				}
			}
			return embeddings
		}
	}
	t.Fatal("adminRepository interface is missing")
	return nil
}

func applyTestAdminDependencies(t *testing.T, server *Server, repository adminRepository) {
	t.Helper()
	dependencies, err := resolveAdminDependencies(repository)
	if err != nil {
		t.Fatalf("resolve admin dependencies: %v", err)
	}
	server.hostTargets = dependencies.hostTargets
	if server.hostManagement == nil {
		authorization := server.authorization
		if isNilAdminAuthorization(authorization) {
			authorization = repositoryTestAuthorization{}
		}
		server.hostManagement, err = service.NewHostManagementService(
			hostManagementRepositoryAdapter{repository: dependencies.hostTargets},
			authorization,
			hostIdentityCollectorAdapter{collector: sshhost.NewCollector(500 * time.Millisecond)},
		)
		if err != nil {
			t.Fatalf("new host management service: %v", err)
		}
	}
	server.databases = dependencies.databases
	if server.applicationService == nil {
		authorization := server.authorization
		if isNilAdminAuthorization(authorization) {
			authorization = repositoryTestAuthorization{}
		}
		server.applicationService, err = service.NewApplicationService(
			dependencies.applications,
			authorization,
			nil,
			47110,
			47199,
		)
		if err != nil {
			t.Fatalf("new application service: %v", err)
		}
	}
	if server.containerManagement == nil {
		authorization := server.authorization
		if isNilAdminAuthorization(authorization) {
			authorization = repositoryTestAuthorization{}
		}
		server.containerManagement, err = service.NewContainerManagementService(
			dependencies.containers,
			authorization,
			service.NewContainerService(),
		)
		if err != nil {
			t.Fatalf("new container management service: %v", err)
		}
	}
	if server.platformAccountService == nil {
		authorization := server.authorization
		if isNilAdminAuthorization(authorization) {
			authorization = repositoryTestAuthorization{}
		}
		server.platformAccountService, err = service.NewPlatformAccountService(dependencies.platformAccounts, authorization)
		if err != nil {
			t.Fatalf("new platform account service: %v", err)
		}
	}
	if server.userSessionCreation == nil {
		server.userSessionCreation, err = service.NewUserSessionCreationService(dependencies.userSessionCreation, repositoryTestAuthorization{})
		if err != nil {
			t.Fatalf("new user session creation service: %v", err)
		}
	}
	if server.aiResources == nil {
		authorization := server.authorization
		if isNilAdminAuthorization(authorization) {
			authorization = repositoryTestAuthorization{}
		}
		server.aiResources, err = service.NewAIResourceService(
			dependencies.aiResources,
			aiResourceAuthorizerAdapter{authorization: authorization},
			aiResourceSessionCreatorAdapter{sessions: server.userSessionCreation},
		)
		if err != nil {
			t.Fatalf("new AI resource service: %v", err)
		}
	}
	server.audit = dependencies.audit
	if server.auditQuery == nil {
		authorization := server.authorization
		if isNilAdminAuthorization(authorization) {
			authorization = repositoryTestAuthorization{}
		}
		server.auditQuery, err = service.NewAuditQueryService(
			adminAuditQueryRepository{repository: dependencies.audit},
			adminAuditQueryAuthorizer{authorization: authorization},
		)
		if err != nil {
			t.Fatalf("new audit query service: %v", err)
		}
	}
	if server.connectionPassword == nil {
		authorization := server.authorization
		if isNilAdminAuthorization(authorization) {
			authorization = repositoryTestAuthorization{}
		}
		server.connectionPassword, err = service.NewConnectionPasswordService(
			dependencies.connectionPassword,
			authorization,
		)
		if err != nil {
			t.Fatalf("new connection password service: %v", err)
		}
	}
	if server.resourceAccess == nil {
		server.resourceAccess = dependencies.resourceAccess
	}
}

func applyTestAdminServices(t *testing.T, server *Server, repository adminRepository) {
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
	if server.aiAccessTokens == nil {
		server.aiAccessTokens, err = service.NewAIAccessTokenService(dependencies.aiTokens)
		if err != nil {
			t.Fatalf("new AI access token service: %v", err)
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
	if server.preferences == nil {
		server.preferences, err = service.NewUserPreferenceService(dependencies.userPreferences)
		if err != nil {
			t.Fatalf("new user preference service: %v", err)
		}
	}
}
