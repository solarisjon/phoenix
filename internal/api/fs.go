package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type statResponse struct {
	Exists bool `json:"exists"`
	IsDir  bool `json:"is_dir"`
}

type mkdirRequest struct {
	Path string `json:"path"`
}

type mkdirResponse struct {
	Created bool `json:"created"`
}

// statHandler handles GET /api/fs/stat?path=<path>
// Returns whether the path exists and whether it is a directory.
func (s *Server) statHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		respondErr(w, http.StatusBadRequest, "path is required")
		return
	}
	if !filepath.IsAbs(path) {
		respondErr(w, http.StatusBadRequest, "path must be absolute")
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			respond(w, http.StatusOK, statResponse{Exists: false, IsDir: false})
			return
		}
		respondErr(w, http.StatusBadRequest, err.Error())
		return
	}

	respond(w, http.StatusOK, statResponse{Exists: true, IsDir: info.IsDir()})
}

// mkdirHandler handles POST /api/fs/mkdir
// Creates the directory (and all parents) at the given path.
func (s *Server) mkdirHandler(w http.ResponseWriter, r *http.Request) {
	var req mkdirRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	path := strings.TrimSpace(req.Path)
	if path == "" {
		respondErr(w, http.StatusBadRequest, "path is required")
		return
	}
	if !filepath.IsAbs(path) {
		respondErr(w, http.StatusBadRequest, "path must be absolute")
		return
	}

	// Check if it already exists.
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			respondErr(w, http.StatusBadRequest, "path exists but is not a directory")
			return
		}
		// Already a directory — idempotent success.
		respond(w, http.StatusOK, mkdirResponse{Created: false})
		return
	}
	if !os.IsNotExist(err) {
		respondErr(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := os.MkdirAll(path, 0755); err != nil {
		respondErr(w, http.StatusBadRequest, err.Error())
		return
	}

	respond(w, http.StatusOK, mkdirResponse{Created: true})
}
