package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

	"socket-tuner/env-ebpf-agent/internal/ebpfmgr"
	"socket-tuner/env-ebpf-agent/internal/executor"
	"socket-tuner/env-ebpf-agent/internal/grpcserver"
	"socket-tuner/env-ebpf-agent/pkg/pb"
)

func main() {
	cgroupPath := flag.String("cgroup", "/sys/fs/cgroup", "Path to cgroup v2 for sockops attachment")
	listenAddr := flag.String("listen", ":50061", "gRPC listen address")
	flag.Parse()

	// 1. Initialize Executor
	exec := executor.NewExecutor()

	// 2. Initialize eBPF Manager
	log.Println("Loading eBPF programs...")
	manager, err := ebpfmgr.NewManager()
	if err != nil {
		log.Fatalf("Failed to initialize eBPF manager: %v", err)
	}
	defer manager.Close()

	if err := manager.AttachCgroup(*cgroupPath); err != nil {
		log.Fatalf("Failed to attach to cgroup %s: %v", *cgroupPath, err)
	}
	log.Printf("Successfully attached eBPF to %s", *cgroupPath)

	// 3. Initialize gRPC Server
	lis, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcSrv := grpc.NewServer()
	envServer := grpcserver.NewServer(manager, exec)
	pb.RegisterEnvAgentServer(grpcSrv, envServer)

	// Handle graceful shutdown
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		log.Println("Shutting down...")
		grpcSrv.GracefulStop()
	}()

	log.Printf("Environment Agent listening on %s", *listenAddr)
	if err := grpcSrv.Serve(lis); err != nil {
		log.Fatalf("Failed to serve gRPC: %v", err)
	}
}
