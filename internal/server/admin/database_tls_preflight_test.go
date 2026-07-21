package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"jianmen/internal/model"
	"jianmen/internal/server/dbproxy"
	"jianmen/internal/service"
)

type recordingDatabaseTLSPreflightProber struct {
	target service.DatabaseInstanceRecord
	err    error
}

func (prober *recordingDatabaseTLSPreflightProber) ProbeTLS(_ context.Context, target service.DatabaseInstanceRecord) error {
	prober.target = target
	return prober.err
}

func TestHandleDatabaseTLSPreflightReusesInstanceCAWithoutCredentials(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")
	caPEM := testCAPEM(t)
	instance := model.DatabaseInstance{
		Name: "orders", Protocol: "postgres", Address: "db.example.com", Port: 5432,
		TLSMode: "verify-ca", TLSCAPEM: caPEM, Status: "active",
	}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatal(err)
	}
	prober := &recordingDatabaseTLSPreflightProber{}
	server.databaseTLSPreflight = newAdminTestDatabaseTLSPreflight(t, server, prober)
	body := `{"instance_id":"` + instance.ID + `","protocol":"postgres","address":"db.example.com","port":5432,"tls_mode":"verify-ca"}`
	request := asTestSuperAdmin(httptest.NewRequest(http.MethodPost, "/api/db/instances/tls-preflight", bytes.NewBufferString(body)))
	recorder := httptest.NewRecorder()

	server.handleDatabaseTLSPreflight(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if prober.target.ID != instance.ID || prober.target.TLSCAPEM != caPEM {
		t.Fatalf("preflight target did not retain saved instance CA: %+v", prober.target)
	}
	if strings.Contains(body, "username") || strings.Contains(body, "password") {
		t.Fatal("TLS preflight request unexpectedly contained account credentials")
	}
}

func TestHandleDatabaseTLSPreflightReturnsSafeFailure(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")
	prober := &recordingDatabaseTLSPreflightProber{err: errors.Join(dbproxy.ErrUpstreamTLSUnsupported, errors.New("private upstream detail"))}
	server.databaseTLSPreflight = newAdminTestDatabaseTLSPreflight(t, server, prober)
	request := asTestSuperAdmin(httptest.NewRequest(
		http.MethodPost,
		"/api/db/instances/tls-preflight",
		bytes.NewBufferString(`{"protocol":"mysql","address":"db.example.com","port":3306,"tls_mode":"verify-ca"}`),
	))
	recorder := httptest.NewRecorder()

	server.handleDatabaseTLSPreflight(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			OK      bool   `json:"ok"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.OK || envelope.Data.Code != "tls_unsupported" || !strings.Contains(envelope.Data.Message, "未启用") {
		t.Fatalf("unexpected response: %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "private upstream detail") {
		t.Fatal("TLS preflight response leaked the upstream error")
	}
}

func newAdminTestDatabaseTLSPreflight(t *testing.T, server *Server, prober service.DatabaseTLSPreflightProber) *service.DatabaseTLSPreflightService {
	t.Helper()
	preflight, err := service.NewDatabaseTLSPreflightService(
		databaseManagementRepositoryAdapter{repository: server.databases},
		server.authorization,
		prober,
	)
	if err != nil {
		t.Fatal(err)
	}
	return preflight
}
