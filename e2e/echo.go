//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

func ensureEchoNamespace(ctx context.Context, client kubernetes.Interface) error {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		ns, err := client.CoreV1().Namespaces().Get(ctx, echoNamespace, metav1.GetOptions{})
		switch {
		case err == nil && ns.DeletionTimestamp == nil:
			return nil
		case err == nil:
			// Namespace is terminating; wait until it is gone before recreating.
		case apierrors.IsNotFound(err):
			_, createErr := client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: echoNamespace},
			}, metav1.CreateOptions{})
			if createErr == nil || apierrors.IsAlreadyExists(createErr) {
				return nil
			}
			if !apierrors.IsForbidden(createErr) || !strings.Contains(createErr.Error(), "being terminated") {
				return createErr
			}
		default:
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("namespace %s still terminating", echoNamespace)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func ensureEchoWorkload(ctx context.Context, client kubernetes.Interface) error {
	if err := ensureEchoNamespace(ctx, client); err != nil {
		return err
	}
	labels := map[string]string{"app": "kubeloop-e2e-echo"}
	replicas := int32(1)
	script := `
import socket, threading

def handle_tcp(c):
    try:
        data = c.recv(64)
        c.sendall(b"cluster-tcp:" + data)
    finally:
        c.close()

def tcp():
    s = socket.socket()
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    s.bind(("0.0.0.0", 8080))
    s.listen()
    while True:
        c, _ = s.accept()
        threading.Thread(target=handle_tcp, args=(c,), daemon=True).start()

def udp():
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    s.bind(("0.0.0.0", 9090))
    while True:
        data, addr = s.recvfrom(64)
        s.sendto(b"cluster-udp:" + data, addr)

threading.Thread(target=tcp, daemon=True).start()
udp()
`
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "echo", Namespace: echoNamespace},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:    "echo",
						Image:   "python:3.12-alpine",
						Command: []string{"python", "-u", "-c", script},
						Ports: []corev1.ContainerPort{
							{Name: "tcp", ContainerPort: 8080, Protocol: corev1.ProtocolTCP},
							{Name: "udp", ContainerPort: 9090, Protocol: corev1.ProtocolUDP},
						},
					}},
				},
			},
		},
	}
	if existing, err := client.AppsV1().Deployments(echoNamespace).Get(ctx, "echo", metav1.GetOptions{}); err == nil {
		deploy.ResourceVersion = existing.ResourceVersion
		_, err = client.AppsV1().Deployments(echoNamespace).Update(ctx, deploy, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	} else if apierrors.IsNotFound(err) {
		_, err = client.AppsV1().Deployments(echoNamespace).Create(ctx, deploy, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	} else {
		return err
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "echo", Namespace: echoNamespace},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{Name: "tcp", Port: 8080, TargetPort: intstr.FromInt32(8080), Protocol: corev1.ProtocolTCP},
				{Name: "udp", Port: 9090, TargetPort: intstr.FromInt32(9090), Protocol: corev1.ProtocolUDP},
			},
		},
	}
	if existing, err := client.CoreV1().Services(echoNamespace).Get(ctx, "echo", metav1.GetOptions{}); err == nil {
		service.ResourceVersion = existing.ResourceVersion
		service.Spec.ClusterIP = existing.Spec.ClusterIP
		_, err = client.CoreV1().Services(echoNamespace).Update(ctx, service, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	} else if apierrors.IsNotFound(err) {
		_, err = client.CoreV1().Services(echoNamespace).Create(ctx, service, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	} else {
		return err
	}

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		pods, err := client.CoreV1().Pods(echoNamespace).List(ctx, metav1.ListOptions{
			LabelSelector: "app=kubeloop-e2e-echo",
		})
		if err == nil {
			for _, pod := range pods.Items {
				if pod.Status.Phase == corev1.PodRunning {
					for _, condition := range pod.Status.Conditions {
						if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
							return nil
						}
					}
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("echo workload not ready")
}

func startLocalTCPEcho(t *testing.T, prefix string) (net.Listener, *net.TCPAddr) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 64)
				n, _ := c.Read(buf)
				_, _ = fmt.Fprintf(c, "%s:%s", prefix, string(buf[:n]))
			}(conn)
		}
	}()
	return listener, listener.Addr().(*net.TCPAddr)
}

func startLocalUDPEcho(t *testing.T, prefix string) (net.PacketConn, *net.UDPAddr) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 64)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = conn.WriteTo([]byte(fmt.Sprintf("%s:%s", prefix, string(buf[:n]))), addr)
		}
	}()
	return conn, conn.LocalAddr().(*net.UDPAddr)
}

func waitClusterProbe(
	t *testing.T,
	ctx context.Context,
	client kubernetes.Interface,
	host string,
	port int,
	protocol, payload, prefix string,
) string {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	var last string
	var lastErr error
	for time.Now().Before(deadline) {
		got, err := probeFromCluster(ctx, client, host, port, protocol, payload)
		if err == nil && strings.HasPrefix(got, prefix) {
			return got
		}
		last, lastErr = got, err
		time.Sleep(2 * time.Second)
	}
	if lastErr != nil {
		t.Fatalf("probe %s %s:%d failed: %v (last=%q)", protocol, host, port, lastErr, last)
	}
	t.Fatalf("probe %s %s:%d unexpected %q want prefix %q", protocol, host, port, last, prefix)
	return ""
}

func probeFromCluster(
	ctx context.Context,
	client kubernetes.Interface,
	host string,
	port int,
	protocol, payload string,
) (string, error) {
	name := fmt.Sprintf("probe-%s-%d", protocol, time.Now().UnixNano())
	script := fmt.Sprintf(`
import socket, sys
host, port, payload = %q, %d, %q
if %q == "udp":
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    s.settimeout(5)
    s.sendto(payload.encode(), (host, port))
    data, _ = s.recvfrom(128)
else:
    s = socket.create_connection((host, port), timeout=5)
    s.sendall(payload.encode())
    data = s.recv(128)
sys.stdout.write(data.decode(errors="replace"))
`, host, port, payload, protocol)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: echoNamespace},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{{
				Name:    "probe",
				Image:   "python:3.12-alpine",
				Command: []string{"python", "-u", "-c", script},
			}},
		},
	}
	if _, err := client.CoreV1().Pods(echoNamespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return "", err
	}
	defer func() {
		_ = client.CoreV1().Pods(echoNamespace).Delete(context.Background(), name, metav1.DeleteOptions{})
	}()

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		current, err := client.CoreV1().Pods(echoNamespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			time.Sleep(300 * time.Millisecond)
			continue
		}
		switch current.Status.Phase {
		case corev1.PodSucceeded, corev1.PodFailed:
			logs, err := client.CoreV1().Pods(echoNamespace).GetLogs(name, &corev1.PodLogOptions{}).DoRaw(ctx)
			if err != nil {
				return "", err
			}
			if current.Status.Phase == corev1.PodFailed {
				return string(logs), fmt.Errorf("probe failed: %s", string(logs))
			}
			return string(logs), nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return "", fmt.Errorf("probe pod timed out")
}
