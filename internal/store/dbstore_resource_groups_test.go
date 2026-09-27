package store

import (
	"context"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

func TestDBStoreResourceGroupLifecycleMaintainsContainersAndGrants(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	group := model.ResourceGroup{
		ID: "group-prod", Name: "prod", GroupType: model.ResourceGroupTypeResource,
	}
	container := model.ContainerEndpoint{
		ID: "container-1", Name: "docker", GroupName: group.Name,
		Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionDockerAPI,
		Address: "tcp://127.0.0.1:2375", Status: "active",
	}
	grant := model.ResourceGrant{
		ID: "grant-group", PrincipalType: "user", PrincipalID: "u1",
		ResourceType: model.ResourceTypeGroup, ResourceID: group.ID,
		Effect: model.PermissionEffectAllow,
	}
	for _, item := range []any{&group, &container, &grant} {
		if err := db.Create(item).Error; err != nil {
			t.Fatalf("create %T: %v", item, err)
		}
	}
	store := NewDBStore(db)

	usage, err := store.ResourceGroupUsage(context.Background(), group.GroupType, group.Name)
	if err != nil {
		t.Fatalf("resource group usage: %v", err)
	}
	if usage["container"] != 1 {
		t.Fatalf("container count = %d, want 1", usage["container"])
	}

	oldName := group.Name
	group.Name = "production"
	if _, err := store.UpdateResourceGroup(context.Background(), group, oldName); err != nil {
		t.Fatalf("update resource group: %v", err)
	}
	var updatedContainer model.ContainerEndpoint
	if err := db.First(&updatedContainer, "id = ?", container.ID).Error; err != nil {
		t.Fatalf("find updated container: %v", err)
	}
	if updatedContainer.GroupName != group.Name {
		t.Fatalf("container group = %q, want %q", updatedContainer.GroupName, group.Name)
	}

	if err := store.DeleteResourceGroup(context.Background(), group); err != nil {
		t.Fatalf("delete resource group: %v", err)
	}
	if err := db.First(&updatedContainer, "id = ?", container.ID).Error; err != nil {
		t.Fatalf("find container after group deletion: %v", err)
	}
	if updatedContainer.GroupName != "" {
		t.Fatalf("container group after deletion = %q, want empty", updatedContainer.GroupName)
	}
	var grantCount int64
	if err := db.Model(&model.ResourceGrant{}).Scopes(ActiveScope).Where("id = ?", grant.ID).Count(&grantCount).Error; err != nil {
		t.Fatalf("count group grants: %v", err)
	}
	if grantCount != 0 {
		t.Fatalf("dangling group grant count = %d, want 0", grantCount)
	}
}
