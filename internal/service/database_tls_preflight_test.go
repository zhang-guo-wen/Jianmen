package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	"jianmen/internal/dbtls"
	"jianmen/internal/model"
	"jianmen/internal/rbac"
)

type tlsPreflightRepository struct {
	record DatabaseInstanceRecord
	calls  int
	err    error
}

func (repository *tlsPreflightRepository) DatabaseInstanceForProbe(context.Context, string) (DatabaseInstanceRecord, error) {
	repository.calls++
	return repository.record, repository.err
}

type tlsPreflightAuthorization struct {
	allowed      bool
	action       string
	resourceType string
	resourceID   string
}

func (authorization *tlsPreflightAuthorization) AuthorizeConnection(_ context.Context, _ string, actions []string, resourceType, resourceID string) (bool, error) {
	if len(actions) == 1 {
		authorization.action = actions[0]
	}
	authorization.resourceType = resourceType
	authorization.resourceID = resourceID
	return authorization.allowed, nil
}

func (authorization *tlsPreflightAuthorization) AuthorizeBatch(_ context.Context, _ string, requests []AuthorizationRequest) ([]AuthorizationDecision, error) {
	decisions := make([]AuthorizationDecision, len(requests))
	for i := range decisions {
		decisions[i].Allowed = authorization.allowed
	}
	return decisions, nil
}

type tlsPreflightProbe struct {
	target DatabaseInstanceRecord
	calls  int
	err    error
}

func (probe *tlsPreflightProbe) ProbeTLS(_ context.Context, target DatabaseInstanceRecord) error {
	probe.calls++
	probe.target = target
	return probe.err
}

func TestDatabaseTLSPreflightUsesCreatePermissionWithoutCredentials(t *testing.T) {
	repository := &tlsPreflightRepository{}
	authorization := &tlsPreflightAuthorization{allowed: true}
	probe := &tlsPreflightProbe{}
	preflight := newTestDatabaseTLSPreflight(t, repository, authorization, probe)

	err := preflight.Probe(context.Background(), "actor", DatabaseTLSPreflightInput{
		Protocol: "mysql", Address: "db.example.com", Port: 3306,
		TLSMode: dbtls.ModeVerifyFull, TLSServerName: "db.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if authorization.action != rbac.ActionDBProxyCreate || authorization.resourceType != "" || authorization.resourceID != "" {
		t.Fatalf("create authorization = %q %q %q", authorization.action, authorization.resourceType, authorization.resourceID)
	}
	if repository.calls != 0 {
		t.Fatalf("new instance loaded stored record %d times", repository.calls)
	}
	if probe.calls != 1 || probe.target.Protocol != "mysql" || probe.target.Address != "db.example.com" {
		t.Fatalf("probe target = %+v, calls = %d", probe.target, probe.calls)
	}
}

func TestDatabaseTLSPreflightEditReusesStoredCA(t *testing.T) {
	storedCA := tlsPreflightTestCA(t)
	repository := &tlsPreflightRepository{record: DatabaseInstanceRecord{ID: "db-1", TLSCAPEM: storedCA}}
	authorization := &tlsPreflightAuthorization{allowed: true}
	probe := &tlsPreflightProbe{}
	preflight := newTestDatabaseTLSPreflight(t, repository, authorization, probe)

	err := preflight.Probe(context.Background(), "actor", DatabaseTLSPreflightInput{
		InstanceID: "db-1", Protocol: "postgres", Address: "db.example.com", Port: 5432,
		TLSMode: dbtls.ModeVerifyCA,
	})
	if err != nil {
		t.Fatal(err)
	}
	if authorization.action != rbac.ActionDBProxyUpdate || authorization.resourceType != model.ResourceTypeDatabaseInstance || authorization.resourceID != "db-1" {
		t.Fatalf("edit authorization = %q %q %q", authorization.action, authorization.resourceType, authorization.resourceID)
	}
	if repository.calls != 1 || probe.target.TLSCAPEM != storedCA {
		t.Fatalf("stored CA was not reused: target=%+v calls=%d", probe.target, repository.calls)
	}
}

func tlsPreflightTestCA(t *testing.T) string {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "TLS preflight test CA"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, privateKey.Public(), privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestDatabaseTLSPreflightRejectsDisabledModeAndForbiddenProbe(t *testing.T) {
	probe := &tlsPreflightProbe{}
	preflight := newTestDatabaseTLSPreflight(t, &tlsPreflightRepository{}, &tlsPreflightAuthorization{allowed: true}, probe)
	if err := preflight.Probe(context.Background(), "actor", DatabaseTLSPreflightInput{
		Protocol: "redis", Address: "127.0.0.1", Port: 6379, TLSMode: dbtls.ModeDisable,
	}); !errors.Is(err, ErrDatabaseManagementInvalid) {
		t.Fatalf("disabled preflight error = %v", err)
	}

	deniedProbe := &tlsPreflightProbe{}
	denied := newTestDatabaseTLSPreflight(t, &tlsPreflightRepository{}, &tlsPreflightAuthorization{}, deniedProbe)
	if err := denied.Probe(context.Background(), "actor", DatabaseTLSPreflightInput{
		Protocol: "redis", Address: "127.0.0.1", Port: 6379, TLSMode: dbtls.ModeVerifyCA,
	}); !errors.Is(err, ErrDatabaseManagementForbidden) {
		t.Fatalf("forbidden preflight error = %v", err)
	}
	if deniedProbe.calls != 0 {
		t.Fatal("forbidden request reached the network prober")
	}
}

func TestDatabaseTLSPreflightWrapsProbeFailure(t *testing.T) {
	cause := errors.New("handshake failed")
	probe := &tlsPreflightProbe{err: cause}
	preflight := newTestDatabaseTLSPreflight(t, &tlsPreflightRepository{}, &tlsPreflightAuthorization{allowed: true}, probe)
	err := preflight.Probe(context.Background(), "actor", DatabaseTLSPreflightInput{
		Protocol: "redis", Address: "127.0.0.1", Port: 6379, TLSMode: dbtls.ModeVerifyCA,
	})
	if !errors.Is(err, ErrDatabaseTLSPreflightFailed) || !errors.Is(err, cause) {
		t.Fatalf("probe failure = %v", err)
	}
}

type tlsPreflightManagementRepository struct {
	DatabaseManagementRepository
	record                   DatabaseInstanceRecord
	lastUpdate               DatabaseInstanceInput
	lastProof                DatabaseInstanceTLSState
	createCalls, updateCalls int
}

func (repository *tlsPreflightManagementRepository) DatabaseInstanceForProbe(context.Context, string) (DatabaseInstanceRecord, error) {
	return repository.record, nil
}

func (repository *tlsPreflightManagementRepository) CreateDatabaseInstanceWithCreatorGrant(_ context.Context, input DatabaseInstanceInput, _ string) (DatabaseInstance, error) {
	repository.createCalls++
	return DatabaseInstance{Protocol: input.Protocol, Address: input.Address, TLSMode: input.TLSMode}, nil
}

func (repository *tlsPreflightManagementRepository) UpdateDatabaseInstance(_ context.Context, id string, input DatabaseInstanceInput) (DatabaseInstance, error) {
	repository.updateCalls++
	return DatabaseInstance{ID: id, Protocol: input.Protocol, Address: input.Address, TLSMode: input.TLSMode}, nil
}

func (repository *tlsPreflightManagementRepository) UpdateDatabaseInstanceWithTLSProof(_ context.Context, id string, input DatabaseInstanceInput, proof DatabaseInstanceTLSState) (DatabaseInstance, error) {
	repository.updateCalls++
	repository.lastUpdate = input
	repository.lastProof = proof
	return DatabaseInstance{ID: id, Protocol: input.Protocol, Address: input.Address, TLSMode: input.TLSMode}, nil
}

type tlsPreflightDeprovisioner struct{}

func (tlsPreflightDeprovisioner) Deprovision(context.Context, string) error { return nil }

func TestDatabaseManagementDoesNotPersistUnverifiedTLS(t *testing.T) {
	cause := errors.New("unverified TLS")
	repository := &tlsPreflightManagementRepository{record: DatabaseInstanceRecord{
		ID: "db-1", Protocol: "mysql", Address: "old.example.com", Port: 3306, TLSMode: dbtls.ModeDisable,
	}}
	authorization := &tlsPreflightAuthorization{allowed: true}
	probe := &tlsPreflightProbe{err: cause}
	preflight := newTestDatabaseTLSPreflight(t, repository, authorization, probe)
	management, err := NewDatabaseManagementService(repository, authorization, tlsPreflightDeprovisioner{}, preflight)
	if err != nil {
		t.Fatal(err)
	}
	input := DatabaseInstanceInput{
		Protocol: "mysql", Address: "db.example.com", Port: 3306,
		TLSMode: dbtls.ModeVerifyFull, TLSServerName: "db.example.com",
	}

	if _, err := management.CreateInstance(context.Background(), "actor", false, input); !errors.Is(err, ErrDatabaseTLSPreflightFailed) {
		t.Fatalf("CreateInstance() error = %v", err)
	}
	if repository.createCalls != 0 {
		t.Fatal("failed TLS create reached persistence")
	}
	if _, err := management.UpdateInstance(context.Background(), "actor", "db-1", input); !errors.Is(err, ErrDatabaseTLSPreflightFailed) {
		t.Fatalf("UpdateInstance() error = %v", err)
	}
	if repository.updateCalls != 0 {
		t.Fatal("failed TLS update reached persistence")
	}
}

func TestDatabaseManagementLeavesDisabledTLSUnprobed(t *testing.T) {
	repository := &tlsPreflightManagementRepository{}
	authorization := &tlsPreflightAuthorization{allowed: true}
	probe := &tlsPreflightProbe{err: errors.New("must not run")}
	preflight := newTestDatabaseTLSPreflight(t, repository, authorization, probe)
	management, err := NewDatabaseManagementService(repository, authorization, tlsPreflightDeprovisioner{}, preflight)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := management.CreateInstance(context.Background(), "actor", false, DatabaseInstanceInput{
		Protocol: "mysql", Address: "db.example.com", Port: 3306, TLSMode: dbtls.ModeDisable,
	}); err != nil {
		t.Fatal(err)
	}
	if probe.calls != 0 || repository.createCalls != 1 {
		t.Fatalf("disabled create probe calls = %d, create calls = %d", probe.calls, repository.createCalls)
	}
}

func TestDatabaseManagementDoesNotReprobeUnchangedEnabledTLS(t *testing.T) {
	repository := &tlsPreflightManagementRepository{record: DatabaseInstanceRecord{
		ID: "db-1", Protocol: "mysql", Address: "db.example.com", Port: 3306,
		TLSMode: dbtls.ModeVerifyFull, TLSServerName: "db.example.com",
	}}
	authorization := &tlsPreflightAuthorization{allowed: true}
	probe := &tlsPreflightProbe{err: errors.New("must not run")}
	preflight := newTestDatabaseTLSPreflight(t, repository, authorization, probe)
	management, err := NewDatabaseManagementService(repository, authorization, tlsPreflightDeprovisioner{}, preflight)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := management.UpdateInstance(context.Background(), "actor", "db-1", DatabaseInstanceInput{
		Protocol: "mysql", Address: "db.example.com", Port: 3306,
		TLSMode: dbtls.ModeVerifyFull, TLSServerName: "db.example.com", Remark: "changed only",
	}); err != nil {
		t.Fatal(err)
	}
	if probe.calls != 0 || repository.updateCalls != 1 {
		t.Fatalf("unchanged TLS probe calls = %d, update calls = %d", probe.calls, repository.updateCalls)
	}
}

func TestDatabaseManagementPartialUpdatePreservesTLSHostname(t *testing.T) {
	repository := &tlsPreflightManagementRepository{record: DatabaseInstanceRecord{
		ID: "db-1", Protocol: "mysql", Address: "192.0.2.10", Port: 3306,
		TLSMode: dbtls.ModeVerifyFull, TLSServerName: "db.example.com",
	}}
	authorization := &tlsPreflightAuthorization{allowed: true}
	probe := &tlsPreflightProbe{err: errors.New("must not run")}
	preflight := newTestDatabaseTLSPreflight(t, repository, authorization, probe)
	management, err := NewDatabaseManagementService(repository, authorization, tlsPreflightDeprovisioner{}, preflight)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := management.UpdateInstance(context.Background(), "actor", "db-1", DatabaseInstanceInput{
		Protocol: "mysql", Address: "192.0.2.10", Port: 3306, Remark: "changed only",
	}); err != nil {
		t.Fatal(err)
	}
	if probe.calls != 0 {
		t.Fatal("partial metadata update unexpectedly reprobed TLS")
	}
	if repository.lastUpdate.TLSMode != dbtls.ModeVerifyFull || repository.lastUpdate.TLSServerName != "db.example.com" {
		t.Fatalf("partial update TLS fields = mode %q, server name %q", repository.lastUpdate.TLSMode, repository.lastUpdate.TLSServerName)
	}
}

func newTestDatabaseTLSPreflight(t *testing.T, repository DatabaseTLSPreflightRepository, authorization DatabaseTLSPreflightAuthorizer, probe DatabaseTLSPreflightProber) *DatabaseTLSPreflightService {
	t.Helper()
	preflight, err := NewDatabaseTLSPreflightService(repository, authorization, probe)
	if err != nil {
		t.Fatal(err)
	}
	return preflight
}
