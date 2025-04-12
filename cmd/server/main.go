// main.go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/firebase/genkit/go/genkit"

	"github.com/ZanzyTHEbar/dragonscale-genkit/internal/adapters"
	"github.com/ZanzyTHEbar/dragonscale-genkit/internal/cache"
	"github.com/ZanzyTHEbar/dragonscale-genkit/internal/executor"
	"github.com/ZanzyTHEbar/dragonscale-genkit/internal/tools"
	"github.com/ZanzyTHEbar/dragonscale-genkit/pkg/dragonscale"
)

// Define available tools globally for easy access
var availableTools = map[string]dragonscale.Tool{
	"search": adapters.NewGoToolAdapter(
		tools.PerformSearch,
		map[string]interface{}{"description": "Performs a web search for a given query."},
	),
	"calculate": adapters.NewGoToolAdapter(
		tools.PerformCalculation,
		map[string]interface{}{"description": "Calculates a mathematical expression."},
	),
}

// Function to get tool schemas for the planner
func getToolSchemas() map[string]string {
	schemas := make(map[string]string)
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
	pluginLoader, err := genkit.Init(ctx,
		genkit.WithPlugins(&googleai.GoogleAI{}),
		genkit.WithDefaultModel("googleai/gemini-1.5-flash"), // Use a capable model
		genkit.WithFlowStateStore(&genkit.NoopStore{}), // Use NoopStore for simplicity, replace for production
		genkit.WithTraceStore(&genkit.NoopStore{}),   // Use NoopStore for simplicity, replace for production
	)
	if err != nil {
		log.Fatal("Genkit initialization failed:", err)
	}
	defer pluginLoader.Shutdown(ctx)

	// Create dependencies
	memCache := cache.NewInMemoryCache(10 * time.Minute)
	dagExecutor := executor.NewDAGExecutor(availableTools, 5) // Max 5 concurrent tool executions

	// --- Define Genkit Flows --- 

	// 1. Planner Flow
	plannerFlow := genkit.DefineFlow("plannerFlow",
		func(ctx context.Context, input *dragonscale.PlannerInput) (*dragonscale.ExecutionPlan, error) {
			genkit.Logger(ctx).Info("Planner flow starting", "query", input.Query)

			// Construct prompt for the LLM
			prompt := fmt.Sprintf(
				`Generate a plan to answer the query: "%s"

Available tools:
%v

Output the plan as a JSON object matching this Go type:

type Task struct {
    ID          string   \`+"`json:"id"`"+\`
    Description string   \`+"`json:"description"`"+\`
    ToolName    string   \`+"`json:"tool_name"`"+\`
    Args        []string \`+"`json:"args"`"+\` // Represent dependencies using placeholders like "${taskID_output}"
    DependsOn   []string \`+"`json:"depends_on"`"+\`
}
type ExecutionPlan struct {
    Tasks []Task \`+"`json:"tasks"`"+\`
}

Ensure IDs are simple strings (e.g., "task1", "task2"). Ensure DependsOn contains the ID(s) of prerequisite tasks.
If a task needs the output of another, use the format "${taskID_output}" within the Args field.

Example Query: "Search for apples then calculate 5*9"
Example Plan:
{
    "tasks": [
        {
            "id": "searchApples",
            "description": "Search for information about apples",
            "tool_name": "search",
            "args": ["apples"],
            "depends_on": []
        },
        {
            "id": "calculateExpr",
            "description": "Calculate the expression 5*9",
            "tool_name": "calculate",
            "args": ["5*9"],
            "depends_on": []
        }
    ]
}

Query: "%s"
JSON Plan:
`,
				input.Query, input.ToolSchema, input.Query,
			)

			// Use Genkit Generate with structured output
			resp, err := genkit.Generate(ctx,
				genkit.WithPrompt(prompt),
				genkit.WithOutputSchema("plan", &dragonscale.ExecutionPlan{}),
				genkit.WithCandidateCount(1),
			)
			if err != nil {
				genkit.Logger(ctx).Error("Planner LLM call failed", "error", err)
				return nil, fmt.Errorf("planner generation failed: %w", err)
			}

			plan, ok := resp.Output.(*dragonscale.ExecutionPlan)
			if !ok || plan == nil {
				// Log the raw output for debugging
				rawOutput, _ := resp.Candidate().Message.MarshalJSON()
				genkit.Logger(ctx).Error("Failed to cast planner output to ExecutionPlan", "raw_output", string(rawOutput))
				return nil, fmt.Errorf("failed to parse execution plan from LLM output")
			}

			genkit.Logger(ctx).Info("Planner flow finished", "task_count", len(plan.Tasks))
			return plan, nil
		},
	)

	// 2. Solver Flow (Placeholder - just combines results)
	solverFlow := genkit.DefineFlow("solverFlow",
		func(ctx context.Context, input *adapters.SolverInput) (string, error) {
			genkit.Logger(ctx).Info("Solver flow starting", "query", input.Query)
			// Simple synthesis for now. Replace with LLM call for complex cases.
			var resultBuilder strings.Builder
			resultBuilder.WriteString(fmt.Sprintf("Based on the query '%s':
", input.Query))
			if input.RetrievedContext != "" {
				resultBuilder.WriteString(fmt.Sprintf("Retrieved Context: %s
", input.RetrievedContext))
			}
			resultBuilder.WriteString("Execution Results:
")
			for key, val := range input.ExecutionResults {
				resultBuilder.WriteString(fmt.Sprintf("- %s: %v
", key, val))
			}
			finalAnswer := resultBuilder.String()
			genkit.Logger(ctx).Info("Solver flow finished")
			return finalAnswer, nil
		},
	)

	// 3. Retriever Flow (Placeholder - returns empty string)
	retrieverFlow := genkit.DefineFlow("retrieverFlow",
		func(ctx context.Context, query *string) (string, error) {
			genkit.Logger(ctx).Info("Retriever flow starting (placeholder)", "query", *query)
			// In a real scenario, this would fetch relevant data
			return "", nil // Returning empty context for now
		},
	)

	// --- Instantiate Adapters ---
	plannerAdapter := adapters.NewGenkitPlannerAdapter(plannerFlow, memCache)
	solverAdapter := adapters.NewGenkitSolverAdapter(solverFlow)
	retrieverAdapter := adapters.NewGenkitRetrieverAdapter(retrieverFlow) // Optional

	// --- Define Main Orchestrator Flow ---
	_ = genkit.DefineFlow("dragonscaleGenkitFlow",
		func(ctx context.Context, query string) (string, error) {
			genkit.Logger(ctx).Info("Dragonscale Genkit Flow starting", "query", query)

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
				genkit.Logger(ctx).Warn("Context retrieval failed", "error", err)
				retrievedContext = "" // Continue without context on failure
			}

			// 5. Synthesize Final Answer (using Solver Adapter)
			finalAnswer, err := solverAdapter.Synthesize(ctx, query, executionResults, retrievedContext)
			if err != nil {
				return "", fmt.Errorf("failed to synthesize final answer: %w", err)
			}

			genkit.Logger(ctx).Info("Dragonscale Genkit Flow finished successfully")
			return finalAnswer, nil
		},
	)

	log.Println("Genkit initialized successfully. Dragonscale flows defined.")
	log.Println("To run: genkit flow run dragonscaleGenkitFlow '{"data": "Your query here"}'")
	// Keep the application running (e.g., for local testing with genkit start)
	select {}
}
