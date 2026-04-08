package executor

import (
	"testing"
)

func TestExecutor_SetCommand(t *testing.T) {
	e := NewExecutor()

	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{
			name:    "Authorized Google Latency",
			command: "curl -4 -sI -m 5 http://google.com",
			wantErr: false,
		},
		{
			name:    "Authorized Hetzner Throughput",
			command: "curl -4 -o /dev/null https://ash-speed.hetzner.com/100MB.bin",
			wantErr: false,
		},
		{
			name:    "Authorized GitHub Bandwidth",
			command: "curl -4 -o /dev/null https://github.com/torvalds/linux/archive/refs/tags/v7.0-rc6.tar.gz",
			wantErr: false,
		},
		{
			name:    "Unauthorized whoami",
			command: "whoami",
			wantErr: true,
		},
		{
			name:    "Unauthorized rm -rf",
			command: "rm -rf /",
			wantErr: true,
		},
		{
			name:    "Unauthorized curl but different domain",
			command: "curl -sI http://malicious.com",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := e.SetCommand(tt.command)
			if (err != nil) != tt.wantErr {
				t.Errorf("Executor.SetCommand() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
