// Package api implements the Phoenix HTTP API server.
package api

import (
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/solarisjon/phoenix/internal/agent"
	"github.com/solarisjon/phoenix/internal/logging"
	"github.com/solarisjon/phoenix/internal/plugin"
	"github.com/solarisjon/phoenix/internal/pricing"
	"github.com/solarisjon/phoenix/internal/provider/registry"
	"github.com/solarisjon/phoenix/internal/store"
	"github.com/solarisjon/phoenix/internal/store/sqlite"
)

// Server holds all dependencies and exposes the HTTP handler.
type Server struct {
	providers      store.ProviderRepo
	agents         store.AgentRepo
	projects       store.ProjectRepo
	tasks          store.TaskRepo
	stats          store.StatsRepo
	users          store.UserRepo
	teams          store.TeamRepo
	agentDrafts    store.AgentDraftRepo
	systemSettings store.SystemSettingsRepo
	memos          store.MemoRepo
	pluginRepo     store.PluginRepo
	ruleRepo       store.NotificationRuleRepo
	pluginManager  *plugin.Manager
	runner         *agent.Runner
	registry       *registry.Registry
	pricingReg     *pricing.Registry
	hub            *Hub
	router         http.Handler
	admin          *sqlite.AdminRepo
	startTime      time.Time
	httpTimeout    time.Duration
}

// New creates a Server and registers all routes.
// httpTimeout controls the per-request handler deadline (0 uses 60s default).
func New(
	providers store.ProviderRepo,
	agents store.AgentRepo,
	projects store.ProjectRepo,
	tasks store.TaskRepo,
	stats store.StatsRepo,
	users store.UserRepo,
	teams store.TeamRepo,
	agentDrafts store.AgentDraftRepo,
	systemSettings store.SystemSettingsRepo,
	memos store.MemoRepo,
	pluginRepo store.PluginRepo,
	ruleRepo store.NotificationRuleRepo,
	pluginManager *plugin.Manager,
	runner *agent.Runner,
	reg *registry.Registry,
	pricingReg *pricing.Registry,
	admin *sqlite.AdminRepo,
	httpTimeout time.Duration,
) *Server {
	if httpTimeout <= 0 {
		httpTimeout = 60 * time.Second
	}
	s := &Server{
		providers:      providers,
		agents:         agents,
		projects:       projects,
		tasks:          tasks,
		stats:          stats,
		users:          users,
		teams:          teams,
		agentDrafts:    agentDrafts,
		systemSettings: systemSettings,
		memos:          memos,
		pluginRepo:     pluginRepo,
		ruleRepo:       ruleRepo,
		pluginManager:  pluginManager,
		runner:         runner,
		registry:       reg,
		pricingReg:     pricingReg,
		hub:            NewHub(),
		admin:          admin,
		startTime:      time.Now(),
		httpTimeout:    httpTimeout,
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
	r.Use(middleware.Timeout(s.httpTimeout))
	r.Use(corsMiddleware)
	r.Use(requestLoggerMiddleware)

	r.Route("/api", func(r chi.Router) {
		// Providers
		r.Get("/providers", s.listProviders)
		r.Post("/providers", s.createProvider)
		r.Get("/providers/{id}", s.getProvider)
		r.Put("/providers/{id}", s.updateProvider)
		r.Delete("/providers/{id}", s.deleteProvider)
		r.Get("/providers/{id}/models", s.listProviderModels)
		r.Put("/providers/{id}/pricing", s.updateProviderPricing) // before /{id} catch-all
		r.Post("/providers/{id}/resync", s.resyncProvider)
		r.Post("/providers/{id}/test", s.testProvider)

		// Agents
		r.Get("/agents", s.listAgents)
		r.Post("/agents", s.createAgent)
		r.Post("/agents/generate", s.generateAgent)
		r.Post("/agents/import", s.importAgent)
		r.Post("/agents/spawn", s.spawnTask)
		r.Get("/agents/{id}/export", s.exportAgent)
		r.Get("/agents/{id}", s.getAgent)
		r.Put("/agents/{id}", s.updateAgent)
		r.Delete("/agents/{id}", s.deleteAgent)
		r.Get("/agents/{id}/tasks", s.listAgentTasks)
		r.Delete("/agents/{id}/memory", s.clearAgentMemory)

		// Teams
		r.Get("/teams", s.listTeams)
		r.Post("/teams", s.createTeam)
		r.Post("/teams/generate-description", s.generateTeamDescription)
		r.Get("/teams/{id}", s.getTeam)
		r.Put("/teams/{id}", s.updateTeam)
		r.Delete("/teams/{id}", s.deleteTeam)
		r.Post("/teams/{id}/agents", s.addTeamAgent)
		r.Delete("/teams/{id}/agents/{agentId}", s.removeTeamAgent)
		r.Post("/teams/{id}/broadcast", s.broadcastTeam)
		r.Get("/teams/{id}/export", s.exportTeam)

		// Import
		r.Post("/import/team", s.importTeam)

		// Projects
		r.Get("/projects", s.listProjects)
		r.Post("/projects", s.createProject)
		r.Post("/projects/generate-description", s.generateProjectDescription)
		r.Get("/projects/summaries", s.listProjectSummaries)
		r.Get("/projects/{id}", s.getProject)
		r.Put("/projects/{id}", s.updateProject)
		r.Delete("/projects/{id}", s.deleteProject)
		r.Post("/projects/{id}/archive", s.archiveProject)
		r.Post("/projects/{id}/restore", s.restoreProject)
		r.Post("/projects/{id}/agents", s.assignAgent)
		r.Delete("/projects/{id}/agents/{agentId}", s.removeAgent)
		r.Get("/projects/{id}/agents", s.listProjectAgents)
		r.Post("/projects/{id}/teams", s.assignTeamToProject)
		r.Get("/projects/{id}/summary", s.getProjectSummary)
		r.Get("/projects/{id}/spend", s.getProjectSpend)
		r.Get("/projects/{id}/files", s.listProjectFiles)
		r.Get("/projects/{id}/files/*", s.getProjectFileContent)
		r.Get("/projects/{id}/history", s.listProjectHistory)
		r.Post("/projects/{id}/suggest", s.suggestProjectNextAction)

		// Tasks
		r.Get("/tasks", s.listTasks)
		r.Post("/tasks", s.createTask)
		r.Post("/tasks/quick", s.quickTask)
		r.Get("/tasks/search", s.searchTasks)
		r.Post("/tasks/estimate", s.estimateTask)
		r.Post("/tasks/generate-description", s.generateTaskDescription)
		r.Get("/tasks/running", s.listRunningTasks)
		r.Get("/tasks/attention", s.listAttentionTasks)
		r.Get("/tasks/{id}", s.getTask)
		r.Put("/tasks/{id}", s.updateTask)
		r.Delete("/tasks/{id}", s.deleteTask)
		r.Post("/tasks/{id}/retry", s.retryTask)
		r.Post("/tasks/{id}/cancel", s.cancelTask)
		r.Post("/tasks/{id}/force-reset", s.forceResetTask)
		r.Post("/tasks/{id}/dismiss", s.dismissTask)
		r.Post("/tasks/{id}/undismiss", s.undismissTask)
		r.Post("/tasks/{id}/followup", s.followUpTask)

		// Agent drafts (pending hire proposals)
		r.Get("/agent-drafts", s.listAgentDrafts)
		r.Post("/agent-drafts", s.createAgentDraft)
		r.Put("/agent-drafts/{id}", s.updateAgentDraft)
		r.Post("/agent-drafts/{id}/approve", s.approveAgentDraft)
		r.Post("/agent-drafts/{id}/reject", s.rejectAgentDraft)
		r.Post("/agent-drafts/{id}/dismiss", s.dismissAgentDraft)

		// Inbox
		r.Get("/inbox", s.listInbox)
		r.Post("/inbox/dismiss-all", s.dismissAllInbox) // static before {taskId}
		r.Post("/inbox/{taskId}/approve", s.approveTask)
		r.Post("/inbox/{taskId}/reject", s.rejectTask)
		r.Post("/inbox/{taskId}/revise", s.reviseTask)

		// Memos (Briefing)
		r.Get("/memos", s.listMemos)
		r.Post("/memos", s.createMemo)
		r.Get("/memos/count", s.getMemoCount)
		r.Put("/memos/{id}/status", s.updateMemoStatus)
		r.Delete("/memos/{id}", s.deleteMemo)

		// Plugins
		r.Get("/plugins", s.listPlugins)
		r.Post("/plugins", s.createPlugin)
		r.Get("/plugins/{id}", s.getPlugin)
		r.Put("/plugins/{id}", s.updatePlugin)
		r.Delete("/plugins/{id}", s.deletePlugin)
		r.Post("/plugins/{id}/enable", s.enablePlugin)
		r.Post("/plugins/{id}/disable", s.disablePlugin)
		r.Post("/plugins/{id}/test", s.testPlugin)
		r.Get("/plugins/{id}/schema", s.getPluginSchema)
		r.Get("/plugins/{id}/chats", s.discoverTelegramChats)
		r.Get("/plugins/{id}/rules", s.listPluginRules)
		r.Post("/plugins/{id}/rules", s.createPluginRule)
		r.Put("/plugins/{id}/rules/{rid}", s.updatePluginRule)
		r.Delete("/plugins/{id}/rules/{rid}", s.deletePluginRule)

		// Themes
		r.Get("/themes", s.listThemes)

		// Filesystem helpers
		r.Get("/fs/stat", s.statHandler)
		r.Post("/fs/mkdir", s.mkdirHandler)

		// Stats
		r.Get("/stats/costs", s.getCosts)
		r.Get("/stats/costs/insights", s.getCostInsights)

		// Admin / system settings
		r.Get("/admin/backup", s.backupDB)
		r.Post("/admin/restore", s.restoreDB)
		r.Get("/admin/sysinfo", s.getSysInfo)
		r.Get("/admin/settings", s.getSystemSettings)
		r.Put("/admin/settings", s.updateSystemSettings)
		r.Post("/admin/settings/generate-guardrails", s.generateGlobalGuardrails)

		// WebSocket
		r.Get("/ws", s.handleWS)
	})

	return r
}

// corsMiddleware adds CORS headers for localhost origins (development) and
// any additional origin supplied via PHOENIX_CORS_ORIGIN. In production the
// embedded frontend is same-origin and no CORS headers are needed, so requests
// from unexpected origins are simply passed through without ACAO headers.
func corsMiddleware(next http.Handler) http.Handler {
	// Build the allowed-origin set from env (comma-separated, optional).
	extra := strings.Split(os.Getenv("PHOENIX_CORS_ORIGIN"), ",")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && isAllowedOrigin(origin, extra) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requestLoggerMiddleware injects a request-scoped slog.Logger carrying the
// chi request ID into the request context. Downstream handlers can retrieve
// it via logging.FromContext(r.Context()).
func requestLoggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := middleware.GetReqID(r.Context())
		l := slog.Default()
		if reqID != "" {
			l = l.With("req_id", reqID)
		}
		next.ServeHTTP(w, r.WithContext(logging.WithLogger(r.Context(), l)))
	})
}

// isAllowedOrigin returns true if origin is a localhost/loopback address or
// matches one of the explicitly configured extra origins.
func isAllowedOrigin(origin string, extra []string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname() // strips port
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	for _, e := range extra {
		if e = strings.TrimSpace(e); e != "" && strings.EqualFold(e, origin) {
			return true
		}
	}
	return false
}
