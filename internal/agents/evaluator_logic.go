package agents

import (
	"fmt"
	"sort"
	"strings"

	"go.uber.org/zap"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"

	"github.com/tushariitr-19/testgen-loop-adk/internal/coverage"
)

// runEvaluator measures coverage on the workspace package and yields a
// single event carrying the baseline state delta. On structural failure
// (coverage runner errored, no profile produced) it yields an event with
// Escalate=true and StateStopReason="error" so the loop terminates
// rather than spinning on a broken pipeline.
//
// agent.InvocationContext embeds context.Context, so we pass it straight to
// the coverage runner for cancellation/timeout propagation.
func runEvaluator(
	ctx agent.InvocationContext,
	deps Deps,
	yield func(*session.Event, error) bool,
) {
	logger := deps.Logger.Named("agents.evaluator")
	iteration := readInt(ctx, StateIteration) + 1

	logger.Info("evaluator start",
		zap.String("phase", "evaluator"),
		zap.String("component", "agent"),
		zap.Int("iteration", iteration),
	)

	result, err := deps.Coverage.Run(ctx, coverage.RunOptions{
		PackageDir: deps.Workspace.PackageDir(),
	})
	if err != nil {
		logger.Error("evaluator coverage run failed",
			zap.String("phase", "evaluator"),
			zap.Int("iteration", iteration),
			zap.Error(err),
		)
		ev := session.NewEvent(ctx.InvocationID())
		ev.Author = evaluatorName
		ev.Actions.StateDelta[StateStopReason] = StopReasonError
		ev.Actions.Escalate = true
		yield(ev, err)
		return
	}

	summary := formatUncoveredSummary(result.Report)

	logger.Info("evaluator baseline",
		zap.String("phase", "evaluator"),
		zap.Int("iteration", iteration),
		zap.Float64("current_coverage", result.Report.Percent),
		zap.Int("uncovered_blocks", countUncovered(result.Report)),
	)

	ev := session.NewEvent(ctx.InvocationID())
	ev.Author = evaluatorName
	ev.Actions.StateDelta[StateIteration] = iteration
	ev.Actions.StateDelta[StateCurrentCoverage] = result.Report.Percent
	ev.Actions.StateDelta[StateUncoveredSummary] = summary
	yield(ev, nil)
}

// formatUncoveredSummary renders uncovered blocks as a compact, LLM- and
// human-friendly string. The shape is intentionally simple so Phase 4's
// Mutator prompt can inject it via {uncovered_summary} without further
// formatting.
func formatUncoveredSummary(report *coverage.CoverageReport) string {
	if report == nil {
		return "<no report>"
	}
	if countUncovered(report) == 0 {
		return "all blocks are covered"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "coverage %.1f%% (%d/%d statements)\n",
		report.Percent, report.CoveredStmts, report.TotalStmts)

	files := append([]coverage.FileCoverage(nil), report.Files...)
	sort.SliceStable(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	for _, f := range files {
		if len(f.Uncovered) == 0 {
			continue
		}
		fmt.Fprintf(&b, "%s\n", f.Path)
		for _, blk := range f.Uncovered {
			fmt.Fprintf(&b, "  L%d:%d-L%d:%d (%d stmt)\n",
				blk.StartLine, blk.StartCol, blk.EndLine, blk.EndCol, blk.NumStmts)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func countUncovered(report *coverage.CoverageReport) int {
	if report == nil {
		return 0
	}
	n := 0
	for _, f := range report.Files {
		n += len(f.Uncovered)
	}
	return n
}
