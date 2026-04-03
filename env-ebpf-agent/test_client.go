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

	// Cloudflare standard IP that supports HTTP HEAD
	targetIP := "1.1.1.1"

	fmt.Println("=== 1. Resetting Environment ===")
	rResp, err := client.Reset(context.Background(), &pb.ResetRequest{
		Command: fmt.Sprintf("curl -sI -m 5 http://%s", targetIP),
	})
	if err != nil {
		log.Fatalf("Reset failed: %v", err)
	}
	fmt.Printf("Reset Success: %v, Response Message: %s\n", rResp.Success, rResp.Message)

	time.Sleep(1 * time.Second)

	fmt.Println("\n=== 2. Stepping Environment (with strict eBPF Pacing Limit) ===")
	// Assuming 100KB/s pacing rate
	sResp, err := client.Step(context.Background(), &pb.StepRequest{
		SessionId:     "test-session",
		TargetIp:      targetIP,
		TargetPort:    80,
		MaxPacingRate: 50 * 1024, // 50 KB/s pacing
		SndCwndClamp:  0,
	})
	if err != nil {
		log.Fatalf("Step failed: %v", err)
	}

	fmt.Printf("Step done: %v\n", sResp.Done)
	if sResp.Observation != nil {
		fmt.Printf("\n--- EXTRACRED eBPF KERNEL METRICS ---\n")
		fmt.Printf("Remote IP:   %s:%d\n", sResp.Observation.RemoteIp, sResp.Observation.RemotePort)
		fmt.Printf("Bytes Sent:  %d\n", sResp.Observation.BytesSent)
		fmt.Printf("Bytes Recv:  %d\n", sResp.Observation.BytesReceived)
		fmt.Printf("SRTT:        %d us (%.3f ms)\n", sResp.Observation.SrttUs, float64(sResp.Observation.SrttUs)/1000.0)
		fmt.Printf("Retransmits: %d\n", sResp.Observation.TotalRetrans)
		fmt.Printf("-------------------------------------\n")
	} else {
		fmt.Println("No observation returned.")
	}
}
