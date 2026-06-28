// Package coverage runs `go test -coverprofile=...` against a target package
// and parses the resulting profile into a structured report describing what
// is and isn't covered. It has no dependency on the agent layer or the LLM.
package coverage

// CoverageReport is the parsed result of a single `go test -coverprofile`
// invocation. Percentages are reported on a 0-100 scale to mirror the way
// `go test` itself displays coverage; config thresholds live on a 0-1 scale
// and are converted at the comparison site.
type CoverageReport struct {
	// Percent is the overall covered-statement percentage across all files
	// in the report, in the closed interval [0, 100].
	Percent float64

	// TotalStmts is the sum of statements across every block in the profile.
	TotalStmts int

	// CoveredStmts is the count of statements hit at least once.
	CoveredStmts int

	// Files holds per-file detail. Order matches the order of files in the
	// underlying profile, which is the order `go test` emitted them.
	Files []FileCoverage
}

// FileCoverage is the slice of a CoverageReport that pertains to one source
// file. Blocks are split into Covered and Uncovered for the Mutator's
// convenience — it consumes Uncovered directly when picking a target branch.
type FileCoverage struct {
	// Path is the file path as recorded in the coverage profile. This is
	// the fully-qualified path the Go toolchain emitted, typically prefixed
	// with the module path (e.g. github.com/foo/bar/baz.go), not an
	// on-disk absolute path.
	Path string

	// Covered lists blocks whose statements ran at least once.
	Covered []Block

	// Uncovered lists blocks whose statements never ran. These are the
	// candidate targets the Mutator should aim a new test at.
	Uncovered []Block

	// Functions is a per-function coverage rollup. It is left nil by the
	// default parser; populating it requires an AST walk that the v1 flow
	// does not need. Future versions may fill it in via a parser option.
	Functions []FunctionCoverage
}

// Block is one contiguous range of statements as reported by the Go cover
// tool. Line and column numbers are 1-indexed and identify the inclusive
// start and end of the range in the source file.
type Block struct {
	// StartLine is the 1-indexed source line where the block begins.
	StartLine int
	// StartCol is the 1-indexed column where the block begins on StartLine.
	StartCol int
	// EndLine is the 1-indexed source line where the block ends.
	EndLine int
	// EndCol is the 1-indexed column where the block ends on EndLine.
	EndCol int
	// NumStmts is the number of statements contained in the block.
	NumStmts int
}

// FunctionCoverage describes one function's coverage in a file. It is not
// populated by the v1 parser; the type is defined here so the report shape
// is stable when a future parser option enables function rollups.
type FunctionCoverage struct {
	// Name is the function name as declared in source.
	Name string
	// StartLine is the 1-indexed line of the function declaration.
	StartLine int
	// Percent is the covered-statement percentage for this function, in
	// the closed interval [0, 100].
	Percent float64
}
