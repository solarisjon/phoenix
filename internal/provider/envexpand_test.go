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
		{`${UNKNOWN_VAR}`, `${UNKNOWN_VAR}`}, // left as-is
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

func TestIsLocalEndpoint(t *testing.T) {
	yes := []string{"http://localhost:8081/v1", "http://127.0.0.1:11434", "https://[::1]:8080", "http://192.168.0.10:8080/v1", "http://10.1.2.3", "http://172.16.5.5:80", "http://studio.local:8081", "http://foo.localhost", "localhost:11434", "0.0.0.0:8080"}
	no := []string{"https://api.openai.com/v1", "https://api.anthropic.com", "http://8.8.8.8", "http://example.com/v1", ""}
	for _, u := range yes {
		if !IsLocalEndpoint(u) {
			t.Errorf("IsLocalEndpoint(%q) = false, want true", u)
		}
	}
	for _, u := range no {
		if IsLocalEndpoint(u) {
			t.Errorf("IsLocalEndpoint(%q) = true, want false", u)
		}
	}
}
