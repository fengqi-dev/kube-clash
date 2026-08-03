package helper

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

const (
	inspectorRootCommonName = "KubeLoop Inspector Root CA"
	maxInspectorRootPEMSize = 64 << 10
)

type inspectorRootCertificate struct {
	pem    []byte
	raw    []byte
	sha1   string
	sha256 string
}

func validateInspectorRootCertificate(value []byte) (inspectorRootCertificate, error) {
	if len(value) == 0 || len(value) > maxInspectorRootPEMSize {
		return inspectorRootCertificate{}, errors.New(
			"Inspector Root CA PEM must be between 1 byte and 64 KiB",
		)
	}
	block, rest := pem.Decode(value)
	if block == nil || block.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return inspectorRootCertificate{}, errors.New("Inspector Root CA PEM is invalid")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return inspectorRootCertificate{}, fmt.Errorf("parse Inspector Root CA: %w", err)
	}
	if !certificate.IsCA ||
		!certificate.BasicConstraintsValid ||
		certificate.Subject.CommonName != inspectorRootCommonName ||
		certificate.Issuer.String() != certificate.Subject.String() {
		return inspectorRootCertificate{}, errors.New("Inspector Root CA identity is invalid")
	}
	if err := certificate.CheckSignatureFrom(certificate); err != nil {
		return inspectorRootCertificate{}, errors.New("Inspector Root CA is not self-signed")
	}
	sha1Sum := sha1.Sum(certificate.Raw)
	sha256Sum := sha256.Sum256(certificate.Raw)
	return inspectorRootCertificate{
		pem:    append([]byte(nil), value...),
		raw:    append([]byte(nil), certificate.Raw...),
		sha1:   strings.ToUpper(hex.EncodeToString(sha1Sum[:])),
		sha256: strings.ToUpper(hex.EncodeToString(sha256Sum[:])),
	}, nil
}
