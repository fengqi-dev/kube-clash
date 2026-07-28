package cluster

import "testing"

func TestGatewayDeploymentIsUnprivileged(t *testing.T) {
	deployment := gatewayDeployment("kube-clash-gateway:test")
	pod := deployment.Spec.Template.Spec
	if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken {
		t.Fatal("service account token must not be mounted")
	}
	container := pod.Containers[0]
	if container.SecurityContext == nil ||
		container.SecurityContext.AllowPrivilegeEscalation == nil ||
		*container.SecurityContext.AllowPrivilegeEscalation {
		t.Fatal("privilege escalation must be disabled")
	}
	if container.ImagePullPolicy != "IfNotPresent" {
		t.Fatalf("unexpected image pull policy %q", container.ImagePullPolicy)
	}
}

func TestGatewayLatestImageIsAlwaysPulled(t *testing.T) {
	deployment := gatewayDeployment("ghcr.io/fengqi-dev/kube-clash/gateway:latest")
	container := deployment.Spec.Template.Spec.Containers[0]
	if container.ImagePullPolicy != "Always" {
		t.Fatalf("latest Gateway image pull policy = %q, want Always", container.ImagePullPolicy)
	}
}
