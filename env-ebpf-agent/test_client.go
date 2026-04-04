package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"socket-tuner/env-ebpf-agent/pkg/pb"
)

func main() {
	conn, err := grpc.Dial("127.0.0.1:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to dial agent: %v", err)
	}
	defer conn.Close()

	client := pb.NewEnvAgentClient(conn)

	// Execute an arbitrary network command
	fmt.Println("=== 1. Resetting Environment ===")
	rResp, err := client.Reset(context.Background(), &pb.ResetRequest{
		// Command: "curl -sI -m 5 http://google.com",
		// Command: "curl -o /dev/null https://ash-speed.hetzner.com/1GB.bin",
		Command: "curl -o /dev/null https://github.com/torvalds/linux/archive/refs/tags/v7.0-rc6.tar.gz",
		// Command: "git clone --depth=0 https://github.com/kubernetes/kubernetes.git",
	})
	if err != nil {
		log.Fatalf("Reset failed: %v", err)
	}
	fmt.Printf("Reset Success: %v, Response Message: %s\n", rResp.Success, rResp.Message)
	if rResp.InitialObservation != nil {
		fmt.Printf("\n--- BASELINE METRICS ---\n")
		fmt.Printf("Remote IP:   %s:%d\n", rResp.InitialObservation.RemoteIp, rResp.InitialObservation.RemotePort)
		fmt.Printf("Bytes Sent:  %d\n", rResp.InitialObservation.BytesSent)
		fmt.Printf("Bytes Recv:  %d\n", rResp.InitialObservation.BytesReceived)
		fmt.Printf("SRTT:        %d us\n", rResp.InitialObservation.SrttUs)
		fmt.Printf("Duration:    %d us\n", rResp.InitialObservation.DurationUs)
		fmt.Printf("------------------------\n")
	}

	time.Sleep(1 * time.Second)

	dynamicIp := rResp.InitialObservation.RemoteIp
	dynamicPort := rResp.InitialObservation.RemotePort

	fmt.Println("\n=== 2. Stepping Environment (with strict eBPF Pacing Limit) ===")
	// Assuming 100KB/s pacing rate
	sResp, err := client.Step(context.Background(), &pb.StepRequest{
		SessionId:     "test-session",
		TargetIp:      dynamicIp,
		TargetPort:    dynamicPort,
		MaxPacingRate: 0,
		SndCwndClamp:  0,
		CongAlgo:      1, // Cubic
		InitCwnd:      0,
		WindowClamp:   33554432, // Large 32MB window
		NoDelay:       0,
	})
	if err != nil {
		log.Fatalf("Step failed: %v", err)
	}

	fmt.Printf("Step done: %v\n", sResp.Done)
	if sResp.Observation != nil {
		fmt.Printf("\n--- EXTRACTED eBPF KERNEL METRICS ---\n")
		fmt.Printf("Remote IP:   %s:%d\n", sResp.Observation.RemoteIp, sResp.Observation.RemotePort)
		fmt.Printf("Bytes Sent:  %d\n", sResp.Observation.BytesSent)
		fmt.Printf("Bytes Recv:  %d\n", sResp.Observation.BytesReceived)
		fmt.Printf("SRTT:        %d us (%.3f ms)\n", sResp.Observation.SrttUs, float64(sResp.Observation.SrttUs)/1000.0)
		fmt.Printf("Retransmits: %d\n", sResp.Observation.TotalRetrans)
		fmt.Printf("Duration:    %d us (%.3f ms)\n", sResp.Observation.DurationUs, float64(sResp.Observation.DurationUs)/1000.0)
		fmt.Printf("-------------------------------------\n")
	} else {
		fmt.Println("No observation returned.")
	}
}
