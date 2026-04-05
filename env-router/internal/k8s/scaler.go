package k8s

import (
	"context"
	"fmt"
	"log"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"socket-tuner/env-router/internal/pool"
)

// AutoScaler periodically checks the warm pool size and adjusts the
// env-ebpf-agent Deployment replica count to maintain the target warm count.
type AutoScaler struct {
	clientset      kubernetes.Interface
	namespace      string
	deploymentName string
	pool           *pool.Pool
	warmPoolSize   int
	interval       time.Duration
	cancel         context.CancelFunc
}

// NewAutoScaler creates a scaler that maintains warmPoolSize idle agents
// by adjusting the Deployment replica count every interval.
func NewAutoScaler(cs kubernetes.Interface, namespace, deploymentName string, p *pool.Pool, warmPoolSize int, interval time.Duration) *AutoScaler {
	return &AutoScaler{
		clientset:      cs,
		namespace:      namespace,
		deploymentName: deploymentName,
		pool:           p,
		warmPoolSize:   warmPoolSize,
		interval:       interval,
	}
}

// Start begins the scaling loop in a background goroutine.
func (as *AutoScaler) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	as.cancel = cancel
	go as.run(ctx)
}

// Stop cancels the scaling loop.
func (as *AutoScaler) Stop() {
	if as.cancel != nil {
		as.cancel()
	}
}

func (as *AutoScaler) run(ctx context.Context) {
	ticker := time.NewTicker(as.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			as.reconcile(ctx)
		}
	}
}

func (as *AutoScaler) reconcile(ctx context.Context) {
	freeCount := as.pool.FreeCount()
	totalCount := as.pool.TotalCount()

	// Get current deployment scale.
	scale, err := as.clientset.AppsV1().Deployments(as.namespace).GetScale(ctx, as.deploymentName, metav1.GetOptions{})
	if err != nil {
		log.Printf("[scaler] Failed to get deployment scale: %v", err)
		return
	}
	currentReplicas := int(scale.Spec.Replicas)

	busyCount := totalCount - freeCount
	desiredReplicas := busyCount + as.warmPoolSize
	if desiredReplicas < 1 {
		desiredReplicas = 1 // never scale to zero
	}

	if desiredReplicas == currentReplicas {
		return
	}

	direction := "up"
	if desiredReplicas < currentReplicas {
		direction = "down"
	}

	log.Printf("[scaler] Scaling %s: %d → %d replicas (free=%d, total=%d, warmTarget=%d)",
		direction, currentReplicas, desiredReplicas, freeCount, totalCount, as.warmPoolSize)

	scale.Spec.Replicas = int32(desiredReplicas)
	_, err = as.clientset.AppsV1().Deployments(as.namespace).UpdateScale(ctx, as.deploymentName, scale, metav1.UpdateOptions{})
	if err != nil {
		log.Printf("[scaler] Failed to update scale: %v", err)
		return
	}

	log.Printf("[scaler] Successfully scaled %s to %d replicas", as.deploymentName, desiredReplicas)
}

// SetWarmPoolSize dynamically updates the warm pool target (e.g. from an API).
func (as *AutoScaler) SetWarmPoolSize(size int) error {
	if size < 0 {
		return fmt.Errorf("warm pool size must be >= 0, got %d", size)
	}
	as.warmPoolSize = size
	log.Printf("[scaler] Warm pool size updated to %d", size)
	return nil
}
