package main

import (
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"google.golang.org/grpc"

	"socket-tuner/env-router/internal/grpcserver"
	"socket-tuner/env-router/internal/pool"
	"socket-tuner/env-router/pkg/pb"
)

func main() {
	listenAddr := flag.String("listen", ":50050", "Router gRPC listen address for RL Inference")
	agentsFlag := flag.String("agents", "127.0.0.1:50051", "Comma separated list of env-ebpf-agent addresses")
	flag.Parse()

	agents := strings.Split(*agentsFlag, ",")
	if len(agents) == 0 || agents[0] == "" {
		log.Fatal("At least one agent must be specified via -agents")
	}

	log.Printf("Dialing %d remote eBPF agents: %v...", len(agents), agents)
	agentPool, err := pool.NewPool(agents)
	if err != nil {
		log.Fatalf("Failed to initialize agent pool: %v", err)
	}
	defer agentPool.Close()

	lis, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	grpcSrv := grpc.NewServer()
	routerServer := grpcserver.NewServer(agentPool)
	pb.RegisterEnvAgentServer(grpcSrv, routerServer)

	// Graceful shutdown handling
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		log.Println("Shutting down router...")
		grpcSrv.GracefulStop()
	}()

	log.Printf("Environment Router listening for RL traffic on %s", *listenAddr)
	if err := grpcSrv.Serve(lis); err != nil {
		log.Fatalf("Failed to serve gRPC: %v", err)
	}
}
