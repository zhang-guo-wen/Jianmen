package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"jianmen/internal/model"
)

type fakeResourceGrantRepository struct {
	grants       []model.ResourceGrant
	principals   map[string]bool
	resources    map[string]bool
	created      model.ResourceGrant
	ensured      model.ResourceGrant
	deletedID    string
	searchErr    error
	findErr      error
	createErr    error
	ensureErr    error
	deleteErr    error
	principalErr error
	resourceErr  error
	principalGrants    []model.ResourceGrant
	principalGrantsErr error
	batchCreated       int
	batchRefreshed     int
	batchErr           error
}

func (f *fakeResourceGrantRepository) SearchResourceGrants(context.Context, string) ([]model.ResourceGrant, error) {
	return append([]model.ResourceGrant(nil), f.grants...), f.searchErr
}

func (f *fakeResourceGrantRepository) FindResourceGrant(_ context.Context, id string) (model.ResourceGrant, bool, error) {
	if f.findErr != nil {
		return model.ResourceGrant{}, false, f.findErr
	}
	for _, grant := range f.grants {
		if grant.ID == id {
			return grant, true, nil
		}
	}
	return model.ResourceGrant{}, false, nil
}

func (f *fakeResourceGrantRepository) CreateResourceGrant(_ context.Context, grant model.ResourceGrant) (model.ResourceGrant, error) {
	if f.createErr != nil {
		return model.ResourceGrant{}, f.createErr
	}
	if grant.ID == "" {
		grant.ID = "generated"
	}
	f.created = grant
	return grant, nil
}

func (f *fakeResourceGrantRepository) EnsureResourceGrant(_ context.Context, grant model.ResourceGrant) error {
	if f.ensureErr != nil {
		return f.ensureErr
	}
	f.ensured = grant
	return nil
}

func (f *fakeResourceGrantRepository) DeleteResourceGrant(_ context.Context, id string) error {
	f.deletedID = id
	return f.deleteErr
}

func (f *fakeResourceGrantRepository) ResourceGrantPrincipalExists(_ context.Context, principalType, principalID string) (bool, error) {
	return f.principals[principalType+":"+principalID], f.principalErr
}

func (f *fakeResourceGrantRepository) ResourceGrantResourceExists(_ context.Context, resourceType, resourceID string) (bool, error) {
	return f.resources[resourceType+":"+resourceID], f.resourceErr
}

func (f *fakeResourceGrantRepository) FindGrantsByPrincipal(_ context.Context, principalType, principalID string) ([]model.ResourceGrant, error) {
	return append([]model.ResourceGrant(nil), f.principalGrants...), f.principalGrantsErr
}

func (f *fakeResourceGrantRepository) BatchUpsertGrants(_ context.Context, grants []model.ResourceGrant, actorID string) (int, int, error) {
	return f.batchCreated, f.batchRefreshed, f.batchErr
}

type fakeResourceGrantChecker struct {
	allowed map[string]bool
	err     error
	calls   []string
}

func (f *fakeResourceGrantChecker) HasGrant(userID, resourceType, resourceID string) (bool, error) {
	key := userID + ":" + resourceType + ":" + resourceID
	f.calls = append(f.calls, key)
	return f.allowed[key], f.err
}

func TestResourceGrantServiceCreateNormalizesAndAuthorizes(t *testing.T) {
	repository := &fakeResourceGrantRepository{
		principals: map[string]bool{"user:u2": true},
		resources:  map[string]bool{model.ResourceTypeContainerEndpoint + ":container-1": true},
	}
	checker := &fakeResourceGrantChecker{allowed: map[string]bool{
		"u1:" + model.ResourceTypeContainerEndpoint + ":container-1": true,
	}}
	service, err := NewResourceGrantService(repository, checker)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	created, err := service.Create(context.Background(), "u1", false, model.ResourceGrant{
		PrincipalType: " USER ", PrincipalID: " u2 ",
		ResourceType: " CONTAINER_ENDPOINT ", ResourceID: " container-1 ",
	})
	if err != nil {
		t.Fatalf("create grant: %v", err)
	}
	if created.ID != "generated" || created.Effect != model.PermissionEffectAllow {
		t.Fatalf("created grant = %#v", created)
	}
	if created.PrincipalType != "user" || created.PrincipalID != "u2" || created.ResourceType != model.ResourceTypeContainerEndpoint {
		t.Fatalf("grant was not normalized: %#v", created)
	}
}

func TestResourceGrantServiceCreateRejectsMissingReferencesBeforePersistence(t *testing.T) {
	repository := &fakeResourceGrantRepository{principals: map[string]bool{}, resources: map[string]bool{}}
	service, err := NewResourceGrantService(repository, &fakeResourceGrantChecker{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = service.Create(context.Background(), "admin", true, model.ResourceGrant{
		PrincipalType: "user", PrincipalID: "missing",
		ResourceType: model.ResourceTypeHost, ResourceID: "host-1",
	})
	if !errors.Is(err, ErrInvalidResourceGrant) {
		t.Fatalf("create error = %v, want invalid grant", err)
	}
	if repository.created.ID != "" {
		t.Fatalf("unexpected persisted grant: %#v", repository.created)
	}
}

func TestResourceGrantServiceGrantCreatedResourceEnsuresCreatorGrant(t *testing.T) {
	repository := &fakeResourceGrantRepository{
		principals: map[string]bool{"user:u1": true},
		resources:  map[string]bool{model.ResourceTypeHost + ":host-1": true},
	}
	service, err := NewResourceGrantService(repository, &fakeResourceGrantChecker{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if err := service.GrantCreatedResource(context.Background(), " u1 ", false, " HOST ", " host-1 "); err != nil {
		t.Fatalf("grant created resource: %v", err)
	}
	want := model.ResourceGrant{
		PrincipalType: "user",
		PrincipalID:   "u1",
		ResourceType:  model.ResourceTypeHost,
		ResourceID:    "host-1",
		Effect:        model.PermissionEffectAllow,
		CreatedBy:     "u1",
	}
	if !reflect.DeepEqual(repository.ensured, want) {
		t.Fatalf("ensured grant = %#v, want %#v", repository.ensured, want)
	}
}

func TestResourceGrantServiceGrantCreatedResourceRejectsMissingActor(t *testing.T) {
	repository := &fakeResourceGrantRepository{}
	service, err := NewResourceGrantService(repository, &fakeResourceGrantChecker{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	err = service.GrantCreatedResource(context.Background(), "", false, model.ResourceTypeHost, "host-1")
	if !errors.Is(err, ErrInvalidResourceGrant) {
		t.Fatalf("grant error = %v, want invalid grant", err)
	}
	if repository.ensured.ResourceID != "" {
		t.Fatalf("unexpected persisted grant: %#v", repository.ensured)
	}
}

func TestResourceGrantServiceGrantCreatedResourceBypassesSuperAdministrator(t *testing.T) {
	repository := &fakeResourceGrantRepository{ensureErr: errors.New("must not be called")}
	service, err := NewResourceGrantService(repository, &fakeResourceGrantChecker{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	if err := service.GrantCreatedResource(context.Background(), "", true, "", ""); err != nil {
		t.Fatalf("super administrator bypass: %v", err)
	}
}

func TestResourceGrantServiceListFiltersAndPaginatesVisibleResources(t *testing.T) {
	repository := &fakeResourceGrantRepository{grants: []model.ResourceGrant{
		{ID: "g1", ResourceType: model.ResourceTypeHost, ResourceID: "h1"},
		{ID: "g2", ResourceType: model.ResourceTypeHost, ResourceID: "h2"},
		{ID: "g3", ResourceType: model.ResourceTypeHost, ResourceID: "h1"},
	}}
	checker := &fakeResourceGrantChecker{allowed: map[string]bool{
		"u1:" + model.ResourceTypeHost + ":h1": true,
	}}
	service, err := NewResourceGrantService(repository, checker)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	page, err := service.List(context.Background(), "u1", false, "", 1, 1)
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != "g1" {
		t.Fatalf("page = %#v", page)
	}
	wantCalls := []string{"u1:host:h1", "u1:host:h2"}
	if !reflect.DeepEqual(checker.calls, wantCalls) {
		t.Fatalf("checker calls = %#v, want %#v", checker.calls, wantCalls)
	}
}

func TestResourceGrantServiceDeleteRequiresResourceAccess(t *testing.T) {
	repository := &fakeResourceGrantRepository{grants: []model.ResourceGrant{{
		ID: "g1", ResourceType: model.ResourceTypeHost, ResourceID: "h1",
	}}}
	service, err := NewResourceGrantService(repository, &fakeResourceGrantChecker{allowed: map[string]bool{}})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	err = service.Delete(context.Background(), "u1", false, "g1")
	if !errors.Is(err, ErrResourceGrantForbidden) {
		t.Fatalf("delete error = %v, want forbidden", err)
	}
	if repository.deletedID != "" {
		t.Fatalf("deleted id = %q", repository.deletedID)
	}
}

func TestResourceGrantServiceBatchCreateAllNew(t *testing.T) {
	repository := &fakeResourceGrantRepository{
		principals:     map[string]bool{"user:u1": true},
		resources:      map[string]bool{model.ResourceTypeHost + ":h1": true, model.ResourceTypeHost + ":h2": true},
		batchCreated:   2,
		batchRefreshed: 0,
	}
	service, err := NewResourceGrantService(repository, &fakeResourceGrantChecker{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := service.BatchCreate(context.Background(), "admin", true, []model.ResourceGrant{
		{PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeHost, ResourceID: "h1", Effect: model.PermissionEffectAllow},
		{PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeHost, ResourceID: "h2", Effect: model.PermissionEffectAllow},
	})
	if err != nil {
		t.Fatalf("batch create: %v", err)
	}
	if result.Created != 2 || result.Refreshed != 0 {
		t.Fatalf("result = %#v, want created=2 refreshed=0", result)
	}
}

func TestResourceGrantServiceBatchCreatePartialRefresh(t *testing.T) {
	repository := &fakeResourceGrantRepository{
		principals:     map[string]bool{"user:u1": true},
		resources:      map[string]bool{model.ResourceTypeHost + ":h1": true, model.ResourceTypeHost + ":h2": true},
		batchCreated:   1,
		batchRefreshed: 1,
	}
	service, err := NewResourceGrantService(repository, &fakeResourceGrantChecker{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := service.BatchCreate(context.Background(), "admin", true, []model.ResourceGrant{
		{PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeHost, ResourceID: "h1", Effect: model.PermissionEffectAllow},
		{PrincipalType: "user", PrincipalID: "u1", ResourceType: model.ResourceTypeHost, ResourceID: "h2", Effect: model.PermissionEffectAllow},
	})
	if err != nil {
		t.Fatalf("batch create: %v", err)
	}
	if result.Created != 1 || result.Refreshed != 1 {
		t.Fatalf("result = %#v, want created=1 refreshed=1", result)
	}
}

func TestResourceGrantServiceBatchCreateRejectsMissingPrincipal(t *testing.T) {
	repository := &fakeResourceGrantRepository{
		principals: map[string]bool{},
		resources:  map[string]bool{model.ResourceTypeHost + ":h1": true},
	}
	service, err := NewResourceGrantService(repository, &fakeResourceGrantChecker{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	_, err = service.BatchCreate(context.Background(), "admin", true, []model.ResourceGrant{
		{PrincipalType: "user", PrincipalID: "missing", ResourceType: model.ResourceTypeHost, ResourceID: "h1", Effect: model.PermissionEffectAllow},
	})
	if !errors.Is(err, ErrInvalidResourceGrant) {
		t.Fatalf("batch create error = %v, want invalid grant", err)
	}
}

func TestResourceGrantServiceBatchCreateEmptyGrants(t *testing.T) {
	repository := &fakeResourceGrantRepository{}
	service, err := NewResourceGrantService(repository, &fakeResourceGrantChecker{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	result, err := service.BatchCreate(context.Background(), "admin", true, nil)
	if err != nil {
		t.Fatalf("batch create empty: %v", err)
	}
	if result.Created != 0 || result.Refreshed != 0 {
		t.Fatalf("result = %#v, want zeros", result)
	}
}

func TestResourceGrantServiceCheckRejectsUnknownResourceType(t *testing.T) {
	service, err := NewResourceGrantService(
		&fakeResourceGrantRepository{},
		&fakeResourceGrantChecker{},
	)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	_, err = service.Check(context.Background(), "u1", "unknown", "resource-1")
	if !errors.Is(err, ErrInvalidResourceGrant) {
		t.Fatalf("check error = %v, want invalid grant", err)
	}
}
