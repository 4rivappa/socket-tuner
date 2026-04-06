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

	fmt.Println("\n=== 2. Stepping Environment (Applying Full eBPF Tuning) ===")
	sResp, err := client.Step(context.Background(), &pb.StepRequest{
		SessionId:      rResp.SessionId,
		TargetIp:       dynamicIp,
		TargetPort:     dynamicPort,
		MaxPacingRate:  1000000,    // 1MB/s
		SndCwndClamp:   100,        // Clamp to 100 segments
		CongAlgo:       2,          // 2 for BBR (if supported by kernel)
		InitCwnd:       20,         // Increase initial CWND to 20
		WindowClamp:    16777216,   // 16MB window
		NoDelay:        1,          // Disable Nagle
		RtoMin:         200,        // 200ms min RTO
		RetransAfter:   3,          // Retransmit after 3 duplicate ACKs
		EnableEcn:      1,          // Enable ECN
		PacingStatus:   1,          // Enable pacing
		KeepaliveIdle:  7200,       // 2 hours idle
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
