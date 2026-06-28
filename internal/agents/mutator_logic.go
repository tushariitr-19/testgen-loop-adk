package agents

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"
)

// buildMutatorPrompt assembles the Gemini prompt by reading the current
// loop state and substituting it into the embedded template. Centralizing
// the substitution here keeps the state shape decoupled from the prompt
// shape, and lets us format each value the way Gemini expects.
func buildMutatorPrompt(rctx agent.ReadonlyContext) (string, error) {
	state := rctx.ReadonlyState()

	targetSource := readStringRO(state, StateTargetSource)
	targetPath := readStringRO(state, StateTargetPath)
	uncoveredSummary := readStringRO(state, StateUncoveredSummary)

	iteration := readIntRO(state, StateIteration)
	currentCov := readFloatRO(state, StateCurrentCoverage)
	accepted := readStringSliceRO(state, StateAcceptedTests)
	rejected := readStringSliceRO(state, StateRejectedTests)

	if targetSource == "" {
		return "", fmt.Errorf("mutator: state has no %s — orchestrator must seed it", StateTargetSource)
	}

	acceptedStr := "(none yet)"
	if len(accepted) > 0 {
		acceptedStr = strings.Join(accepted, ", ")
	}
	rejectedStr := "(none yet)"
	if len(rejected) > 0 {
		rejectedStr = strings.Join(rejected, ", ")
	}
	if uncoveredSummary == "" {
		uncoveredSummary = "(no summary available)"
	}

	prompt := mutatorTemplate
	prompt = strings.ReplaceAll(prompt, "{{TARGET_PATH}}", targetPath)
	prompt = strings.ReplaceAll(prompt, "{{TARGET_SOURCE}}", targetSource)
	prompt = strings.ReplaceAll(prompt, "{{ITERATION}}", fmt.Sprintf("%d", iteration))
	prompt = strings.ReplaceAll(prompt, "{{CURRENT_COVERAGE}}", fmt.Sprintf("%.1f", currentCov))
	prompt = strings.ReplaceAll(prompt, "{{UNCOVERED_SUMMARY}}", uncoveredSummary)
	prompt = strings.ReplaceAll(prompt, "{{ACCEPTED_TESTS}}", acceptedStr)
	prompt = strings.ReplaceAll(prompt, "{{REJECTED_TESTS}}", rejectedStr)
	return prompt, nil
}

// fenceRe matches a triple-backtick code fence with an optional language hint
// surrounding the entire string. We strip the fence only if it wraps the
// whole response; partial / inline backticks inside the code are left alone.
var fenceRe = regexp.MustCompile("(?s)^```[a-zA-Z]*\\s*\\n(.*?)\\n```\\s*$")

// cleanCandidateCode strips common markdown wrappers from a Gemini response
// to leave plain Go source. The prompt asks Gemini to omit fences, but
// production code does not trust prompt-following alone. Returns the input
// trimmed of leading/trailing whitespace if no fence is present.
func cleanCandidateCode(raw string) string {
	s := strings.TrimSpace(raw)
	if m := fenceRe.FindStringSubmatch(s); m != nil {
		s = m[1]
	}
	return strings.TrimSpace(s)
}

// ReadonlyState-aware helpers, mirroring the InvocationContext readers in
// state.go. These exist as a separate set because session.State and
// session.ReadonlyState are distinct interfaces in the ADK, and the
// Mutator's InstructionProvider receives a ReadonlyContext, not an
// InvocationContext.

func readStringRO(s session.ReadonlyState, key string) string {
	v, err := s.Get(key)
	if errors.Is(err, session.ErrStateKeyNotExist) || v == nil {
		return ""
	}
	if str, ok := v.(string); ok {
		return str
	}
	return ""
}

func readIntRO(s session.ReadonlyState, key string) int {
	v, err := s.Get(key)
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

func readFloatRO(s session.ReadonlyState, key string) float64 {
	v, err := s.Get(key)
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

func readStringSliceRO(s session.ReadonlyState, key string) []string {
	v, err := s.Get(key)
	if errors.Is(err, session.ErrStateKeyNotExist) || v == nil {
		return nil
	}
	switch ss := v.(type) {
	case []string:
		return ss
	case []any:
		out := make([]string, 0, len(ss))
		for _, a := range ss {
			if str, ok := a.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}
