package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

var allowedHashes = map[string]bool{
	"b09f967a0e12ebf17a37cf7e6592477209a75ea0c7e54cc289db78fddf28830b": true, // google-latency
	"22166f407dd0189cf36be886f83be1453aecc42edc5a3fa80978b547f4041f2f": true, // hetzner-throughput
	"6d13daa22f70d3852eab8178e645d2209407b5a356339474304e5d108777ecee": true, // github-bandwidth
}

type Executor struct {
	lastCommand string
}

func NewExecutor() *Executor {
	return &Executor{}
}

// SetCommand stores the command for the current episode after validating its SHA256 hash
func (e *Executor) SetCommand(cmdStr string) error {
	h := sha256.New()
	h.Write([]byte(cmdStr))
	hashStr := hex.EncodeToString(h.Sum(nil))

	if !allowedHashes[hashStr] {
		return fmt.Errorf("unauthorized command: hash %s not in allowed list", hashStr)
	}

	e.lastCommand = cmdStr
	return nil
}

// Run executes the currently configured command synchronously and waits for it to complete, returning its PID.
func (e *Executor) Run(ctx context.Context) (uint32, error) {
	if e.lastCommand == "" {
		return 0, fmt.Errorf("no command set or command unauthorized")
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
