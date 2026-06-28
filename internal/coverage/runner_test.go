package coverage

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// classifyExampleDir is the package directory of the demo target, resolved
// relative to this test file's working directory (internal/coverage).
const classifyExampleDir = "../../examples/classify"

// pristineClassifyTest is the canonical contents of the example test file
// before any LLM-generated tests are appended to it. The integration test
// writes this back at the start of the test (with a restore on cleanup)
// so the assertions are robust to a prior `./bin/testgen-loop` run having
// left the example mutated.
const pristineClassifyTest = `package classify

import "testing"

// TestPlaceholder exists so this package has at least one test function from
// the start. testgen-loop will append real tests alongside it during a run.
func TestPlaceholder(t *testing.T) {}
`

// restoreClassifyTestFile snapshots the example test file, overwrites it
// with the pristine canonical contents, and schedules a t.Cleanup to put
// the snapshot back when the test finishes. This isolates the integration
// test from earlier `./bin/testgen-loop` runs that may have left the
// example with extra test functions.
func restoreClassifyTestFile(t *testing.T) {
	t.Helper()
	path := filepath.Join(classifyExampleDir, "classify_test.go")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("snapshotting example test file: %v", err)
	}
	if err := os.WriteFile(path, []byte(pristineClassifyTest), 0o644); err != nil {
		t.Fatalf("writing pristine example test file: %v", err)
	}
	t.Cleanup(func() {
		if err := os.WriteFile(path, original, 0o644); err != nil {
			t.Errorf("restoring example test file: %v", err)
		}
	})
}

func TestRunner_AgainstClassifyExample(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	restoreClassifyTestFile(t)

	runner := NewRunner(nil)
	profile := filepath.Join(t.TempDir(), "out.cov")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := runner.Run(ctx, RunOptions{
		PackageDir:  classifyExampleDir,
		ProfilePath: profile,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v\nstderr=%s", err, "")
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0\nstderr=%s", result.ExitCode, result.Stderr)
	}
	if result.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", result.Duration)
	}
	if result.ProfilePath != profile {
		t.Errorf("ProfilePath = %q, want %q", result.ProfilePath, profile)
	}
	if result.Report == nil {
		t.Fatal("Report is nil")
	}
	// TestPlaceholder doesn't call Classify, so all 6 branches are uncovered.
	if got, want := result.Report.TotalStmts, 6; got != want {
		t.Errorf("TotalStmts = %d, want %d", got, want)
	}
	if got, want := result.Report.CoveredStmts, 0; got != want {
		t.Errorf("CoveredStmts = %d, want %d", got, want)
	}
	if result.Report.Percent != 0 {
		t.Errorf("Percent = %v, want 0", result.Report.Percent)
	}
	if got := len(result.Report.Files); got != 1 {
		t.Fatalf("Files = %d entries, want 1", got)
	}
	if got := len(result.Report.Files[0].Uncovered); got != 6 {
		t.Errorf("Uncovered = %d blocks, want 6", got)
	}
}

func TestRunner_AutoTempProfile(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	runner := NewRunner(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	result, err := runner.Run(ctx, RunOptions{PackageDir: classifyExampleDir})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.ProfilePath == "" {
		t.Error("ProfilePath is empty; auto-temp should have populated it")
	}
	if !filepath.IsAbs(result.ProfilePath) {
		t.Errorf("ProfilePath %q is not absolute", result.ProfilePath)
	}
}

func TestRunner_EmptyPackageDir(t *testing.T) {
	_, err := NewRunner(nil).Run(context.Background(), RunOptions{})
	if err == nil {
		t.Fatal("expected error for empty PackageDir, got nil")
	}
}

func TestRunner_MissingPackageDir(t *testing.T) {
	_, err := NewRunner(nil).Run(context.Background(), RunOptions{
		PackageDir: filepath.Join(t.TempDir(), "does-not-exist"),
	})
	if err == nil {
		t.Fatal("expected error for missing PackageDir, got nil")
	}
}

func TestRunner_MissingGoToolchain(t *testing.T) {
	runner := NewRunner(nil)
	runner.goBin = "definitely-not-a-real-binary-xyzzy"
	_, err := runner.Run(context.Background(), RunOptions{
		PackageDir: classifyExampleDir,
	})
	if err == nil {
		t.Fatal("expected error for missing toolchain, got nil")
	}
	// exec wraps the missing-binary case in an *exec.Error; we should not
	// confuse it with an exit code.
	var execErr *exec.Error
	if !errors.As(err, &execErr) && !contains(err.Error(), "invoking") {
		t.Errorf("error %q does not look like an exec failure", err.Error())
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
