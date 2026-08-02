package cluster

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/netip"
	"sort"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// InventorySnapshot is the live Pod/Service/Deployment inventory used by the UI.
type InventorySnapshot struct {
	Pods         int
	Services     int
	Deployments  int
	ServiceIPs   []string
	DNSServer    string
	PodItems     []PodInfo
	ServiceItems []ServiceInfo
}

type inventoryWatcher struct {
	cancel    context.CancelFunc
	factories []informers.SharedInformerFactory
	onChange  func(InventorySnapshot)
	debounce  *time.Timer
	mu        sync.Mutex
	closed    bool
	closeOnce sync.Once
}

// WatchInventory starts shared informers for Pods, Services, and Deployments.
// namespaces empty = all namespaces; otherwise one factory per namespace.
func (p *Provider) WatchInventory(
	ctx context.Context,
	contextName string,
	namespaces []string,
	onChange func(InventorySnapshot),
) (io.Closer, error) {
	if onChange == nil {
		return nil, fmt.Errorf("inventory callback is required")
	}
	client, err := p.client(contextName)
	if err != nil {
		return nil, err
	}
	watchCtx, cancel := context.WithCancel(ctx)
	watcher := &inventoryWatcher{
		cancel:   cancel,
		onChange: onChange,
	}

	targets := namespaces
	if len(targets) == 0 {
		targets = []string{""}
	}
	synced := make([]cache.InformerSynced, 0, len(targets)*3)
	for _, ns := range targets {
		factory, syncers, startErr := startNamespaceInformers(client, ns, watcher)
		if startErr != nil {
			cancel()
			return nil, startErr
		}
		watcher.factories = append(watcher.factories, factory)
		synced = append(synced, syncers...)
		factory.Start(watchCtx.Done())
	}

	if !cache.WaitForCacheSync(watchCtx.Done(), synced...) {
		cancel()
		return nil, fmt.Errorf("timed out waiting for inventory informers")
	}
	watcher.emit()
	return watcher, nil
}

func startNamespaceInformers(
	client kubernetes.Interface,
	namespace string,
	watcher *inventoryWatcher,
) (informers.SharedInformerFactory, []cache.InformerSynced, error) {
	var factory informers.SharedInformerFactory
	if namespace == "" {
		factory = informers.NewSharedInformerFactory(client, 0)
	} else {
		factory = informers.NewSharedInformerFactoryWithOptions(
			client, 0, informers.WithNamespace(namespace),
		)
	}
	podInformer := factory.Core().V1().Pods().Informer()
	serviceInformer := factory.Core().V1().Services().Informer()
	deploymentInformer := factory.Apps().V1().Deployments().Informer()
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(any) { watcher.schedule() },
		UpdateFunc: func(any, any) { watcher.schedule() },
		DeleteFunc: func(any) { watcher.schedule() },
	}
	if _, err := podInformer.AddEventHandler(handler); err != nil {
		return nil, nil, fmt.Errorf("watch pods: %w", err)
	}
	if _, err := serviceInformer.AddEventHandler(handler); err != nil {
		return nil, nil, fmt.Errorf("watch services: %w", err)
	}
	if _, err := deploymentInformer.AddEventHandler(handler); err != nil {
		return nil, nil, fmt.Errorf("watch deployments: %w", err)
	}
	return factory, []cache.InformerSynced{
		podInformer.HasSynced, serviceInformer.HasSynced, deploymentInformer.HasSynced,
	}, nil
}

func (w *inventoryWatcher) Close() error {
	w.closeOnce.Do(func() {
		w.mu.Lock()
		w.closed = true
		if w.debounce != nil {
			w.debounce.Stop()
		}
		w.mu.Unlock()
		w.cancel()
		for _, factory := range w.factories {
			factory.Shutdown()
		}
	})
	return nil
}

func (w *inventoryWatcher) schedule() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	if w.debounce != nil {
		w.debounce.Stop()
	}
	w.debounce = time.AfterFunc(300*time.Millisecond, w.emit)
}

func (w *inventoryWatcher) emit() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	onChange := w.onChange
	factories := append([]informers.SharedInformerFactory{}, w.factories...)
	w.mu.Unlock()

	var pods []*corev1.Pod
	var services []*corev1.Service
	var deployments []*appsv1.Deployment
	for _, factory := range factories {
		listedPods, err := factory.Core().V1().Pods().Lister().List(labels.Everything())
		if err != nil {
			log.Printf("inventory watcher list pods: %v", err)
			return
		}
		pods = append(pods, listedPods...)
		listedServices, err := factory.Core().V1().Services().Lister().List(labels.Everything())
		if err != nil {
			log.Printf("inventory watcher list services: %v", err)
			return
		}
		services = append(services, listedServices...)
		listedDeployments, err := factory.Apps().V1().Deployments().Lister().List(labels.Everything())
		if err != nil {
			log.Printf("inventory watcher list deployments: %v", err)
			return
		}
		deployments = append(deployments, listedDeployments...)
	}
	onChange(snapshotFromLists(pods, services, deployments))
}

func snapshotFromLists(
	pods []*corev1.Pod,
	services []*corev1.Service,
	deployments []*appsv1.Deployment,
) InventorySnapshot {
	serviceIPs := make(map[string]struct{})
	dnsServer := ""
	for _, service := range services {
		for _, raw := range service.Spec.ClusterIPs {
			if ip, err := netip.ParseAddr(raw); err == nil {
				serviceIPs[ip.String()] = struct{}{}
			}
		}
		if service.Namespace == "kube-system" &&
			(service.Name == "kube-dns" || service.Name == "coredns") {
			for _, raw := range service.Spec.ClusterIPs {
				if ip, err := netip.ParseAddr(raw); err == nil {
					dnsServer = ip.String()
					break
				}
			}
		}
	}
	ips := make([]string, 0, len(serviceIPs))
	for ip := range serviceIPs {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	return InventorySnapshot{
		Pods:         len(pods),
		Services:     len(services),
		Deployments:  len(deployments),
		ServiceIPs:   ips,
		DNSServer:    dnsServer,
		PodItems:     podInfosFromList(pods),
		ServiceItems: serviceInfosFromList(services),
	}
}
