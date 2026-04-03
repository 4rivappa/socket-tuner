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

func printObservation(label string, obs *pb.Observation) {
	if obs == nil {
		fmt.Printf("[%s] No observation returned.\n", label)
		return
	}
	fmt.Printf("\n--- %s eBPF KERNEL METRICS ---\n", label)
	fmt.Printf("Remote IP:   %s:%d\n", obs.RemoteIp, obs.RemotePort)
	fmt.Printf("Bytes Sent:  %d\n", obs.BytesSent)
	fmt.Printf("Bytes Recv:  %d\n", obs.BytesReceived)
	fmt.Printf("SRTT:        %d us (%.3f ms)\n", obs.SrttUs, float64(obs.SrttUs)/1000.0)
	fmt.Printf("Retransmits: %d\n", obs.TotalRetrans)
	fmt.Printf("Duration:    %d us (%.3f ms)\n", obs.DurationUs, float64(obs.DurationUs)/1000.0)
	fmt.Printf("------------------------------------\n")
}

func main() {
	conn, err := grpc.Dial("127.0.0.1:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("Failed to dial agent: %v", err)
	}
	defer conn.Close()

	client := pb.NewEnvAgentClient(conn)

	// 1. Reset — baseline run (no tuning)
	fmt.Println("=== 1. Resetting Environment (Baseline) ===")
	rResp, err := client.Reset(context.Background(), &pb.ResetRequest{
		// Command: "curl -sI -m 5 http://1.1.1.1",
		Command: "wget -O /dev/null https://github.com/torvalds/linux/archive/refs/tags/v7.0-rc6.tar.gz",
		// Command: "rm -rf /tmp/ig-clone && git clone --depth=1 https://github.com/inspektor-gadget/inspektor-gadget /tmp/ig-clone && rm -rf /tmp/ig-clone",
	})
	if err != nil {
		log.Fatalf("Reset failed: %v", err)
	}
	fmt.Printf("Reset Success: %v, Message: %s\n", rResp.Success, rResp.Message)
	printObservation("BASELINE", rResp.Observation)

	time.Sleep(1 * time.Second)

	// 2. Step — tuned run (with eBPF pacing limit and BBR)
	fmt.Println("\n=== 2. Stepping Environment (with 50KB/s Pacing + BBR + InitCWND:20) ===")
	sResp, err := client.Step(context.Background(), &pb.StepRequest{
		SessionId:     "test-session",
		MaxPacingRate: 50 * 1024, // 50 KB/s
		SndCwndClamp:  100,       // 100 segments
		CongAlgo:      "bbr",
		InitCwnd:      20,
		NoDelay:       true,
	})
	if err != nil {
		log.Fatalf("Step failed: %v", err)
	}
	fmt.Printf("Step done: %v\n", sResp.Done)
	printObservation("TUNED (BBR)", sResp.Observation)
}
