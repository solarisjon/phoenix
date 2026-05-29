// Package api implements the Phoenix HTTP API server.
package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/solarisjon/phoenix/internal/agent"
	"github.com/solarisjon/phoenix/internal/provider/registry"
	"github.com/solarisjon/phoenix/internal/store"
	"github.com/solarisjon/phoenix/internal/store/sqlite"
)

// Server holds all dependencies and exposes the HTTP handler.
type Server struct {
	providers store.ProviderRepo
	agents    store.AgentRepo
	projects  store.ProjectRepo
	tasks     store.TaskRepo
	stats     store.StatsRepo
	users     store.UserRepo
	teams     store.TeamRepo
	runner    *agent.Runner
	registry  *registry.Registry
	hub       *Hub
	router    http.Handler
	admin     *sqlite.AdminRepo
}

// New creates a Server and registers all routes.
func New(
	providers store.ProviderRepo,
	agents store.AgentRepo,
	projects store.ProjectRepo,
	tasks store.TaskRepo,
	stats store.StatsRepo,
	users store.UserRepo,
	teams store.TeamRepo,
	runner *agent.Runner,
	reg *registry.Registry,
	admin *sqlite.AdminRepo,
) *Server {
	s := &Server{
		providers: providers,
		agents:    agents,
		projects:  projects,
		tasks:     tasks,
		stats:     stats,
		users:     users,
		teams:     teams,
		runner:    runner,
		registry:  reg,
		hub:       NewHub(),
		admin:     admin,
	}
	s.router = s.buildRouter()
	return s
}

// Hub returns the event hub so callers can broadcast events.
func (s *Server) Hub() *Hub { return s.hub }

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) buildRouter() http.Handler {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(corsMiddleware)

	r.Route("/api", func(r chi.Router) {
		// Providers
		r.Get("/providers", s.listProviders)
		r.Post("/providers", s.createProvider)
		r.Get("/providers/{id}", s.getProvider)
		r.Put("/providers/{id}", s.updateProvider)
		r.Delete("/providers/{id}", s.deleteProvider)

		// Agents
		r.Get("/agents", s.listAgents)
		r.Post("/agents", s.createAgent)
		r.Post("/agents/generate", s.generateAgent)
		r.Post("/agents/spawn", s.spawnTask)
		r.Get("/agents/{id}", s.getAgent)
		r.Put("/agents/{id}", s.updateAgent)
		r.Delete("/agents/{id}", s.deleteAgent)

		// Teams
		r.Get("/teams", s.listTeams)
		r.Post("/teams", s.createTeam)
		r.Get("/teams/{id}", s.getTeam)
		r.Put("/teams/{id}", s.updateTeam)
		r.Delete("/teams/{id}", s.deleteTeam)
		r.Post("/teams/{id}/agents", s.addTeamAgent)
		r.Delete("/teams/{id}/agents/{agentId}", s.removeTeamAgent)
		r.Get("/teams/{id}/export", s.exportTeam)

		// Import
		r.Post("/import/team", s.importTeam)

		// Projects
		r.Get("/projects", s.listProjects)
		r.Post("/projects", s.createProject)
		r.Get("/projects/{id}", s.getProject)
		r.Put("/projects/{id}", s.updateProject)
		r.Delete("/projects/{id}", s.deleteProject)
		r.Post("/projects/{id}/agents", s.assignAgent)
		r.Delete("/projects/{id}/agents/{agentId}", s.removeAgent)
		r.Get("/projects/{id}/agents", s.listProjectAgents)
		r.Post("/projects/{id}/teams", s.assignTeamToProject)

		// Tasks
		r.Get("/tasks", s.listTasks)
		r.Post("/tasks", s.createTask)
		r.Get("/tasks/running", s.listRunningTasks)
		r.Get("/tasks/attention", s.listAttentionTasks)
		r.Get("/tasks/{id}", s.getTask)
		r.Put("/tasks/{id}", s.updateTask)
		r.Delete("/tasks/{id}", s.deleteTask)
		r.Post("/tasks/{id}/retry", s.retryTask)
		r.Post("/tasks/{id}/dismiss", s.dismissTask)
		r.Post("/tasks/{id}/followup", s.followUpTask)

		// Inbox
		r.Get("/inbox", s.listInbox)
		r.Post("/inbox/{taskId}/approve", s.approveTask)
		r.Post("/inbox/{taskId}/reject", s.rejectTask)
		r.Post("/inbox/{taskId}/revise", s.reviseTask)

		// Stats
		r.Get("/stats/costs", s.getCosts)

		// Admin
		r.Get("/admin/backup", s.backupDB)

		// WebSocket
		r.Get("/ws", s.handleWS)
	})

	return r
}

// corsMiddleware adds permissive CORS headers for local development.
// In production (same-origin, embedded frontend) these are not needed but
// do no harm.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
