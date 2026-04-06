package grpcserver

import (
	"context"
	"fmt"
	"log"

	"socket-tuner/env-router/internal/pool"
	"socket-tuner/env-router/pkg/pb"
)

type Server struct {
	pb.UnimplementedEnvAgentServer
	
	agentPool *pool.Pool
}

func NewServer(p *pool.Pool) *Server {
	return &Server{
		agentPool: p,
	}
}

func (s *Server) Reset(ctx context.Context, req *pb.ResetRequest) (*pb.ResetResponse, error) {
	log.Printf("Received Reset request for command: %s", req.Command)
	
	// Lock the first available physical node
	agent, err := s.agentPool.LockFreeAgent()
	if err != nil {
		log.Printf("Rejected Reset: %v", err)
		return nil, err
	}
	log.Printf("Assigned new episode to agent on %s", agent.Addr)
	s.agentPool.TouchAgent(agent.Addr)

	// Forward the network scenario initialization
	resp, err := agent.Client.Reset(ctx, req)
	if err != nil {
		// If setup fails, immediately free the node
		s.agentPool.FreeAgent(agent.Addr)
		return nil, fmt.Errorf("agent reset failed: %w", err)
	}
	
	// Tag the response with the physical address acting as a "Session ID"
	resp.SessionId = agent.Addr
	
	return resp, nil
}

func (s *Server) Step(ctx context.Context, req *pb.StepRequest) (*pb.StepResponse, error) {
	if req.SessionId == "" {
		return nil, fmt.Errorf("missing session_id in Step Request. Cannot route Action.")
	}
	
	// Find the dedicated node that holds this episode's BPF Maps
	agent, err := s.agentPool.GetAgentBySession(req.SessionId)
	if err != nil {
		log.Printf("Step routing failed: %v", err)
		return nil, err
	}
	s.agentPool.TouchAgent(req.SessionId)

	// Forward step down
	resp, err := agent.Client.Step(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("agent step failed: %w", err)
	}
	
	// If the network trace indicates termination, release the node back to pool
	if resp.Done {
		log.Printf("Episode on %s marked done. Releasing agent.", req.SessionId)
		s.agentPool.FreeAgent(req.SessionId)
	}

	return resp, nil
}
