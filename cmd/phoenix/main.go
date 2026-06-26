package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // embed timezone data for scratch/minimal containers

	"github.com/solarisjon/phoenix/internal/agent"
	"github.com/solarisjon/phoenix/internal/api"
	"github.com/solarisjon/phoenix/internal/config"
	"github.com/solarisjon/phoenix/internal/frontend"
	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/paths"
	"github.com/solarisjon/phoenix/internal/plugin"
	"github.com/solarisjon/phoenix/internal/pricing"
	"github.com/solarisjon/phoenix/internal/provider/registry"
	"github.com/solarisjon/phoenix/internal/scheduler"
	"github.com/solarisjon/phoenix/internal/store/sqlite"
)

func main() {
	noPlugins := flag.Bool("no-plugins", false, "disable all plugin dispatch for this session")
	flag.Parse()

	// Resolve and create config/data directories.
	if err := paths.Init(); err != nil {
		log.Fatalf("failed to initialise paths: %v", err)
	}

	log.Printf("Config dir : %s", paths.ConfigDir())
	log.Printf("Data dir   : %s", paths.DataDir())

	cfg := config.Load(paths.DataFile("phoenix.db"))

	// Open database.
	dbPath := cfg.DBPath
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
	memoRepo := sqlite.NewMemoRepo(db)
	pluginRepo := sqlite.NewPluginRepo(db)
	notificationRuleRepo := sqlite.NewNotificationRuleRepo(db)

	// Wire up plugin manager.
	pluginManager := plugin.NewManager(pluginRepo, notificationRuleRepo, systemSettingsRepo, plugin.ManagerOpts{
		NoPlugins: *noPlugins,
	})
	if err := pluginManager.SeedCorePlugins(context.Background()); err != nil {
		log.Fatalf("failed to seed core plugins: %v", err)
	}
	if err := pluginManager.LoadAll(context.Background()); err != nil {
		log.Fatalf("failed to load plugins: %v", err)
	}
	if *noPlugins {
		log.Println("Plugin dispatch disabled via --no-plugins flag")
	}

	// Wire up pricing registry: load user overrides from DB, refresh from OpenRouter.
	pricingReg := pricing.New()
	if overridesJSON, err := systemSettingsRepo.GetRaw(context.Background(), "pricing_overrides"); err != nil {
		log.Printf("pricing: failed to load overrides: %v", err)
	} else if err := pricingReg.LoadOverrides(overridesJSON); err != nil {
		log.Printf("pricing: failed to parse overrides: %v", err)
	}
	if err := pricingReg.Refresh(context.Background()); err != nil {
		log.Printf("pricing: initial OpenRouter refresh failed (using built-in table): %v", err)
	}
	pricingReg.StartRefreshLoop(context.Background(), 24*time.Hour)

	// Wire up provider registry.
	reg := registry.NewRegistry(providerRepo)

	// Build API server first so we have the hub.
	// Runner is created with a nil handler initially; we swap it after.
	runner := agent.New(agentRepo, taskRepo, projectRepo, systemSettingsRepo, memoRepo, reg, nil)
	defer runner.Shutdown()

	adminRepo := sqlite.NewAdminRepo(db)

	apiServer := api.New(
		providerRepo, agentRepo, projectRepo,
		taskRepo, statsRepo, userRepo, teamRepo,
		agentDraftRepo, systemSettingsRepo,
		memoRepo,
		pluginRepo, notificationRuleRepo, pluginManager,
		runner, reg, pricingReg,
		adminRepo,
		cfg.HTTPTimeout,
	)

	// Wire the hub as the runner's event handler so stream events
	// are broadcast to all WebSocket clients.
	hub := apiServer.Hub()

	// Wire plugin manager to receive hub events for notification dispatch.
	hub.OnEvent(pluginManager.HandleEvent)
	runner.SetEventHandler(func(ev agent.StreamEvent) {
		hub.BroadcastAgentEvent(ev, taskRepo)
	})
	// Wire the hub as the runner's memo handler so new memos
	// trigger a real-time badge update on all connected clients.
	runner.SetMemoHandler(func(memo *model.Memo) {
		hub.Broadcast(api.Event{
			Type:    api.EventMemoCreated,
			Payload: map[string]string{"memo_id": memo.ID, "title": memo.Title},
		})
	})

	// Start the monitor scheduler. Scans monitors every SchedulerInterval and
	// fires tasks for monitors with schedule_interval set.
	sched := scheduler.New(agentRepo, projectRepo, taskRepo, runner, cfg.SchedulerInterval)
	sched.Start()
	defer sched.Stop()

	// Apply configurable task timeout.
	runner.SetTaskTimeout(cfg.TaskTimeout)

	// Start the watchdog that reaps tasks past their timeout_at that slipped
	// through without a DB update (e.g. because the task context was already
	// expired when the goroutine tried to write the final status).
	runner.StartTimeoutWatchdog()

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

	addr := fmt.Sprintf(":%s", cfg.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Capture SIGINT / SIGTERM so we can drain in-flight requests cleanly.
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("Phoenix listening on http://localhost%s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-sigCtx.Done()
	log.Println("Shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP shutdown error: %v", err)
	}
	log.Println("Shutdown complete")
}
