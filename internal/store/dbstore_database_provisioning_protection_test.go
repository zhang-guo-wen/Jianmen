package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/service"
)

func provisioningProtectionInput(instance model.DatabaseInstance, admin model.DatabaseAccount) service.DatabaseProvisioningOperationInput {
	return service.DatabaseProvisioningOperationInput{
		ID: "jmo_abcdefghijklmnopqrst", InstanceID: instance.ID, AdminAccountID: admin.ID,
		Username: "jm_abcdefghijklmnopqrst", Password: "generated-secret", Host: "10.0.0.8",
		GrantsJSON:            `[{"database":"orders","privilege":"read"}]`,
		Lease:                 service.DatabaseProvisioningLease{Owner: "worker", Token: "lease", Duration: time.Minute},
		AdministratorUsername: admin.Username, AdministratorPassword: admin.Password.GetPlaintext(),
		InstanceProof: databaseProvisioningInstanceProof(instance),
	}
}

func TestDatabaseProvisioningOperationRejectsMissingOrStaleProof(t *testing.T) {
	repository, db := newDatabaseProvisioningStoreTest(t)
	instance, administrator := seedProvisioningStoreAdministrator(t, db)
	input := provisioningProtectionInput(instance, administrator)
	for _, mutate := range []func(*service.DatabaseProvisioningOperationInput){
		func(in *service.DatabaseProvisioningOperationInput) { in.InstanceProof = "" },
		func(in *service.DatabaseProvisioningOperationInput) { in.AdministratorUsername = "" },
		func(in *service.DatabaseProvisioningOperationInput) { in.AdministratorPassword = "" },
		func(in *service.DatabaseProvisioningOperationInput) { in.InstanceProof = strings.Repeat("0", 64) },
	} {
		candidate := input
		mutate(&candidate)
		if _, _, err := repository.CreateDatabaseProvisioningOperation(context.Background(), candidate); err == nil {
			t.Fatal("operation with absent or stale proof was created")
		}
	}
}

func TestProvisioningReferencedAdministratorProtection(t *testing.T) {
	repository, db := newDatabaseProvisioningStoreTest(t)
	instance, administrator := seedProvisioningStoreAdministrator(t, db)
	operation := seedReferencedProvisioningOperation(t, db, instance.ID, administrator.ID)

	for _, update := range []struct {
		name     string
		username string
		password string
		status   string
	}{
		{name: "username", username: "renamed", status: "active"},
		{name: "password", password: "changed", status: "active"},
		{name: "disabled", status: "disabled"},
	} {
		t.Run(update.name, func(t *testing.T) {
			_, err := repository.UpdateDatabaseAccount(context.Background(), administrator.ID, update.username, update.password, "", "", nil, update.status)
			if !errors.Is(err, ErrReferencedDatabaseAdministrator) {
				t.Fatalf("update error = %v, want reference conflict", err)
			}
		})
	}
	if err := repository.DeleteDatabaseAccount(context.Background(), administrator.ID); !errors.Is(err, ErrReferencedDatabaseAdministrator) {
		t.Fatalf("delete error = %v, want reference conflict", err)
	}
	var unchanged model.DatabaseAccount
	if err := db.First(&unchanged, "id = ?", administrator.ID).Error; err != nil {
		t.Fatal(err)
	}
	if unchanged.Username != administrator.Username || unchanged.Password.GetPlaintext() != "admin-secret" || unchanged.Status != "active" {
		t.Fatalf("protected administrator changed: %#v", unchanged)
	}

	expires := time.Now().UTC().Add(time.Hour)
	if _, err := repository.UpdateDatabaseAccount(context.Background(), administrator.ID, "", "", "ops", "recovery only", &expires, "active"); err != nil {
		t.Fatalf("metadata update: %v", err)
	}
	if err := db.Delete(&operation).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repository.UpdateDatabaseAccount(context.Background(), administrator.ID, "renamed", "changed", "", "", nil, "active"); err != nil {
		t.Fatalf("update after operation removal: %v", err)
	}
}

func TestProvisioningReferencedInstanceBlocksDeleteBeforeAccountMutation(t *testing.T) {
	repository, db := newDatabaseProvisioningStoreTest(t)
	instance, administrator := seedProvisioningStoreAdministrator(t, db)
	seedReferencedProvisioningOperation(t, db, instance.ID, administrator.ID)
	if err := repository.DeleteDatabaseInstance(context.Background(), instance.ID); !errors.Is(err, ErrReferencedDatabaseInstance) {
		t.Fatalf("delete instance error = %v, want reference conflict", err)
	}
	var retainedInstance model.DatabaseInstance
	var retainedAdministrator model.DatabaseAccount
	if err := db.First(&retainedInstance, "id = ?", instance.ID).Error; err != nil {
		t.Fatalf("instance deleted despite conflict: %v", err)
	}
	if err := db.First(&retainedAdministrator, "id = ?", administrator.ID).Error; err != nil {
		t.Fatalf("administrator deleted despite conflict: %v", err)
	}
}

func TestProvisioningReferencedInstanceBlocksCriticalUpdateButAllowsMetadata(t *testing.T) {
	repository, db := newDatabaseProvisioningStoreTest(t)
	instance, administrator := seedProvisioningStoreAdministrator(t, db)
	seedReferencedProvisioningOperation(t, db, instance.ID, administrator.ID)
	var current model.DatabaseInstance
	if err := db.First(&current, "id = ?", instance.ID).Error; err != nil {
		t.Fatal(err)
	}
	ca := current.TLSCAPEM
	metadata := DatabaseInstanceInput{
		Name: current.Name, Protocol: current.Protocol, Address: current.Address, Port: current.Port,
		TLSMode: current.TLSMode, TLSServerName: current.TLSServerName, TLSCAPEM: &ca,
		Group: "operations", Remark: "retained operation", Status: current.Status,
	}
	if _, err := repository.UpdateDatabaseInstance(context.Background(), current.ID, metadata); err != nil {
		t.Fatalf("metadata update: %v", err)
	}
	critical := metadata
	critical.Address = "127.0.0.2"
	if _, err := repository.UpdateDatabaseInstance(context.Background(), current.ID, critical); !errors.Is(err, ErrReferencedDatabaseInstance) {
		t.Fatalf("critical update error = %v, want reference conflict", err)
	}
}

func seedReferencedProvisioningOperation(t *testing.T, db *gorm.DB, instanceID, adminID string) model.DatabaseProvisioningOperation {
	t.Helper()
	operation := model.DatabaseProvisioningOperation{
		ID: "jmo_abcdefghijklmnopqrst", Kind: "create", InstanceID: instanceID, AdminAccountID: adminID,
		ActorID: "operator", UpstreamUsername: "jm_abcdefghijklmnopqrst", Password: model.NewEncryptedField("generated"),
		Host: "10.0.0.8", GrantsJSON: `[]`, Stage: "not_created", CleanupStatus: "none", Revision: 1,
	}
	if err := db.Create(&operation).Error; err != nil {
		t.Fatalf("create retained operation: %v", err)
	}
	return operation
}
