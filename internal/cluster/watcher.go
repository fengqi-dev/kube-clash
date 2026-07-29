package cluster

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"sort"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

// InventorySnapshot is the live Pod/Service/Deployment inventory used by the UI.
type InventorySnapshot struct {
	Pods        int
	Services    int
	Deployments int
	ServiceIPs  []string
	DNSServer   string
}

type inventoryWatcher struct {
	cancel    context.CancelFunc
	factory   informers.SharedInformerFactory
	onChange  func(InventorySnapshot)
	debounce  *time.Timer
	mu        sync.Mutex
	closed    bool
	closeOnce sync.Once
}

// WatchInventory starts shared informers for Pods, Services, and Deployments.
// onChange is invoked (debounced) whenever the inventory changes.
func (p *Provider) WatchInventory(
	ctx context.Context,
	contextName string,
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
	factory := informers.NewSharedInformerFactory(client, 0)
	podInformer := factory.Core().V1().Pods().Informer()
	serviceInformer := factory.Core().V1().Services().Informer()
	deploymentInformer := factory.Apps().V1().Deployments().Informer()

	watcher := &inventoryWatcher{
		cancel:   cancel,
		factory:  factory,
		onChange: onChange,
	}
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc:    func(any) { watcher.schedule() },
		UpdateFunc: func(any, any) { watcher.schedule() },
		DeleteFunc: func(any) { watcher.schedule() },
	}
	if _, err := podInformer.AddEventHandler(handler); err != nil {
		cancel()
		return nil, fmt.Errorf("watch pods: %w", err)
	}
	if _, err := serviceInformer.AddEventHandler(handler); err != nil {
		cancel()
		return nil, fmt.Errorf("watch services: %w", err)
	}
	if _, err := deploymentInformer.AddEventHandler(handler); err != nil {
		cancel()
		return nil, fmt.Errorf("watch deployments: %w", err)
	}

	factory.Start(watchCtx.Done())
	if !cache.WaitForCacheSync(
		watchCtx.Done(),
		podInformer.HasSynced,
		serviceInformer.HasSynced,
		deploymentInformer.HasSynced,
	) {
		cancel()
		return nil, fmt.Errorf("timed out waiting for inventory informers")
	}
	watcher.emit()
	return watcher, nil
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
		w.factory.Shutdown()
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
	w.mu.Unlock()

	pods, err := w.factory.Core().V1().Pods().Lister().List(labels.Everything())
	if err != nil {
		return
	}
	services, err := w.factory.Core().V1().Services().Lister().List(labels.Everything())
	if err != nil {
		return
	}
	deployments, err := w.factory.Apps().V1().Deployments().Lister().List(labels.Everything())
	if err != nil {
		return
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
		Pods:        len(pods),
		Services:    len(services),
		Deployments: len(deployments),
		ServiceIPs:  ips,
		DNSServer:   dnsServer,
	}
}
