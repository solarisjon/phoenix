package paths

import (
	"os"
	"strings"
	"sync"
	"testing"
)

func resetOnce() {
	once = sync.Once{}
	cfgDir = ""
	dataDir = ""
}

func TestDefaultPaths(t *testing.T) {
	resetOnce()
	os.Unsetenv("XDG_CONFIG_HOME")
	os.Unsetenv("XDG_DATA_HOME")

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	if !strings.HasSuffix(ConfigDir(), "phoenix") {
		t.Errorf("ConfigDir() = %q, want suffix 'phoenix'", ConfigDir())
	}
	if !strings.HasSuffix(DataDir(), "phoenix") {
		t.Errorf("DataDir() = %q, want suffix 'phoenix'", DataDir())
	}
}

func TestXDGOverride(t *testing.T) {
	resetOnce()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	if !strings.HasPrefix(ConfigDir(), tmp) {
		t.Errorf("ConfigDir() = %q, want prefix %q", ConfigDir(), tmp)
	}
}

func TestConfigAndDataFiles(t *testing.T) {
	resetOnce()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	if err := Init(); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	if !strings.Contains(ConfigFile("config.yaml"), "config.yaml") {
		t.Error("ConfigFile() should contain the filename")
	}
	if !strings.Contains(DataFile("phoenix.db"), "phoenix.db") {
		t.Error("DataFile() should contain the filename")
	}
}
