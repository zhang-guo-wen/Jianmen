package dbtls

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

type ServerIdentityTrustMode string

const (
	ServerIdentityTrustModeCustom ServerIdentityTrustMode = "custom"
	ServerIdentityTrustModeSystem ServerIdentityTrustMode = "system"
)

type ServerIdentityMaterial struct {
	CAPEM      string
	LeafSHA256 string
	TrustMode  ServerIdentityTrustMode
}

func LoadServerIdentity(certFile, caFile, serverName string) (ServerIdentityMaterial, error) {
	return loadServerIdentity(certFile, caFile, serverName, x509.SystemCertPool)
}

func loadServerIdentity(
	certFile,
	caFile,
	serverName string,
	loadSystemRoots func() (*x509.CertPool, error),
) (ServerIdentityMaterial, error) {
	if serverName == "" || serverName != strings.TrimSpace(serverName) {
		return ServerIdentityMaterial{}, errors.New("TLS server name is required without surrounding whitespace")
	}
	certPEM, chain, err := readCertificateFile(certFile)
	if err != nil {
		return ServerIdentityMaterial{}, fmt.Errorf("read server certificate: %w", err)
	}
	leaf := chain[0]
	if leaf.IsCA {
		return ServerIdentityMaterial{}, errors.New("server leaf certificate must not be a CA")
	}
	if err := leaf.VerifyHostname(serverName); err != nil {
		return ServerIdentityMaterial{}, fmt.Errorf("verify server certificate name: %w", err)
	}

	intermediates := x509.NewCertPool()
	for _, certificate := range chain[1:] {
		intermediates.AddCert(certificate)
	}

	roots := x509.NewCertPool()
	trustMode := ServerIdentityTrustModeCustom
	trustAnchorPEM := ""
	switch {
	case strings.TrimSpace(caFile) != "":
		var anchors []*x509.Certificate
		trustAnchorPEM, anchors, err = readCertificateFile(caFile)
		if err != nil {
			return ServerIdentityMaterial{}, fmt.Errorf("read server CA: %w", err)
		}
		for _, certificate := range anchors {
			if !certificate.IsCA ||
				!certificate.BasicConstraintsValid ||
				certificate.KeyUsage&x509.KeyUsageCertSign == 0 {
				return ServerIdentityMaterial{}, errors.New("server CA file contains a certificate that is not a usable CA trust anchor")
			}
			roots.AddCert(certificate)
		}
	case isSelfSignedCertificate(leaf):
		if len(chain) != 1 {
			return ServerIdentityMaterial{}, errors.New("self-signed server certificate must be the only certificate in cert_file")
		}
		trustAnchorPEM = certPEM
		roots.AddCert(leaf)
	default:
		if loadSystemRoots == nil {
			return ServerIdentityMaterial{}, errors.New("system certificate pool loader is unavailable")
		}
		roots, err = loadSystemRoots()
		if err != nil {
			return ServerIdentityMaterial{}, fmt.Errorf("load system certificate pool: %w", err)
		}
		if roots == nil {
			return ServerIdentityMaterial{}, errors.New("system certificate pool is unavailable")
		}
		trustMode = ServerIdentityTrustModeSystem
	}

	verifiedChains, err := leaf.Verify(x509.VerifyOptions{
		DNSName:       serverName,
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   time.Now(),
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	if err != nil {
		return ServerIdentityMaterial{}, fmt.Errorf("verify server certificate identity: %w", err)
	}
	if trustMode == ServerIdentityTrustModeSystem {
		verifiedChain := verifiedChainWithConfiguredIntermediates(verifiedChains, chain[1:])
		if len(verifiedChain) == 0 {
			return ServerIdentityMaterial{}, errors.New("cert_file must include every intermediate certificate required by the system-trusted chain")
		}
		trustAnchor := verifiedChain[len(verifiedChain)-1]
		trustAnchorPEM = string(pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: trustAnchor.Raw,
		}))
	}

	fingerprint := sha256.Sum256(leaf.Raw)
	return ServerIdentityMaterial{
		CAPEM:      trustAnchorPEM,
		LeafSHA256: hex.EncodeToString(fingerprint[:]),
		TrustMode:  trustMode,
	}, nil
}
