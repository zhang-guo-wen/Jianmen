package admin

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestUpdateDatabaseInstanceRetainsAndExplicitlyClearsTLSCA(t *testing.T) {
	server, db := newAdminDBTestServer(t)
	seedTestSuperAdmin(t, db, "u-admin")
	instance := model.DatabaseInstance{Name: "orders", Protocol: "mysql", Address: "127.0.0.1", Port: 3306, TLSMode: "verify-full", TLSCAPEM: testCAPEM(t), Status: "active"}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatal(err)
	}
	update := func(body string) *httptest.ResponseRecorder {
		req := asTestSuperAdmin(httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/db/instances/%s", instance.ID), bytes.NewBufferString(body)))
		recorder := httptest.NewRecorder()
		server.handleDBInstance(recorder, req)
		return recorder
	}
	if recorder := update(`{"name":"orders","protocol":"mysql","address":"127.0.0.1","port":3306,"tls_mode":"verify-full","status":"active"}`); recorder.Code != http.StatusOK {
		t.Fatalf("update without tls_ca_pem status = %d, body=%s", recorder.Code, recorder.Body.String())
	} else if strings.Contains(recorder.Body.String(), instance.TLSCAPEM) || !strings.Contains(recorder.Body.String(), `"has_tls_ca":true`) {
		t.Fatalf("database instance response exposed or omitted CA state: %s", recorder.Body.String())
	}
	var persisted model.DatabaseInstance
	if err := db.First(&persisted, "id = ?", instance.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.TLSCAPEM != instance.TLSCAPEM {
		t.Fatal("omitted tls_ca_pem cleared the existing CA")
	}
	if recorder := update(`{"name":"orders","protocol":"mysql","address":"127.0.0.1","port":3306,"tls_mode":"verify-full","clear_tls_ca":true,"status":"active"}`); recorder.Code != http.StatusOK {
		t.Fatalf("explicit CA clear status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if err := db.First(&persisted, "id = ?", instance.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.TLSCAPEM != "" {
		t.Fatal("clear_tls_ca did not clear the existing CA")
	}
	if err := db.Model(&persisted).Update("tls_ca_pem", instance.TLSCAPEM).Error; err != nil {
		t.Fatal(err)
	}
	if recorder := update(`{"name":"orders","protocol":"mysql","address":"127.0.0.1","port":3306,"tls_mode":"disable","status":"active"}`); recorder.Code != http.StatusOK {
		t.Fatalf("disable TLS status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if err := db.First(&persisted, "id = ?", instance.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.TLSCAPEM != "" {
		t.Fatal("disabling TLS did not clear the existing CA")
	}
}

func testCAPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test CA"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, KeyUsage: x509.KeyUsageCertSign}
	certificate, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate}))
}
