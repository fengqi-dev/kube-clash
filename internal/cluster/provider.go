package cluster

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type ContextInfo struct {
	Name    string `json:"name"`
	Cluster string `json:"cluster"`
	Current bool   `json:"current"`
}

type Discovery struct {
	PodCIDRs     []string `json:"podCIDRs"`
	ServiceCIDRs []string `json:"serviceCIDRs"`
	ServiceIPs   []string `json:"serviceIPs"`
	DNSServer    string   `json:"dnsServer"`
	Pods         int      `json:"pods"`
	Services     int      `json:"services"`
	Deployments  int      `json:"deployments"`
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

type Provider struct {
	rules *clientcmd.ClientConfigLoadingRules
}

func NewProvider() *Provider {
	return &Provider{rules: clientcmd.NewDefaultClientConfigLoadingRules()}
}

func (p *Provider) Contexts() ([]ContextInfo, error) {
	cfg, err := p.rules.Load()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	items := make([]ContextInfo, 0, len(cfg.Contexts))
	for name, value := range cfg.Contexts {
		items = append(items, ContextInfo{
			Name: name, Cluster: value.Cluster, Current: name == cfg.CurrentContext,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Current != items[j].Current {
			return items[i].Current
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (p *Provider) RESTConfig(contextName string) (*rest.Config, error) {
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	cfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(p.rules, overrides)
	restConfig, err := cfg.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load context %q: %w", contextName, err)
	}
	restConfig.UserAgent = "kube-loop/0.1"
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
	if namespace == "" {
		namespace = "default"
	}
	client, err := p.client(contextName)
	if err != nil {
		return nil, err
	}
	list, err := client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	items := make([]ServiceInfo, 0, len(list.Items))
	for _, service := range list.Items {
		if service.Spec.ClusterIP == "" || service.Spec.ClusterIP == "None" {
			continue
		}
		if strings.EqualFold(string(service.Spec.Type), "ExternalName") {
			continue
		}
		ports := make([]ServicePortInfo, 0, len(service.Spec.Ports))
		for _, port := range service.Spec.Ports {
			protocol := string(port.Protocol)
			if protocol == "" {
				protocol = "TCP"
			}
			ports = append(ports, ServicePortInfo{
				Name: port.Name, Port: port.Port, Protocol: protocol,
			})
		}
		if len(ports) == 0 {
			continue
		}
		items = append(items, ServiceInfo{
			Name:      service.Name,
			Namespace: service.Namespace,
			ClusterIP: service.Spec.ClusterIP,
			Ports:     ports,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (p *Provider) Discover(ctx context.Context, contextName string) (Discovery, error) {
	client, err := p.client(contextName)
	if err != nil {
		return Discovery{}, err
	}
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return Discovery{}, fmt.Errorf("list nodes: %w", err)
	}
	services, err := client.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return Discovery{}, fmt.Errorf("list services: %w", err)
	}
	pods, err := client.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return Discovery{}, fmt.Errorf("list pods: %w", err)
	}
	deployments, err := client.AppsV1().Deployments("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return Discovery{}, fmt.Errorf("list deployments: %w", err)
	}

	podCIDRs := make(map[string]struct{})
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
	if len(podCIDRs) == 0 {
		for _, pod := range pods.Items {
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
	for _, service := range services.Items {
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

	serviceCIDRs := discoverServiceCIDRs(ctx, client)

	return Discovery{
		PodCIDRs:     sortedKeys(podCIDRs),
		ServiceCIDRs: serviceCIDRs,
		ServiceIPs:   sortedKeys(serviceIPs),
		DNSServer:    dnsServer,
		Pods:         len(pods.Items),
		Services:     len(services.Items),
		Deployments:  len(deployments.Items),
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
