package adapters

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ZanzyTHEbar/dragonscale-genkit"
	"github.com/firebase/genkit/go/ai"
)

// VectorStoreRetriever implements the Retriever interface using vector storage.
type VectorStoreRetriever struct {
	genkitRetriever ai.Retriever
	embedder        ai.Embedder
	maxResults      int
	minScore        float64
	contextWindow   int
}

// VectorStoreRetrieverOption is a function that configures a VectorStoreRetriever.
type VectorStoreRetrieverOption func(*VectorStoreRetriever)

// WithMaxResults sets the maximum number of results to return.
func WithMaxResults(max int) VectorStoreRetrieverOption {
	return func(r *VectorStoreRetriever) {
		r.maxResults = max
	}
}

// WithMinScore sets the minimum similarity score for results.
func WithMinScore(min float64) VectorStoreRetrieverOption {
	return func(r *VectorStoreRetriever) {
		r.minScore = min
	}
}

// WithContextWindow sets the maximum tokens to include in context window.
func WithContextWindow(windowSize int) VectorStoreRetrieverOption {
	return func(r *VectorStoreRetriever) {
		r.contextWindow = windowSize
	}
}

// NewVectorStoreRetriever creates a new vector store-based retriever.
func NewVectorStoreRetriever(
	genkitRetriever ai.Retriever,
	embedder ai.Embedder,
	options ...VectorStoreRetrieverOption,
) *VectorStoreRetriever {
	// Default configuration
	retriever := &VectorStoreRetriever{
		genkitRetriever: genkitRetriever,
		embedder:        embedder,
		maxResults:      5,
		minScore:        0.7,
		contextWindow:   2048,
	}

	// Apply options
	for _, option := range options {
		option(retriever)
	}

	return retriever
}

// RetrieveContext implements the dragonscale.Retriever interface.
func (r *VectorStoreRetriever) RetrieveContext(ctx context.Context, query string, dag *dragonscale.ExecutionPlan) (string, error) {
	startTime := time.Now()
	log.Printf("Vector retrieval starting (query: %s)", query)

	// Retrieve documents based on query
	resp, err := ai.Retrieve(ctx, r.genkitRetriever,
		ai.WithTextDocs(query),
		ai.WithConfig(map[string]interface{}{
			"k":            r.maxResults,
			"minScore":     r.minScore,
			"returnScores": true,
		}),
	)
	if err != nil {
		return "", fmt.Errorf("vector retrieval failed: %w", err)
	}

	// Format retrieved documents
	totalTokens := 0
	var formattedContext string
	for i, doc := range resp.Documents {
		score := 0.0
		if scoreVal, ok := doc.Metadata["score"]; ok {
			if s, ok := scoreVal.(float64); ok {
				score = s
			}
		}

		// Simple token count estimation (can be replaced with a proper tokenizer)
		estTokens := len(doc.Content) / 4 // Rough estimate: ~4 chars per token

		// Check if adding this document would exceed our context window
		if totalTokens+estTokens > r.contextWindow {
			log.Printf("Context window limit reached (docs_included: %d, total_docs: %d, estimated_tokens: %d)",
				i,
				len(resp.Documents),
				totalTokens)
			break
		}

		formattedContext += fmt.Sprintf("--- Document %d (score: %.4f) ---\n%s\n\n",
			i+1, score, doc.Content)
		totalTokens += estTokens
	}

	log.Printf("Vector retrieval complete (documents_retrieved: %d, documents_used: %d, estimated_tokens: %d, duration_ms: %d)",
		len(resp.Documents),
		len(formattedContext),
		totalTokens,
		time.Since(startTime).Milliseconds())

	if formattedContext == "" {
		return "No relevant documents found.", nil
	}

	return formattedContext, nil
}
