package adapters

import (
	"context"

	"github.com/ZanzyTHEbar/dragonscale-genkit"
	"github.com/firebase/genkit/go/core"
)

// SolverInput is the expected input structure for the solver flow.
type SolverInput struct {
	Query            string                 `json:"query"`
	ExecutionResults map[string]interface{} `json:"execution_results"`
	RetrievedContext string                 `json:"retrieved_context"`
}

// GenkitSolverAdapter uses a Genkit Flow to implement the Solver interface.
type GenkitSolverAdapter struct {
	solverFlow *core.Flow[*SolverInput, string, struct{}]
}

// NewGenkitSolverAdapter creates a new adapter for the solver flow.
func NewGenkitSolverAdapter(flow *core.Flow[*SolverInput, string, struct{}]) *GenkitSolverAdapter {
	return &GenkitSolverAdapter{solverFlow: flow}
}

// Synthesize implements the dragonscale.Solver interface.
func (a *GenkitSolverAdapter) Synthesize(ctx context.Context, query string, executionResults map[string]interface{}, retrievedContext string) (string, error) {
	if a.solverFlow == nil {
		// Use NewConfigurationError if the flow is not set
		return "", dragonscale.NewConfigurationError("solver", "solver flow is not configured", nil)
	}

	input := SolverInput{
		Query:            query,
		ExecutionResults: executionResults,
		RetrievedContext: retrievedContext,
	}

	finalAnswer, err := a.solverFlow.Run(ctx, &input)
	if err != nil {
		// Wrap the error using NewSolverError
		return "", dragonscale.NewSolverError("solver flow execution failed", err)
	}

	return finalAnswer, nil
}
