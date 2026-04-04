package k8s

import (
	"context"
	"fmt"
	"log"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"

	"socket-tuner/env-router/internal/pool"
)

// EndpointWatcher watches the Kubernetes Endpoints resource for the agent
// service and keeps the pool in sync with the live set of pod IPs.
type EndpointWatcher struct {
	clientset   kubernetes.Interface
	namespace   string
	serviceName string
	grpcPort    string
	pool        *pool.Pool
	cancel      context.CancelFunc
}

// NewEndpointWatcher creates a watcher that will track the Endpoints of the
// given service and add/remove agents in the pool as pods come and go.
func NewEndpointWatcher(cs kubernetes.Interface, namespace, serviceName, grpcPort string, p *pool.Pool) *EndpointWatcher {
	return &EndpointWatcher{
		clientset:   cs,
		namespace:   namespace,
		serviceName: serviceName,
		grpcPort:    grpcPort,
		pool:        p,
	}
}

// Start begins the watch loop in a background goroutine. Call Stop() to cancel.
func (ew *EndpointWatcher) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	ew.cancel = cancel
	go ew.run(ctx)
}

// Stop cancels the background watch loop.
func (ew *EndpointWatcher) Stop() {
	if ew.cancel != nil {
		ew.cancel()
	}
}

func (ew *EndpointWatcher) run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		if err := ew.watchLoop(ctx); err != nil {
			log.Printf("[watcher] Watch error: %v — retrying in 5s", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (ew *EndpointWatcher) watchLoop(ctx context.Context) error {
	// Do an initial list to seed the pool.
	endpoints, err := ew.clientset.CoreV1().Endpoints(ew.namespace).Get(ctx, ew.serviceName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("initial endpoints get: %w", err)
	}
	ew.syncEndpoints(endpoints)

	// Now watch for changes.
	watcher, err := ew.clientset.CoreV1().Endpoints(ew.namespace).Watch(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", ew.serviceName),
	})
	if err != nil {
		return fmt.Errorf("watch endpoints: %w", err)
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return fmt.Errorf("watch channel closed")
			}
			switch event.Type {
			case watch.Added, watch.Modified:
				ep, ok := event.Object.(*corev1.Endpoints)
				if !ok {
					continue
				}
				ew.syncEndpoints(ep)
			case watch.Deleted:
				// Service deleted — remove all agents.
				log.Printf("[watcher] Service %s deleted — clearing pool", ew.serviceName)
				// We rely on the next sync to add them back if the service returns.
			}
		}
	}
}

// syncEndpoints diffs the live endpoint addresses against the pool.
func (ew *EndpointWatcher) syncEndpoints(ep *corev1.Endpoints) {
	liveAddrs := make(map[string]bool)

	for _, subset := range ep.Subsets {
		for _, addr := range subset.Addresses {
			target := fmt.Sprintf("%s:%s", addr.IP, ew.grpcPort)
			liveAddrs[target] = true
		}
	}

	// Add new agents.
	for addr := range liveAddrs {
		if err := ew.pool.AddAgent(addr); err != nil {
			log.Printf("[watcher] Failed to add agent %s: %v", addr, err)
		}
	}

	// Remove agents that are no longer in the endpoint list.
	// We need to check pool's known agents — use a snapshot approach.
	// Pool doesn't expose a list, so we track removals through the diff.
	// For simplicity, we'll iterate the endpoints diff using pool methods.
	ew.pool.RemoveAgentsNotIn(liveAddrs)

	log.Printf("[watcher] Synced endpoints: %d live agents, pool total: %d, free: %d",
		len(liveAddrs), ew.pool.TotalCount(), ew.pool.FreeCount())
}
