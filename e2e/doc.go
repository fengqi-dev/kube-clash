// Package e2e contains Minikube end-to-end tests for KubeLoop core paths.
//
// Covered flows:
//   - Port Forward (TCP) — e2e/portfwd_test.go
//   - Exchange (TCP/UDP) — e2e/intercept_test.go
//   - Mirror (TCP/UDP)   — e2e/mirror_test.go
//   - Preview (TCP/UDP)  — e2e/preview_test.go
//   - Gateway / SOCKS TCP+UDP echo + DNS — gateway_test.go, session_test.go
//
// Run against a local Minikube cluster:
//
//	./e2e/run.sh
//
// Or:
//
//	KUBELOOP_E2E=1 go test -tags=e2e ./e2e -count=1 -timeout=20m -v
package e2e
