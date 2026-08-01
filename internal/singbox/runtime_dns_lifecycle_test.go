package singbox

import (
	"context"
	"sync"
	"testing"
)

func TestProcessUpdateDNSNamespaceConcurrentClose(t *testing.T) {
	for i := 0; i < 100; i++ {
		done := make(chan struct{})
		close(done)
		process := &Process{
			done:     done,
			stopCh:   make(chan struct{}),
			dnsProxy: &dnsSearchProxy{},
			spec:     validSessionSpec(),
			updateDNS: func(context.Context, string, DNSMeta) error {
				return nil
			},
		}

		start := make(chan struct{})
		errs := make(chan error, 2)
		var workers sync.WaitGroup
		workers.Add(2)
		go func() {
			defer workers.Done()
			<-start
			errs <- process.UpdateDNSNamespace(context.Background(), "kubeloop-e2e")
		}()
		go func() {
			defer workers.Done()
			<-start
			errs <- process.Close()
		}()
		close(start)
		workers.Wait()
		close(errs)

		for err := range errs {
			if err != nil {
				t.Fatalf("iteration %d: concurrent lifecycle operation failed: %v", i, err)
			}
		}
	}
}
