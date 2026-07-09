package api

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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

var skillSlugInvalidChars = regexp.MustCompile(`[^a-z0-9]+`)

// skillSlugify normalises a name into a lowercase, underscore-separated
// token, e.g. "Morning Coffee" -> "morning_coffee".
func skillSlugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = skillSlugInvalidChars.ReplaceAllString(s, "_")
	return strings.Trim(s, "_")
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
