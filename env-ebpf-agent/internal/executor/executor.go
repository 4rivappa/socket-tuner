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

// Run executes the currently configured command synchronously and waits for it to complete.
func (e *Executor) Run(ctx context.Context) error {
	if e.lastCommand == "" {
		return fmt.Errorf("no command set")
	}

	parts := strings.Fields(e.lastCommand)
	if len(parts) == 0 {
		return fmt.Errorf("empty command")
	}

	// Adding a small timeout bound to network commands to avoid hanging forever
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, parts[0], parts[1:]...)
	
	// We run it and wait. We discard output since this is just an environment trigger
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("command execution failed: %w", err)
	}

	return nil
}
