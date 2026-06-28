package agents

import (
	"iter"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"
)

const selectorName = "Selector"

// NewSelector builds the Selector custom agent. The Selector reads the
// candidate test from session state, applies it through the Workspace, runs
// coverage to verify it improves, and either accepts (discarding the
// snapshot) or rolls back (restoring it). When the target is hit or the
// patience budget is exhausted it sets Escalate=true to terminate the
// LoopAgent.
//
// All real work lives in [runSelector] in selector_logic.go so it can be
// unit-tested without an ADK runtime.
func NewSelector(deps Deps) (agent.Agent, error) {
	return agent.New(agent.Config{
		Name:        selectorName,
		Description: "Applies the candidate test, verifies the gain, accepts or rolls back, and stops the loop on target / plateau.",
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				runSelector(ctx, deps, yield)
			}
		},
	})
}
