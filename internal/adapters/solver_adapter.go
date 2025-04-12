package adapters

import (
	"context"
	"fmt"

	"github.com/firebase/genkit/go/genkit"
)

// SolverInput is the expected input structure for the solver flow.
type SolverInput struct {
	Query            string                 `json:"query"`
	ExecutionResults map[string]interface{} `json:"execution_results"`
	RetrievedContext string                 `json:"retrieved_context"`
}

// GenkitSolverAdapter uses a Genkit Flow to implement the Solver interface.
type GenkitSolverAdapter struct {
	solverFlow *genkit.Flow[SolverInput, string, struct{}]
}

// NewGenkitSolverAdapter creates a new adapter for the solver flow.
func NewGenkitSolverAdapter(flow *genkit.Flow[SolverInput, string, struct{}]) *GenkitSolverAdapter {
	return &GenkitSolverAdapter{solverFlow: flow}
}

// Synthesize implements the dragonscale.Solver interface.
func (a *GenkitSolverAdapter) Synthesize(ctx context.Context, query string, executionResults map[string]interface{}, retrievedContext string) (string, error) {
	if a.solverFlow == nil {
		return "", fmt.Errorf("solver flow is not configured")
	}

	input := SolverInput{
		Query:            query,
		ExecutionResults: executionResults,
		RetrievedContext: retrievedContext,
	}

	finalAnswer, err := genkit.RunFlow(ctx, a.solverFlow, &input)
	if err != nil {
		return "", fmt.Errorf("solver flow execution failed: %w", err)
	}

	return finalAnswer, nil
}
