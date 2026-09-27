package dbtls

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
)

const maxServerIdentityPEMSize = 1 << 20

func verifiedChainMatchingConfiguredCertificates(
	verifiedChains [][]*x509.Certificate,
	configured []*x509.Certificate,
) []*x509.Certificate {
	for _, verifiedChain := range verifiedChains {
		if len(verifiedChain) == 0 {
			continue
		}
		requiredIntermediates := []*x509.Certificate(nil)
		if len(verifiedChain) > 2 {
			requiredIntermediates = verifiedChain[1 : len(verifiedChain)-1]
		}
		if len(configured) != len(requiredIntermediates) &&
			len(configured) != len(requiredIntermediates)+1 {
			continue
		}
		matches := true
		for index, required := range requiredIntermediates {
			if !required.Equal(configured[index]) {
				matches = false
				break
			}
		}
		if !matches {
			continue
		}
		if len(configured) == len(requiredIntermediates)+1 &&
			!verifiedChain[len(verifiedChain)-1].Equal(configured[len(configured)-1]) {
			continue
		}
		return verifiedChain
	}
	return nil
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
