package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"time"

	"github.com/solarisjon/phoenix/internal/agent"
	"github.com/solarisjon/phoenix/internal/api"
	"github.com/solarisjon/phoenix/internal/frontend"
	"github.com/solarisjon/phoenix/internal/paths"
	"github.com/solarisjon/phoenix/internal/provider/registry"
	"github.com/solarisjon/phoenix/internal/scheduler"
	"github.com/solarisjon/phoenix/internal/store/sqlite"
)

func main() {
	// Resolve and create config/data directories.
	if err := paths.Init(); err != nil {
		log.Fatalf("failed to initialise paths: %v", err)
	}

	log.Printf("Config dir : %s", paths.ConfigDir())
	log.Printf("Data dir   : %s", paths.DataDir())

	// Open database.
	dbPath := paths.DataFile("phoenix.db")
	db, err := sqlite.Open(dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()
	log.Printf("Database   : %s", dbPath)

	if err := db.Seed(context.Background()); err != nil {
		log.Fatalf("failed to seed database: %v", err)
	}
	if err := db.ResetOrphanedTasks(context.Background()); err != nil {
		log.Fatalf("failed to reset orphaned tasks: %v", err)
	}
	db.StartupHealthCheck(context.Background())

	// Wire up repositories.
	providerRepo := sqlite.NewProviderRepo(db)
	agentRepo := sqlite.NewAgentRepo(db)
	projectRepo := sqlite.NewProjectRepo(db)
	taskRepo := sqlite.NewTaskRepo(db)
	statsRepo := sqlite.NewStatsRepo(db)
	userRepo := sqlite.NewUserRepo(db)
	teamRepo := sqlite.NewTeamRepo(db)
	agentDraftRepo := sqlite.NewAgentDraftRepo(db)
	systemSettingsRepo := sqlite.NewSystemSettingsRepo(db)

	// Wire up provider registry.
	reg := registry.NewRegistry(providerRepo)

	// Build API server first so we have the hub.
	// Runner is created with a nil handler initially; we swap it after.
	runner := agent.New(agentRepo, taskRepo, projectRepo, systemSettingsRepo, reg, nil)
	defer runner.Shutdown()

	adminRepo := sqlite.NewAdminRepo(db)

	apiServer := api.New(
		providerRepo, agentRepo, projectRepo,
		taskRepo, statsRepo, userRepo, teamRepo,
		agentDraftRepo, systemSettingsRepo,
		runner, reg,
		adminRepo,
	)

	// Wire the hub as the runner's event handler so stream events
	// are broadcast to all WebSocket clients.
	hub := apiServer.Hub()
	runner.SetEventHandler(func(ev agent.StreamEvent) {
		hub.BroadcastAgentEvent(ev, taskRepo)
	})

	// Start the heartbeat scheduler. Scans agents/projects every 60s and
	// fires tasks for agents with heartbeat_interval set.
	sched := scheduler.New(agentRepo, projectRepo, taskRepo, runner, 60*time.Second)
	sched.Start()
	defer sched.Stop()

	port := os.Getenv("PHOENIX_PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()

	// Mount API.
	mux.Handle("/api/", apiServer)

	// Serve the embedded React frontend for all other routes.
	// Any path that doesn't correspond to a real file falls back to index.html
	// so that React Router can handle client-side navigation (e.g. /monitors).
	sub, err := frontend.FS()
	if err != nil {
		log.Fatalf("failed to load frontend fs: %v", err)
	}
	fileServer := http.FileServer(http.FS(sub))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to open the requested path in the embedded FS.
		// fs.FS requires paths without a leading slash or "./" prefix.
		fsPath := strings.TrimPrefix(r.URL.Path, "/")
		f, err := sub.Open(fsPath)
		if err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// File not found — serve index.html and let React Router handle it.
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	}))

	addr := fmt.Sprintf(":%s", port)
	log.Printf("Phoenix listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
