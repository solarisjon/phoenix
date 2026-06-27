package agent

import "strings"

// ParsedArtifact holds a single parsed ARTIFACT_START…ARTIFACT_END block from
// agent output. Fields are exported so both the runner and API packages can use them.
type ParsedArtifact struct {
	ArtType string // "file" | "url" | "jira" | "confluence" | "html"
	Path    string // file path or URL
	Title   string
}

// ParseArtifactBlocks extracts all ARTIFACT_START … ARTIFACT_END sections from text.
//
// Expected format in agent output:
//
//	ARTIFACT_START
//	Type: file          (or "url", "jira", "confluence", "html")
//	Path: /abs/path     (use URL: for non-file types)
//	Title: My Document
//	ARTIFACT_END
func ParseArtifactBlocks(output string) []ParsedArtifact {
	var results []ParsedArtifact
	lines := strings.Split(output, "\n")
	i := 0
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) != "ARTIFACT_START" {
			i++
			continue
		}
		i++
		var a ParsedArtifact
		for i < len(lines) {
			if strings.TrimSpace(lines[i]) == "ARTIFACT_END" {
				i++
				break
			}
			line := lines[i]
			switch {
			case strings.HasPrefix(line, "Type:"):
				a.ArtType = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "Type:")))
			case strings.HasPrefix(line, "Path:"):
				a.Path = strings.TrimSpace(strings.TrimPrefix(line, "Path:"))
			case strings.HasPrefix(line, "URL:"):
				a.Path = strings.TrimSpace(strings.TrimPrefix(line, "URL:"))
			case strings.HasPrefix(line, "Title:"):
				a.Title = strings.TrimSpace(strings.TrimPrefix(line, "Title:"))
			}
			i++
		}
		if a.ArtType != "" && a.Path != "" {
			results = append(results, a)
		}
	}
	return results
}
