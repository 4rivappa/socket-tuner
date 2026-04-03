package executor

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

type Executor struct {
	lastCommand string
	LastPID     int
}

func NewExecutor() *Executor {
	return &Executor{}
}

// SetCommand stores the command for the current episode
func (e *Executor) SetCommand(cmdStr string) {
	e.lastCommand = cmdStr
	e.LastPID = 0
}

// Start launches the command and returns immediately, exposing the PID.
func (e *Executor) Start(ctx context.Context) (*exec.Cmd, error) {
	if e.lastCommand == "" {
		return nil, fmt.Errorf("no command set")
	}

	// Use bash -c to support shell operators like &&, |, etc.
	// We use a generous 60s timeout for benchmarks like git clone.
	runCtx, _ := context.WithTimeout(ctx, 60*time.Second)

	cmd := exec.CommandContext(runCtx, "bash", "-c", e.lastCommand)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("command start failed: %w", err)
	}

	e.LastPID = cmd.Process.Pid
	return cmd, nil
}

// Run executes the command synchronously (used for baseline Reset runs without actions)
func (e *Executor) Run(ctx context.Context) error {
	cmd, err := e.Start(ctx)
	if err != nil {
		return err
	}
	return cmd.Wait()
}
