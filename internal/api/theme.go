package api

import (
	"encoding/json"
	"net/http"

	"github.com/solarisjon/phoenix/internal/model"
)

// ThemeResponse represents a theme for the frontend, combining built-in and community themes.
type ThemeResponse struct {
	ID          string            `json:"id"`
	Kind        string            `json:"kind"`    // "dark" | "light"
	Label       string            `json:"label"`
	Description string            `json:"description,omitempty"`
	Preview     []string          `json:"preview"` // 3 hex colors
	Vars        map[string]string `json:"vars,omitempty"`
	IsBuiltIn   bool              `json:"is_built_in"`
	PluginID    string            `json:"plugin_id,omitempty"` // set for community themes
}

// themeConfig is the structure stored in the plugin config JSON for theme plugins.
type themeConfig struct {
	Kind    string            `json:"kind"`
	Preview []string          `json:"preview"`
	Vars    map[string]string `json:"vars"`
}

// listThemes returns community themes from the plugins table.
// Built-in themes are handled client-side and merged by the frontend.
func (s *Server) listThemes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	themes, err := s.pluginRepo.ListByType(ctx, model.PluginTypeTheme)
	if err != nil {
		respondErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	result := []ThemeResponse{}
	for _, p := range themes {
		var cfg themeConfig
		if err := json.Unmarshal([]byte(p.Config), &cfg); err != nil {
			continue // skip malformed themes
		}
		result = append(result, ThemeResponse{
			ID:        "plugin-" + p.ID,
			Kind:      cfg.Kind,
			Label:     p.Name,
			Preview:   cfg.Preview,
			Vars:      cfg.Vars,
			IsBuiltIn: false,
			PluginID:  p.ID,
		})
	}

	respond(w, http.StatusOK, result)
}
