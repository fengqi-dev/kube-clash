package helper

import (
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/inspectorca"
	"github.com/zalando/go-keyring"
)

type certificateTestStore struct {
	value string
}

func (s *certificateTestStore) Get(_, _ string) (string, error) {
	if s.value == "" {
		return "", keyring.ErrNotFound
	}
	return s.value, nil
}

func (s *certificateTestStore) Set(_, _, value string) error {
	s.value = value
	return nil
}

func (s *certificateTestStore) Delete(_, _ string) error {
	s.value = ""
	return nil
}

func TestValidateInspectorRootCertificate(t *testing.T) {
	manager := &inspectorca.Manager{
		Secrets: &certificateTestStore{},
		Now:     func() time.Time { return time.Now().UTC() },
	}
	root, err := manager.EnsureRoot()
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := validateInspectorRootCertificate(root.CertificatePEM)
	if err != nil {
		t.Fatal(err)
	}
	if certificate.sha1 == "" || certificate.sha256 == "" {
		t.Fatal("expected certificate fingerprints")
	}
}

func TestValidateInspectorRootCertificateRejectsArbitraryPEM(t *testing.T) {
	if _, err := validateInspectorRootCertificate([]byte("not a certificate")); err == nil {
		t.Fatal("expected invalid certificate to be rejected")
	}
}
