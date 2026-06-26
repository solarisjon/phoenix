package pricing

import "strings"

// Model tier classification for recommendation logic.
// Tier 1 = expensive flagship models.
// Tier 2 = capable mid-range models (recommended swap targets).
// Tier 3 = cheap/local models.

var tier1Prefixes = []string{
	"gpt-4o", "gpt-4-turbo", "gpt-4",
	"claude-3-5-sonnet", "claude-3-opus", "claude-opus-4", "claude-sonnet-4",
	"o1", "gemini-1.5-pro", "gemini-2.5-pro",
}

var tier2Prefixes = []string{
	"gpt-4o-mini",
	"claude-3-5-haiku", "claude-3-haiku",
	"o3-mini", "o1-mini",
	"gemini-1.5-flash", "gemini-2.0-flash",
	"llama-3.1-70b", "llama-3.3-70b",
	"mistral-small", "mixtral-8x7b",
	"deepseek-chat",
}

var tier3Prefixes = []string{
	"llama-3.1-8b",
	"mistral-7b",
	"gemini-2.0-flash",
	"gpt-3.5-turbo",
}

// ModelTier classifies a model name into 1, 2, 3, or 0 (unknown).
// Tier 2 is checked before Tier 1 so that more-specific prefixes like
// "gpt-4o-mini" are not swallowed by the broader "gpt-4o" Tier 1 prefix.
func ModelTier(modelName string) int {
	n := strings.ToLower(modelName)
	for _, p := range tier2Prefixes {
		if strings.HasPrefix(n, p) {
			return 2
		}
	}
	for _, p := range tier3Prefixes {
		if strings.HasPrefix(n, p) {
			return 3
		}
	}
	for _, p := range tier1Prefixes {
		if strings.HasPrefix(n, p) {
			return 1
		}
	}
	return 0
}

// SuggestCheaperModel returns a recommended cheaper model if currentModel is
// Tier 1. It tries to pick a same-family Tier 2 alternative when possible.
// Returns ("", 0, false) if no suggestion applies.
func SuggestCheaperModel(currentModel string, reg *Registry) (suggested string, savingPct float64, ok bool) {
	if ModelTier(currentModel) != 1 {
		return "", 0, false
	}

	// Family-based swap map: if current model starts with key, suggest value.
	familySwaps := []struct{ from, to string }{
		{"gpt-4o", "gpt-4o-mini"},
		{"gpt-4-turbo", "gpt-4o-mini"},
		{"gpt-4", "gpt-4o-mini"},
		{"claude-opus-4", "claude-3-5-haiku"},
		{"claude-sonnet-4", "claude-3-5-haiku"},
		{"claude-3-opus", "claude-3-haiku"},
		{"claude-3-5-sonnet", "claude-3-5-haiku"},
		{"o1", "o3-mini"},
		{"gemini-1.5-pro", "gemini-1.5-flash"},
		{"gemini-2.5-pro", "gemini-2.0-flash"},
	}

	n := strings.ToLower(currentModel)
	for _, swap := range familySwaps {
		if strings.HasPrefix(n, swap.from) {
			currentPrice, hasCurrent := reg.GetPrice(currentModel)
			suggestedPrice, hasSuggested := reg.GetPrice(swap.to)
			if !hasCurrent || !hasSuggested {
				// Fall back to builtin comparison via the Registry's builtin map.
				// Return the suggestion even without exact saving pct.
				return swap.to, 0, true
			}
			currentAvg := (currentPrice.InputPerMToken + currentPrice.OutputPerMToken) / 2
			suggestedAvg := (suggestedPrice.InputPerMToken + suggestedPrice.OutputPerMToken) / 2
			if suggestedAvg >= currentAvg {
				return swap.to, 0, true // still suggest even if saving is minimal
			}
			saving := (currentAvg - suggestedAvg) / currentAvg * 100
			return swap.to, saving, true
		}
	}
	return "", 0, false
}
