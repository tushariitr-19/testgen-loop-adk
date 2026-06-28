package workspace

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleTarget = `package thing

func Thing(n int) string {
	if n < 0 {
		return "neg"
	}
	return "nonneg"
}
`

const validCandidate = `func TestThingNegative(t *testing.T) {
	if got := Thing(-1); got != "neg" {
		t.Errorf("got %q, want neg", got)
	}
}`

// setupTarget writes a sample target.go into a fresh temp dir and returns
// the target path. It does NOT create the test file — callers exercise New
// to drive that path.
func setupTarget(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "thing.go")
	if err := os.WriteFile(target, []byte(sampleTarget), 0o644); err != nil {
		t.Fatal(err)
	}
	return target
}

func TestNew_HappyPath_CreatesTestFile(t *testing.T) {
	target := setupTarget(t)
	ws, err := New(nil, target)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if ws.TargetFilePath() != target {
		t.Errorf("TargetFilePath = %q, want %q", ws.TargetFilePath(), target)
	}
	wantTest := strings.TrimSuffix(target, ".go") + "_test.go"
	if ws.TestFilePath() != wantTest {
		t.Errorf("TestFilePath = %q, want %q", ws.TestFilePath(), wantTest)
	}
	if ws.PackageDir() != filepath.Dir(target) {
		t.Errorf("PackageDir = %q, want %q", ws.PackageDir(), filepath.Dir(target))
	}
	contents, err := os.ReadFile(ws.TestFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "package thing\n" {
		t.Errorf("created test file contents = %q, want %q",
			string(contents), "package thing\n")
	}
}

func TestNew_PreservesExistingTestFile(t *testing.T) {
	target := setupTarget(t)
	testPath := strings.TrimSuffix(target, ".go") + "_test.go"
	pre := "package thing\n\nimport \"testing\"\n\nfunc TestExisting(t *testing.T) {}\n"
	if err := os.WriteFile(testPath, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, err := New(nil, target)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	contents, _ := os.ReadFile(ws.TestFilePath())
	if string(contents) != pre {
		t.Errorf("existing test file was modified by New\nbefore=%q\nafter=%q", pre, contents)
	}
}

func TestNew_RejectsNonGoFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "thing.txt")
	_ = os.WriteFile(target, []byte("nope"), 0o644)
	_, err := New(nil, target)
	if err == nil || !strings.Contains(err.Error(), "not a .go file") {
		t.Errorf("expected non-go error, got %v", err)
	}
}

func TestNew_RejectsTestFileAsTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "thing_test.go")
	_ = os.WriteFile(target, []byte("package thing\n"), 0o644)
	_, err := New(nil, target)
	if err == nil || !strings.Contains(err.Error(), "test file") {
		t.Errorf("expected test-file error, got %v", err)
	}
}

func TestNew_RejectsMissingTarget(t *testing.T) {
	_, err := New(nil, filepath.Join(t.TempDir(), "missing.go"))
	if err == nil {
		t.Error("expected error for missing target")
	}
}

func TestSnapshot_Append_Restore_IsByteIdentity(t *testing.T) {
	ws, err := New(nil, setupTarget(t))
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(ws.TestFilePath())
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ws.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if ws.SnapshotDepth() != 1 {
		t.Fatalf("depth = %d, want 1", ws.SnapshotDepth())
	}

	result, err := ws.AppendTest(validCandidate)
	if err != nil {
		t.Fatalf("AppendTest: %v", err)
	}
	if result.TestName != "TestThingNegative" {
		t.Errorf("TestName = %q, want TestThingNegative", result.TestName)
	}

	mid, _ := os.ReadFile(ws.TestFilePath())
	if bytes.Equal(mid, before) {
		t.Fatal("test file unchanged after AppendTest")
	}

	if err := ws.Restore(); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if ws.SnapshotDepth() != 0 {
		t.Errorf("depth after Restore = %d, want 0", ws.SnapshotDepth())
	}
	after, _ := os.ReadFile(ws.TestFilePath())
	if !bytes.Equal(after, before) {
		t.Errorf("not byte-identical after restore:\nbefore=%q\nafter=%q", before, after)
	}
}

func TestSnapshot_Append_Discard_KeepsChange(t *testing.T) {
	ws, _ := New(nil, setupTarget(t))
	if _, err := ws.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.AppendTest(validCandidate); err != nil {
		t.Fatal(err)
	}
	if err := ws.Discard(); err != nil {
		t.Fatalf("Discard: %v", err)
	}
	if ws.SnapshotDepth() != 0 {
		t.Errorf("depth after Discard = %d, want 0", ws.SnapshotDepth())
	}
	contents, _ := os.ReadFile(ws.TestFilePath())
	if !strings.Contains(string(contents), "TestThingNegative") {
		t.Errorf("test file does not contain appended test:\n%s", contents)
	}
}

func TestAppendTest_RejectsInvalidSyntax_LeavesFileUntouched(t *testing.T) {
	ws, _ := New(nil, setupTarget(t))
	before, _ := os.ReadFile(ws.TestFilePath())
	_, err := ws.AppendTest("this is not go")
	if err == nil {
		t.Fatal("expected validation error")
	}
	after, _ := os.ReadFile(ws.TestFilePath())
	if !bytes.Equal(before, after) {
		t.Error("test file was modified despite validation failure")
	}
}

func TestAppendTest_RejectsCollision_LeavesFileUntouched(t *testing.T) {
	ws, _ := New(nil, setupTarget(t))
	if _, err := ws.AppendTest(validCandidate); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(ws.TestFilePath())
	_, err := ws.AppendTest(validCandidate) // same name again
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected collision error, got %v", err)
	}
	after, _ := os.ReadFile(ws.TestFilePath())
	if !bytes.Equal(before, after) {
		t.Error("test file was modified despite collision")
	}
}

func TestRestore_WithoutSnapshot_Errors(t *testing.T) {
	ws, _ := New(nil, setupTarget(t))
	if err := ws.Restore(); err == nil {
		t.Error("expected error from Restore without snapshot")
	}
}

func TestDiscard_WithoutSnapshot_Errors(t *testing.T) {
	ws, _ := New(nil, setupTarget(t))
	if err := ws.Discard(); err == nil {
		t.Error("expected error from Discard without snapshot")
	}
}

func TestNestedSnapshots_RestoreUnwindsOneAtATime(t *testing.T) {
	ws, _ := New(nil, setupTarget(t))
	before, _ := os.ReadFile(ws.TestFilePath())

	// outer snapshot
	if _, err := ws.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.AppendTest(validCandidate); err != nil {
		t.Fatal(err)
	}
	afterOuter, _ := os.ReadFile(ws.TestFilePath())

	// inner snapshot, append a second test
	if _, err := ws.Snapshot(); err != nil {
		t.Fatal(err)
	}
	inner := strings.Replace(validCandidate, "TestThingNegative", "TestThingNonneg", 1)
	if _, err := ws.AppendTest(inner); err != nil {
		t.Fatal(err)
	}

	// restore inner: should bring us back to afterOuter
	if err := ws.Restore(); err != nil {
		t.Fatal(err)
	}
	mid, _ := os.ReadFile(ws.TestFilePath())
	if !bytes.Equal(mid, afterOuter) {
		t.Error("inner restore did not return to outer state")
	}

	// restore outer: should bring us back to before
	if err := ws.Restore(); err != nil {
		t.Fatal(err)
	}
	now, _ := os.ReadFile(ws.TestFilePath())
	if !bytes.Equal(now, before) {
		t.Error("outer restore did not return to initial state")
	}
	if ws.SnapshotDepth() != 0 {
		t.Errorf("depth = %d, want 0", ws.SnapshotDepth())
	}
}
