package cluster

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestGatewayDeploymentIsUnprivileged(t *testing.T) {
	deployment := gatewayDeployment("kube-loop-gateway:test")
	pod := deployment.Spec.Template.Spec
	if pod.AutomountServiceAccountToken == nil || *pod.AutomountServiceAccountToken {
		t.Fatal("service account token must not be mounted")
	}
	if pod.SecurityContext == nil || pod.SecurityContext.RunAsNonRoot == nil ||
		!*pod.SecurityContext.RunAsNonRoot {
		t.Fatal("pod must run as non-root")
	}
	if pod.SecurityContext.FSGroup == nil || *pod.SecurityContext.FSGroup != 65532 {
		t.Fatal("pod must make the shared runtime volume writable by the sidecar")
	}
	if len(pod.Containers) != 2 {
		t.Fatalf("container count = %d, want 2", len(pod.Containers))
	}
	for _, container := range pod.Containers {
		if container.SecurityContext == nil ||
			container.SecurityContext.AllowPrivilegeEscalation == nil ||
			*container.SecurityContext.AllowPrivilegeEscalation ||
			container.SecurityContext.ReadOnlyRootFilesystem == nil ||
			!*container.SecurityContext.ReadOnlyRootFilesystem {
			t.Fatalf("%s must use a locked-down security context", container.Name)
		}
		if len(container.SecurityContext.Capabilities.Drop) != 1 ||
			container.SecurityContext.Capabilities.Drop[0] != "ALL" {
			t.Fatalf("%s must drop all capabilities", container.Name)
		}
		if len(container.VolumeMounts) != 1 ||
			container.VolumeMounts[0].Name != "inspector-runtime" {
			t.Fatalf("%s must mount inspector runtime", container.Name)
		}
		if container.Resources.Requests.Cpu().IsZero() ||
			container.Resources.Requests.Memory().IsZero() ||
			container.Resources.Limits.Cpu().IsZero() ||
			container.Resources.Limits.Memory().IsZero() {
			t.Fatalf("%s must define CPU and memory requests and limits", container.Name)
		}
	}
	if len(pod.Volumes) != 1 || pod.Volumes[0].EmptyDir == nil ||
		pod.Volumes[0].EmptyDir.Medium != corev1.StorageMediumMemory {
		t.Fatal("inspector runtime must use a memory-backed emptyDir")
	}

	gateway, agent := pod.Containers[0], pod.Containers[1]
	if gateway.ImagePullPolicy != corev1.PullIfNotPresent ||
		agent.ImagePullPolicy != corev1.PullIfNotPresent {
		t.Fatalf("unexpected pull policies: gateway=%q agent=%q",
			gateway.ImagePullPolicy, agent.ImagePullPolicy)
	}
	if agent.Image != "kube-loop-inspector-agent:test" {
		t.Fatalf("agent image = %q", agent.Image)
	}
	if agent.ReadinessProbe == nil || agent.ReadinessProbe.Exec == nil {
		t.Fatal("agent must have an exec readiness probe")
	}
}

func TestGatewayLatestImageIsAlwaysPulled(t *testing.T) {
	deployment := gatewayDeployment("ghcr.io/fengqi-dev/kube-loop/gateway:latest")
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.ImagePullPolicy != corev1.PullAlways {
			t.Fatalf("latest %s image pull policy = %q, want Always",
				container.Name, container.ImagePullPolicy)
		}
	}
}

func TestInspectorAgentImage(t *testing.T) {
	tests := map[string]string{
		"":                         "ghcr.io/fengqi-dev/kube-loop/inspector-agent:latest",
		"gateway:test":             "inspector-agent:test",
		"kube-loop-gateway:v1.7.0": "kube-loop-inspector-agent:v1.7.0",
		"ghcr.io/fengqi-dev/kube-loop/gateway@sha256:deadbeef": "ghcr.io/fengqi-dev/kube-loop/inspector-agent@sha256:deadbeef",
		"example.com/custom/image:v1":                          "ghcr.io/fengqi-dev/kube-loop/inspector-agent:latest",
	}
	for input, want := range tests {
		if got := InspectorAgentImage(input); got != want {
			t.Errorf("InspectorAgentImage(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestGatewayInstallManifestIncludesInspectorSidecar(t *testing.T) {
	manifest := GatewayInstallManifest("example.com/kube-loop-gateway:v1")
	for _, want := range []string{
		"name: inspector-agent",
		"image: example.com/kube-loop-inspector-agent:v1",
		"--inspector-agent-socket=" + InspectorSocketPath,
		"--socket=" + InspectorSocketPath,
		"automountServiceAccountToken: false",
		"runAsNonRoot: true",
		"fsGroup: 65532",
		"type: RuntimeDefault",
		"allowPrivilegeEscalation: false",
		"readOnlyRootFilesystem: true",
		"medium: Memory",
	} {
		if !strings.Contains(manifest, want) {
			t.Errorf("manifest does not contain %q", want)
		}
	}
}
