// Package config loads and validates runtime configuration for testgen-loop.
//
// Precedence: CLI flag > environment variable > built-in default.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
)

// Built-in defaults.
const (
	DefaultGeminiModel    = "gemini-2.5-flash"
	DefaultTargetCoverage = 0.90
	DefaultMaxIterations  = 8
	DefaultPatience       = 2
)

// Environment variable names.
const (
	EnvAPIKey         = "TESTGEN_GEMINI_API_KEY"
	EnvGeminiModel    = "TESTGEN_GEMINI_MODEL"
	EnvDebug          = "TESTGEN_DEBUG"
	EnvTargetCoverage = "TESTGEN_TARGET_COVERAGE"
	EnvMaxIterations  = "TESTGEN_MAX_ITERATIONS"
	EnvPatience       = "TESTGEN_PATIENCE"
	EnvTargetFile     = "TESTGEN_TARGET_FILE"
	EnvWorkDir        = "TESTGEN_WORK_DIR"
)

// Config is the resolved runtime configuration for a single invocation.
type Config struct {
	GeminiAPIKey   string
	GeminiModel    string
	Debug          bool
	TargetCoverage float64
	MaxIterations  int
	Patience       int
	TargetFile     string
	WorkDir        string

	// DryRun, when true, asks the binary to run the coverage subsystem
	// against TargetFile once and print the initial report, then exit. It
	// does not call any LLM and does not modify any files on disk.
	DryRun bool
}

// Load resolves Config from defaults, environment variables, and CLI args, in
// that order. It does not validate semantic correctness — call Validate before
// using the result for a real run. The caller passes args (typically
// os.Args[1:]) so Load can be exercised from tests.
//
// If args contains -h/--help, Load returns pflag.ErrHelp after printing usage
// to stderr; callers should treat that as a clean exit, not a failure.
func Load(args []string) (*Config, error) {
	cfg := &Config{
		GeminiModel:    DefaultGeminiModel,
		TargetCoverage: DefaultTargetCoverage,
		MaxIterations:  DefaultMaxIterations,
		Patience:       DefaultPatience,
		WorkDir:        os.TempDir(),
	}

	if err := cfg.applyEnv(); err != nil {
		return nil, err
	}
	if err := cfg.applyFlags(args); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) applyEnv() error {
	if v := os.Getenv(EnvAPIKey); v != "" {
		c.GeminiAPIKey = v
	}

	if v := os.Getenv(EnvGeminiModel); v != "" {
		c.GeminiModel = v
	}
	if v := os.Getenv(EnvTargetFile); v != "" {
		c.TargetFile = v
	}
	if v := os.Getenv(EnvWorkDir); v != "" {
		c.WorkDir = v
	}
	if v := os.Getenv(EnvDebug); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("parsing %s=%q: %w", EnvDebug, v, err)
		}
		c.Debug = b
	}

	if v := os.Getenv(EnvTargetCoverage); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("parsing %s=%q: %w", EnvTargetCoverage, v, err)
		}
		c.TargetCoverage = f
	}
	if v := os.Getenv(EnvMaxIterations); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("parsing %s=%q: %w", EnvMaxIterations, v, err)
		}
		c.MaxIterations = n
	}
	if v := os.Getenv(EnvPatience); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("parsing %s=%q: %w", EnvPatience, v, err)
		}
		c.Patience = n
	}
	return nil
}

func (c *Config) applyFlags(args []string) error {
	fs := pflag.NewFlagSet("testgen-loop", pflag.ContinueOnError)

	fs.StringVarP(&c.TargetFile, "target", "t", c.TargetFile,
		"Go source file to generate tests for (required)")
	fs.StringVar(&c.GeminiModel, "model", c.GeminiModel,
		"Gemini model name")
	fs.Float64Var(&c.TargetCoverage, "target-coverage", c.TargetCoverage,
		"Stop once coverage reaches this fraction (0-1)")
	fs.IntVar(&c.MaxIterations, "max-iterations", c.MaxIterations,
		"Hard cap on loop iterations")
	fs.IntVar(&c.Patience, "patience", c.Patience,
		"Stop after this many consecutive iterations without coverage gain")
	fs.StringVar(&c.WorkDir, "work-dir", c.WorkDir,
		"Directory for intermediate artifacts (coverage profiles, snapshots)")
	fs.BoolVar(&c.Debug, "debug", c.Debug,
		"Enable debug-level structured logging (default: info)")
	fs.BoolVar(&c.DryRun, "dry-run", c.DryRun,
		"Run the coverage subsystem once and print the report, then exit (no LLM)")

	return fs.Parse(args)
}

// ValidateTarget checks only the --target requirement: that it was provided
// and points at an existing file. Use this from paths that do not need
// credentials or loop knobs, such as --dry-run.
func (c *Config) ValidateTarget() error {
	if c.TargetFile == "" {
		return errors.New("--target is required")
	}
	if _, err := os.Stat(c.TargetFile); err != nil {
		return fmt.Errorf("target file: %w", err)
	}
	return nil
}

// Validate checks that the resolved config can drive a real run. Call this
// from the orchestrator entry point, not from Load, so that --help and
// --dry-run-style introspection paths work without an API key.
func (c *Config) Validate() error {
	var errs []error

	if c.GeminiAPIKey == "" {
		errs = append(errs, fmt.Errorf("API key required: set %s", EnvAPIKey))
	}
	if err := c.ValidateTarget(); err != nil {
		errs = append(errs, err)
	}
	if c.TargetCoverage < 0 || c.TargetCoverage > 1 {
		errs = append(errs, fmt.Errorf(
			"target-coverage must be in [0, 1], got %v", c.TargetCoverage))
	}
	if c.MaxIterations < 1 {
		errs = append(errs, fmt.Errorf(
			"max-iterations must be >= 1, got %d", c.MaxIterations))
	}
	if c.Patience < 1 {
		errs = append(errs, fmt.Errorf(
			"patience must be >= 1, got %d", c.Patience))
	}

	return errors.Join(errs...)
}

// String renders the resolved config as a multi-line, human-readable summary
// suitable for startup logs. The API key is redacted to "set" / "unset" so the
// summary is safe to print.
func (c *Config) String() string {
	apiKey := "unset"
	if c.GeminiAPIKey != "" {
		apiKey = "set"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  target-file:     %s\n", c.TargetFile)
	fmt.Fprintf(&b, "  gemini-model:    %s\n", c.GeminiModel)
	fmt.Fprintf(&b, "  api-key:         %s\n", apiKey)
	fmt.Fprintf(&b, "  target-coverage: %.2f\n", c.TargetCoverage)
	fmt.Fprintf(&b, "  max-iterations:  %d\n", c.MaxIterations)
	fmt.Fprintf(&b, "  patience:        %d\n", c.Patience)
	fmt.Fprintf(&b, "  work-dir:        %s\n", c.WorkDir)
	fmt.Fprintf(&b, "  debug:           %t", c.Debug)
	return b.String()
}
