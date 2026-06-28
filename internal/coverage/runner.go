package coverage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"go.uber.org/zap"
)

const (
	defaultGoBin   = "go"
	defaultTimeout = 60 * time.Second
)

// Runner shells out to `go test -coverprofile=...` against a package directory
// and returns the parsed coverage report. A Runner has no per-run state; the
// same instance is safe to invoke from concurrent goroutines.
type Runner struct {
	logger *zap.Logger
	goBin  string
}

// NewRunner returns a Runner that uses the supplied logger and locates the
// Go toolchain on $PATH as "go". A nil logger is replaced with a no-op
// logger so call sites don't need a guard.
func NewRunner(logger *zap.Logger) *Runner {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Runner{
		logger: logger.Named("coverage.runner"),
		goBin:  defaultGoBin,
	}
}

// RunOptions controls a single coverage run.
type RunOptions struct {
	// PackageDir is the directory containing the Go package to test.
	// Required.
	PackageDir string

	// ProfilePath is where the coverage profile should be written. If empty,
	// a temporary file is created and its path is returned on RunResult.
	ProfilePath string

	// Timeout caps wall-clock time for the go test invocation. Zero means
	// use the default (60s). Cancellation via the run context is honored
	// independently.
	Timeout time.Duration

	// ExtraArgs is appended verbatim to the go test command. Escape hatch;
	// the loop itself does not use it.
	ExtraArgs []string
}

// RunResult captures the outcome of a single Run call. Report is non-nil
// whenever the returned error is nil — even if the underlying go test exited
// non-zero (e.g. some tests failed). Callers can inspect ExitCode and Stderr
// to decide how to react.
type RunResult struct {
	Report      *CoverageReport
	Stdout      string
	Stderr      string
	ExitCode    int
	Duration    time.Duration
	ProfilePath string
}

// Run executes `go test -count=1 -run=. -coverprofile=<path> ./` inside
// opts.PackageDir and parses the produced profile.
//
// Returned errors describe structural failures: missing package directory,
// missing Go toolchain, run timed out, no coverage profile produced, or
// profile produced but malformed. Test failures inside the package are not
// errors — they surface as non-zero ExitCode on RunResult.
func (r *Runner) Run(ctx context.Context, opts RunOptions) (*RunResult, error) {
	if opts.PackageDir == "" {
		return nil, errors.New("coverage: RunOptions.PackageDir is empty")
	}
	info, err := os.Stat(opts.PackageDir)
	if err != nil {
		return nil, fmt.Errorf("coverage: stat package dir %q: %w", opts.PackageDir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("coverage: package dir %q is not a directory", opts.PackageDir)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}

	profilePath, err := resolveProfilePath(opts.ProfilePath)
	if err != nil {
		return nil, err
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := append(
		[]string{"test", "-count=1", "-run=.", "-coverprofile=" + profilePath, "./"},
		opts.ExtraArgs...,
	)
	cmd := exec.CommandContext(runCtx, r.goBin, args...)
	cmd.Dir = opts.PackageDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	r.logger.Debug("running go test",
		zap.String("phase", "coverage"),
		zap.String("component", "runner"),
		zap.String("dir", opts.PackageDir),
		zap.String("profile", profilePath),
		zap.Strings("args", args),
		zap.Duration("timeout", timeout),
	)

	started := time.Now()
	runErr := cmd.Run()
	duration := time.Since(started)
	exitCode := cmd.ProcessState.ExitCode()

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("coverage: go test timed out after %s", timeout)
	}
	// `go test` returning a non-zero exit code (ExitError) is acceptable —
	// that's a test failure, not a structural failure. Any other error
	// (toolchain missing, fork failed, etc.) is structural.
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		return nil, fmt.Errorf("coverage: invoking %s test: %w", r.goBin, runErr)
	}

	if _, err := os.Stat(profilePath); err != nil {
		return nil, fmt.Errorf(
			"coverage: profile not produced (exit=%d, stderr=%s): %w",
			exitCode, truncate(stderr.String(), 1024), err,
		)
	}
	report, parseErr := Parse(profilePath)
	if parseErr != nil {
		return nil, fmt.Errorf("coverage: parsing produced profile: %w", parseErr)
	}

	r.logger.Info("coverage run complete",
		zap.String("phase", "coverage"),
		zap.String("component", "runner"),
		zap.Int("exit_code", exitCode),
		zap.Duration("duration", duration),
		zap.Float64("percent", report.Percent),
		zap.Int("total_stmts", report.TotalStmts),
		zap.Int("covered_stmts", report.CoveredStmts),
		zap.Int("uncovered_blocks", countUncovered(report)),
	)

	return &RunResult{
		Report:      report,
		Stdout:      stdout.String(),
		Stderr:      stderr.String(),
		ExitCode:    exitCode,
		Duration:    duration,
		ProfilePath: profilePath,
	}, nil
}

func resolveProfilePath(requested string) (string, error) {
	if requested == "" {
		f, err := os.CreateTemp("", "testgen-coverage-*.cov")
		if err != nil {
			return "", fmt.Errorf("coverage: creating temp profile: %w", err)
		}
		path := f.Name()
		_ = f.Close()
		return path, nil
	}
	abs, err := filepath.Abs(requested)
	if err != nil {
		return "", fmt.Errorf("coverage: resolving profile path %q: %w", requested, err)
	}
	return abs, nil
}

func countUncovered(report *CoverageReport) int {
	n := 0
	for _, f := range report.Files {
		n += len(f.Uncovered)
	}
	return n
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...[truncated]"
}
