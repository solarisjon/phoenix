package provider

import (
	"os"
	"regexp"
)

var envPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// ExpandEnv replaces all ${VAR_NAME} occurrences in s with the
// corresponding environment variable value. Unknown variables are
// left as-is so the user can see what's missing.
func ExpandEnv(s string) string {
	return envPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := envPattern.FindStringSubmatch(match)[1]
		if val := os.Getenv(name); val != "" {
			return val
		}
		return match // leave unreplaced so it's visible
	})
}
