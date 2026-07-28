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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

const (
	GatewayNamespace = "kube-clash-system"
	GatewayName      = "kube-clash-gateway"
	GatewayPort      = 1080
)

var gatewayLabels = map[string]string{
	"app.kubernetes.io/name":       GatewayName,
	"app.kubernetes.io/part-of":    "kube-clash",
	"app.kubernetes.io/managed-by": "kube-clash",
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

func (p *Provider) EnsureGateway(ctx context.Context, contextName, image string) (string, error) {
	if image == "" {
		return "", errors.New("gateway image is required")
	}
	client, err := p.client(contextName)
	if err != nil {
		return "", err
	}
	if err := ensureNamespace(ctx, client); err != nil {
		return "", err
	}
	if err := ensureDeployment(ctx, client, image); err != nil {
		return "", err
	}
	return waitForGatewayPod(ctx, client)
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
		if existing.Labels["app.kubernetes.io/managed-by"] != "kube-clash" {
			return errors.New("gateway deployment exists but is not managed by kube-clash")
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
	allowPrivilegeEscalation := false
	readOnlyRootFilesystem := true
	pullPolicy := corev1.PullIfNotPresent
	if strings.HasSuffix(image, ":latest") {
		pullPolicy = corev1.PullAlways
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
					AutomountServiceAccountToken: boolPointer(false),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot:   &runAsNonRoot,
						SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
					Containers: []corev1.Container{{
						Name:            "gateway",
						Image:           image,
						ImagePullPolicy: pullPolicy,
						Ports: []corev1.ContainerPort{{
							Name: "tunnel", ContainerPort: GatewayPort, Protocol: corev1.ProtocolTCP,
						}},
						SecurityContext: &corev1.SecurityContext{
							AllowPrivilegeEscalation: &allowPrivilegeEscalation,
							ReadOnlyRootFilesystem:   &readOnlyRootFilesystem,
							Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
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
					}},
				},
			},
		},
	}
}

func waitForGatewayPod(ctx context.Context, client kubernetes.Interface) (string, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		list, err := client.CoreV1().Pods(GatewayNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app.kubernetes.io/name=" + GatewayName,
		})
		if err != nil {
			return "", fmt.Errorf("list gateway pods: %w", err)
		}
		for _, pod := range list.Items {
			if pod.Status.Phase == corev1.PodRunning && podReady(pod) {
				return pod.Name, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("wait for gateway: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (p *Provider) StartPortForward(
	ctx context.Context, contextName, podName string, remotePort uint16,
) (PortForward, error) {
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
		"/api/v1/namespaces/%s/pods/%s/portforward", GatewayNamespace, podName,
	)
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, serverURL)
	stop := make(chan struct{})
	ready := make(chan struct{})
	done := make(chan error, 1)
	errorsOutput := &lockedBuffer{}
	forward, err := portforward.NewOnAddresses(
		dialer, []string{"127.0.0.1"}, []string{fmt.Sprintf("0:%d", remotePort)},
		stop, ready, io.Discard, errorsOutput,
	)
	if err != nil {
		return nil, fmt.Errorf("create gateway port-forward: %w", err)
	}
	go func() { done <- forward.ForwardPorts() }()
	select {
	case <-ready:
	case err := <-done:
		return nil, fmt.Errorf("start gateway port-forward: %w: %s", err, errorsOutput.String())
	case <-ctx.Done():
		close(stop)
		return nil, ctx.Err()
	}
	ports, err := forward.GetPorts()
	if err != nil || len(ports) != 1 {
		close(stop)
		return nil, fmt.Errorf("get gateway local port: %w", err)
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

func boolPointer(value bool) *bool { return &value }

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
