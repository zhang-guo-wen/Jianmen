package dbtls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"runtime"
	"strings"
	"time"
)

const maxLocalGatewayKeyPEMSize = 1 << 20

func generateLocalGatewayIdentity(certFile, keyFile, serverName string) error {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate database gateway private key: %w", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return fmt.Errorf("generate database gateway certificate serial: %w", err)
	}
	if serialNumber.Sign() == 0 {
		serialNumber.SetInt64(1)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               pkix.Name{CommonName: "Jianmen Local Database Gateway"},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{localGatewayDefaultServerName},
		IPAddresses: []net.IP{
			net.ParseIP("127.0.0.1"),
			net.ParseIP("::1"),
		},
	}
	addGatewayServerName(template, serverName)
	certificateDER, err := x509.CreateCertificate(
		rand.Reader,
		template,
		template,
		&privateKey.PublicKey,
		privateKey,
	)
	if err != nil {
		return fmt.Errorf("create database gateway certificate: %w", err)
	}
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("encode database gateway private key: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER})
	if err := writeSyncedIdentityFile(certFile, certPEM); err != nil {
		return fmt.Errorf("write database gateway certificate: %w", err)
	}
	if err := writeSyncedIdentityFile(keyFile, keyPEM); err != nil {
		return fmt.Errorf("write database gateway private key: %w", err)
	}
	return nil
}

func addGatewayServerName(certificate *x509.Certificate, serverName string) {
	if ip := net.ParseIP(serverName); ip != nil {
		for _, existing := range certificate.IPAddresses {
			if existing.Equal(ip) {
				return
			}
		}
		certificate.IPAddresses = append(certificate.IPAddresses, ip)
		return
	}
	for _, existing := range certificate.DNSNames {
		if strings.EqualFold(existing, serverName) {
			return
		}
	}
	certificate.DNSNames = append(certificate.DNSNames, serverName)
}

func validateManagedLocalGatewayIdentity(certFile, keyFile, serverName string) error {
	if err := validateManagedIdentityFile(certFile, false); err != nil {
		return err
	}
	if err := validateManagedIdentityFile(keyFile, true); err != nil {
		return err
	}
	certPEM, certificates, err := readCertificateFile(certFile)
	if err != nil {
		return fmt.Errorf("read managed database gateway certificate: %w", err)
	}
	if len(certificates) != 1 {
		return errors.New("managed database gateway certificate must contain exactly one certificate")
	}
	keyPEM, err := readLimitedIdentityFile(keyFile, maxLocalGatewayKeyPEMSize)
	if err != nil {
		return fmt.Errorf("read managed database gateway private key: %w", err)
	}
	if _, err := tls.X509KeyPair([]byte(certPEM), keyPEM); err != nil {
		return fmt.Errorf("load managed database gateway key pair: %w", err)
	}
	if _, err := LoadServerIdentity(certFile, "", serverName); err != nil {
		return fmt.Errorf("validate managed database gateway server identity: %w", err)
	}
	for _, requiredName := range []string{"localhost", "127.0.0.1", "::1"} {
		if err := certificates[0].VerifyHostname(requiredName); err != nil {
			return fmt.Errorf("managed database gateway certificate is missing SAN %q: %w", requiredName, err)
		}
	}
	return nil
}

func validateManagedIdentityFile(path string, private bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect managed database gateway TLS file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("managed database gateway TLS file %q is not a regular file", path)
	}
	if private && runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("managed database gateway private key %q has unsafe permissions", path)
	}
	return nil
}

func readLimitedIdentityFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) > limit {
		return nil, errors.New("PEM file exceeds size limit")
	}
	return contents, nil
}

func writeSyncedIdentityFile(path string, contents []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(contents); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
