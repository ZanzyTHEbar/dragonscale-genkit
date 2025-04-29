package adapters

import (
	"context"
	"log"

	"github.com/ZanzyTHEbar/dragonscale-genkit"
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
		log.Println("Retriever flow is not configured, skipping retrieval.")
		return "", nil // Return empty string, not an error
	}

	// Prepare input for the retriever flow (example: just the query)
	input := query // Adjust as needed

	// Use genkit.RunFlow with the updated API
	retrievedData, err := a.retrieverFlow.Run(ctx, &input)
	if err != nil {
		// Wrap the error using NewRetrieverError
		return "", dragonscale.NewRetrieverError("retriever flow execution failed", err)
	}

	return retrievedData, nil
}
