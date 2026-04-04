package executor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type Executor struct {
	lastCommand string
}

func NewExecutor() *Executor {
	return &Executor{}
}

// SetCommand stores the command for the current episode
func (e *Executor) SetCommand(cmdStr string) {
	e.lastCommand = cmdStr
}

// Run executes the currently configured command synchronously and waits for it to complete, returning its PID.
func (e *Executor) Run(ctx context.Context) (uint32, error) {
	if e.lastCommand == "" {
		return 0, fmt.Errorf("no command set")
	}

	parts := strings.Fields(e.lastCommand)
	if len(parts) == 0 {
		return 0, fmt.Errorf("empty command")
	}

	// Adding a small timeout bound to network commands to avoid hanging forever
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, parts[0], parts[1:]...)
	
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("command start failed: %w", err)
	}
	
	pid := uint32(cmd.Process.Pid)
	
	if err := cmd.Wait(); err != nil {
		return pid, fmt.Errorf("command execution failed: %w", err)
	}

	return pid, nil
}
