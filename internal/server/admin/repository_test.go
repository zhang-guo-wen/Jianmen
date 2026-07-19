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

	"jianmen/internal/config"
	"jianmen/internal/handler/accessrequest"
	"jianmen/internal/handler/systemsettings"
	"jianmen/internal/handler/webrdp"
	"jianmen/internal/online"
	"jianmen/internal/rbac"
	"jianmen/internal/service"
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
	if _, err := New(&config.Config{}, repository, db, identity, browserSessions, nilAuthorization, resourceGrants, resourceGroups, repositoryTestProvisioning{}, slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir(), nil, online.NewRegistry(), &webrdp.Handler{}, &accessrequest.Handler{}, nil); err == nil {
		t.Fatal("New accepted typed-nil authorization service")
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
		&accessrequest.Handler{},
		&systemsettings.Handler{},
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
}

func TestResolveAdminDependenciesRejectsTypedNil(t *testing.T) {
	var repository *store.DBStore

	dependencies, err := resolveAdminDependencies(repository)

	if !errors.Is(err, errAdminStoreRequired) {
		t.Fatalf("resolve typed-nil repository error = %v, want %v", err, errAdminStoreRequired)
	}
	if dependencies.hostTargets != nil || dependencies.roles != nil {
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
		"aiAccessTokens":      reflect.TypeOf((*service.AIAccessTokenService)(nil)),
		"hostTargets":         reflect.TypeOf((*adminHostTargetRepository)(nil)).Elem(),
		"databases":           reflect.TypeOf((*adminDatabaseRepository)(nil)).Elem(),
		"applicationService":  reflect.TypeOf((*service.ApplicationService)(nil)),
		"containerManagement": reflect.TypeOf((*service.ContainerManagementService)(nil)),
		"platformAccounts":    reflect.TypeOf((*adminPlatformAccountRepository)(nil)).Elem(),
		"userSessionCreation": reflect.TypeOf((*service.UserSessionCreationService)(nil)),
		"audit":               reflect.TypeOf((*adminAuditRepository)(nil)).Elem(),
		"connectionPassword":  reflect.TypeOf((*service.ConnectionPasswordService)(nil)),
		"preferences":         reflect.TypeOf((*adminUserPreferenceRepository)(nil)).Elem(),
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

	file, err := parser.ParseFile(token.NewFileSet(), "repository.go", nil, 0)
	if err != nil {
		t.Fatalf("parse repository boundary: %v", err)
	}
	wantEmbedded := map[string]bool{
		"adminAIAccessTokenRepository":       true,
		"adminHostTargetRepository":          true,
		"adminDatabaseRepository":            true,
		"adminApplicationRepository":         true,
		"adminContainerRepository":           true,
		"adminPlatformAccountRepository":     true,
		"adminUserSessionCreationRepository": true,
		"adminAuditRepository":               true,
		"adminConnectionPasswordRepository":  true,
		"adminUserPreferenceRepository":      true,
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
	server.platformAccounts = dependencies.platformAccounts
	if server.userSessionCreation == nil {
		server.userSessionCreation, err = service.NewUserSessionCreationService(dependencies.userSessionCreation, repositoryTestAuthorization{})
		if err != nil {
			t.Fatalf("new user session creation service: %v", err)
		}
	}
	server.audit = dependencies.audit
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
	server.preferences = dependencies.preferences
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
}
