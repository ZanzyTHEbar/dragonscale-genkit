package dragonscale

import "context"

// Planner is responsible for generating an execution plan (DAG) from user input.
type Planner interface {
	GeneratePlan(ctx context.Context, input PlannerInput) (*ExecutionPlan, error)
}

// Tool represents an executable action that can be part of a plan.
type Tool interface {
	// Execute performs the tool's action.
	// input contains resolved arguments based on the task definition and dependencies.
	Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error)
	// Schema returns a description or definition of the tool, used by the Planner.
	Schema() map[string]interface{}
}

// Retriever fetches additional context relevant to the query or plan.
type Retriever interface {
	RetrieveContext(ctx context.Context, query string, dag *ExecutionPlan) (string, error)
}

// Solver synthesizes the final response from execution results and retrieved context.
type Solver interface {
	Synthesize(ctx context.Context, query string, executionResults map[string]interface{}, retrievedContext string) (string, error)
}

// Cache provides storage for frequently accessed data, like generated plans.
type Cache interface {
	Get(ctx context.Context, key string) (interface{}, bool)
	Set(ctx context.Context, key string, value interface{})
}
