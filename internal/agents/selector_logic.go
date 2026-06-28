package agents

import (
	"fmt"
	"strings"

	"go.uber.org/zap"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"

	"github.com/tushariitr-19/testgen-loop-adk/internal/coverage"
)

// runSelector implements the Selector's per-iteration decision. It is
// kept here so it can be reasoned about (and eventually tested) without
// going through the ADK runtime.
//
// agent.InvocationContext embeds context.Context, so we pass it straight
// to the coverage runner for cancellation/timeout propagation.
func runSelector(
	ctx agent.InvocationContext,
	deps Deps,
	yield func(*session.Event, error) bool,
) {
	logger := deps.Logger.Named("agents.selector")
	iteration := readInt(ctx, StateIteration)
	prevCov := readFloat(ctx, StateCurrentCoverage)
	// Candidate may arrive wrapped in markdown fences when the Mutator is
	// an LlmAgent (Phase 4+). Strip them defensively before validation.
	candidate := cleanCandidateCode(readString(ctx, StateCandidateTest))
	noGain := readInt(ctx, StateConsecutiveNoGain)
	accepted := readStringSlice(ctx, StateAcceptedTests)
	rejected := readStringSlice(ctx, StateRejectedTests)

	delta := map[string]any{}

	// Path 1: no candidate proposed (Mutator's stub list exhausted, or
	// LLM returned an empty response in Phase 4). Treat as no-gain, then
	// check stop conditions.
	if strings.TrimSpace(candidate) == "" {
		logger.Info("selector: no candidate",
			zap.String("phase", "selector"),
			zap.String("component", "agent"),
			zap.Int("iteration", iteration),
		)
		noGain++
		delta[StateConsecutiveNoGain] = noGain
		applyStopCheck(delta, prevCov, noGain, deps.Config)
		yieldSelectorEvent(ctx, delta, yield)
		return
	}

	// Path 2: snapshot before mutating. A snapshot failure is structural
	// (disk full, permissions); escalate immediately.
	if _, err := deps.Workspace.Snapshot(); err != nil {
		logger.Error("selector: snapshot failed",
			zap.String("phase", "selector"),
			zap.Int("iteration", iteration),
			zap.Error(err),
		)
		delta[StateStopReason] = StopReasonError
		yieldErrorEvent(ctx, delta, err, yield)
		return
	}

	// Path 3: append validation. AppendTest can reject for syntax errors
	// or name collisions; both are non-fatal — roll back and continue.
	appendResult, appendErr := deps.Workspace.AppendTest(candidate)
	if appendErr != nil {
		_ = deps.Workspace.Restore()
		logger.Warn("selector: append rejected",
			zap.String("phase", "selector"),
			zap.Int("iteration", iteration),
			zap.Error(appendErr),
		)
		rejected = append(rejected,
			fmt.Sprintf("candidate:validation:%s", truncErr(appendErr)))
		noGain++
		delta[StateRejectedTests] = rejected
		delta[StateConsecutiveNoGain] = noGain
		applyStopCheck(delta, prevCov, noGain, deps.Config)
		yieldSelectorEvent(ctx, delta, yield)
		return
	}

	// Path 4: measure post-append coverage. A structural coverage failure
	// (toolchain missing, profile not produced) is fatal.
	result, runErr := deps.Coverage.Run(ctx, coverage.RunOptions{
		PackageDir: deps.Workspace.PackageDir(),
	})
	if runErr != nil {
		_ = deps.Workspace.Restore()
		logger.Error("selector: coverage run failed",
			zap.String("phase", "selector"),
			zap.Int("iteration", iteration),
			zap.Error(runErr),
		)
		delta[StateStopReason] = StopReasonError
		yieldErrorEvent(ctx, delta, runErr, yield)
		return
	}

	// Path 5: go test ran but exited non-zero — the appended test broke
	// compilation or itself failed. Roll back as a rejection.
	if result.ExitCode != 0 {
		_ = deps.Workspace.Restore()
		logger.Warn("selector: appended test failed go test",
			zap.String("phase", "selector"),
			zap.Int("iteration", iteration),
			zap.Int("exit_code", result.ExitCode),
			zap.String("test_name", appendResult.TestName),
		)
		rejected = append(rejected,
			fmt.Sprintf("%s:compile_fail", appendResult.TestName))
		noGain++
		delta[StateRejectedTests] = rejected
		delta[StateConsecutiveNoGain] = noGain
		applyStopCheck(delta, prevCov, noGain, deps.Config)
		yieldSelectorEvent(ctx, delta, yield)
		return
	}

	newCov := result.Report.Percent

	// Path 6: did it improve coverage? Strict ">" so floating-point noise
	// can't cause a spurious accept.
	if newCov > prevCov {
		_ = deps.Workspace.Discard()
		accepted = append(accepted, appendResult.TestName)
		logger.Info("selector: accepted",
			zap.String("phase", "selector"),
			zap.Int("iteration", iteration),
			zap.String("test_name", appendResult.TestName),
			zap.Float64("prev_coverage", prevCov),
			zap.Float64("new_coverage", newCov),
			zap.Float64("delta", newCov-prevCov),
		)
		delta[StateCurrentCoverage] = newCov
		delta[StateAcceptedTests] = accepted
		delta[StateConsecutiveNoGain] = 0
		applyStopCheck(delta, newCov, 0, deps.Config)
		yieldSelectorEvent(ctx, delta, yield)
		return
	}

	// Path 7: ran cleanly but no coverage gain. Roll back.
	_ = deps.Workspace.Restore()
	logger.Info("selector: rejected (no gain)",
		zap.String("phase", "selector"),
		zap.Int("iteration", iteration),
		zap.String("test_name", appendResult.TestName),
		zap.Float64("prev_coverage", prevCov),
		zap.Float64("new_coverage", newCov),
	)
	rejected = append(rejected,
		fmt.Sprintf("%s:no_gain", appendResult.TestName))
	noGain++
	delta[StateRejectedTests] = rejected
	delta[StateConsecutiveNoGain] = noGain
	applyStopCheck(delta, prevCov, noGain, deps.Config)
	yieldSelectorEvent(ctx, delta, yield)
}

// applyStopCheck writes StateStopReason into delta if the loop should
// terminate. The orchestrator's escalate logic relies on the presence of
// this key.
func applyStopCheck(
	delta map[string]any,
	currentCov float64,
	noGain int,
	cfg DepsConfig,
) {
	if currentCov >= cfg.TargetCoverage*100 {
		delta[StateStopReason] = StopReasonTarget
		return
	}
	if cfg.Patience > 0 && noGain >= cfg.Patience {
		delta[StateStopReason] = StopReasonPlateau
	}
}

func yieldSelectorEvent(
	ctx agent.InvocationContext,
	delta map[string]any,
	yield func(*session.Event, error) bool,
) {
	ev := session.NewEvent(ctx.InvocationID())
	ev.Author = selectorName
	for k, v := range delta {
		ev.Actions.StateDelta[k] = v
	}
	if _, hasStop := delta[StateStopReason]; hasStop {
		ev.Actions.Escalate = true
	}
	yield(ev, nil)
}

func yieldErrorEvent(
	ctx agent.InvocationContext,
	delta map[string]any,
	err error,
	yield func(*session.Event, error) bool,
) {
	ev := session.NewEvent(ctx.InvocationID())
	ev.Author = selectorName
	for k, v := range delta {
		ev.Actions.StateDelta[k] = v
	}
	ev.Actions.Escalate = true
	yield(ev, err)
}

func truncErr(err error) string {
	s := err.Error()
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}
