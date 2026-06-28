// Package logging builds the project's *zap.Logger. The logger is JSON-only,
// ISO8601-timestamped, and written to stderr. Output is consistent across
// terminals and pipelines so log lines are always machine-parseable.
//
// The logger is passed explicitly through component constructors; there is
// no package-level logger and nothing here reads global state.
package logging

import (
	"fmt"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// New returns a *zap.Logger writing JSON to stderr.
//
// When debug is true the level is Debug; otherwise Info. The logger always
// records the call site of each log statement, and emits a stacktrace at
// error level and above. Output goes to stderr so stdout stays clean for
// tool output (e.g. the dry-run report and final run summary).
func New(debug bool) (*zap.Logger, error) {
	encCfg := zap.NewProductionEncoderConfig()
	encCfg.TimeKey = "timestamp"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	encoder := zapcore.NewJSONEncoder(encCfg)
	writer := zapcore.AddSync(os.Stderr)

	level := zapcore.InfoLevel
	if debug {
		level = zapcore.DebugLevel
	}

	core := zapcore.NewCore(encoder, writer, level)
	logger := zap.New(core,
		zap.AddCaller(),
		zap.AddStacktrace(zapcore.ErrorLevel),
	)
	if logger == nil {
		return nil, fmt.Errorf("logging: nil logger from zap.New")
	}
	return logger, nil
}
