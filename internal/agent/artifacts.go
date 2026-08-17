package agent

import "strings"

// ParsedArtifact holds a single parsed ARTIFACT_START…ARTIFACT_END block from
// agent output. Fields are exported so both the runner and API packages can use them.
type ParsedArtifact struct {
	ArtType string // "file" | "url" | "jira" | "confluence" | "html" | "obsidian"
	Path    string // file path or URL
	Title   string
	Vault   string // only set when ArtType == "obsidian"
}

// ParseArtifactBlocks extracts all ARTIFACT_START … ARTIFACT_END sections from text.
//
// Expected format in agent output:
//
//	ARTIFACT_START
//	Type: file          (or "url", "jira", "confluence", "html", "obsidian")
//	Path: /abs/path     (use URL: for non-file types)
//	Title: My Document
//	Vault: VaultName    (only for obsidian type)
//	ARTIFACT_END
func ParseArtifactBlocks(output string) []ParsedArtifact {
	var results []ParsedArtifact
	lines := strings.Split(output, "\n")
	i := 0
	for i < len(lines) {
		if !isBareMarker(lines[i], "ARTIFACT_START") {
			i++
			continue
		}
		i++
		var a ParsedArtifact
		for i < len(lines) {
			if isBareMarker(lines[i], "ARTIFACT_END") {
				i++
				break
			}
			line := lines[i]
			switch {
			case has(line, "Type"):
				a.ArtType = strings.ToLower(val(line, "Type"))
			case has(line, "Path"):
				a.Path = val(line, "Path")
			case has(line, "URL"):
				a.Path = val(line, "URL")
			case has(line, "Title"):
				a.Title = val(line, "Title")
			case has(line, "Vault"):
				a.Vault = val(line, "Vault")
			}
			i++
		}
		if a.ArtType != "" && a.Path != "" {
			results = append(results, a)
		}
	}
	return results
}

// has / val are small readability wrappers over headerValue (markers.go) so the
// switch above stays declarative. Header keys are matched case-insensitively
// and tolerate Markdown decoration ("- **Path:** `/x`").
func has(line, key string) bool   { _, ok := headerValue(line, key); return ok }
func val(line, key string) string { v, _ := headerValue(line, key); return v }
