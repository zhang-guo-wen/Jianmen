package rbac

import (
	"reflect"
	"testing"
)

func TestPermissionCatalogContainsEveryAction(t *testing.T) {
	expected := []string{
		ActionDBConnect, ActionSessionConnect, ActionSFTPRead, ActionSFTPWrite,
		ActionAuditView, ActionDBAuditView,
		ActionHostCreate, ActionHostUpdate, ActionHostDelete, ActionHostView,
		ActionTargetCreate, ActionTargetUpdate, ActionTargetDelete, ActionTargetView,
		ActionDBProxyCreate, ActionDBProxyUpdate, ActionDBProxyDelete, ActionDBProxyView,
		ActionRBACManage, ActionSessionView, ActionDashboardView,
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
	if _, ok := FindPermissionDefinition("changed"); ok {
		t.Fatal("caller mutated catalog index")
	}
}

func TestValidateAssignableActions(t *testing.T) {
	got, err := ValidateAssignableActions([]string{ActionHostView, " " + ActionAppView + " ", ActionHostView})
	if err != nil {
		t.Fatalf("ValidateAssignableActions() error = %v", err)
	}
	want := []string{ActionAppView, ActionHostView}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %#v, want %#v", got, want)
	}
	if _, err := ValidateAssignableActions([]string{"missing:action"}); err == nil {
		t.Fatal("unknown action was accepted")
	}
}
