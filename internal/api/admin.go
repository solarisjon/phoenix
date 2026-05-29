package api

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// backupDB streams a consistent snapshot of the SQLite database to the client.
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
