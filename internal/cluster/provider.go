package cluster

import (
	"context"
	"fmt"
	"net/netip"
	"sort"

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
	PodCIDRs   []string `json:"podCIDRs"`
	ServiceIPs []string `json:"serviceIPs"`
	DNSServer  string   `json:"dnsServer"`
	Pods       int      `json:"pods"`
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
	restConfig.UserAgent = "kube-clash/0.1"
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

	return Discovery{
		PodCIDRs:   sortedKeys(podCIDRs),
		ServiceIPs: sortedKeys(serviceIPs),
		DNSServer:  dnsServer,
		Pods:       len(pods.Items),
	}, nil
}

func sortedKeys(values map[string]struct{}) []string {
	items := make([]string, 0, len(values))
	for item := range values {
		items = append(items, item)
	}
	sort.Strings(items)
	return items
}
