// Package orchestrator wires the three custom agents (Evaluator, Mutator,
// Selector) into an ADK LoopAgent, drives it through a runner, and reads
// the final session state into a structured RunResult.
//
// This is the only package that imports the ADK runtime types directly
// alongside our internal agent package — everything below the orchestrator
// is either pure-Go business logic or thin agent.Config wiring.
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/workflowagents/loopagent"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/tushariitr-19/testgen-loop-adk/internal/agents"
	"github.com/tushariitr-19/testgen-loop-adk/internal/config"
	"github.com/tushariitr-19/testgen-loop-adk/internal/coverage"
	"github.com/tushariitr-19/testgen-loop-adk/internal/workspace"
)

const (
	appName = "testgen-loop"
	userID  = "local"
)

// RunResult summarizes a completed loop run. Coverage values are on a
// 0-100 scale, mirroring the report shown by `go test`.
type RunResult struct {
	Iterations    int
	StartCoverage float64
	FinalCoverage float64
	StopReason    string
	AcceptedTests []string
	RejectedTests []string
	Duration      time.Duration
}

// Orchestrator owns the agent tree, the workspace, and the in-memory ADK
// services. A single Orchestrator runs one loop and is then discarded.
type Orchestrator struct {
	logger *zap.Logger
	cfg    *config.Config
	cov    *coverage.Runner
	ws     *workspace.Workspace
}

// New constructs an Orchestrator for cfg.TargetFile. It builds the
// Workspace eagerly so that startup errors (missing file, unwritable dir)
// surface before the agent tree is constructed.
func New(logger *zap.Logger, cfg *config.Config) (*Orchestrator, error) {
	ws, err := workspace.New(logger, cfg.TargetFile)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: workspace: %w", err)
	}
	return &Orchestrator{
		logger: logger.Named("orchestrator"),
		cfg:    cfg,
		cov:    coverage.NewRunner(logger),
		ws:     ws,
	}, nil
}

// Run constructs the agent tree, drives the LoopAgent through one full
// loop, and returns a RunResult built from the final session state.
//
// Errors from inside the runner stream (an agent yielding (event, err))
// are surfaced via the returned error but do not abort the result
// collection — the StateDelta of the same event is still applied, so the
// RunResult will reflect a partial run with StopReason="error".
func (o *Orchestrator) Run(ctx context.Context) (*RunResult, error) {
	deps := agents.Deps{
		Logger:    o.logger,
		Coverage:  o.cov,
		Workspace: o.ws,
		Config: agents.DepsConfig{
			TargetCoverage: o.cfg.TargetCoverage,
			Patience:       o.cfg.Patience,
		},
	}

	evaluator, err := agents.NewEvaluator(deps)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: evaluator: %w", err)
	}
	mutator, err := agents.NewMutator(ctx, deps, agents.MutatorOptions{
		APIKey:    o.cfg.GeminiAPIKey,
		ModelName: o.cfg.GeminiModel,
	})
	if err != nil {
		return nil, fmt.Errorf("orchestrator: mutator: %w", err)
	}
	selector, err := agents.NewSelector(deps)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: selector: %w", err)
	}

	loop, err := loopagent.New(loopagent.Config{
		AgentConfig: agent.Config{
			Name:        "TestgenLoop",
			Description: "Drives Evaluator -> Mutator -> Selector until target coverage, plateau, or budget.",
			SubAgents:   []agent.Agent{evaluator, mutator, selector},
		},
		MaxIterations: uint(o.cfg.MaxIterations),
	})
	if err != nil {
		return nil, fmt.Errorf("orchestrator: loop agent: %w", err)
	}

	sessSvc := session.InMemoryService()
	memSvc := memory.InMemoryService()

	r, err := runner.New(runner.Config{
		AppName:        appName,
		Agent:          loop,
		SessionService: sessSvc,
		MemoryService:  memSvc,
	})
	if err != nil {
		return nil, fmt.Errorf("orchestrator: runner: %w", err)
	}

	sessionID := fmt.Sprintf("testgen-%s", time.Now().UTC().Format("20060102-150405"))

	// Seed the session with the target source and path so the Mutator's
	// InstructionProvider can read them at every iteration without
	// re-reading the file. Read once here so it is consistent across
	// the run.
	targetSource, err := os.ReadFile(o.cfg.TargetFile)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: read target file: %w", err)
	}
	initialState := map[string]any{
		agents.StateTargetSource: string(targetSource),
		agents.StateTargetPath:   o.cfg.TargetFile,
	}

	if _, err := sessSvc.Create(ctx, &session.CreateRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
		State:     initialState,
	}); err != nil {
		return nil, fmt.Errorf("orchestrator: create session: %w", err)
	}

	o.logger.Info("loop start",
		zap.String("phase", "orchestrator"),
		zap.String("session_id", sessionID),
		zap.String("target_file", o.cfg.TargetFile),
		zap.Float64("target_coverage", o.cfg.TargetCoverage),
		zap.Int("max_iterations", o.cfg.MaxIterations),
		zap.Int("patience", o.cfg.Patience),
	)

	startedAt := time.Now()
	userMsg := genai.NewContentFromText(
		fmt.Sprintf("Generate Go unit tests for %s until coverage reaches %.0f%%.",
			o.cfg.TargetFile, o.cfg.TargetCoverage*100),
		"user",
	)

	var (
		startCov     float64
		gotStartCov  bool
		streamErrs   []error
		eventCount   int
		escalatedBy  string
	)

	for ev, runErr := range r.Run(ctx, userID, sessionID, userMsg, agent.RunConfig{}) {
		eventCount++
		if runErr != nil {
			o.logger.Error("runner stream error",
				zap.String("phase", "orchestrator"),
				zap.String("author", evAuthor(ev)),
				zap.Error(runErr),
			)
			streamErrs = append(streamErrs, runErr)
		}
		// First Evaluator event in the run carries the starting baseline.
		if !gotStartCov && ev != nil {
			if v, ok := ev.Actions.StateDelta[agents.StateCurrentCoverage]; ok {
				if f, ok := v.(float64); ok {
					startCov = f
					gotStartCov = true
				}
			}
		}
		if ev != nil && ev.Actions.Escalate {
			escalatedBy = ev.Author
			o.logger.Info("loop escalated",
				zap.String("phase", "orchestrator"),
				zap.String("author", ev.Author),
			)
		}
	}
	duration := time.Since(startedAt)

	result, err := o.collect(ctx, sessSvc, sessionID, startCov, duration)
	if err != nil {
		return nil, err
	}

	o.logger.Info("loop done",
		zap.String("phase", "orchestrator"),
		zap.Int("event_count", eventCount),
		zap.String("escalated_by", escalatedBy),
		zap.String("stop_reason", result.StopReason),
		zap.Float64("start_coverage", result.StartCoverage),
		zap.Float64("final_coverage", result.FinalCoverage),
		zap.Int("iterations", result.Iterations),
		zap.Int("accepted", len(result.AcceptedTests)),
		zap.Int("rejected", len(result.RejectedTests)),
		zap.Duration("duration", result.Duration),
	)

	return result, errors.Join(streamErrs...)
}

func (o *Orchestrator) collect(
	ctx context.Context,
	sessSvc session.Service,
	sessionID string,
	startCov float64,
	duration time.Duration,
) (*RunResult, error) {
	resp, err := sessSvc.Get(ctx, &session.GetRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("orchestrator: get final session: %w", err)
	}
	state := resp.Session.State()

	result := &RunResult{
		StartCoverage: startCov,
		Duration:      duration,
	}

	if v, _ := state.Get(agents.StateIteration); v != nil {
		switch n := v.(type) {
		case int:
			result.Iterations = n
		case int64:
			result.Iterations = int(n)
		case float64:
			result.Iterations = int(n)
		}
	}
	if v, _ := state.Get(agents.StateCurrentCoverage); v != nil {
		if f, ok := v.(float64); ok {
			result.FinalCoverage = f
		}
	}
	if v, _ := state.Get(agents.StateStopReason); v != nil {
		if s, ok := v.(string); ok {
			result.StopReason = s
		}
	}
	if result.StopReason == "" {
		// No agent escalated — the LoopAgent stopped on its own
		// MaxIterations counter.
		result.StopReason = agents.StopReasonBudget
	}
	result.AcceptedTests = stateStringSlice(state, agents.StateAcceptedTests)
	result.RejectedTests = stateStringSlice(state, agents.StateRejectedTests)
	return result, nil
}

func evAuthor(ev *session.Event) string {
	if ev == nil {
		return "<nil>"
	}
	return ev.Author
}

func stateStringSlice(s session.State, key string) []string {
	v, err := s.Get(key)
	if err != nil || v == nil {
		return nil
	}
	switch ss := v.(type) {
	case []string:
		return ss
	case []any:
		out := make([]string, 0, len(ss))
		for _, a := range ss {
			if str, ok := a.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}
