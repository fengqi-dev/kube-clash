package cluster

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

const (
	GatewayNamespace    = "kubeloop-system"
	GatewayName         = "kubeloop-gateway"
	GatewayPort         = 1080
	InspectorSocketPath = "/var/run/kubeloop-inspector/agent.sock"
)

// GatewayInfo identifies the running in-cluster Gateway Pod.
type GatewayInfo struct {
	Name string
	IP   string
}

var gatewayLabels = map[string]string{
	"app.kubernetes.io/name":       GatewayName,
	"app.kubernetes.io/part-of":    "kubeloop",
	"app.kubernetes.io/managed-by": "kubeloop",
}

type Forwarder struct {
	LocalPort uint16
	stop      chan struct{}
	done      chan error
	once      sync.Once
}

type PortForward interface {
	Address() string
	Close() error
}

func (f *Forwarder) Address() string {
	return fmt.Sprintf("127.0.0.1:%d", f.LocalPort)
}

func (f *Forwarder) Close() error {
	f.once.Do(func() { close(f.stop) })
	select {
	case err := <-f.done:
		return err
	case <-time.After(5 * time.Second):
		return errors.New("timed out stopping gateway port-forward")
	}
}

func (p *Provider) EnsureGateway(ctx context.Context, contextName, image string) (GatewayInfo, error) {
	if image == "" {
		return GatewayInfo{}, errors.New("gateway image is required")
	}
	client, err := p.client(contextName)
	if err != nil {
		return GatewayInfo{}, err
	}
	if err := ensureNamespace(ctx, client); err != nil {
		return GatewayInfo{}, err
	}
	if err := ensureDeployment(ctx, client, image); err != nil {
		return GatewayInfo{}, err
	}
	return waitForGatewayPod(ctx, client)
}

// GetGateway finds an already-running Gateway Pod without installing resources.
func (p *Provider) GetGateway(ctx context.Context, contextName string) (GatewayInfo, error) {
	client, err := p.client(contextName)
	if err != nil {
		return GatewayInfo{}, err
	}
	info, err := findReadyGatewayPod(ctx, client)
	if err != nil {
		return GatewayInfo{}, err
	}
	return info, nil
}

// GatewayInstallManifest returns a YAML snippet admins can apply when the user
// lacks install RBAC.
func GatewayInstallManifest(image string) string {
	if image == "" {
		image = "ghcr.io/fengqi-dev/kube-loop/gateway:latest"
	}
	inspectorImage := InspectorAgentImage(image)
	return fmt.Sprintf(`apiVersion: v1
kind: Namespace
metadata:
  name: %s
  labels:
    app.kubernetes.io/name: %s
    app.kubernetes.io/part-of: kubeloop
    app.kubernetes.io/managed-by: kubeloop
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
  labels:
    app.kubernetes.io/name: %s
    app.kubernetes.io/part-of: kubeloop
    app.kubernetes.io/managed-by: kubeloop
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: %s
  template:
    metadata:
      labels:
        app.kubernetes.io/name: %s
        app.kubernetes.io/part-of: kubeloop
        app.kubernetes.io/managed-by: kubeloop
    spec:
      automountServiceAccountToken: false
      securityContext:
        runAsNonRoot: true
        fsGroup: 65532
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: gateway
          image: %s
          args:
            - --inspector-agent-socket=%s
          ports:
            - name: tunnel
              containerPort: %d
              protocol: TCP
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
          resources:
            requests:
              cpu: 10m
              memory: 16Mi
            limits:
              cpu: 500m
              memory: 128Mi
          volumeMounts:
            - name: inspector-runtime
              mountPath: /var/run/kubeloop-inspector
        - name: inspector-agent
          image: %s
          args:
            - --socket=%s
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop:
                - ALL
          resources:
            requests:
              cpu: 20m
              memory: 32Mi
            limits:
              cpu: "1"
              memory: 256Mi
          readinessProbe:
            exec:
              command:
                - /kube-loop-inspector-agent
                - --probe
                - --socket=%s
            initialDelaySeconds: 1
            periodSeconds: 2
          volumeMounts:
            - name: inspector-runtime
              mountPath: /var/run/kubeloop-inspector
      volumes:
        - name: inspector-runtime
          emptyDir:
            medium: Memory
`, GatewayNamespace, GatewayName, GatewayName, GatewayNamespace, GatewayName,
		GatewayName, GatewayName, image, InspectorSocketPath, GatewayPort,
		inspectorImage, InspectorSocketPath, InspectorSocketPath)
}

func InspectorAgentImage(gatewayImage string) string {
	if gatewayImage == "" {
		return "ghcr.io/fengqi-dev/kube-loop/inspector-agent:latest"
	}
	slash := strings.LastIndex(gatewayImage, "/")
	prefix := gatewayImage[:slash+1]
	name := gatewayImage[slash+1:]
	if strings.Contains(name, "gateway") {
		return prefix + strings.Replace(name, "gateway", "inspector-agent", 1)
	}
	return "ghcr.io/fengqi-dev/kube-loop/inspector-agent:latest"
}

func ensureNamespace(ctx context.Context, client kubernetes.Interface) error {
	_, err := client.CoreV1().Namespaces().Get(ctx, GatewayNamespace, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get gateway namespace: %w", err)
	}
	_, err = client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: GatewayNamespace, Labels: gatewayLabels},
	}, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create gateway namespace: %w", err)
	}
	return nil
}

func ensureDeployment(ctx context.Context, client kubernetes.Interface, image string) error {
	expected := gatewayDeployment(image)
	existing, err := client.AppsV1().Deployments(GatewayNamespace).Get(
		ctx, GatewayName, metav1.GetOptions{},
	)
	switch {
	case apierrors.IsNotFound(err):
		_, err = client.AppsV1().Deployments(GatewayNamespace).Create(
			ctx, expected, metav1.CreateOptions{},
		)
	case err != nil:
		return fmt.Errorf("get gateway deployment: %w", err)
	default:
		if existing.Labels["app.kubernetes.io/managed-by"] != "kubeloop" {
			return errors.New("gateway deployment exists but is not managed by kube-loop")
		}
		expected.ResourceVersion = existing.ResourceVersion
		_, err = client.AppsV1().Deployments(GatewayNamespace).Update(
			ctx, expected, metav1.UpdateOptions{},
		)
	}
	if err != nil {
		return fmt.Errorf("apply gateway deployment: %w", err)
	}
	return nil
}

func gatewayDeployment(image string) *appsv1.Deployment {
	replicas := int32(1)
	runAsNonRoot := true
	fsGroup := int64(65532)
	allowPrivilegeEscalation := false
	readOnlyRootFilesystem := true
	pullPolicy := corev1.PullIfNotPresent
	if strings.HasSuffix(image, ":latest") {
		pullPolicy = corev1.PullAlways
	}
	inspectorImage := InspectorAgentImage(image)
	inspectorPullPolicy := corev1.PullIfNotPresent
	if strings.HasSuffix(inspectorImage, ":latest") {
		inspectorPullPolicy = corev1.PullAlways
	}
	securityContext := func() *corev1.SecurityContext {
		return &corev1.SecurityContext{
			AllowPrivilegeEscalation: &allowPrivilegeEscalation,
			ReadOnlyRootFilesystem:   &readOnlyRootFilesystem,
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		}
	}
	volumeMount := corev1.VolumeMount{
		Name: "inspector-runtime", MountPath: "/var/run/kubeloop-inspector",
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: GatewayName, Namespace: GatewayNamespace, Labels: gatewayLabels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{
				"app.kubernetes.io/name": GatewayName,
			}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: gatewayLabels},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken: new(false),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot:   &runAsNonRoot,
						FSGroup:        &fsGroup,
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					Containers: []corev1.Container{{
						Name:            "gateway",
						Image:           image,
						ImagePullPolicy: pullPolicy,
						Args: []string{
							"--inspector-agent-socket=" + InspectorSocketPath,
						},
						Ports: []corev1.ContainerPort{{
							Name: "tunnel", ContainerPort: GatewayPort, Protocol: corev1.ProtocolTCP,
						}},
						SecurityContext: securityContext(),
						VolumeMounts:    []corev1.VolumeMount{volumeMount},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("10m"),
								corev1.ResourceMemory: resource.MustParse("16Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("500m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								TCPSocket: &corev1.TCPSocketAction{
									Port: intstr.FromInt32(GatewayPort),
								},
							},
							InitialDelaySeconds: 1,
							PeriodSeconds:       2,
						},
					}, {
						Name:            "inspector-agent",
						Image:           inspectorImage,
						ImagePullPolicy: inspectorPullPolicy,
						Args:            []string{"--socket=" + InspectorSocketPath},
						SecurityContext: securityContext(),
						VolumeMounts:    []corev1.VolumeMount{volumeMount},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("20m"),
								corev1.ResourceMemory: resource.MustParse("32Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
						},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								Exec: &corev1.ExecAction{Command: []string{
									"/kube-loop-inspector-agent",
									"--probe",
									"--socket=" + InspectorSocketPath,
								}},
							},
							InitialDelaySeconds: 1,
							PeriodSeconds:       2,
						},
					}},
					Volumes: []corev1.Volume{{
						Name: "inspector-runtime",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{
								Medium: corev1.StorageMediumMemory,
							},
						},
					}},
				},
			},
		},
	}
}

func findReadyGatewayPod(ctx context.Context, client kubernetes.Interface) (GatewayInfo, error) {
	list, err := client.CoreV1().Pods(GatewayNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=" + GatewayName,
	})
	if err != nil {
		return GatewayInfo{}, fmt.Errorf("list gateway pods: %w", err)
	}
	for _, pod := range list.Items {
		if pod.Status.Phase != corev1.PodRunning || pod.Status.PodIP == "" {
			continue
		}
		if !podReady(pod) {
			continue
		}
		return GatewayInfo{Name: pod.Name, IP: pod.Status.PodIP}, nil
	}
	return GatewayInfo{}, errors.New("gateway pod not found; ask an admin to install kubeloop-gateway")
}

func waitForGatewayPod(ctx context.Context, client kubernetes.Interface) (GatewayInfo, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		info, err := findReadyGatewayPod(ctx, client)
		if err == nil {
			return info, nil
		}
		if apierrors.IsForbidden(err) {
			return GatewayInfo{}, err
		}
		lastErr = err
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return GatewayInfo{}, fmt.Errorf("wait for gateway: %w", lastErr)
			}
			return GatewayInfo{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

// StartPortForward opens an API Server port-forward to a Gateway Pod.
func (p *Provider) StartPortForward(
	ctx context.Context, contextName, podName string, remotePort uint16,
) (PortForward, error) {
	return p.StartPodPortForward(ctx, contextName, GatewayNamespace, podName, 0, remotePort)
}

// StartPodPortForward forwards 127.0.0.1:localPort to podName:remotePort.
// When localPort is 0, the OS allocates an ephemeral port.
func (p *Provider) StartPodPortForward(
	ctx context.Context,
	contextName, namespace, podName string,
	localPort, remotePort uint16,
) (PortForward, error) {
	if namespace == "" {
		return nil, errors.New("namespace is required")
	}
	if podName == "" {
		return nil, errors.New("pod name is required")
	}
	if remotePort == 0 {
		return nil, errors.New("remote port is required")
	}
	config, err := p.RESTConfig(contextName)
	if err != nil {
		return nil, err
	}
	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return nil, fmt.Errorf("create port-forward transport: %w", err)
	}
	serverURL, err := url.Parse(config.Host)
	if err != nil {
		return nil, fmt.Errorf("parse API server URL: %w", err)
	}
	serverURL.Path = fmt.Sprintf(
		"/api/v1/namespaces/%s/pods/%s/portforward", namespace, podName,
	)
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, serverURL)
	stop := make(chan struct{})
	ready := make(chan struct{})
	done := make(chan error, 1)
	errorsOutput := &lockedBuffer{}
	forward, err := portforward.NewOnAddresses(
		dialer,
		[]string{"127.0.0.1"},
		[]string{fmt.Sprintf("%d:%d", localPort, remotePort)},
		stop, ready, io.Discard, errorsOutput,
	)
	if err != nil {
		return nil, fmt.Errorf("create port-forward: %w", err)
	}
	go func() { done <- forward.ForwardPorts() }()
	select {
	case <-ready:
	case err := <-done:
		return nil, fmt.Errorf("start port-forward: %w: %s", err, errorsOutput.String())
	case <-ctx.Done():
		close(stop)
		return nil, ctx.Err()
	}
	ports, err := forward.GetPorts()
	if err != nil || len(ports) != 1 {
		close(stop)
		return nil, fmt.Errorf("get local port: %w", err)
	}
	return &Forwarder{LocalPort: ports[0].Local, stop: stop, done: done}, nil
}

func podReady(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

//go:fix inline
func boolPointer(value bool) *bool { return new(value) }

type lockedBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *lockedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(value)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.String()
}
