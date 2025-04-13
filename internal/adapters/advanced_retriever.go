package adapters

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ZanzyTHEbar/dragonscale-genkit"
	"github.com/firebase/genkit/go/ai"
)

// AdvancedRetriever implements the Retriever interface using vector storage.
// We want to use: 
// - vector-based semantic retrieval
// - BM25 key-word retrieval
// - SQL retrieval
// - Graph retrieval
// - Genkit Flow-based retrieval
// - Hybrid retriever utilizing Matryoshka Retrieval strategy
// - Combines semantic and keyword-based retrieval
// - Supports multiple vector stores through an interface


type AdvancedRetriever struct {
	genkitRetriever ai.Retriever
	embedder        ai.Embedder
	maxResults      int
	minScore        float64
	contextWindow   int
}

// AdvancedRetrieverOption is a function that configures a AdvancedRetriever.
type AdvancedRetrieverOption func(*AdvancedRetriever)

// NewAdvancedRetriever creates a new vector store-based retriever.
func NewAdvancedRetriever(
	genkitRetriever ai.Retriever,
	embedder ai.Embedder,
	options ...AdvancedRetrieverOption,
) *AdvancedRetriever {
	// Default configuration
	retriever := &AdvancedRetriever{
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
func (r *AdvancedRetriever) RetrieveContext(ctx context.Context, query string, dag *dragonscale.ExecutionPlan) (string, error) {
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
