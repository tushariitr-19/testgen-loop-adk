package workspace

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Workspace owns a single Go target file and its companion _test.go file.
// It provides Snapshot, AppendTest, Restore, and Discard so the Selector
// agent can treat each iteration as a transaction: snapshot first, append
// the candidate test, then either Restore (rejection) or Discard
// (acceptance).
//
// A Workspace is not safe for concurrent use; one Workspace is intended to
// live for the duration of a single loop run.
type Workspace struct {
	logger     *zap.Logger
	targetFile string
	testFile   string
	snapshots  []Snapshot
}

// New constructs a Workspace for targetFile.
//
// The test file is derived by replacing the .go suffix with _test.go in the
// same directory. If the test file does not exist, it is created with a
// minimal "package X" declaration matching the target's package. If it
// exists but does not parse as Go, New returns an error.
func New(logger *zap.Logger, targetFile string) (*Workspace, error) {
	if logger == nil {
		logger = zap.NewNop()
	}
	logger = logger.Named("workspace")
	if targetFile == "" {
		return nil, errors.New("workspace: targetFile is empty")
	}
	if filepath.Ext(targetFile) != ".go" {
		return nil, fmt.Errorf("workspace: targetFile %q is not a .go file", targetFile)
	}
	if strings.HasSuffix(targetFile, "_test.go") {
		return nil, fmt.Errorf("workspace: targetFile %q is itself a test file", targetFile)
	}
	abs, err := filepath.Abs(targetFile)
	if err != nil {
		return nil, fmt.Errorf("workspace: resolving targetFile: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("workspace: stat target: %w", err)
	}
	ws := &Workspace{
		logger:     logger,
		targetFile: abs,
		testFile:   strings.TrimSuffix(abs, ".go") + "_test.go",
	}
	if err := ws.ensureTestFile(); err != nil {
		return nil, err
	}
	if err := ws.checkWritable(); err != nil {
		return nil, err
	}
	return ws, nil
}

// TargetFilePath returns the absolute path of the source file under test.
func (w *Workspace) TargetFilePath() string { return w.targetFile }

// TestFilePath returns the absolute path of the test file the Workspace
// appends to.
func (w *Workspace) TestFilePath() string { return w.testFile }

// PackageDir returns the absolute path of the directory containing both
// files. The coverage runner uses this as its PackageDir.
func (w *Workspace) PackageDir() string { return filepath.Dir(w.targetFile) }

// SnapshotDepth returns the number of snapshots currently on the rollback
// stack. Useful for instrumentation and tests.
func (w *Workspace) SnapshotDepth() int { return len(w.snapshots) }

// Snapshot captures the current state of the test file and pushes it onto
// the rollback stack. It must be paired with either Restore (rollback) or
// Discard (commit) so the stack does not grow unbounded.
func (w *Workspace) Snapshot() (*Snapshot, error) {
	contents, err := os.ReadFile(w.testFile)
	if err != nil {
		return nil, fmt.Errorf("workspace: snapshot read: %w", err)
	}
	snap := Snapshot{
		Path:     w.testFile,
		Contents: contents,
		Size:     int64(len(contents)),
		TakenAt:  time.Now(),
	}
	w.snapshots = append(w.snapshots, snap)
	w.logger.Info("snapshot taken",
		zap.String("phase", "workspace"),
		zap.String("component", "snapshot"),
		zap.String("path", w.testFile),
		zap.Int64("size", snap.Size),
		zap.Int("depth", len(w.snapshots)),
	)
	return &snap, nil
}

// AppendTest validates the supplied source as a single Go test function and,
// if its name does not collide with an existing test in the file, appends
// it to the test file. It does not manage imports — the candidate must rely
// only on packages already imported by the test file.
//
// Validation errors are returned without modifying the file.
func (w *Workspace) AppendTest(code string) (*AppendResult, error) {
	testName, err := ValidateTestFunc(code)
	if err != nil {
		w.logger.Warn("append rejected: invalid test function",
			zap.String("phase", "workspace"),
			zap.String("component", "append"),
			zap.Error(err),
		)
		return nil, err
	}
	existing, err := os.ReadFile(w.testFile)
	if err != nil {
		return nil, fmt.Errorf("workspace: reading test file: %w", err)
	}
	names, err := ExtractTestNames(string(existing))
	if err != nil {
		return nil, fmt.Errorf("workspace: parsing test file: %w", err)
	}
	for _, n := range names {
		if n == testName {
			w.logger.Warn("append rejected: name collision",
				zap.String("phase", "workspace"),
				zap.String("component", "append"),
				zap.String("test_name", testName),
			)
			return nil, fmt.Errorf(
				"workspace: test function %q already exists", testName)
		}
	}

	var buf strings.Builder
	buf.Write(existing)
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		buf.WriteByte('\n')
	}
	buf.WriteByte('\n')
	buf.WriteString(strings.TrimSpace(code))
	buf.WriteByte('\n')

	contents := []byte(buf.String())
	if err := os.WriteFile(w.testFile, contents, 0o644); err != nil {
		return nil, fmt.Errorf("workspace: writing test file: %w", err)
	}
	w.logger.Info("test appended",
		zap.String("phase", "workspace"),
		zap.String("component", "append"),
		zap.String("test_name", testName),
		zap.Int("bytes_written", len(contents)-len(existing)),
		zap.Int64("new_size", int64(len(contents))),
	)
	return &AppendResult{
		TestName:     testName,
		BytesWritten: len(contents) - len(existing),
		NewSize:      int64(len(contents)),
	}, nil
}

// Restore pops the most recent snapshot and writes its contents back to
// the test file. It is the rollback half of a Snapshot/Append transaction.
func (w *Workspace) Restore() error {
	if len(w.snapshots) == 0 {
		return errors.New("workspace: no snapshot to restore")
	}
	last := len(w.snapshots) - 1
	snap := w.snapshots[last]
	w.snapshots = w.snapshots[:last]
	if err := os.WriteFile(snap.Path, snap.Contents, 0o644); err != nil {
		return fmt.Errorf("workspace: restoring snapshot: %w", err)
	}
	w.logger.Info("snapshot restored",
		zap.String("phase", "workspace"),
		zap.String("component", "restore"),
		zap.String("path", snap.Path),
		zap.Int64("size", snap.Size),
		zap.Int("depth", len(w.snapshots)),
	)
	return nil
}

// Discard pops the most recent snapshot without restoring. It is the
// commit half of a Snapshot/Append transaction.
func (w *Workspace) Discard() error {
	if len(w.snapshots) == 0 {
		return errors.New("workspace: no snapshot to discard")
	}
	last := len(w.snapshots) - 1
	w.snapshots = w.snapshots[:last]
	w.logger.Info("snapshot discarded",
		zap.String("phase", "workspace"),
		zap.String("component", "discard"),
		zap.Int("depth", len(w.snapshots)),
	)
	return nil
}

// ensureTestFile creates the companion test file if it is missing,
// populating it with a minimal "package X" declaration matching the
// target's package. If the file exists, it must parse as Go.
func (w *Workspace) ensureTestFile() error {
	if contents, err := os.ReadFile(w.testFile); err == nil {
		if _, perr := parser.ParseFile(
			token.NewFileSet(), w.testFile, contents, parser.SkipObjectResolution,
		); perr != nil {
			return fmt.Errorf(
				"workspace: existing test file %q does not parse: %w",
				w.testFile, perr)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("workspace: stat test file: %w", err)
	}
	pkgName, err := readPackageName(w.targetFile)
	if err != nil {
		return err
	}
	contents := fmt.Sprintf("package %s\n", pkgName)
	if err := os.WriteFile(w.testFile, []byte(contents), 0o644); err != nil {
		return fmt.Errorf("workspace: creating test file: %w", err)
	}
	w.logger.Info("created test file",
		zap.String("phase", "workspace"),
		zap.String("component", "init"),
		zap.String("path", w.testFile),
		zap.String("package", pkgName),
	)
	return nil
}

func readPackageName(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("workspace: reading target: %w", err)
	}
	f, err := parser.ParseFile(
		token.NewFileSet(), path, contents, parser.PackageClauseOnly)
	if err != nil {
		return "", fmt.Errorf("workspace: parsing target package clause: %w", err)
	}
	return f.Name.Name, nil
}

// checkWritable verifies the directory holding the test file accepts writes.
// It creates a temp file and removes it immediately; on failure the
// directory is reported as not writable.
func (w *Workspace) checkWritable() error {
	dir := filepath.Dir(w.testFile)
	f, err := os.CreateTemp(dir, ".testgen-loop-write-check-*")
	if err != nil {
		return fmt.Errorf("workspace: directory %q not writable: %w", dir, err)
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}
