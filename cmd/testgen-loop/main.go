// Command testgen-loop drives a goal-driven loop agent that generates Go unit
// tests for a target file until coverage hits a configured threshold.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/pflag"
	"go.uber.org/zap"

	"github.com/tushariitr-19/testgen-loop-adk/internal/config"
	"github.com/tushariitr-19/testgen-loop-adk/internal/coverage"
	"github.com/tushariitr-19/testgen-loop-adk/internal/logging"
	"github.com/tushariitr-19/testgen-loop-adk/internal/orchestrator"
)

// version is the binary's reported version. It is a var (not a const) so
// release builds can override it at link time:
//
//	go build -ldflags "-X main.version=v0.1.0" ./cmd/testgen-loop
var version = "0.0.0-dev"

// Outermost wall-clock safety nets for the two CLI entry points. The inner
// subsystems (coverage runner, agent loop, per-iteration contexts) carry
// their own finer-grained timeouts; these exist so a stuck upstream loop
// can never run forever from the user's perspective.
const (
	dryRunMaxRuntime = 2 * time.Minute
	loopMaxRuntime   = 10 * time.Minute
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cfg, err := config.Load(args)
	if err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "testgen-loop: %v\n", err)
		return 2
	}

	logger, err := logging.New(cfg.Debug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "testgen-loop: %v\n", err)
		return 2
	}
	defer func() { _ = logger.Sync() }()

	logger.Info("starting",
		zap.String("phase", "startup"),
		zap.String("component", "main"),
		zap.String("version", version),
		zap.String("target_file", cfg.TargetFile),
		zap.String("gemini_model", cfg.GeminiModel),
		zap.Bool("api_key_set", cfg.GeminiAPIKey != ""),
		zap.Float64("target_coverage", cfg.TargetCoverage),
		zap.Int("max_iterations", cfg.MaxIterations),
		zap.Int("patience", cfg.Patience),
		zap.String("work_dir", cfg.WorkDir),
		zap.Bool("debug", cfg.Debug),
		zap.Bool("dry_run", cfg.DryRun),
	)

	if cfg.DryRun {
		return runDryRun(cfg, logger)
	}
	return runLoop(cfg, logger)
}

// runLoop drives the full Evaluator -> Mutator -> Selector loop via the
// orchestrator and prints a final summary to stdout.
func runLoop(cfg *config.Config, logger *zap.Logger) int {
	// Full validation: the loop needs the API key (Phase 4 onward) plus
	// every loop knob inside its expected range.
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "testgen-loop: %v\n", err)
		return 2
	}

	orch, err := orchestrator.New(logger, cfg)
	if err != nil {
		logger.Error("orchestrator init failed",
			zap.String("phase", "startup"),
			zap.String("component", "main"),
			zap.Error(err),
		)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), loopMaxRuntime)
	defer cancel()

	result, err := orch.Run(ctx)
	if err != nil {
		logger.Error("loop run returned error (partial result may follow)",
			zap.String("phase", "loop"),
			zap.String("component", "main"),
			zap.Error(err),
		)
		// Still print whatever we got so the user sees how far we made it.
	}
	if result == nil {
		return 1
	}

	printRunSummary(cfg, result)
	if result.StopReason == "error" {
		return 1
	}
	return 0
}

// printRunSummary writes the final, human-readable summary of the loop
// run to stdout. Logs went to stderr; this is what a viewer reads to see
// the outcome.
func printRunSummary(cfg *config.Config, r *orchestrator.RunResult) {
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "=== Run Summary ===")
	fmt.Fprintf(os.Stdout, "target:           %s\n", cfg.TargetFile)
	fmt.Fprintf(os.Stdout, "iterations:       %d / %d\n", r.Iterations, cfg.MaxIterations)
	fmt.Fprintf(os.Stdout, "coverage:         %.1f%% -> %.1f%% (target %.0f%%)\n",
		r.StartCoverage, r.FinalCoverage, cfg.TargetCoverage*100)
	fmt.Fprintf(os.Stdout, "stop reason:      %s\n", r.StopReason)
	fmt.Fprintf(os.Stdout, "duration:         %s\n", r.Duration.Round(time.Millisecond))
	fmt.Fprintf(os.Stdout, "accepted tests:   %d\n", len(r.AcceptedTests))
	for _, t := range r.AcceptedTests {
		fmt.Fprintf(os.Stdout, "  + %s\n", t)
	}
	fmt.Fprintf(os.Stdout, "rejected entries: %d\n", len(r.RejectedTests))
	for _, t := range r.RejectedTests {
		fmt.Fprintf(os.Stdout, "  - %s\n", t)
	}
}

// runDryRun executes the coverage subsystem once against cfg.TargetFile's
// package and prints a human-readable report to stdout. It does not invoke
// the LLM or mutate any files.
func runDryRun(cfg *config.Config, logger *zap.Logger) int {
	if err := cfg.ValidateTarget(); err != nil {
		fmt.Fprintf(os.Stderr, "testgen-loop: %v\n", err)
		return 2
	}
	pkgDir := filepath.Dir(cfg.TargetFile)

	logger.Info("dry run starting",
		zap.String("phase", "dry-run"),
		zap.String("component", "main"),
		zap.String("target_file", cfg.TargetFile),
		zap.String("package_dir", pkgDir),
	)

	ctx, cancel := context.WithTimeout(context.Background(), dryRunMaxRuntime)
	defer cancel()

	runner := coverage.NewRunner(logger)
	result, err := runner.Run(ctx, coverage.RunOptions{PackageDir: pkgDir})
	if err != nil {
		logger.Error("coverage run failed",
			zap.String("phase", "dry-run"),
			zap.String("component", "main"),
			zap.Error(err),
		)
		return 1
	}
	if result.ExitCode != 0 {
		logger.Warn("go test exited non-zero; the report below reflects what was captured anyway",
			zap.String("phase", "dry-run"),
			zap.String("component", "main"),
			zap.Int("exit_code", result.ExitCode),
		)
	}

	// Report goes to stdout so it can be captured separately from logs.
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "=== Coverage Report ===")
	fmt.Fprintf(os.Stdout, "target:  %s\n", cfg.TargetFile)
	fmt.Fprintf(os.Stdout, "profile: %s\n", result.ProfilePath)
	fmt.Fprint(os.Stdout, result.Report.String())
	return 0
}
