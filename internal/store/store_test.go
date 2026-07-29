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
