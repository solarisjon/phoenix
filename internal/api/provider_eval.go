package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/agent"
	"github.com/solarisjon/phoenix/internal/agent/eval"
	"github.com/solarisjon/phoenix/internal/model"
)

// Model evaluation ("Phoenix compatibility") runs are asynchronous: POST
// starts one and returns a run ID; GET polls it; progress is also broadcast
// over WebSocket as provider.eval_progress. On completion the result is
// stored on the provider's model-pool entry (ModelEntry.Compatibility).

// EventProviderEval is the WS event type for eval progress/completion.
const EventProviderEval EventType = "provider.eval_progress"

type evalRun struct {
	ID         string       `json:"id"`
	ProviderID string       `json:"provider_id"`
	Model      string       `json:"model"`
	Status     string       `json:"status"` // running | done | error
	Progress   string       `json:"progress"`
	Index      int          `json:"index"`
	Total      int          `json:"total"`
	Report     *eval.Report `json:"report,omitempty"`
	Error      string       `json:"error,omitempty"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt *time.Time   `json:"finished_at,omitempty"`
}

var (
	evalRunsMu sync.Mutex
	evalRuns   = map[string]*evalRun{}
)

func snapshotRun(run *evalRun) evalRun {
	evalRunsMu.Lock()
	defer evalRunsMu.Unlock()
	return *run
}

// startProviderEval — POST /api/providers/{id}/eval  {model?, profile?, skip_perf?}
func (s *Server) startProviderEval(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body struct {
		Model    string `json:"model"`
		Profile  string `json:"profile"`
		SkipPerf bool   `json:"skip_perf"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	rec, err := s.providers.Get(r.Context(), id)
	if err != nil || rec == nil {
		respondErr(w, http.StatusNotFound, "provider not found")
		return
	}
	if rec.Type != model.ProviderTypeLLM {
		respondErr(w, http.StatusBadRequest, "evaluation is only supported for LLM providers")
		return
	}
	prov, err := s.registry.GetWithOverride(context.Background(), rec.ID, body.Model)
	if err != nil {
		respondErr(w, http.StatusBadRequest, fmt.Sprintf("provider load failed: %v", err))
		return
	}
	profile := agent.ResolveModelProfile(r.Context(), prov, rec, body.Model)
	opts := eval.Options{
		Profile:       profile.Profile,
		ContextWindow: profile.ContextWindow,
		Model:         profile.ModelID,
		ProviderID:    rec.ID,
		SkipPerf:      body.SkipPerf,
		Timeout:       5 * time.Minute,
	}
	if body.Profile != "" {
		opts.Profile = model.PromptProfile(body.Profile)
	}

	run := &evalRun{ID: uuid.New().String(), ProviderID: rec.ID, Model: opts.Model, Status: "running", StartedAt: time.Now()}
	evalRunsMu.Lock()
	evalRuns[run.ID] = run
	evalRunsMu.Unlock()

	broadcast := func() {
		if s.hub != nil {
			s.hub.Broadcast(Event{Type: EventProviderEval, Payload: snapshotRun(run)})
		}
	}
	opts.Progress = func(name string, i, n int) {
		evalRunsMu.Lock()
		run.Progress, run.Index, run.Total = name, i, n
		evalRunsMu.Unlock()
		broadcast()
	}

	go func() {
		// Detached from the request; bounded overall.
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
		defer cancel()
		rep := eval.Run(ctx, prov, opts)
		now := time.Now()
		evalRunsMu.Lock()
		run.Report, run.Status, run.FinishedAt = &rep, "done", &now
		run.Progress, run.Index = "done", run.Total
		evalRunsMu.Unlock()
		if err := s.saveCompatibility(ctx, rec.ID, opts.Model, rep); err != nil {
			slog.Warn("provider eval: save compatibility", "provider_id", rec.ID, "error", err)
			evalRunsMu.Lock()
			run.Error = "result not saved: " + err.Error()
			evalRunsMu.Unlock()
		}
		broadcast()
		slog.Info("provider eval finished", "provider_id", rec.ID, "model", opts.Model, "score", rep.Score, "grade", rep.Grade)
	}()

	respond(w, http.StatusAccepted, snapshotRun(run))
}

// getProviderEval — GET /api/providers/{id}/eval/{runID}
func (s *Server) getProviderEval(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runID")
	evalRunsMu.Lock()
	run, ok := evalRuns[runID]
	evalRunsMu.Unlock()
	if !ok || run.ProviderID != chi.URLParam(r, "id") {
		respondErr(w, http.StatusNotFound, "eval run not found")
		return
	}
	respond(w, http.StatusOK, snapshotRun(run))
}

// saveCompatibility writes the eval result onto the provider's model-pool
// entry for the model, creating the entry (with the suggested tier/profile)
// when the model isn't in the pool yet.
func (s *Server) saveCompatibility(ctx context.Context, providerID, modelID string, rep eval.Report) error {
	rec, err := s.providers.Get(ctx, providerID)
	if err != nil || rec == nil {
		return fmt.Errorf("provider not found")
	}
	comp := eval.ToCompatibility(rep)
	found := false
	for i := range rec.AllowedModels {
		if rec.AllowedModels[i].ModelID == modelID {
			rec.AllowedModels[i].Compatibility = &comp
			found = true
		}
	}
	if !found {
		rec.AllowedModels = append(rec.AllowedModels, model.ModelEntry{
			ModelID: modelID, Label: modelID,
			CapabilityTier: model.ModelCapabilityTier(rep.SuggestedTier),
			PromptProfile:  rep.SuggestedProfile,
			ContextWindow:  rep.ContextWindow,
			Compatibility:  &comp,
		})
	}
	if err := s.providers.UpdateAllowedModels(ctx, providerID, rec.AllowedModels); err != nil {
		return err
	}
	s.registry.Invalidate(providerID)
	return nil
}
