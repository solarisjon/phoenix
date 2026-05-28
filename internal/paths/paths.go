// Package paths resolves platform-aware config and data directories.
// It honours XDG_CONFIG_HOME / XDG_DATA_HOME on Linux/macOS and
// APPDATA / LOCALAPPDATA on Windows, falling back to sensible defaults.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

var (
	once     sync.Once
	cfgDir   string
	dataDir  string
)

// Init resolves and creates config and data directories once.
// Subsequent calls are no-ops.
func Init() error {
	var initErr error
	once.Do(func() {
		cfgDir, dataDir, initErr = resolve()
		if initErr != nil {
			return
		}
		for _, d := range []string{cfgDir, dataDir} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				initErr = err
				return
			}
		}
	})
	return initErr
}

// ConfigDir returns the resolved config directory (e.g. ~/.config/phoenix).
// Init must have been called first.
func ConfigDir() string { return cfgDir }

// DataDir returns the resolved data directory (e.g. ~/.local/share/phoenix).
// Init must have been called first.
func DataDir() string { return dataDir }

// ConfigFile returns the full path to a named file inside the config directory.
func ConfigFile(name string) string { return filepath.Join(cfgDir, name) }

// DataFile returns the full path to a named file inside the data directory.
func DataFile(name string) string { return filepath.Join(dataDir, name) }

func resolve() (cfg, data string, err error) {
	switch runtime.GOOS {
	case "windows":
		cfg = windowsDir("APPDATA", "phoenix")
		data = windowsDir("LOCALAPPDATA", "phoenix")
	default:
		cfg = xdgDir("XDG_CONFIG_HOME", ".config", "phoenix")
		data = xdgDir("XDG_DATA_HOME", filepath.Join(".local", "share"), "phoenix")
	}
	return cfg, data, nil
}

func xdgDir(envKey, fallbackRel, appName string) string {
	if v := os.Getenv(envKey); v != "" {
		return filepath.Join(v, appName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, fallbackRel, appName)
}

func windowsDir(envKey, appName string) string {
	if v := os.Getenv(envKey); v != "" {
		return filepath.Join(v, appName)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "AppData", "Roaming", appName)
}
