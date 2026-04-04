package pool

import (
	"fmt"
	"log"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"socket-tuner/env-router/pkg/pb"
)

type AgentClient struct {
	Addr           string
	Conn           *grpc.ClientConn
	Client         pb.EnvAgentClient
	Busy           bool
	pendingRemoval bool // marked for cleanup after episode ends
}

type Pool struct {
	mu     sync.Mutex
	agents map[string]*AgentClient
}

// NewPool creates an empty dynamic pool — agents are added/removed by the watcher.
func NewPool() *Pool {
	return &Pool{
		agents: make(map[string]*AgentClient),
	}
}

// AddAgent dials a new agent and adds it to the pool. No-op if already present.
func (p *Pool) AddAgent(addr string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.agents[addr]; exists {
		// If it was pending removal but came back, cancel the removal.
		if p.agents[addr].pendingRemoval {
			p.agents[addr].pendingRemoval = false
			log.Printf("[pool] Agent %s un-marked for removal (pod returned)", addr)
		}
		return nil
	}

	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to dial agent %s: %w", addr, err)
	}

	p.agents[addr] = &AgentClient{
		Addr:   addr,
		Conn:   conn,
		Client: pb.NewEnvAgentClient(conn),
		Busy:   false,
	}
	log.Printf("[pool] Added agent %s (total: %d)", addr, len(p.agents))
	return nil
}

// RemoveAgent removes an idle agent immediately, or marks a busy agent for
// deferred removal once its episode completes.
func (p *Pool) RemoveAgent(addr string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ac, exists := p.agents[addr]
	if !exists {
		return
	}

	if ac.Busy {
		ac.pendingRemoval = true
		log.Printf("[pool] Agent %s is busy — marked for deferred removal", addr)
		return
	}

	ac.Conn.Close()
	delete(p.agents, addr)
	log.Printf("[pool] Removed agent %s (total: %d)", addr, len(p.agents))
}

// LockFreeAgent returns the first non-busy agent, locking it for an episode.
func (p *Pool) LockFreeAgent() (*AgentClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, ac := range p.agents {
		if !ac.Busy && !ac.pendingRemoval {
			ac.Busy = true
			return ac, nil
		}
	}
	return nil, fmt.Errorf("no free agents available (capacity: %d)", len(p.agents))
}

// FreeAgent releases the agent back to the pool, cleaning up if pending removal.
func (p *Pool) FreeAgent(addr string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ac, exists := p.agents[addr]
	if !exists {
		return
	}

	ac.Busy = false

	if ac.pendingRemoval {
		ac.Conn.Close()
		delete(p.agents, addr)
		log.Printf("[pool] Deferred removal completed for agent %s (total: %d)", addr, len(p.agents))
	}
}

// GetAgentBySession directly grabs an agent by its address (session ID).
func (p *Pool) GetAgentBySession(addr string) (*AgentClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if ac, exists := p.agents[addr]; exists {
		return ac, nil
	}
	return nil, fmt.Errorf("session agent %s not found", addr)
}

// FreeCount returns the number of idle, available agents.
func (p *Pool) FreeCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	count := 0
	for _, ac := range p.agents {
		if !ac.Busy && !ac.pendingRemoval {
			count++
		}
	}
	return count
}

// TotalCount returns the total number of tracked agents.
func (p *Pool) TotalCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.agents)
}

// RemoveAgentsNotIn removes all agents whose address is not in the provided set.
// Busy agents are marked for deferred removal.
func (p *Pool) RemoveAgentsNotIn(liveAddrs map[string]bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for addr, ac := range p.agents {
		if !liveAddrs[addr] {
			if ac.Busy {
				ac.pendingRemoval = true
				log.Printf("[pool] Agent %s disappeared — marked for deferred removal", addr)
			} else {
				ac.Conn.Close()
				delete(p.agents, addr)
				log.Printf("[pool] Removed stale agent %s (total: %d)", addr, len(p.agents))
			}
		}
	}
}

// Close drops all active connections.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, ac := range p.agents {
		ac.Conn.Close()
	}
}
