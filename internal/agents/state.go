// Package agents wires the testgen-loop three-agent flow on top of Google
// ADK Go. The Evaluator measures coverage, the Mutator proposes a new test
// (a hardcoded stub in Phase 3; LLM-backed in Phase 4), and the Selector
// applies, verifies, and accepts or rolls back the change.
//
// The agents communicate only through session state — never by direct
// function calls. The keys they read and write are declared in this file.
package agents

import (
	"errors"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"
)

// Session-state keys used by the loop. These are the single source of
// truth for the agent <-> agent contract; do not introduce new keys ad-hoc
// elsewhere.
const (
	// StateIteration is the 1-based count of completed Evaluator runs.
	// Evaluator increments it; Mutator and Selector read it for logging.
	StateIteration = "iteration"

	// StateCurrentCoverage is the latest measured coverage percentage in
	// the closed interval [0, 100]. Evaluator writes the baseline at the
	// top of each iteration; Selector overwrites it after an accepted
	// append.
	StateCurrentCoverage = "current_coverage"

	// StateUncoveredSummary is a human/LLM-readable summary of uncovered
	// blocks. Evaluator writes it; Mutator consumes it in its prompt.
	StateUncoveredSummary = "uncovered_summary"

	// StateCandidateTest is the Go source of a single test function the
	// Mutator proposes for this iteration. Selector consumes it.
	StateCandidateTest = "candidate_test"

	// StateAcceptedTests is the list of test-function names accepted so
	// far. Mutator can read it to discourage repeats (Phase 4).
	StateAcceptedTests = "accepted_tests"

	// StateRejectedTests is the list of rejected proposals, formatted as
	// "name:reason" entries. Used by the final report.
	StateRejectedTests = "rejected_tests"

	// StateConsecutiveNoGain counts iterations in a row that did not
	// raise coverage. Reset on accept-with-gain; compared to Patience to
	// detect a plateau.
	StateConsecutiveNoGain = "consecutive_no_gain"

	// StateStopReason names the condition under which the Selector
	// escalated to terminate the loop.
	StateStopReason = "stop_reason"

	// StateTargetSource is the full Go source of the target file, seeded
	// into session state once by the orchestrator at startup. The Mutator
	// reads it when building the prompt for Gemini.
	StateTargetSource = "target_source"

	// StateTargetPath is the absolute path of the target file, seeded into
	// session state once by the orchestrator at startup.
	StateTargetPath = "target_path"
)

// Stop reasons written to StateStopReason when the Selector escalates.
const (
	StopReasonTarget  = "target"  // current_coverage >= target
	StopReasonPlateau = "plateau" // consecutive_no_gain >= patience
	StopReasonBudget  = "budget"  // MaxIterations exhausted (set by orchestrator)
	StopReasonError   = "error"   // unrecoverable failure during an agent run
)

// readInt returns the int value of key, or 0 if the key is absent or its
// value is not int-shaped. State serialization can round-trip ints through
// float64 (JSON-style), so both are accepted.
func readInt(ctx agent.InvocationContext, key string) int {
	v, err := ctx.Session().State().Get(key)
	if errors.Is(err, session.ErrStateKeyNotExist) || v == nil {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// readFloat returns the float64 value of key, or 0 if absent / wrong type.
func readFloat(ctx agent.InvocationContext, key string) float64 {
	v, err := ctx.Session().State().Get(key)
	if errors.Is(err, session.ErrStateKeyNotExist) || v == nil {
		return 0
	}
	switch f := v.(type) {
	case float64:
		return f
	case int:
		return float64(f)
	case int64:
		return float64(f)
	default:
		return 0
	}
}

// readString returns the string value of key, or "" if absent / wrong type.
func readString(ctx agent.InvocationContext, key string) string {
	v, err := ctx.Session().State().Get(key)
	if errors.Is(err, session.ErrStateKeyNotExist) || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// readStringSlice returns the []string value of key. It accepts both
// []string and []any (the latter is what a JSON round-trip produces).
func readStringSlice(ctx agent.InvocationContext, key string) []string {
	v, err := ctx.Session().State().Get(key)
	if errors.Is(err, session.ErrStateKeyNotExist) || v == nil {
		return nil
	}
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, a := range s {
			if str, ok := a.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}
