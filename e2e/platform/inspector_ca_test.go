//go:build e2e

package platform

import (
	"context"
	"testing"
	"time"

	"github.com/fengqi-dev/kube-loop/internal/helper"
	"github.com/fengqi-dev/kube-loop/internal/inspectorca"
	"github.com/zalando/go-keyring"
)

type inspectorCATestStore struct {
	value string
}

func (s *inspectorCATestStore) Get(_, _ string) (string, error) {
	if s.value == "" {
		return "", keyring.ErrNotFound
	}
	return s.value, nil
}

func (s *inspectorCATestStore) Set(_, _, value string) error {
	s.value = value
	return nil
}

func (s *inspectorCATestStore) Delete(_, _ string) error {
	s.value = ""
	return nil
}

func TestInspectorRootCATrustLifecycle(t *testing.T) {
	requirePlatformE2E(t)
	manager := &inspectorca.Manager{
		Secrets: &inspectorCATestStore{},
		Now:     func() time.Time { return time.Now().UTC() },
	}
	root, err := manager.EnsureRoot()
	if err != nil {
		t.Fatal(err)
	}
	client, err := helper.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = client.RemoveInspectorCA(cleanupCtx, root.CertificatePEM)
	})

	before, err := client.InspectorCAStatus(ctx, root.CertificatePEM)
	if err != nil {
		t.Fatal(err)
	}
	if before.CertificateTrusted {
		t.Fatal("new Inspector Root CA unexpectedly already trusted")
	}
	installed, err := client.InstallInspectorCA(ctx, root.CertificatePEM)
	if err != nil {
		t.Fatal(err)
	}
	if !installed.CertificateTrusted {
		t.Fatal("Helper did not confirm installed Inspector Root CA")
	}
	status, err := client.InspectorCAStatus(ctx, root.CertificatePEM)
	if err != nil {
		t.Fatal(err)
	}
	if !status.CertificateTrusted {
		t.Fatal("installed Inspector Root CA is not reported as trusted")
	}
	removed, err := client.RemoveInspectorCA(ctx, root.CertificatePEM)
	if err != nil {
		t.Fatal(err)
	}
	if removed.CertificateTrusted {
		t.Fatal("removed Inspector Root CA is still reported as trusted")
	}
}
