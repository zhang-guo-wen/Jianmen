package rbac

import (
	"reflect"
	"testing"
)

func TestPermissionCatalogContainsEveryAction(t *testing.T) {
	expected := []string{
		ActionDBConnect, ActionSessionConnect, ActionSFTPConnect,
		ActionAuditView, ActionDBAuditView,
		ActionHostCreate, ActionHostUpdate, ActionHostDelete, ActionHostView,
		ActionTargetCreate, ActionTargetUpdate, ActionTargetDelete, ActionTargetView,
		ActionDBProxyCreate, ActionDBProxyUpdate, ActionDBProxyDelete, ActionDBProxyView,
		ActionRBACManage, ActionSessionView,
		ActionAppCreate, ActionAppUpdate, ActionAppDelete, ActionAppView, ActionAppConnect,
		ActionPlatformAccountCreate, ActionPlatformAccountUpdate, ActionPlatformAccountDelete,
		ActionPlatformAccountView, ActionPlatformAccountUse,
	}

	if err := ValidatePermissionCatalog(); err != nil {
		t.Fatalf("ValidatePermissionCatalog() error = %v", err)
	}
	if got := len(PermissionCatalog()); got != len(expected) {
		t.Fatalf("catalog length = %d, want %d", got, len(expected))
	}
	for _, action := range expected {
		item, ok := FindPermissionDefinition(action)
		if !ok || item.Action != action || !item.Assignable {
			t.Fatalf("catalog entry for %q = %#v, %v", action, item, ok)
		}
	}
}

func TestPermissionCatalogReturnsDefensiveCopies(t *testing.T) {
	items := PermissionCatalog()
	items[0].Action = "changed"
	if len(items[0].ResourceTypes) > 0 {
		items[0].ResourceTypes[0] = "changed"
	}
	pages := PermissionPages()
	pages[0].Key = "changed"
	pages[0].Actions[0].Action = "changed"
	if _, ok := FindPermissionDefinition("changed"); ok {
		t.Fatal("caller mutated catalog index")
	}
	if PermissionPages()[0].Key == "changed" {
		t.Fatal("caller mutated page catalog")
	}
}

func TestValidateAssignableActionsAddsDependencies(t *testing.T) {
	got, err := ValidateAssignableActions([]string{ActionTargetDelete, ActionAppCreate, ActionTargetDelete})
	if err != nil {
		t.Fatalf("ValidateAssignableActions() error = %v", err)
	}
	want := []string{ActionAppCreate, ActionAppView, ActionHostView, ActionTargetDelete, ActionTargetView}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %#v, want %#v", got, want)
	}
	if _, err := ValidateAssignableActions([]string{"missing:action"}); err == nil {
		t.Fatal("unknown action was accepted")
	}
}

func TestSFTPConnectDoesNotGrantSSHConnect(t *testing.T) {
	got, err := ValidateAssignableActions([]string{ActionSFTPConnect})
	if err != nil {
		t.Fatalf("ValidateAssignableActions() error = %v", err)
	}
	want := []string{ActionSFTPConnect}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %#v, want %#v", got, want)
	}
}

func TestAccessiblePagesUsesAnyChildAction(t *testing.T) {
	got := AccessiblePages([]string{ActionDBConnect, ActionDBAuditView})
	want := []PageAccess{
		{Key: "quickConnect", Path: "/quick-connect", Order: 10},
		{Key: "audit", Path: "/audit", Order: 60},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pages = %#v, want %#v", got, want)
	}
	if all := AccessiblePages([]string{"*"}); len(all) != len(PermissionPages()) {
		t.Fatalf("wildcard pages = %d, want %d", len(all), len(PermissionPages()))
	}
}
