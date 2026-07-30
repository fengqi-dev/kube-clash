package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetUI("minikube", "default"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetConnected("minikube", "default", true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPortForwards("minikube", []PortForwardSpec{{
		Namespace: "default", Kind: "service", Name: "api", RemotePort: 80, LocalPort: 8080,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetExchanges("minikube", []ExchangeSpec{{
		Namespace: "default", Service: "api",
		Ports: []PortMapping{{ServicePort: 80, Protocol: "TCP", LocalHost: "127.0.0.1", LocalPort: 3000}},
	}}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	snap := reloaded.Snapshot()
	if snap.UI.LastContext != "minikube" || snap.UI.LastNamespace != "default" {
		t.Fatalf("ui=%#v", snap.UI)
	}
	cluster := reloaded.Cluster("minikube")
	if !cluster.Connected || len(cluster.PortForwards) != 1 || len(cluster.Exchanges) != 1 {
		t.Fatalf("cluster=%#v", cluster)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestClearSessionIntents(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetPortForwards("minikube", []PortForwardSpec{{
		Namespace: "default", Kind: "pod", Name: "web", RemotePort: 80, LocalPort: 8080,
	}})
	_ = s.SetExchanges("minikube", []ExchangeSpec{{
		Namespace: "default", Service: "api",
		Ports: []PortMapping{{ServicePort: 80, Protocol: "TCP", LocalHost: "127.0.0.1", LocalPort: 3000}},
	}})
	_ = s.SetMirrors("minikube", []MirrorSpec{{
		Namespace: "default", Service: "web",
		Ports: []PortMapping{{ServicePort: 80, Protocol: "TCP", LocalHost: "127.0.0.1", LocalPort: 3001}},
	}})
	_ = s.SetPreviews("minikube", []PreviewSpec{{
		Namespace: "default", Name: "local-api",
		Ports: []PortMapping{{ServicePort: 80, Protocol: "TCP", LocalHost: "127.0.0.1", LocalPort: 3002}},
	}})
	_ = s.SetHostAliases("minikube", []HostAliasSpec{{Domain: "app.dev", IP: "10.96.0.50"}})

	counts := s.SessionIntentCounts()
	if counts.PodPortForwards != 1 || counts.Exchanges != 1 || counts.Mirrors != 1 {
		t.Fatalf("counts=%#v", counts)
	}
	if err := s.ClearSessionIntents(); err != nil {
		t.Fatal(err)
	}
	cluster := s.Cluster("minikube")
	if len(cluster.PortForwards) != 0 || len(cluster.Exchanges) != 0 || len(cluster.Mirrors) != 0 {
		t.Fatalf("session intents not cleared: %#v", cluster)
	}
	if len(cluster.Previews) != 1 || len(cluster.HostAliases) != 1 {
		t.Fatalf("previews/host aliases should remain: %#v", cluster)
	}
	if got := s.SessionIntentCounts(); got != (SessionIntentCounts{}) {
		t.Fatalf("expected zero counts, got %#v", got)
	}
}

func TestHostAliasesClear(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetHostAliases("minikube", []HostAliasSpec{
		{Domain: "app.dev", IP: "10.96.0.50"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(s.HostAliases("minikube")) != 1 {
		t.Fatal("expected one host alias")
	}
	if err := s.SetHostAliases("minikube", nil); err != nil {
		t.Fatal(err)
	}
	if got := s.HostAliases("minikube"); len(got) != 0 {
		t.Fatalf("expected cleared host aliases, got %#v", got)
	}
	if s.Cluster("minikube").HostAliases != nil {
		t.Fatalf("stored HostAliases should be nil after clear: %#v", s.Cluster("minikube").HostAliases)
	}
}

func TestOnlyOneConnectedContext(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetConnected("a", "default", true)
	_ = s.SetConnected("b", "apps", true)
	if s.Cluster("a").Connected {
		t.Fatal("expected previous connected flag cleared")
	}
	if !s.Cluster("b").Connected || s.Cluster("b").Namespace != "apps" {
		t.Fatalf("b=%#v", s.Cluster("b"))
	}
}
