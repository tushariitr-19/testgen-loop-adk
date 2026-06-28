package agents

import (
	"iter"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"
)

const evaluatorName = "Evaluator"

// NewEvaluator builds the Evaluator custom agent. The agent runs the
// coverage subsystem against the workspace package and writes the
// baseline reading (iteration, current_coverage, uncovered_summary) into
// session state. It never modifies the test file.
//
// All real work lives in [runEvaluator] in evaluator_logic.go so it can
// be unit-tested without an ADK runtime.
func NewEvaluator(deps Deps) (agent.Agent, error) {
	return agent.New(agent.Config{
		Name:        evaluatorName,
		Description: "Measures coverage and publishes the baseline to session state.",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				runEvaluator(ctx, deps, yield)
			}
		},
	})
}
