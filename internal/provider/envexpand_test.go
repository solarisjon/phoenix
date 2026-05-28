package provider

import (
	"os"
	"testing"
)

func TestExpandEnv(t *testing.T) {
	os.Setenv("TEST_KEY", "sk-abc123")
	os.Setenv("TEST_URL", "https://api.example.com/v1")
	defer os.Unsetenv("TEST_KEY")
	defer os.Unsetenv("TEST_URL")

	tests := []struct {
		input string
		want  string
	}{
		{`Bearer ${TEST_KEY}`, `Bearer sk-abc123`},
		{`${TEST_URL}/chat`, `https://api.example.com/v1/chat`},
		{`${UNKNOWN_VAR}`, `${UNKNOWN_VAR}`},           // left as-is
		{`no vars here`, `no vars here`},
		{`${TEST_KEY} and ${TEST_URL}`, `sk-abc123 and https://api.example.com/v1`},
	}

	for _, tt := range tests {
		got := ExpandEnv(tt.input)
		if got != tt.want {
			t.Errorf("ExpandEnv(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
