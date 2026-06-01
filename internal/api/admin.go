package api

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// restoreDB accepts a multipart file upload of a Phoenix SQLite backup.
// The file is validated, staged as {dbPath}.restore-pending, and applied
// on the next server start (safe hot-swap is not possible with an open DB).
func (s *Server) restoreDB(w http.ResponseWriter, r *http.Request) {
	if s.admin == nil {
		respondErr(w, http.StatusServiceUnavailable, "restore not available")
		return
	}

	const maxUpload = 512 << 20 // 512 MB
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		respondErr(w, http.StatusBadRequest, "failed to parse multipart form")
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		respondErr(w, http.StatusBadRequest, "missing 'file' field in form")
		return
	}
	defer file.Close()

	// Write to a temp file first so StageRestore can validate + rename atomically.
	tmp, err := os.CreateTemp("", "phoenix-restore-upload-*.db")
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		respondInternalErr(w, fmt.Errorf("write upload: %w", err))
		return
	}
	tmp.Close()

	if err := s.admin.StageRestore(tmpPath); err != nil {
		log.Printf("restore: stage failed: %v", err)
		respondErr(w, http.StatusBadRequest, err.Error())
		return
	}

	log.Printf("restore: backup staged; restart required to apply")
	respond(w, http.StatusOK, map[string]string{
		"message": "Restore staged. Restart the Phoenix server to apply the backup. All current data will be replaced.",
	})
}


func (s *Server) getSysInfo(w http.ResponseWriter, r *http.Request) {
	type taskCount struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
	}
	type sysInfoResponse struct {
		Version      string      `json:"version"`
		UptimeSeconds float64    `json:"uptime_seconds"`
		GoVersion    string      `json:"go_version"`
		DBSizeBytes  int64       `json:"db_size_bytes"`
		DBPath       string      `json:"db_path"`
		TotalTasks   int         `json:"total_tasks"`
		TaskCounts   []taskCount `json:"task_counts"`
		ActiveTasks  int         `json:"active_tasks"`
	}

	resp := sysInfoResponse{
		Version:       "v0.1",
		UptimeSeconds: time.Since(s.startTime).Seconds(),
		GoVersion:     runtime.Version(),
		ActiveTasks:   len(s.runner.ActiveTasks()),
	}

	if s.admin != nil {
		resp.DBPath = s.admin.DBPath()
		if fi, err := os.Stat(resp.DBPath); err == nil {
			resp.DBSizeBytes = fi.Size()
		}
	}

	if counts, err := s.stats.TaskCountByStatus(r.Context()); err == nil {
		for _, c := range counts {
			resp.TaskCounts = append(resp.TaskCounts, taskCount{Status: c.Status, Count: c.Count})
			resp.TotalTasks += c.Count
		}
	}

	respond(w, http.StatusOK, resp)
}
//
// It uses VACUUM INTO to produce a clean, WAL-consolidated copy in a temp file,
// then streams that file as an application/octet-stream download and removes it.
// Safe to call while the server is running — VACUUM INTO takes a read lock and
// does not block normal reads or writes.
func (s *Server) backupDB(w http.ResponseWriter, r *http.Request) {
	if s.admin == nil {
		respondErr(w, http.StatusServiceUnavailable, "backup not available")
		return
	}

	dbPath := s.admin.DBPath()
	ts := time.Now().Format("20060102-150405")
	tmpPath := filepath.Join(filepath.Dir(dbPath), fmt.Sprintf(".backup-%s.db", ts))
	defer os.Remove(tmpPath) // clean up even if streaming fails

	if err := s.admin.VacuumInto(r.Context(), tmpPath); err != nil {
		log.Printf("backup: %v", err)
		respondInternalErr(w, err)
		return
	}

	f, err := os.Open(tmpPath)
	if err != nil {
		log.Printf("backup: open temp file: %v", err)
		respondInternalErr(w, fmt.Errorf("backup open: %w", err))
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		respondInternalErr(w, fmt.Errorf("backup stat: %w", err))
		return
	}

	filename := fmt.Sprintf("phoenix-backup-%s.db", ts)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, f); err != nil {
		log.Printf("backup: stream error: %v", err)
	}
}
