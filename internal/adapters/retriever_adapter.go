package adapters

import (
	"context"
	"fmt"

	"github.com/ZanzyTHEbar/dragonscale-genkit/pkg/dragonscale"
	"github.com/firebase/genkit/go/core"
	"github.com/firebase/genkit/go/genkit"
)

// GenkitRetrieverAdapter uses a Genkit Flow to implement the Retriever interface.
type GenkitRetrieverAdapter struct {
	retrieverFlow *core.Flow[*string, string, struct{}] // Updated Flow type declaration
	g             *genkit.Genkit                       // Genkit instance
}

// NewGenkitRetrieverAdapter creates a new adapter for the retriever flow.
func NewGenkitRetrieverAdapter(flow *core.Flow[*string, string, struct{}]) *GenkitRetrieverAdapter {
	return &GenkitRetrieverAdapter{retrieverFlow: flow}
}

// RetrieveContext implements the dragonscale.Retriever interface.
func (a *GenkitRetrieverAdapter) RetrieveContext(ctx context.Context, query string, dag *dragonscale.ExecutionPlan) (string, error) {
	if a.retrieverFlow == nil {
		// Use standard Go logger since we might not have a genkit instance
		return "", nil // Or return an error if retrieval is mandatory
	}

	// Prepare input for the retriever flow (example: just the query)
	input := query // Adjust as needed

	// Use genkit.RunFlow with the updated API
	retrievedData, err := a.retrieverFlow.Run(ctx, &input)
	if err != nil {
		return "", fmt.Errorf("retriever flow execution failed: %w", err)
	}

	return retrievedData, nil
}
