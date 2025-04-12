package adapters

import (
	"context"
	"fmt"

	"github.com/ZanzyTHEbar/dragonscale-genkit/pkg/dragonscale"
	"github.com/firebase/genkit/go/genkit"
)

// GenkitRetrieverAdapter uses a Genkit Flow to implement the Retriever interface.
type GenkitRetrieverAdapter struct {
	retrieverFlow *genkit.Flow[ /* Input Type */ string /* Output Type */, string, struct{}] // Define types based on flow
}

// NewGenkitRetrieverAdapter creates a new adapter for the retriever flow.
func NewGenkitRetrieverAdapter(flow *genkit.Flow[string, string, struct{}]) *GenkitRetrieverAdapter {
	return &GenkitRetrieverAdapter{retrieverFlow: flow}
}

// RetrieveContext implements the dragonscale.Retriever interface.
func (a *GenkitRetrieverAdapter) RetrieveContext(ctx context.Context, query string, dag *dragonscale.ExecutionPlan) (string, error) {
	if a.retrieverFlow == nil {
		genkit.Logger(ctx).Warn("Retriever flow is not configured, returning empty context")
		return "", nil // Or return an error if retrieval is mandatory
	}

	// Prepare input for the retriever flow (example: just the query)
	input := query // Adjust as needed

	retrievedData, err := genkit.RunFlow(ctx, a.retrieverFlow, &input)
	if err != nil {
		return "", fmt.Errorf("retriever flow execution failed: %w", err)
	}

	return retrievedData, nil
}
