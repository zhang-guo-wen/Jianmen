//go:build integration

package integration

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"jianmen/internal/model"
	"jianmen/internal/service"
)

func TestProvisionMySQLAccountAgainstMySQL57NoBackslashEscapes(t *testing.T) {
	requireDocker(t)

	const (
		rootPassword = "root-password"
		databaseName = "appdb"
	)
	containerID := runContainer(t, "jianmen-it-mysql-provision",
		"-e", "MYSQL_ROOT_PASSWORD="+rootPassword,
		"-e", "MYSQL_ROOT_HOST=%",
		"-e", "MYSQL_DATABASE="+databaseName,
		"-p", "127.0.0.1::3306",
		"mysql:5.7",
	)
	waitMySQLContainerInitialized(t, containerID)
	upstreamAddr := containerAddress(t, containerID, "3306/tcp")
	waitMySQL(t, upstreamAddr, "root", rootPassword)

	rootDB := openMySQLIntegrationDB(t, upstreamAddr, "root", rootPassword, "")
	defer rootDB.Close()
	accountHost := mysqlIntegrationClientHost(t, rootDB)
	for _, statement := range []string{
		"CREATE TABLE appdb.provision_probe (value VARCHAR(32) NOT NULL)",
		"INSERT INTO appdb.provision_probe (value) VALUES ('provisioned')",
		"SET GLOBAL sql_mode = CONCAT_WS(',', NULLIF(@@GLOBAL.sql_mode, ''), 'NO_BACKSLASH_ESCAPES')",
		"SET SESSION sql_mode = CONCAT_WS(',', NULLIF(@@SESSION.sql_mode, ''), 'NO_BACKSLASH_ESCAPES')",
	} {
		if _, err := rootDB.Exec(statement); err != nil {
			t.Fatalf("execute MySQL fixture statement %q: %v", statement, err)
		}
	}

	host, port := splitAddress(t, upstreamAddr)
	instance := model.DatabaseInstance{
		ID: "mysql57-provision-instance", Name: "mysql57-provision", Protocol: "mysql",
		Address: host, Port: port, TLSMode: "disable",
	}
	admin := model.DatabaseAccount{
		ID: "mysql57-provision-admin", InstanceID: instance.ID, Username: "root",
		Password: model.NewEncryptedField(rootPassword),
	}
	newUsername := "provision'oops"
	newPassword := `p\'secret`
	if err := service.ProvisionMySQLAccount(
		context.Background(), instance, admin, newUsername, newPassword, accountHost,
		[]service.DBGrant{{Database: databaseName, Privilege: "read"}},
	); err != nil {
		t.Fatalf("provision MySQL account: %v", err)
	}

	clientDB := openMySQLIntegrationDB(t, upstreamAddr, newUsername, newPassword, databaseName)
	defer clientDB.Close()
	var value string
	if err := clientDB.QueryRow("SELECT value FROM provision_probe").Scan(&value); err != nil {
		t.Fatalf("select with provisioned MySQL account: %v", err)
	}
	if value != "provisioned" {
		t.Fatalf("provisioned MySQL account returned %q, want %q", value, "provisioned")
	}

	_, err := clientDB.Exec("INSERT INTO provision_probe (value) VALUES ('should-not-be-allowed')")
	if err == nil {
		t.Fatal("provisioned read-only MySQL account unexpectedly inserted a row")
	}
	var mysqlErr *mysql.MySQLError
	if !errors.As(err, &mysqlErr) {
		t.Fatalf("insert denial error type = %T, want *mysql.MySQLError: %v", err, err)
	}
	if mysqlErr.Number != 1142 {
		t.Fatalf("insert denial MySQL error number = %d, want 1142: %v", mysqlErr.Number, err)
	}
}

func TestProvisionMySQLAccountAgainstMySQL8CachingSHA2OverVerifiedTLS(t *testing.T) {
	requireDocker(t)

	const (
		rootPassword = "root-password"
		databaseName = "appdb"
	)
	certificateDirectory, caPEM := writeMySQLIntegrationServerIdentity(t)
	containerID := runContainer(t, "jianmen-it-mysql8-provision",
		"-e", "MYSQL_ROOT_PASSWORD="+rootPassword,
		"-e", "MYSQL_ROOT_HOST=%",
		"-e", "MYSQL_DATABASE="+databaseName,
		"-p", "127.0.0.1::3306",
		"-v", dockerBindPath(t, certificateDirectory)+":/certs:ro",
		"mysql:8.0",
		"--ssl-ca=/certs/ca.pem",
		"--ssl-cert=/certs/server-cert.pem",
		"--ssl-key=/certs/server-key.pem",
	)
	waitMySQLContainerInitialized(t, containerID)
	upstreamAddr := containerAddress(t, containerID, "3306/tcp")
	waitMySQL(t, upstreamAddr, "root", rootPassword)

	rootDB := openMySQLIntegrationDB(t, upstreamAddr, "root", rootPassword, "")
	defer rootDB.Close()
	if _, err := rootDB.Exec(
		"CREATE TABLE appdb.provision_probe (value VARCHAR(32) NOT NULL)",
	); err != nil {
		t.Fatalf("create MySQL 8 provisioning fixture: %v", err)
	}
	if _, err := rootDB.Exec(
		"INSERT INTO appdb.provision_probe (value) VALUES ('provisioned')",
	); err != nil {
		t.Fatalf("seed MySQL 8 provisioning fixture: %v", err)
	}
	accountHost := mysqlIntegrationClientHost(t, rootDB)

	host, port := splitAddress(t, upstreamAddr)
	instance := model.DatabaseInstance{
		ID: "mysql8-provision-instance", Name: "mysql8-provision", Protocol: "mysql",
		Address: host, Port: port, TLSMode: "verify-full",
		TLSServerName: "localhost", TLSCAPEM: caPEM,
	}
	admin := model.DatabaseAccount{
		ID: "mysql8-provision-admin", InstanceID: instance.ID, Username: "root",
		Password: model.NewEncryptedField(rootPassword),
	}
	if err := service.ProvisionMySQLAccount(
		context.Background(),
		instance,
		admin,
		"app_reader",
		"server-generated-secret",
		accountHost,
		[]service.DBGrant{{Database: databaseName, Privilege: "read"}},
	); err != nil {
		t.Fatalf("provision MySQL 8 caching_sha2 account: %v", err)
	}

	clientDB := openMySQLIntegrationDB(
		t,
		upstreamAddr,
		"app_reader",
		"server-generated-secret",
		databaseName,
	)
	defer clientDB.Close()
	var value string
	if err := clientDB.QueryRow("SELECT value FROM provision_probe").Scan(&value); err != nil {
		t.Fatalf("query through provisioned MySQL 8 account: %v", err)
	}
	if value != "provisioned" {
		t.Fatalf("MySQL 8 provisioned account returned %q, want provisioned", value)
	}

	wrongName := instance
	wrongName.TLSServerName = "wrong.localhost"
	_, err := service.ListMySQLDatabases(context.Background(), wrongName, admin)
	if err == nil {
		t.Fatal("verify-full accepted a leaf certificate for the wrong server name")
	}
	var hostnameError x509.HostnameError
	if !errors.As(err, &hostnameError) {
		t.Fatalf("wrong server name error = %T, want x509.HostnameError: %v", err, err)
	}
}

func writeMySQLIntegrationServerIdentity(t *testing.T) (string, string) {
	t.Helper()
	now := time.Now().UTC()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate integration CA key: %v", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Jianmen integration CA"},
		NotBefore:    now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	caDER, err := x509.CreateCertificate(
		rand.Reader,
		caTemplate,
		caTemplate,
		&caKey.PublicKey,
		caKey,
	)
	if err != nil {
		t.Fatalf("create integration CA: %v", err)
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate integration leaf key: %v", err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		NotBefore:    now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour),
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	leafDER, err := x509.CreateCertificate(
		rand.Reader,
		leafTemplate,
		caTemplate,
		&leafKey.PublicKey,
		caKey,
	)
	if err != nil {
		t.Fatalf("create integration leaf certificate: %v", err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(leafKey),
	})
	directory := t.TempDir()
	files := make([]string, 0, 3)
	for name, contents := range map[string][]byte{
		"ca.pem": caPEM, "server-cert.pem": leafPEM, "server-key.pem": keyPEM,
	} {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			t.Fatalf("write integration certificate %s: %v", name, err)
		}
		files = append(files, path)
	}
	makeDockerBindReadable(t, directory, files...)
	return directory, string(caPEM)
}

func mysqlIntegrationClientHost(t *testing.T, db *sql.DB) string {
	t.Helper()
	var host string
	if err := db.QueryRow("SELECT SUBSTRING_INDEX(USER(), '@', -1)").Scan(&host); err != nil {
		t.Fatalf("resolve MySQL client host: %v", err)
	}
	if host == "" || host == "%" || host == "_" {
		t.Fatalf("MySQL returned unsafe client host %q", host)
	}
	return host
}

func openMySQLIntegrationDB(t *testing.T, addr, username, password, databaseName string) *sql.DB {
	t.Helper()
	config := mysql.Config{
		User: username, Passwd: password, Net: "tcp", Addr: addr, DBName: databaseName,
		AllowNativePasswords: true,
		Timeout:              3 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second,
	}
	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatalf("open MySQL connection for %q: %v", username, err)
	}
	waitFor(t, 30*time.Second, 500*time.Millisecond, db.Ping)
	return db
}
