package provider

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// CheckCodingAgentBinary verifies that the binary configured for a coding-agent
// provider is findable on PATH (or as an absolute path). This is a fast preflight
// that avoids spawning a full subprocess.
func CheckCodingAgentBinary(configJSON string) error {
	var cfg struct {
		Kind       string `json:"kind"`
		BinaryPath string `json:"binary_path"`
	}
	if err := json.Unmarshal([]byte(ExpandEnv(configJSON)), &cfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	bin := strings.TrimSpace(cfg.BinaryPath)
	if bin == "" {
		switch cfg.Kind {
		case "pi":
			bin = "pi"
		case "claudecode", "claude":
			bin = "claude"
		case "crush":
			bin = "crush"
		default: // opencode or unset
			bin = "opencode"
		}
	}

	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("binary %q not found on PATH", bin)
	}
	return nil
}
