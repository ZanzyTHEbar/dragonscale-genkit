package adapters

// AdvancedRetriever implements the Retriever interface using vector storage.
// We want to use:
// - Graphiti-go core
//     - vector-based semantic retrieval
//     - BM25 key-word retrieval
//     - SQL retrieval
//     - Graph retrieval
// - Genkit Flow-based retrieval
// - Hybrid retriever utilizing Matryoshka Retrieval strategy
// - Combines semantic and keyword-based retrieval
// - Combines SQL and Graph retrieval
// - LLM Driven Retrieval for advanced & dynamic context manipulation