package cluster

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
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

func TestFindReadyGatewayPodPrefersCurrentPod(t *testing.T) {
	oldCreated := metav1.NewTime(time.Now().Add(-time.Minute))
	deletingAt := metav1.Now()
	currentCreated := metav1.Now()
	ready := []corev1.PodCondition{{
		Type: corev1.PodReady, Status: corev1.ConditionTrue,
	}}
	client := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "gateway-old",
				Namespace:         GatewayNamespace,
				Labels:            gatewayLabels,
				CreationTimestamp: oldCreated,
				DeletionTimestamp: &deletingAt,
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning, PodIP: "10.0.0.1", Conditions: ready,
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "gateway-current",
				Namespace:         GatewayNamespace,
				Labels:            gatewayLabels,
				CreationTimestamp: currentCreated,
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning, PodIP: "10.0.0.2", Conditions: ready,
			},
		},
	)

	info, err := findReadyGatewayPod(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "gateway-current" || info.IP != "10.0.0.2" {
		t.Fatalf("gateway = %+v, want current ready pod", info)
	}
}

func TestGatewayDeploymentRolledOut(t *testing.T) {
	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Generation: 2},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status: appsv1.DeploymentStatus{
			ObservedGeneration: 2,
			Replicas:           1,
			UpdatedReplicas:    1,
			ReadyReplicas:      1,
			AvailableReplicas:  1,
		},
	}
	if !gatewayDeploymentRolledOut(deployment, 2) {
		t.Fatal("completed rollout was not recognized")
	}

	deployment.Status.Replicas = 2
	if gatewayDeploymentRolledOut(deployment, 2) {
		t.Fatal("rollout with an old replica was considered complete")
	}
	deployment.Status.Replicas = 1
	deployment.Status.ObservedGeneration = 1
	if gatewayDeploymentRolledOut(deployment, 2) {
		t.Fatal("unobserved rollout was considered complete")
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
