package api

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/solarisjon/phoenix/internal/agent"
)

// ---- File browser ----

// projectFileEntry is a single entry returned by listProjectFiles.
type projectFileEntry struct {
	Name       string    `json:"name"`
	RelPath    string    `json:"rel_path"` // relative to working_dir
	SizeBytes  int64     `json:"size_bytes"`
	ModifiedAt time.Time `json:"modified_at"`
	Ext        string    `json:"ext"`         // e.g. ".md", ".html", ".txt"
	IsArtifact bool      `json:"is_artifact"` // true when tagged in task output
}

// listProjectFiles lists regular files under the project's working_dir.
// Files are returned sorted by modification time (newest first).
// Hidden files and directories are excluded. Walk depth is capped at 3.
func (s *Server) listProjectFiles(w http.ResponseWriter, r *http.Request) {
	proj, err := s.projects.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if proj == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}
	if proj.WorkingDir == "" {
		respond(w, http.StatusOK, []projectFileEntry{})
		return
	}

	root := filepath.Clean(proj.WorkingDir)
	if _, err := os.Stat(root); os.IsNotExist(err) {
		respond(w, http.StatusOK, []projectFileEntry{})
		return
	}

	// Collect artifact paths from task outputs so we can badge them.
	artifactPaths := collectArtifactPaths(r.Context(), s, proj.ID)

	var entries []projectFileEntry
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		// Skip hidden files/dirs.
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			// Limit depth to 3 levels below root.
			rel, _ := filepath.Rel(root, path)
			if strings.Count(rel, string(os.PathSeparator)) >= 3 {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		_, isArtifact := artifactPaths[path]
		entries = append(entries, projectFileEntry{
			Name:       d.Name(),
			RelPath:    rel,
			SizeBytes:  info.Size(),
			ModifiedAt: info.ModTime(),
			Ext:        strings.ToLower(filepath.Ext(d.Name())),
			IsArtifact: isArtifact,
		})
		return nil
	})

	// Sort newest-first.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ModifiedAt.After(entries[j].ModifiedAt)
	})

	respond(w, http.StatusOK, entries)
}

// getProjectFileContent returns the text content of a file inside the project's
// working_dir. The file path is passed as the URL wildcard segment after /files/.
// Read is limited to 256 KB for safety.
func (s *Server) getProjectFileContent(w http.ResponseWriter, r *http.Request) {
	proj, err := s.projects.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if proj == nil {
		respondErr(w, http.StatusNotFound, "project not found")
		return
	}
	if proj.WorkingDir == "" {
		respondErr(w, http.StatusBadRequest, "project has no working directory")
		return
	}

	// Decode the relative path from the URL wildcard.
	relPath := chi.URLParam(r, "*")
	if relPath == "" {
		respondErr(w, http.StatusBadRequest, "file path required")
		return
	}

	root := filepath.Clean(proj.WorkingDir)
	abs := filepath.Clean(filepath.Join(root, relPath))

	// Guard: resolved path must be within the project root.
	if !strings.HasPrefix(abs, root+string(os.PathSeparator)) && abs != root {
		respondErr(w, http.StatusForbidden, "path outside project directory")
		return
	}

	f, err := os.Open(abs)
	if os.IsNotExist(err) {
		respondErr(w, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	defer f.Close()

	const maxRead = 256 * 1024
	lr := io.LimitReader(f, maxRead)
	data, err := io.ReadAll(lr)
	if err != nil {
		respondInternalErr(w, err)
		return
	}

	respond(w, http.StatusOK, map[string]any{
		"content":   string(data),
		"ext":       strings.ToLower(filepath.Ext(abs)),
		"truncated": int64(len(data)) == maxRead,
	})
}

// collectArtifactPaths queries recent task outputs for the project and returns
// a set of absolute paths that were declared as ARTIFACT_START file artifacts.
func collectArtifactPaths(ctx context.Context, s *Server, projectID string) map[string]struct{} {
	out := map[string]struct{}{}
	tasks, err := s.tasks.ListByProject(ctx, projectID, "", 500)
	if err != nil {
		return out
	}
	for _, t := range tasks {
		for _, a := range agent.ParseArtifactBlocks(t.Output) {
			if a.ArtType == "file" && a.Path != "" {
				out[filepath.Clean(a.Path)] = struct{}{}
			}
		}
	}
	return out
}
