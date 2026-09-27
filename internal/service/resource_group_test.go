package service

import (
	"context"
	"errors"
	"testing"

	"jianmen/internal/model"
)

type fakeResourceGroupRepository struct {
	groups     []model.ResourceGroup
	nameExists bool
	usage      map[string]int64
	created    model.ResourceGroup
	updated    model.ResourceGroup
	oldName    string
	deleted    model.ResourceGroup
}

func (f *fakeResourceGroupRepository) SearchResourceGroups(context.Context, string, string, int, int) ([]model.ResourceGroup, int64, error) {
	return append([]model.ResourceGroup(nil), f.groups...), int64(len(f.groups)), nil
}

func (f *fakeResourceGroupRepository) FindResourceGroup(_ context.Context, id string) (model.ResourceGroup, bool, error) {
	for _, group := range f.groups {
		if group.ID == id {
			return group, true, nil
		}
	}
	return model.ResourceGroup{}, false, nil
}

func (f *fakeResourceGroupRepository) ResourceGroupNameExists(context.Context, string, string, string) (bool, error) {
	return f.nameExists, nil
}

func (f *fakeResourceGroupRepository) ResourceGroupUsage(context.Context, string, string) (map[string]int64, error) {
	return f.usage, nil
}

func (f *fakeResourceGroupRepository) CreateResourceGroup(_ context.Context, group model.ResourceGroup) (model.ResourceGroup, error) {
	group.ID = "generated"
	f.created = group
	return group, nil
}

func (f *fakeResourceGroupRepository) UpdateResourceGroup(_ context.Context, group model.ResourceGroup, oldName string) (model.ResourceGroup, error) {
	f.updated = group
	f.oldName = oldName
	return group, nil
}

func (f *fakeResourceGroupRepository) DeleteResourceGroup(_ context.Context, group model.ResourceGroup) error {
	f.deleted = group
	return nil
}

func TestResourceGroupServiceCreateDefaultsAndNormalizes(t *testing.T) {
	repository := &fakeResourceGroupRepository{}
	resourceGroups, err := NewResourceGroupService(repository)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	group, err := resourceGroups.Create(context.Background(), CreateResourceGroupInput{
		Name: " production ", Description: " primary ",
	})
	if err != nil {
		t.Fatalf("create resource group: %v", err)
	}
	if group.ID != "generated" || group.Name != "production" ||
		group.GroupType != model.ResourceGroupTypeResource || group.Description != "primary" {
		t.Fatalf("created group = %#v", group)
	}
}

func TestResourceGroupServiceRejectsDuplicateName(t *testing.T) {
	resourceGroups, err := NewResourceGroupService(&fakeResourceGroupRepository{nameExists: true})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	_, err = resourceGroups.Create(context.Background(), CreateResourceGroupInput{
		Name: "prod", GroupType: model.ResourceGroupTypeResource,
	})
	if !errors.Is(err, ErrResourceGroupConflict) {
		t.Fatalf("create error = %v, want conflict", err)
	}
}

func TestResourceGroupServiceUpdateCanClearDescription(t *testing.T) {
	repository := &fakeResourceGroupRepository{groups: []model.ResourceGroup{{
		ID: "group-1", Name: "prod", GroupType: model.ResourceGroupTypeResource, Description: "old",
	}}}
	resourceGroups, err := NewResourceGroupService(repository)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	name := "production"
	description := ""
	group, err := resourceGroups.Update(context.Background(), "group-1", UpdateResourceGroupInput{
		Name: &name, Description: &description,
	})
	if err != nil {
		t.Fatalf("update resource group: %v", err)
	}
	if group.Name != "production" || group.Description != "" || repository.oldName != "prod" {
		t.Fatalf("updated group = %#v oldName=%q", group, repository.oldName)
	}
}

func TestResourceGroupServiceListRejectsUnknownType(t *testing.T) {
	resourceGroups, err := NewResourceGroupService(&fakeResourceGroupRepository{})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	_, _, _, err = resourceGroups.List(context.Background(), ResourceGroupListParams{GroupType: "unknown"})
	if !errors.Is(err, ErrInvalidResourceGroup) {
		t.Fatalf("list error = %v, want invalid group", err)
	}
}
