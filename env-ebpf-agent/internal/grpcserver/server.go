package grpcserver

import (
	"context"
	"log"
	
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

func (s *Server) Reset(ctx context.Context, req *pb.ResetRequest) (*pb.ResetResponse, error) {
	log.Printf("Received Reset with command: %s", req.Command)
	
	s.executor.SetCommand(req.Command)
	
	// Execute it once as a baseline without specific eBPF parameters.
	if err := s.executor.Run(ctx); err != nil {
		log.Printf("Baseline command execution failed: %v", err)
		return &pb.ResetResponse{Success: false, Message: err.Error()}, nil
	}

	metric, ipStr, port, err := s.manager.GetLatestMetric()
	var obs *pb.Observation
	if err == nil && metric != nil {
		obs = &pb.Observation{
			RemoteIp:      ipStr,
			RemotePort:    port,
			SrttUs:        metric.SrttUs,
			TotalRetrans:  metric.TotalRetrans,
			BytesSent:     metric.BytesSent,
			BytesReceived: metric.BytesReceived,
			DurationUs:    metric.DurationUs,
		}
	} else {
		log.Printf("Warning: failed to get latest metric after baseline run: %v", err)
	}

	return &pb.ResetResponse{
		Success: true,
		Message: "Environment reset. Baseline run complete.",
		InitialObservation: obs,
	}, nil
}

func (s *Server) Step(ctx context.Context, req *pb.StepRequest) (*pb.StepResponse, error) {
	log.Printf("Received Step for IP %s:%d", req.TargetIp, req.TargetPort)
	
	action := &ebpf.BpfTuningAction{
		MaxPacingRate: req.MaxPacingRate,
		SndCwndClamp:  req.SndCwndClamp,
		CongAlgo:      req.CongAlgo,
		InitCwnd:      req.InitCwnd,
		WindowClamp:   req.WindowClamp,
		NoDelay:       req.NoDelay,
		RtoMin:        req.RtoMin,
		RetransAfter:  req.RetransAfter,
		EnableEcn:     uint8(req.EnableEcn),
		PacingStatus:  uint8(req.PacingStatus),
		KeepaliveIdle: req.KeepaliveIdle,
	}

	// 1. Install RL tuning action into eBPF maps
	if err := s.manager.SetAction(req.TargetIp, req.TargetPort, action); err != nil {
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
			DurationUs:    metrics.DurationUs,
		},
	}, nil
}
