package pool

import (
	"fmt"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"socket-tuner/env-router/pkg/pb"
)

type AgentClient struct {
	Addr   string
	Conn   *grpc.ClientConn
	Client pb.EnvAgentClient
	Busy   bool
}

type Pool struct {
	mu      sync.Mutex
	agents  map[string]*AgentClient
	targets []string
}

// NewPool initializes gRPC clients to all provided agent addresses
func NewPool(addresses []string) (*Pool, error) {
	p := &Pool{
		agents:  make(map[string]*AgentClient),
		targets: addresses,
	}

	for _, addr := range addresses {
		// insecure for local EKS cluster interconnect
		conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, fmt.Errorf("failed to dial agent %s: %w", addr, err)
		}

		p.agents[addr] = &AgentClient{
			Addr:   addr,
			Conn:   conn,
			Client: pb.NewEnvAgentClient(conn),
			Busy:   false,
		}
	}

	return p, nil
}

// LockFreeAgent returns the first non-busy agent, locking it for an episode
func (p *Pool) LockFreeAgent() (*AgentClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, ac := range p.agents {
		if !ac.Busy {
			ac.Busy = true
			return ac, nil
		}
	}
	return nil, fmt.Errorf("no free agents available (capacity: %d)", len(p.agents))
}

// FreeAgent releases the agent back to the pool
func (p *Pool) FreeAgent(addr string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if ac, exists := p.agents[addr]; exists {
		ac.Busy = false
	}
}

// GetAgentBySession directly grabs an agent by its ID (address) whether busy or not
func (p *Pool) GetAgentBySession(addr string) (*AgentClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	if ac, exists := p.agents[addr]; exists {
		return ac, nil
	}
	return nil, fmt.Errorf("session agent %s not found", addr)
}

// Close drops all active connections
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, ac := range p.agents {
		ac.Conn.Close()
	}
}
