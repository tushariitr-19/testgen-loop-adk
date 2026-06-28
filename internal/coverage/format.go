package coverage

import (
	"fmt"
	"strings"
)

// String renders a multi-line, human-readable summary of the report. It is
// intended for CLI output (e.g. the --dry-run path), not for log lines —
// structured fields belong on the logger.
//
// The output begins with a one-line overall summary, then a per-file listing
// of uncovered blocks. Files with no uncovered blocks are omitted.
func (r *CoverageReport) String() string {
	if r == nil {
		return "coverage: <nil report>\n"
	}
	var b strings.Builder

	fmt.Fprintf(&b, "coverage: %.1f%% (%d/%d statements covered)\n",
		r.Percent, r.CoveredStmts, r.TotalStmts)

	uncoveredBlocks := 0
	for _, f := range r.Files {
		uncoveredBlocks += len(f.Uncovered)
	}
	fmt.Fprintf(&b, "uncovered blocks: %d across %d file(s)\n",
		uncoveredBlocks, countFilesWithUncovered(r))

	for _, f := range r.Files {
		if len(f.Uncovered) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s\n", f.Path)
		for _, blk := range f.Uncovered {
			fmt.Fprintf(&b, "  L%d:%d-L%d:%d  (%d stmt)\n",
				blk.StartLine, blk.StartCol,
				blk.EndLine, blk.EndCol,
				blk.NumStmts,
			)
		}
	}
	return b.String()
}

func countFilesWithUncovered(r *CoverageReport) int {
	n := 0
	for _, f := range r.Files {
		if len(f.Uncovered) > 0 {
			n++
		}
	}
	return n
}
