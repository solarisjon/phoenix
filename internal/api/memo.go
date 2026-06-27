package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
)

// listMemos returns memos filtered by ?status= (default: all non-archived).
func (s *Server) listMemos(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status") // "unread" | "read" | "flagged" | "archived" | ""
	list, err := s.memos.List(r.Context(), status)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if list == nil {
		list = []*model.Memo{}
	}
	respond(w, http.StatusOK, list)
}

// getMemoCount returns the unread+flagged count for the sidebar badge.
func (s *Server) getMemoCount(w http.ResponseWriter, r *http.Request) {
	count, err := s.memos.UnreadCount(r.Context())
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]int{"count": count})
}

// createMemo lets the frontend (or an agent via the API) post a memo manually.
func (s *Server) createMemo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID   string `json:"project_id"`
		ProjectName string `json:"project_name"`
		TaskID      string `json:"task_id"`
		AgentID     string `json:"agent_id"`
		AgentName   string `json:"agent_name"`
		Title       string `json:"title"`
		Body        string `json:"body"`
		Priority    string `json:"priority"` // "normal" | "high"
	}
	if !decode(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		respondErr(w, http.StatusBadRequest, "title is required")
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		respondErr(w, http.StatusBadRequest, "body is required")
		return
	}

	priority := model.MemoPriorityNormal
	if req.Priority == string(model.MemoPriorityHigh) {
		priority = model.MemoPriorityHigh
	}

	memo := &model.Memo{
		ID:          uuid.New().String(),
		ProjectID:   req.ProjectID,
		ProjectName: req.ProjectName,
		TaskID:      req.TaskID,
		AgentID:     req.AgentID,
		AgentName:   req.AgentName,
		Title:       strings.TrimSpace(req.Title),
		Body:        req.Body,
		Priority:    priority,
		Status:      model.MemoStatusUnread,
		CreatedAt:   time.Now(),
	}
	if err := s.memos.Create(r.Context(), memo); err != nil {
		respondInternalErr(w, err)
		return
	}

	// Broadcast so the briefing badge updates in real-time.
	s.hub.Broadcast(Event{
		Type:    EventMemoCreated,
		Payload: map[string]string{"memo_id": memo.ID, "title": memo.Title},
	})

	respond(w, http.StatusCreated, memo)
}

// updateMemoStatus changes a memo's status (read / flagged / archived / unread).
func (s *Server) updateMemoStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	memo, err := s.memos.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if memo == nil {
		respondErr(w, http.StatusNotFound, "memo not found")
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &req) {
		return
	}

	var status model.MemoStatus
	switch req.Status {
	case string(model.MemoStatusUnread):
		status = model.MemoStatusUnread
	case string(model.MemoStatusRead):
		status = model.MemoStatusRead
	case string(model.MemoStatusFlagged):
		status = model.MemoStatusFlagged
	case string(model.MemoStatusArchived):
		status = model.MemoStatusArchived
	default:
		respondErr(w, http.StatusBadRequest, "status must be unread, read, flagged, or archived")
		return
	}

	if err := s.memos.UpdateStatus(r.Context(), id, status); err != nil {
		respondInternalErr(w, err)
		return
	}

	memo.Status = status
	respond(w, http.StatusOK, memo)
}

// deleteMemo permanently removes a memo.
func (s *Server) deleteMemo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.memos.Delete(r.Context(), id); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusNoContent, nil)
}

// getMemoFileContent serves the raw text of an absolute .md file path.
// Used by the Briefing UI to render artifact markdown files inline.
// Only .md files are served; all other extensions are rejected.
func (s *Server) getMemoFileContent(w http.ResponseWriter, r *http.Request) {
	rawPath := r.URL.Query().Get("path")
	if rawPath == "" {
		respondErr(w, http.StatusBadRequest, "path is required")
		return
	}

	// Require an absolute path and a .md extension.
	if !filepath.IsAbs(rawPath) {
		respondErr(w, http.StatusBadRequest, "path must be absolute")
		return
	}
	if !strings.EqualFold(filepath.Ext(rawPath), ".md") {
		respondErr(w, http.StatusBadRequest, "only .md files may be viewed")
		return
	}

	clean := filepath.Clean(rawPath)
	data, err := os.ReadFile(clean)
	if os.IsNotExist(err) {
		respondErr(w, http.StatusNotFound, "file not found")
		return
	}
	if err != nil {
		respondInternalErr(w, err)
		return
	}

	const maxBytes = 512 * 1024 // 512 KB
	truncated := false
	if len(data) > maxBytes {
		data = data[:maxBytes]
		truncated = true
	}

	respond(w, http.StatusOK, map[string]interface{}{
		"content":   string(data),
		"truncated": truncated,
	})
}
