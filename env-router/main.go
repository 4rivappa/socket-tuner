package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"socket-tuner/env-router/internal/grpcserver"
	k8sutil "socket-tuner/env-router/internal/k8s"
	"socket-tuner/env-router/internal/pool"
	"socket-tuner/env-router/pkg/pb"
)

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOrDefault(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Printf("WARN: invalid %s=%q, using default %d", key, v, fallback)
		return fallback
	}
	return n
}

func main() {
	// ── Configuration from environment variables ──
	listenAddr := envOrDefault("LISTEN_ADDR", ":50050")
	agentServiceName := os.Getenv("AGENT_SERVICE_NAME")
	agentNamespace := envOrDefault("AGENT_NAMESPACE", "default")
	agentDeploymentName := os.Getenv("AGENT_DEPLOYMENT_NAME")
	agentGRPCPort := envOrDefault("AGENT_GRPC_PORT", "50051")
	warmPoolSize := envIntOrDefault("WARM_POOL_SIZE", 1)
	scalerIntervalSec := envIntOrDefault("SCALER_INTERVAL_SECONDS", 10)

	if agentServiceName == "" {
		log.Fatal("AGENT_SERVICE_NAME environment variable is required")
	}
	if agentDeploymentName == "" {
		log.Fatal("AGENT_DEPLOYMENT_NAME environment variable is required")
	}

	log.Printf("Config: service=%s ns=%s deployment=%s port=%s warm=%d interval=%ds",
		agentServiceName, agentNamespace, agentDeploymentName, agentGRPCPort, warmPoolSize, scalerIntervalSec)

	// ── Kubernetes client (in-cluster) ──
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Failed to get in-cluster K8s config: %v", err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create K8s client: %v", err)
	}

	// ── Dynamic agent pool ──
	agentPool := pool.NewPool()
	defer agentPool.Close()

	// ── Start Kubernetes Endpoints watcher ──
	watcher := k8sutil.NewEndpointWatcher(clientset, agentNamespace, agentServiceName, agentGRPCPort, agentPool)
	watcher.Start()
	defer watcher.Stop()
	log.Printf("Endpoint watcher started for service %s/%s", agentNamespace, agentServiceName)

	// ── Start auto-scaler ──
	scalerInterval := time.Duration(scalerIntervalSec) * time.Second
	scaler := k8sutil.NewAutoScaler(clientset, agentNamespace, agentDeploymentName, agentPool, warmPoolSize, scalerInterval)
	scaler.Start()
	defer scaler.Stop()
	log.Printf("Auto-scaler started: target warm pool = %d, interval = %s", warmPoolSize, scalerInterval)

	// ── gRPC server ──
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcSrv := grpc.NewServer()
	routerServer := grpcserver.NewServer(agentPool)
	pb.RegisterEnvAgentServer(grpcSrv, routerServer)

	// Graceful shutdown
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		log.Println("Shutting down router...")
		grpcSrv.GracefulStop()
	}()

	log.Printf("Environment Router listening for RL traffic on %s", listenAddr)
	if err := grpcSrv.Serve(lis); err != nil {
		log.Fatalf("Failed to serve gRPC: %v", err)
	}
}
