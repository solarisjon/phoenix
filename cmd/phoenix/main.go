package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // embed timezone data for scratch/minimal containers

	"github.com/google/uuid"
	"github.com/solarisjon/phoenix/internal/agent"
	"github.com/solarisjon/phoenix/internal/api"
	"github.com/solarisjon/phoenix/internal/config"
	"github.com/solarisjon/phoenix/internal/frontend"
	"github.com/solarisjon/phoenix/internal/logging"
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

	logging.Init()

	// Resolve and create config/data directories.
	if err := paths.Init(); err != nil {
		slog.Error("failed to initialise paths", "error", err)
		os.Exit(1)
	}

	slog.Info("startup", "config_dir", paths.ConfigDir(), "data_dir", paths.DataDir())

	cfg := config.Load(paths.DataFile("phoenix.db"))

	// Open database.
	dbPath := cfg.DBPath
	db, err := sqlite.Open(dbPath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	slog.Info("database opened", "path", dbPath)

	if err := db.Seed(context.Background()); err != nil {
		slog.Error("failed to seed database", "error", err)
		os.Exit(1)
	}
	if err := db.ResetOrphanedTasks(context.Background()); err != nil {
		slog.Error("failed to reset orphaned tasks", "error", err)
		os.Exit(1)
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
	obsidianVaultRepo := sqlite.NewObsidianVaultRepo(db)

	// Wire up plugin manager.
	pluginManager := plugin.NewManager(pluginRepo, notificationRuleRepo, systemSettingsRepo, plugin.ManagerOpts{
		NoPlugins: *noPlugins,
	})
	if err := pluginManager.SeedCorePlugins(context.Background()); err != nil {
		slog.Error("failed to seed core plugins", "error", err)
		os.Exit(1)
	}
	if err := pluginManager.LoadAll(context.Background()); err != nil {
		slog.Error("failed to load plugins", "error", err)
		os.Exit(1)
	}
	if *noPlugins {
		slog.Info("plugin dispatch disabled via --no-plugins flag")
	}
	// Wire up pricing registry: load user overrides from DB, refresh from OpenRouter.
	pricingReg := pricing.New()
	if overridesJSON, err := systemSettingsRepo.GetRaw(context.Background(), "pricing_overrides"); err != nil {
		slog.Warn("pricing: failed to load overrides", "error", err)
	} else if err := pricingReg.LoadOverrides(overridesJSON); err != nil {
		slog.Warn("pricing: failed to parse overrides", "error", err)
	}
	if err := pricingReg.Refresh(context.Background()); err != nil {
		slog.Warn("pricing: initial OpenRouter refresh failed (using built-in table)", "error", err)
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
		pluginRepo, notificationRuleRepo, obsidianVaultRepo, pluginManager,
		runner, reg, pricingReg,
		adminRepo,
		cfg.HTTPTimeout,
	)

	// Wire Obsidian vault repo into runner for prompt injection and auto-write.
	runner.SetObsidianVaultRepo(obsidianVaultRepo)

	// Wire memory client into runner (nil if plugin is disabled).
	runner.SetMemoryClient(pluginManager.MemoryClient())

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

	// Wire inbound Telegram → task creation handler.
	pluginManager.SetInboundHandler(func(ctx context.Context, projectID, agentID, title, source string) (string, error) {
		assigned, err := projectRepo.IsAgentAssigned(ctx, projectID, agentID)
		if err != nil {
			return "", fmt.Errorf("check agent assignment: %w", err)
		}
		if !assigned {
			return "", fmt.Errorf("agent %s is not assigned to project %s", agentID, projectID)
		}
		t := &model.Task{
			ID:          uuid.New().String(),
			ProjectID:   projectID,
			AgentID:     agentID,
			Title:       title,
			Description: title,
			Status:      model.TaskStatusPending,
			Source:      source,
			CriticMode:  model.CriticModeInherit,
			Input:       "{}",
			Output:      "{}",
			CreatedAt:   time.Now().UTC(),
		}
		if err := taskRepo.Create(ctx, t); err != nil {
			return "", fmt.Errorf("create task: %w", err)
		}
		if err := runner.RunTask(ctx, t.ID); err != nil {
			slog.Error("telegram inbound: task created but failed to queue", "task_id", t.ID, "error", err)
		}
		return t.ID, nil
	})

	// Capture SIGINT / SIGTERM so we can drain in-flight requests cleanly.
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start Telegram inbound pollers (after sigCtx so they share the same lifetime).
	pluginManager.StartPollers(sigCtx)
	defer pluginManager.StopPollers()

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
		slog.Error("failed to load frontend fs", "error", err)
		os.Exit(1)
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

	go func() {
		slog.Info("Phoenix listening", "addr", fmt.Sprintf("http://localhost%s", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-sigCtx.Done()
	slog.Info("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("HTTP shutdown error", "error", err)
	}
	slog.Info("shutdown complete")
}
