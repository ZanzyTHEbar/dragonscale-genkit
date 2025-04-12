// main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	"github.com/firebase/genkit/go/plugins/googlegenai"

	"github.com/ZanzyTHEbar/dragonscale-genkit/internal/adapters"
	"github.com/ZanzyTHEbar/dragonscale-genkit/internal/cache"
	"github.com/ZanzyTHEbar/dragonscale-genkit/internal/executor"
	"github.com/ZanzyTHEbar/dragonscale-genkit/internal/tools"
	"github.com/ZanzyTHEbar/dragonscale-genkit/pkg/dragonscale"
)

// Function to get tool schemas for the planner
func getToolSchemas() map[string]string {
	schemas := make(map[string]string)
	availableTools := tools.SetupTools()

	for name, tool := range availableTools {
		schema := tool.Schema()
		// Extract description or format schema as needed for the planner prompt
		if desc, ok := schema["description"].(string); ok {
			schemas[name] = desc
		} else {
			schemas[name] = "No description available."
		}
	}
	return schemas
}

func main() {
	ctx := context.Background()

	// Configure Google AI plugin (Gemini)
	// Ensure GEMINI_API_KEY environment variable is set
	geminiKey := os.Getenv("GEMINI_API_KEY")
	if geminiKey == "" {
		log.Fatal("GEMINI_API_KEY environment variable not set.")
	}

	// Initialize Genkit with the Google AI plugin
	g, err := genkit.Init(ctx,
		genkit.WithPlugins(&googlegenai.GoogleAI{}),
	)
	if err != nil {
		log.Fatal("Genkit initialization failed:", err)
	}

	// Create dependencies
	memCache := cache.NewInMemoryCache(10 * time.Minute)

	// Set up tools with our enhanced implementation
	availableTools := tools.SetupTools()

	// Create enhanced executor with options
	dagExecutor := executor.NewDAGExecutor(
		availableTools,
		5, // Max 5 concurrent tool executions
		executor.WithMaxRetries(3),
		executor.WithRetryDelay(time.Second*2),
	)

	// --- Define Genkit Flows ---

	// 1. Planner Flow
	plannerFlow := genkit.DefineFlow(g, "plannerFlow",
		func(ctx context.Context, input *dragonscale.PlannerInput) (*dragonscale.ExecutionPlan, error) {
			log.Printf("Planner flow starting (query: %s)", input.Query)

			// Construct prompt for the LLM
			prompt := fmt.Sprintf("Plan the execution of the following query: %s\nTool Schemas: %v", input.Query, input.ToolSchema)
			log.Printf("Planner prompt: %s", prompt)

			// Use Genkit Generate with structured output
			resp, err := genkit.Generate(ctx, g,
				ai.WithPrompt(prompt),
				ai.WithOutputType(&dragonscale.ExecutionPlan{}),
			)
			if err != nil {
				return nil, fmt.Errorf("planner generation failed: %w", err)
			}

			var plan *dragonscale.ExecutionPlan
			if err := resp.Output(&plan); err != nil {
				return nil, fmt.Errorf("failed to parse execution plan from LLM output")
			}

			return plan, nil
		},
	)

	// 2. Solver Flow (Placeholder - just combines results)
	solverFlow := genkit.DefineFlow(g, "solverFlow",
		func(ctx context.Context, input *adapters.SolverInput) (string, error) {
			log.Printf("Solver flow starting (query: %s)", input.Query)
			// Simple synthesis for now. Replace with LLM call for complex cases.
			var resultBuilder strings.Builder
			resultBuilder.WriteString(fmt.Sprintf("Based on the query '%s':\n", input.Query))
			if input.RetrievedContext != "" {
				resultBuilder.WriteString(fmt.Sprintf("Retrieved Context: %s\n", input.RetrievedContext))
			}
			resultBuilder.WriteString("Execution Results:\n")
			for key, val := range input.ExecutionResults {
				resultBuilder.WriteString(fmt.Sprintf("- %s: %v\n", key, val))
			}
			finalAnswer := resultBuilder.String()
			log.Printf("Solver flow finished")
			return finalAnswer, nil
		},
	)

	// 3. Retriever Flow (Placeholder - returns empty string)
	retrieverFlow := genkit.DefineFlow(g, "retrieverFlow",
		func(ctx context.Context, query *string) (string, error) {
			log.Printf("Retriever flow starting (placeholder) (query: %s)", *query)
			// TODO: In a real scenario, this would fetch relevant data
			return "", nil // Returning empty context for now
		},
	)

	// --- Instantiate Adapters ---
	plannerAdapter := adapters.NewGenkitPlannerAdapter(plannerFlow, memCache)
	solverAdapter := adapters.NewGenkitSolverAdapter(solverFlow)
	retrieverAdapter := adapters.NewGenkitRetrieverAdapter(retrieverFlow) // Optional

	// --- Define Main Orchestrator Flow ---
	_ = genkit.DefineFlow(g, "dragonscaleGenkitFlow",
		func(ctx context.Context, query string) (string, error) {
			log.Printf("Dragonscale Genkit Flow starting (query: %s)", query)

			// 1. Prepare Planner Input
			plannerInput := dragonscale.PlannerInput{
				Query:      query,
				ToolSchema: getToolSchemas(),
			}

			// 2. Get Execution Plan (using Adapter with Cache)
			executionPlan, err := plannerAdapter.GeneratePlan(ctx, plannerInput)
			if err != nil {
				return "", fmt.Errorf("failed to generate execution plan: %w", err)
			}

			// 3. Execute the Plan (using DAGExecutor)
			executionResults, err := dagExecutor.ExecutePlan(ctx, executionPlan)
			if err != nil {
				// Maybe try replanning here in the future
				return "", fmt.Errorf("DAG execution failed: %w", err)
			}

			// 4. Retrieve Context (Optional, could run in parallel with executor)
			retrievedContext, err := retrieverAdapter.RetrieveContext(ctx, query, executionPlan)
			if err != nil {
				log.Printf("Context retrieval failed (error: %v)", err)
				retrievedContext = "" // Continue without context on failure
			}

			// 5. Synthesize Final Answer (using Solver Adapter)
			finalAnswer, err := solverAdapter.Synthesize(ctx, query, executionResults, retrievedContext)
			if err != nil {
				return "", fmt.Errorf("failed to synthesize final answer: %w", err)
			}

			log.Printf("Dragonscale Genkit Flow finished successfully")
			return finalAnswer, nil
		},
	)

	log.Println("Genkit initialized successfully. Dragonscale flows defined.")
	log.Println("To run: genkit flow run dragonscaleGenkitFlow '{\"data\": \"Your query here\"}'")
	// Keep the application running (e.g., for local testing with genkit start)
	select {}
}
