package grpcserver

import (
	"context"
	"log"
	
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

func (s *Server) Reset(ctx context.Context, req *pb.ResetRequest) (*pb.ResetResponse, error) {
	log.Printf("Received Reset with command: %s", req.Command)
	
	s.executor.SetCommand(req.Command)
	
	// Execute it once as a baseline without specific eBPF parameters.
	if err := s.executor.Run(ctx); err != nil {
		log.Printf("Baseline command execution failed: %v", err)
		return &pb.ResetResponse{Success: false, Message: err.Error()}, nil
	}

	return &pb.ResetResponse{
		Success: true,
		Message: "Environment reset. Baseline run complete.",
	}, nil
}

func (s *Server) Step(ctx context.Context, req *pb.StepRequest) (*pb.StepResponse, error) {
	log.Printf("Received Step for IP %s:%d (MaxPacing: %d, CwndClamp: %d)", req.TargetIp, req.TargetPort, req.MaxPacingRate, req.SndCwndClamp)
	
	// 1. Install RL tuning action into eBPF maps
	if err := s.manager.SetAction(req.TargetIp, req.TargetPort, req.MaxPacingRate, req.SndCwndClamp); err != nil {
		log.Printf("Failed to set action: %v", err)
		return nil, err
	}
	
	// 2. Re-trigger the network task
	if err := s.executor.Run(ctx); err != nil {
		log.Printf("Command execution failed during step: %v", err)
	}
	
	// 3. Collect the resulting BPF metrics
	metrics, err := s.manager.GetMetrics(req.TargetIp, req.TargetPort)
	if err != nil {
		log.Printf("Failed to get metrics: %v", err)
		return &pb.StepResponse{Done: true}, err
	}
	
	return &pb.StepResponse{
		Done: true,
		Observation: &pb.Observation{
			RemoteIp:      req.TargetIp,
			RemotePort:    req.TargetPort,
			SrttUs:        metrics.SrttUs,
			TotalRetrans:  metrics.TotalRetrans,
			BytesSent:     metrics.BytesSent,
			BytesReceived: metrics.BytesReceived,
		},
	}, nil
}
