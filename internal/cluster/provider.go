package cluster

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type Discovery struct {
	PodCIDRs       []string `json:"podCIDRs"`
	ServiceCIDRs   []string `json:"serviceCIDRs"`
	ServiceIPs     []string `json:"serviceIPs"`
	DNSServer      string   `json:"dnsServer"`
	ClusterDomains []string `json:"clusterDomains,omitempty"`
	Pods           int      `json:"pods"`
	Services       int      `json:"services"`
	Deployments    int      `json:"deployments"`
}

// ServicePortInfo describes one Service port for the intercept UI.
type ServicePortInfo struct {
	Name     string `json:"name"`
	Port     int32  `json:"port"`
	Protocol string `json:"protocol"`
}

// ServiceInfo is a ClusterIP Service that can be intercepted.
type ServiceInfo struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	ClusterIP string            `json:"clusterIP"`
	Ports     []ServicePortInfo `json:"ports"`
}

func (p *Provider) RESTConfig(contextName string) (*rest.Config, error) {
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	cfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(p.loadingRules(), overrides)
	restConfig, err := cfg.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load context %q: %w", contextName, err)
	}
	p.mu.RLock()
	userAgent := p.userAgent
	p.mu.RUnlock()
	if userAgent == "" {
		userAgent = "kube-loop/dev"
	}
	restConfig.UserAgent = userAgent
	return restConfig, nil
}

func (p *Provider) client(contextName string) (kubernetes.Interface, error) {
	cfg, err := p.RESTConfig(contextName)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	return client, nil
}

func (p *Provider) Namespaces(ctx context.Context, contextName string) ([]string, error) {
	client, err := p.client(contextName)
	if err != nil {
		return nil, err
	}
	list, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}
	names := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		names = append(names, item.Name)
	}
	sort.Strings(names)
	return names, nil
}

func (p *Provider) ListServices(
	ctx context.Context, contextName, namespace string,
) ([]ServiceInfo, error) {
	listNS := apiNamespace(namespace)
	client, err := p.client(contextName)
	if err != nil {
		return nil, err
	}
	list, err := client.CoreV1().Services(listNS).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	refs := make([]*corev1.Service, 0, len(list.Items))
	for i := range list.Items {
		refs = append(refs, &list.Items[i])
	}
	return serviceInfosFromList(refs), nil
}

// apiNamespace maps UI namespace selection to the Kubernetes API namespace.
// "*" means all namespaces; empty falls back to default.
func apiNamespace(namespace string) string {
	if namespace == "*" {
		return ""
	}
	if namespace == "" {
		return "default"
	}
	return namespace
}

// Discover collects routable CIDRs / ClusterIPs. namespaces empty = all namespaces.
// Node / deployment / kube-system reads are best-effort so ns-scoped users can connect.
func (p *Provider) Discover(ctx context.Context, contextName string, namespaces []string) (Discovery, error) {
	client, err := p.client(contextName)
	if err != nil {
		return Discovery{}, err
	}

	podCIDRs := make(map[string]struct{})
	if nodes, nodeErr := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{}); nodeErr == nil {
		for _, node := range nodes.Items {
			for _, cidr := range node.Spec.PodCIDRs {
				if prefix, parseErr := netip.ParsePrefix(cidr); parseErr == nil {
					podCIDRs[prefix.Masked().String()] = struct{}{}
				}
			}
			if node.Spec.PodCIDR != "" {
				if prefix, parseErr := netip.ParsePrefix(node.Spec.PodCIDR); parseErr == nil {
					podCIDRs[prefix.Masked().String()] = struct{}{}
				}
			}
		}
	}

	var pods []corev1.Pod
	var services []corev1.Service
	if len(namespaces) == 0 {
		podList, podErr := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
		if podErr != nil {
			return Discovery{}, fmt.Errorf("list pods: %w", podErr)
		}
		pods = podList.Items
		svcList, svcErr := client.CoreV1().Services("").List(ctx, metav1.ListOptions{})
		if svcErr != nil {
			return Discovery{}, fmt.Errorf("list services: %w", svcErr)
		}
		services = svcList.Items
	} else {
		for _, ns := range namespaces {
			podList, podErr := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
			if podErr != nil {
				return Discovery{}, fmt.Errorf("list pods in %s: %w", ns, podErr)
			}
			pods = append(pods, podList.Items...)
			svcList, svcErr := client.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
			if svcErr != nil {
				return Discovery{}, fmt.Errorf("list services in %s: %w", ns, svcErr)
			}
			services = append(services, svcList.Items...)
		}
		// Best-effort CoreDNS lookup outside scoped namespaces.
		for _, dnsNS := range []string{"kube-system"} {
			if svc, getErr := client.CoreV1().Services(dnsNS).Get(ctx, "kube-dns", metav1.GetOptions{}); getErr == nil {
				services = append(services, *svc)
			} else if svc, getErr := client.CoreV1().Services(dnsNS).Get(ctx, "coredns", metav1.GetOptions{}); getErr == nil {
				services = append(services, *svc)
			}
		}
	}

	if len(podCIDRs) == 0 {
		for _, pod := range pods {
			for _, raw := range pod.Status.PodIPs {
				if ip, parseErr := netip.ParseAddr(raw.IP); parseErr == nil {
					podCIDRs[netip.PrefixFrom(ip, ip.BitLen()).String()] = struct{}{}
				}
			}
			if pod.Status.PodIP != "" {
				if ip, parseErr := netip.ParseAddr(pod.Status.PodIP); parseErr == nil {
					podCIDRs[netip.PrefixFrom(ip, ip.BitLen()).String()] = struct{}{}
				}
			}
		}
	}

	serviceIPs := make(map[string]struct{})
	dnsServer := ""
	for _, service := range services {
		for _, raw := range service.Spec.ClusterIPs {
			if ip, parseErr := netip.ParseAddr(raw); parseErr == nil {
				serviceIPs[ip.String()] = struct{}{}
				if service.Namespace == "kube-system" &&
					(service.Name == "kube-dns" || service.Name == "coredns") {
					dnsServer = ip.String()
				}
			}
		}
	}

	deployments := 0
	if list, depErr := client.AppsV1().Deployments("").List(ctx, metav1.ListOptions{}); depErr == nil {
		deployments = len(list.Items)
	}

	return Discovery{
		PodCIDRs:     sortedKeys(podCIDRs),
		ServiceCIDRs: discoverServiceCIDRs(ctx, client),
		ServiceIPs:   sortedKeys(serviceIPs),
		DNSServer:    dnsServer,
		Pods:         len(pods),
		Services:     len(services),
		Deployments:  deployments,
	}, nil
}

func discoverServiceCIDRs(ctx context.Context, client kubernetes.Interface) []string {
	cidrs := make(map[string]struct{})
	if list, err := client.NetworkingV1().ServiceCIDRs().List(ctx, metav1.ListOptions{}); err == nil {
		for _, item := range list.Items {
			for _, raw := range item.Spec.CIDRs {
				if prefix, parseErr := netip.ParsePrefix(raw); parseErr == nil {
					cidrs[prefix.Masked().String()] = struct{}{}
				}
			}
		}
	}
	if len(cidrs) == 0 {
		if subnet, err := serviceSubnetFromKubeadm(ctx, client); err == nil && subnet != "" {
			if prefix, parseErr := netip.ParsePrefix(subnet); parseErr == nil {
				cidrs[prefix.Masked().String()] = struct{}{}
			}
		}
	}
	return sortedKeys(cidrs)
}

func serviceSubnetFromKubeadm(ctx context.Context, client kubernetes.Interface) (string, error) {
	configMap, err := client.CoreV1().ConfigMaps("kube-system").Get(
		ctx, "kubeadm-config", metav1.GetOptions{},
	)
	if err != nil {
		return "", err
	}
	raw, ok := configMap.Data["ClusterConfiguration"]
	if !ok || strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("kubeadm-config missing ClusterConfiguration")
	}
	var parsed struct {
		Networking struct {
			ServiceSubnet string `yaml:"serviceSubnet"`
		} `yaml:"networking"`
	}
	if err := yaml.Unmarshal([]byte(raw), &parsed); err != nil {
		return "", err
	}
	return strings.TrimSpace(parsed.Networking.ServiceSubnet), nil
}

func sortedKeys(values map[string]struct{}) []string {
	items := make([]string, 0, len(values))
	for item := range values {
		items = append(items, item)
	}
	sort.Strings(items)
	return items
}
