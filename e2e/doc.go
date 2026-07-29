// Package e2e contains Minikube end-to-end tests for KubeLoop core paths.
//
// Run against a local Minikube cluster:
//
//	./e2e/run.sh
//
// Or:
//
//	KUBELOOP_E2E=1 go test -tags=e2e ./e2e -count=1 -timeout=20m -v
package e2e
