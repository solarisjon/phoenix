package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/solarisjon/phoenix/internal/agent"
	"github.com/solarisjon/phoenix/internal/model"
)

// ---- Skill CRUD ----
//
// A skill is a reusable, named instruction set. Users bind one to a project
// as its default, or invoke one ad hoc by mentioning its slug in a task or
// project's text. See internal/agent/prompt.go InjectSkills.

func (s *Server) listSkills(w http.ResponseWriter, r *http.Request) {
	skills, err := s.skills.List(r.Context())
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if skills == nil {
		skills = []*model.Skill{}
	}
	respond(w, http.StatusOK, skills)
}

func (s *Server) getSkill(w http.ResponseWriter, r *http.Request) {
	sk, err := s.skills.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if sk == nil {
		respondErr(w, http.StatusNotFound, "skill not found")
		return
	}
	respond(w, http.StatusOK, sk)
}

// skillSlugify normalises a name into a lowercase, underscore-separated
// token, e.g. "Morning Coffee" -> "morning_coffee".
func skillSlugify(s string) string {
	return agent.NormalizeSkillSlug(s)
}

func (s *Server) createSkill(w http.ResponseWriter, r *http.Request) {
	var sk model.Skill
	if !decode(w, r, &sk) {
		return
	}
	if strings.TrimSpace(sk.Name) == "" || strings.TrimSpace(sk.Instructions) == "" {
		respondErr(w, http.StatusBadRequest, "name and instructions are required")
		return
	}
	if strings.TrimSpace(sk.Slug) == "" {
		sk.Slug = skillSlugify(sk.Name)
	} else {
		sk.Slug = skillSlugify(sk.Slug)
	}
	if sk.Slug == "" {
		respondErr(w, http.StatusBadRequest, "name must contain at least one alphanumeric character")
		return
	}
	if existing, err := s.skills.GetBySlug(r.Context(), sk.Slug); err != nil {
		respondInternalErr(w, err)
		return
	} else if existing != nil {
		respondErr(w, http.StatusConflict, "a skill with this slug already exists")
		return
	}
	sk.ID = uuid.New().String()
	sk.Enabled = true
	sk.CreatedAt = time.Now().UTC()
	if err := s.skills.Create(r.Context(), &sk); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusCreated, sk)
}

func (s *Server) updateSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := s.skills.Get(r.Context(), id)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	if existing == nil {
		respondErr(w, http.StatusNotFound, "skill not found")
		return
	}
	var sk model.Skill
	if !decode(w, r, &sk) {
		return
	}
	if strings.TrimSpace(sk.Name) == "" || strings.TrimSpace(sk.Instructions) == "" {
		respondErr(w, http.StatusBadRequest, "name and instructions are required")
		return
	}
	if strings.TrimSpace(sk.Slug) == "" {
		sk.Slug = skillSlugify(sk.Name)
	} else {
		sk.Slug = skillSlugify(sk.Slug)
	}
	if sk.Slug == "" {
		respondErr(w, http.StatusBadRequest, "name must contain at least one alphanumeric character")
		return
	}
	if sk.Slug != existing.Slug {
		if other, err := s.skills.GetBySlug(r.Context(), sk.Slug); err != nil {
			respondInternalErr(w, err)
			return
		} else if other != nil && other.ID != id {
			respondErr(w, http.StatusConflict, "a skill with this slug already exists")
			return
		}
	}
	sk.ID = id
	sk.CreatedAt = existing.CreatedAt
	if err := s.skills.Update(r.Context(), &sk); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, sk)
}

func (s *Server) deleteSkill(w http.ResponseWriter, r *http.Request) {
	if err := s.skills.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) importSkills(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dirs      []string `json:"dirs"`
		Slugs     []string `json:"slugs"`
		Overwrite bool     `json:"overwrite"`
		DryRun    bool     `json:"dry_run"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if !decode(w, r, &req) {
			return
		}
	}

	dirs := req.Dirs
	if len(dirs) == 0 {
		settings, err := s.systemSettings.Get(r.Context())
		if err != nil {
			respondInternalErr(w, err)
			return
		}
		if settings != nil {
			dirs = settings.SkillImportDirs
		}
	}
	if len(dirs) == 0 {
		respondErr(w, http.StatusBadRequest, "no skill import directories configured — add paths in Settings → Plugins → Skills")
		return
	}

	if req.DryRun {
		scanned, err := agent.ScanFilesystemSkills(r.Context(), s.skills, dirs)
		if err != nil {
			respondInternalErr(w, err)
			return
		}
		respond(w, http.StatusOK, map[string]any{
			"discovered": len(scanned),
			"skills":     scanned,
		})
		return
	}

	if len(req.Slugs) == 0 {
		respondErr(w, http.StatusBadRequest, "select at least one skill to import")
		return
	}

	result, err := agent.ImportFilesystemSkills(r.Context(), s.skills, dirs, req.Slugs, req.Overwrite)
	if err != nil {
		respondInternalErr(w, err)
		return
	}
	respond(w, http.StatusOK, result)
}

func (s *Server) bulkDeleteSkills(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if !decode(w, r, &req) {
		return
	}
	if len(req.IDs) == 0 {
		respondErr(w, http.StatusBadRequest, "ids is required")
		return
	}
	deleted := 0
	var errors []string
	for _, id := range req.IDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if err := s.skills.Delete(r.Context(), id); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", id, err))
			continue
		}
		deleted++
	}
	respond(w, http.StatusOK, map[string]any{
		"deleted": deleted,
		"errors":  errors,
	})
}
