package coverage

import (
	"path/filepath"
	"strings"
	"testing"
)

const classifyFile = "github.com/tushariitr-19/testgen-loop-adk/examples/classify/classify.go"

func TestParse_Empty(t *testing.T) {
	report, err := Parse(filepath.Join("testdata", "empty.cov"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if report.TotalStmts != 0 {
		t.Errorf("TotalStmts = %d, want 0", report.TotalStmts)
	}
	if report.Percent != 0 {
		t.Errorf("Percent = %v, want 0", report.Percent)
	}
	if len(report.Files) != 0 {
		t.Errorf("Files = %d entries, want 0", len(report.Files))
	}
}

func TestParse_AllUncovered(t *testing.T) {
	report, err := Parse(filepath.Join("testdata", "classify_zero.cov"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got, want := report.TotalStmts, 6; got != want {
		t.Errorf("TotalStmts = %d, want %d", got, want)
	}
	if got, want := report.CoveredStmts, 0; got != want {
		t.Errorf("CoveredStmts = %d, want %d", got, want)
	}
	if report.Percent != 0 {
		t.Errorf("Percent = %v, want 0", report.Percent)
	}
	if len(report.Files) != 1 {
		t.Fatalf("Files = %d entries, want 1", len(report.Files))
	}
	f := report.Files[0]
	if f.Path != classifyFile {
		t.Errorf("Files[0].Path = %q, want %q", f.Path, classifyFile)
	}
	if len(f.Covered) != 0 {
		t.Errorf("Covered = %d blocks, want 0", len(f.Covered))
	}
	if len(f.Uncovered) != 6 {
		t.Errorf("Uncovered = %d blocks, want 6", len(f.Uncovered))
	}
}

func TestParse_PartiallyCovered(t *testing.T) {
	report, err := Parse(filepath.Join("testdata", "classify_partial.cov"))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if got, want := report.TotalStmts, 6; got != want {
		t.Errorf("TotalStmts = %d, want %d", got, want)
	}
	if got, want := report.CoveredStmts, 2; got != want {
		t.Errorf("CoveredStmts = %d, want %d", got, want)
	}
	// 2 / 6 == 33.333...
	if report.Percent < 33.3 || report.Percent > 33.4 {
		t.Errorf("Percent = %v, want ~33.33", report.Percent)
	}
	f := report.Files[0]
	if len(f.Covered) != 2 {
		t.Errorf("Covered = %d blocks, want 2", len(f.Covered))
	}
	if len(f.Uncovered) != 4 {
		t.Errorf("Uncovered = %d blocks, want 4", len(f.Uncovered))
	}
	// Sanity check the first covered block matches the fixture's first
	// hit line (line 8).
	if f.Covered[0].StartLine != 8 {
		t.Errorf("Covered[0].StartLine = %d, want 8", f.Covered[0].StartLine)
	}
}

func TestParse_MissingFile(t *testing.T) {
	_, err := Parse(filepath.Join("testdata", "does_not_exist.cov"))
	if err == nil {
		t.Fatal("expected error for missing profile, got nil")
	}
	if !strings.Contains(err.Error(), "stat profile") {
		t.Errorf("error %q does not mention stat", err.Error())
	}
}

func TestParse_EmptyPath(t *testing.T) {
	_, err := Parse("")
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

func TestParse_Malformed(t *testing.T) {
	_, err := Parse(filepath.Join("testdata", "malformed.cov"))
	if err == nil {
		t.Fatal("expected error for malformed profile, got nil")
	}
}

func TestParseReader_Nil(t *testing.T) {
	_, err := ParseReader(nil)
	if err == nil {
		t.Fatal("expected error for nil reader, got nil")
	}
}

func TestParseReader_Roundtrip(t *testing.T) {
	const profile = `mode: set
example.com/foo/bar.go:1.1,2.2 3 1
example.com/foo/bar.go:3.1,4.2 2 0
`
	report, err := ParseReader(strings.NewReader(profile))
	if err != nil {
		t.Fatalf("ParseReader returned error: %v", err)
	}
	if report.TotalStmts != 5 {
		t.Errorf("TotalStmts = %d, want 5", report.TotalStmts)
	}
	if report.CoveredStmts != 3 {
		t.Errorf("CoveredStmts = %d, want 3", report.CoveredStmts)
	}
	if report.Percent != 60 {
		t.Errorf("Percent = %v, want 60", report.Percent)
	}
}
