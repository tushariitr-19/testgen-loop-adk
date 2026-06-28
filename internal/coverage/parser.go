package coverage

import (
	"fmt"
	"io"
	"os"
	"sort"

	"golang.org/x/tools/cover"
)

// Parse reads a Go coverage profile from disk and returns the structured
// report. It returns an error if the file is missing, unreadable, or
// malformed.
func Parse(profilePath string) (*CoverageReport, error) {
	if profilePath == "" {
		return nil, fmt.Errorf("coverage: profile path is empty")
	}
	if _, err := os.Stat(profilePath); err != nil {
		return nil, fmt.Errorf("coverage: stat profile %q: %w", profilePath, err)
	}
	profiles, err := cover.ParseProfiles(profilePath)
	if err != nil {
		return nil, fmt.Errorf("coverage: parsing profile %q: %w", profilePath, err)
	}
	return reportFromProfiles(profiles), nil
}

// ParseReader is the io.Reader-based parser, useful for tests and for
// pipelines that hand the parser a profile already in memory.
func ParseReader(r io.Reader) (*CoverageReport, error) {
	if r == nil {
		return nil, fmt.Errorf("coverage: nil reader")
	}
	profiles, err := cover.ParseProfilesFromReader(r)
	if err != nil {
		return nil, fmt.Errorf("coverage: parsing profile: %w", err)
	}
	return reportFromProfiles(profiles), nil
}

func reportFromProfiles(profiles []*cover.Profile) *CoverageReport {
	report := &CoverageReport{
		Files: make([]FileCoverage, 0, len(profiles)),
	}
	// Stable file order: the underlying library already sorts by file name,
	// but sort again defensively so the report is deterministic regardless
	// of library version.
	sort.SliceStable(profiles, func(i, j int) bool {
		return profiles[i].FileName < profiles[j].FileName
	})

	for _, p := range profiles {
		fc := FileCoverage{Path: p.FileName}
		for _, b := range p.Blocks {
			block := Block{
				StartLine: b.StartLine,
				StartCol:  b.StartCol,
				EndLine:   b.EndLine,
				EndCol:    b.EndCol,
				NumStmts:  b.NumStmt,
			}
			report.TotalStmts += b.NumStmt
			if b.Count > 0 {
				report.CoveredStmts += b.NumStmt
				fc.Covered = append(fc.Covered, block)
			} else {
				fc.Uncovered = append(fc.Uncovered, block)
			}
		}
		report.Files = append(report.Files, fc)
	}

	if report.TotalStmts > 0 {
		report.Percent = 100 * float64(report.CoveredStmts) / float64(report.TotalStmts)
	}
	return report
}
