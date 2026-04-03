package grpcserver

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"time"

	"socket-tuner/env-ebpf-agent/ebpf"
	"socket-tuner/env-ebpf-agent/internal/ebpfmgr"
	"socket-tuner/env-ebpf-agent/internal/executor"
	"socket-tuner/env-ebpf-agent/pkg/pb"
)

type Server struct {
	pb.UnimplementedEnvAgentServer

	manager  *ebpfmgr.Manager
	executor *executor.Executor
}

func NewServer(m *ebpfmgr.Manager, e *executor.Executor) *Server {
	return &Server{
		manager:  m,
		executor: e,
	}
}

// metricsToObservation converts eBPF kernel metrics into a gRPC Observation
func metricsToObservation(metrics *ebpf.BpfTuningMetrics) *pb.Observation {
	if metrics == nil {
		return nil
	}

	// Convert raw uint32 IP from kernel to dotted notation
	// remote_ip4 is in network byte order (__be32)
	ipBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(ipBytes, metrics.RemoteIp4)
	remoteIP := net.IPv4(ipBytes[0], ipBytes[1], ipBytes[2], ipBytes[3]).String()

	// Kernel stores remote_port in network byte order (big-endian) as __be32
	// We need to swap the full 32-bit value to get the port in host order
	portBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(portBytes, metrics.RemotePort)
	remotePort := binary.BigEndian.Uint32(portBytes)

	return &pb.Observation{
		RemoteIp:      remoteIP,
		RemotePort:    remotePort,
		SrttUs:        metrics.SrttUs,
		TotalRetrans:  metrics.TotalRetrans,
		BytesSent:     metrics.BytesSent,
		BytesReceived: metrics.BytesReceived,
		DurationUs:    metrics.DurationUs,
	}
}

func (s *Server) Reset(ctx context.Context, req *pb.ResetRequest) (*pb.ResetResponse, error) {
	log.Printf("Received Reset with command: %s", req.Command)

	s.executor.SetCommand(req.Command)

	// Baseline run (no eBPF action — but we set a 'dummy' action in the map to trigger tracking)
	cmd, err := s.executor.Start(ctx)
	if err != nil {
		log.Printf("Baseline command start failed: %v", err)
		return &pb.ResetResponse{Success: false, Message: err.Error()}, nil
	}

	pid := uint32(s.executor.LastPID)
	log.Printf("Baseline command started, PID: %d", pid)

	// Neutral action (all zeros) just to trigger the eBPF 'infection' flow
	if err := s.manager.SetAction(pid, 0, 0, "", 0, 0, false); err != nil {
		log.Printf("Failed to set baseline tracking for PID %d: %v", pid, err)
		cmd.Wait()
		return nil, err
	}

	// Wait for baseline run to complete
	if err := cmd.Wait(); err != nil {
		log.Printf("Baseline command execution completed with error: %v", err)
	}

	log.Printf("Baseline run completed, PID was: %d", pid)

	// Small sleep to let the kernel flush final state change callbacks
	time.Sleep(100 * time.Millisecond)

	// Retrieve baseline metrics by PID
	var obs *pb.Observation
	metrics, err := s.manager.GetMetrics(pid)
	if err != nil {
		log.Printf("Warning: could not retrieve baseline metrics for PID %d: %v", pid, err)
	} else {
		obs = metricsToObservation(metrics)
		log.Printf("Baseline metrics: SRTT=%d us, Bytes Sent=%d, Duration=%d us", metrics.SrttUs, metrics.BytesSent, metrics.DurationUs)
	}

	// Cleanup the PID entry from maps
	s.manager.CleanupPID(pid)

	return &pb.ResetResponse{
		Success:     true,
		Message:     "Environment reset. Baseline run complete.",
		Observation: obs,
	}, nil
}

func (s *Server) Step(ctx context.Context, req *pb.StepRequest) (*pb.StepResponse, error) {
	log.Printf("Received Step (MaxPacing: %d, CwndClamp: %d, Algo: %s, InitCwnd: %d, WindowClamp: %d, NoDelay: %v)",
		req.MaxPacingRate, req.SndCwndClamp, req.CongAlgo, req.InitCwnd, req.WindowClamp, req.NoDelay)

	// 1. Start the command to get the PID
	cmd, err := s.executor.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	pid := uint32(s.executor.LastPID)
	log.Printf("Step command started, PID: %d", pid)

	// 2. Immediately install the RL tuning action keyed by PID
	if err := s.manager.SetAction(pid, req.MaxPacingRate, req.SndCwndClamp, req.CongAlgo, req.InitCwnd, req.WindowClamp, req.NoDelay); err != nil {
		log.Printf("Failed to set action for PID %d: %v", pid, err)
		cmd.Wait() // still wait to avoid zombies
		return nil, err
	}

	// 3. Wait for the command to finish
	if err := cmd.Wait(); err != nil {
		log.Printf("Command execution failed during step (PID %d): %v", pid, err)
	}

	// Small sleep to let the kernel flush final state change callbacks
	time.Sleep(100 * time.Millisecond)

	// 4. Collect the resulting BPF metrics by PID
	var obs *pb.Observation
	metrics, err := s.manager.GetMetrics(pid)
	if err != nil {
		log.Printf("Failed to get metrics for PID %d: %v", pid, err)
	} else {
		obs = metricsToObservation(metrics)
	}

	// 5. Cleanup
	s.manager.CleanupPID(pid)

	return &pb.StepResponse{
		Done:        true,
		Observation: obs,
	}, nil
}
