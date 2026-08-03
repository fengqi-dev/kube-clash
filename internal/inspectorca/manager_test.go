package inspectorca

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

type memorySecrets struct {
	value string
}

func (s *memorySecrets) Get(_, _ string) (string, error) {
	if s.value == "" {
		return "", keyring.ErrNotFound
	}
	return s.value, nil
}

func (s *memorySecrets) Set(_, _, value string) error {
	s.value = value
	return nil
}

func (s *memorySecrets) Delete(_, _ string) error {
	if s.value == "" {
		return keyring.ErrNotFound
	}
	s.value = ""
	return nil
}

func TestRootAndIntermediateLifecycle(t *testing.T) {
	now := time.Date(2026, time.August, 3, 6, 0, 0, 0, time.UTC)
	secrets := &memorySecrets{}
	manager := &Manager{Secrets: secrets, Now: func() time.Time { return now }}

	status, err := manager.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.Present {
		t.Fatal("Root CA unexpectedly present")
	}

	root, err := manager.EnsureRoot()
	if err != nil {
		t.Fatal(err)
	}
	again, err := manager.EnsureRoot()
	if err != nil {
		t.Fatal(err)
	}
	if Fingerprint(root.Certificate) != Fingerprint(again.Certificate) {
		t.Fatal("EnsureRoot replaced the existing Root CA")
	}

	status, err = manager.Status()
	if err != nil {
		t.Fatal(err)
	}
	if !status.Present || status.Fingerprint == "" ||
		!status.NotAfter.Equal(root.Certificate.NotAfter) {
		t.Fatalf("unexpected status: %+v", status)
	}

	material, err := manager.IssueIntermediate("session-test", []byte("test-upstream-ca"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(material.CertificatePEM)
	if block == nil {
		t.Fatal("Intermediate certificate is not PEM")
	}
	intermediate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !intermediate.IsCA || intermediate.MaxPathLen != 0 {
		t.Fatal("Intermediate certificate constraints are invalid")
	}
	if err := intermediate.CheckSignatureFrom(root.Certificate); err != nil {
		t.Fatalf("Intermediate signature: %v", err)
	}
	if intermediate.NotAfter.After(now.Add(24 * time.Hour)) {
		t.Fatalf("Intermediate validity exceeds 24 hours: %s", intermediate.NotAfter)
	}
	if string(material.UpstreamCAPEM) != "test-upstream-ca" {
		t.Fatal("upstream CA bundle was not preserved")
	}

	if err := manager.DeleteRoot(); err != nil {
		t.Fatal(err)
	}
	if err := manager.DeleteRoot(); err != nil {
		t.Fatal(err)
	}
	_, err = manager.LoadRoot()
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("LoadRoot error = %v, want ErrNotFound", err)
	}
}

func TestStoredRootRejectsMismatchedKey(t *testing.T) {
	now := time.Now().UTC()
	first, err := generateRoot(now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := generateRoot(now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseRoot(first.CertificatePEM, second.PrivateKeyPEM); err == nil {
		t.Fatal("mismatched Root CA key was accepted")
	}
}
