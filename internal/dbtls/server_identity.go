package dbtls

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const maxServerIdentityPEMSize = 1 << 20

type ServerIdentityMaterial struct {
	CAPEM      string
	LeafSHA256 string
}

func LoadServerIdentity(certFile, caFile, serverName string) (ServerIdentityMaterial, error) {
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

	roots := x509.NewCertPool()
	intermediates := x509.NewCertPool()
	for _, certificate := range chain[1:] {
		intermediates.AddCert(certificate)
	}
	trustAnchorPEM := certPEM
	if strings.TrimSpace(caFile) == "" {
		if len(chain) != 1 || !isSelfSignedCertificate(leaf) {
			return ServerIdentityMaterial{}, errors.New("server certificate fallback requires one self-signed leaf certificate")
		}
		roots.AddCert(leaf)
	} else {
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
	}

	if _, err := leaf.Verify(x509.VerifyOptions{
		DNSName:       serverName,
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   time.Now(),
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		return ServerIdentityMaterial{}, fmt.Errorf("verify server certificate identity: %w", err)
	}

	fingerprint := sha256.Sum256(leaf.Raw)
	return ServerIdentityMaterial{
		CAPEM:      trustAnchorPEM,
		LeafSHA256: hex.EncodeToString(fingerprint[:]),
	}, nil
}

func readCertificateFile(path string) (string, []*x509.Certificate, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer file.Close()

	contents, err := io.ReadAll(io.LimitReader(file, maxServerIdentityPEMSize+1))
	if err != nil {
		return "", nil, err
	}
	if len(contents) > maxServerIdentityPEMSize {
		return "", nil, errors.New("certificate PEM exceeds size limit")
	}

	rest := contents
	certificates := make([]*x509.Certificate, 0, 1)
	for {
		block, remaining := pem.Decode(rest)
		if block == nil {
			if len(bytes.TrimSpace(rest)) != 0 {
				return "", nil, errors.New("certificate file contains malformed PEM data")
			}
			break
		}
		if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return "", nil, errors.New("certificate file contains a non-certificate PEM block")
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return "", nil, fmt.Errorf("parse certificate: %w", err)
		}
		certificates = append(certificates, certificate)
		rest = remaining
	}
	if len(certificates) == 0 {
		return "", nil, errors.New("certificate file does not contain a certificate")
	}
	return string(contents), certificates, nil
}

func isSelfSignedCertificate(certificate *x509.Certificate) bool {
	if !bytes.Equal(certificate.RawIssuer, certificate.RawSubject) {
		return false
	}
	return certificate.CheckSignature(
		certificate.SignatureAlgorithm,
		certificate.RawTBSCertificate,
		certificate.Signature,
	) == nil
}
