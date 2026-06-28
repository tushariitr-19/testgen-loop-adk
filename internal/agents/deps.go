package agents

import (
	"go.uber.org/zap"

	"github.com/tushariitr-19/testgen-loop-adk/internal/coverage"
	"github.com/tushariitr-19/testgen-loop-adk/internal/workspace"
)

// Deps bundles the dependencies every custom agent receives at construction
// time. Each factory captures the Deps in its Run closure so the closure
// stays the only place that imports our internal packages — keeping the
// agent files themselves thin.
type Deps struct {
	Logger    *zap.Logger
	Coverage  *coverage.Runner
	Workspace *workspace.Workspace
	Config    DepsConfig
}

// DepsConfig is the subset of the global *config.Config that the agent
// layer actually needs. The agents never see config knobs they have no
// business reading (API keys, log levels, etc.).
type DepsConfig struct {
	// TargetCoverage is the stop threshold, in the closed interval [0, 1].
	// The Selector compares against StateCurrentCoverage (0-100) after
	// converting.
	TargetCoverage float64
	// Patience is the number of consecutive non-gain iterations that
	// triggers a plateau stop.
	Patience int
}
