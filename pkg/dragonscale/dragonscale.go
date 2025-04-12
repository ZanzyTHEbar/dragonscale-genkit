// Package dragonscale provides the core runtime for AI-powered workflow automation.
package dragonscale

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/firebase/genkit/go/genkit"
)

// DragonScale is the main entry point into the dragonscale-genkit runtime.
// It encapsulates all components required for executing AI workflows.
type DragonScale struct {
	// Core components
	planner   Planner
	executor  Executor
	retriever Retriever
	solver    Solver
	cache     Cache

	// Available tools
	tools map[string]Tool

	// Configuration
	config Config
}

// Config holds the configuration options for the DragonScale runtime.
type Config struct {
	// Maximum number of concurrent tool executions
	MaxConcurrentExecutions int

	// Retry configuration
	MaxRetries int
	RetryDelay time.Duration

	// Execution timeout
	ExecutionTimeout time.Duration

	// Enable/disable context retrieval
	EnableRetrieval bool
}

// Executor interface for running execution plans
type Executor interface {
	ExecutePlan(ctx context.Context, plan *ExecutionPlan) (map[string]interface{}, error)
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxConcurrentExecutions: 5,
		MaxRetries:              3,
		RetryDelay:              time.Second * 2,
		ExecutionTimeout:        time.Minute * 5,
		EnableRetrieval:         true,
	}
}

// Option is a function that configures a DragonScale instance.
type Option func(*DragonScale)

// WithConfig sets the configuration for DragonScale.
func WithConfig(config Config) Option {
	return func(d *DragonScale) {
		d.config = config
	}
}

// WithPlanner sets the planner component.
func WithPlanner(planner Planner) Option {
	return func(d *DragonScale) {
		d.planner = planner
	}
}

// WithExecutor sets the executor component.
func WithExecutor(executor Executor) Option {
	return func(d *DragonScale) {
		d.executor = executor
	}
}

// WithRetriever sets the retriever component.
func WithRetriever(retriever Retriever) Option {
	return func(d *DragonScale) {
		d.retriever = retriever
	}
}

// WithSolver sets the solver component.
func WithSolver(solver Solver) Option {
	return func(d *DragonScale) {
		d.solver = solver
	}
}

// WithCache sets the cache component.
func WithCache(cache Cache) Option {
	return func(d *DragonScale) {
		d.cache = cache
	}
}

// WithTools adds tools to the runtime.
func WithTools(tools map[string]Tool) Option {
	return func(d *DragonScale) {
		if d.tools == nil {
			d.tools = make(map[string]Tool)
		}

		for name, tool := range tools {
			d.tools[name] = tool
		}
	}
}

// New creates a new DragonScale instance with the provided options.
func New(ctx context.Context, g *genkit.Genkit, options ...Option) (*DragonScale, error) {
	if g == nil {
		return nil, fmt.Errorf("genkit instance is required")
	}

	// Create with default configuration
	ds := &DragonScale{
		config: DefaultConfig(),
		tools:  make(map[string]Tool),
	}

	// Apply options
	for _, option := range options {
		option(ds)
	}

	// Validate required components
	if ds.planner == nil {
		return nil, fmt.Errorf("planner is required")
	}

	if ds.executor == nil {
		return nil, fmt.Errorf("executor is required")
	}

	if ds.solver == nil {
		return nil, fmt.Errorf("solver is required")
	}

	if ds.cache == nil {
		return nil, fmt.Errorf("cache is required")
	}

	if len(ds.tools) == 0 {
		return nil, fmt.Errorf("at least one tool is required")
	}

	return ds, nil
}

// RegisterTool adds a new tool to the DragonScale runtime.
func (d *DragonScale) RegisterTool(name string, tool Tool) error {
	if _, exists := d.tools[name]; exists {
		return fmt.Errorf("tool with name '%s' already exists", name)
	}

	d.tools[name] = tool
	return nil
}

// GetToolSchemas returns a map of tool names to their descriptions,
// suitable for use in planner prompts.
func (d *DragonScale) GetToolSchemas() map[string]string {
	schemas := make(map[string]string)

	for name, tool := range d.tools {
		schema := tool.Schema()
		if desc, ok := schema["description"].(string); ok {
			schemas[name] = desc
		} else {
			schemas[name] = "No description available."
		}
	}

	return schemas
}

// Process handles an end-to-end query execution through the DragonScale runtime.
func (d *DragonScale) Process(ctx context.Context, query string) (string, error) {
	// 1. Prepare Planner Input
	plannerInput := PlannerInput{
		Query:      query,
		ToolSchema: d.GetToolSchemas(),
	}

	// 2. Generate Execution Plan
	executionPlan, err := d.planner.GeneratePlan(ctx, plannerInput)
	if err != nil {
		return "", fmt.Errorf("failed to generate execution plan: %w", err)
	}

	// 3. Execute the Plan
	executionResults, err := d.executor.ExecutePlan(ctx, executionPlan)
	if err != nil {
		return "", fmt.Errorf("DAG execution failed: %w", err)
	}

	// 4. Retrieve Context (if enabled)
	var retrievedContext string
	if d.config.EnableRetrieval && d.retriever != nil {
		retrievedContext, err = d.retriever.RetrieveContext(ctx, query, executionPlan)
		if err != nil {
			log.Printf("Context retrieval failed: %v", err)
			// Continue without context on failure
		}
	}

	// 5. Synthesize Final Answer
	finalAnswer, err := d.solver.Synthesize(ctx, query, executionResults, retrievedContext)
	if err != nil {
		return "", fmt.Errorf("failed to synthesize final answer: %w", err)
	}

	return finalAnswer, nil
}

// ProcessAsync starts an asynchronous query execution.
// It returns a unique execution ID that can be used to check the status or get the result.
func (d *DragonScale) ProcessAsync(ctx context.Context, query string) (string, error) {
	// This is a placeholder for future implementation
	// In a real implementation, this would:
	// 1. Generate a unique execution ID
	// 2. Start a goroutine to process the query
	// 3. Store intermediate results in a state store
	// 4. Return the execution ID immediately

	return "not-implemented-yet", fmt.Errorf("async processing not implemented yet")
}

// GetToolByName returns a tool by its name, or an error if not found.
func (d *DragonScale) GetToolByName(name string) (Tool, error) {
	if tool, exists := d.tools[name]; exists {
		return tool, nil
	}
	return nil, fmt.Errorf("tool with name '%s' not found", name)
}

// ListTools returns a list of all registered tool names.
func (d *DragonScale) ListTools() []string {
	names := make([]string, 0, len(d.tools))
	for name := range d.tools {
		names = append(names, name)
	}
	return names
}
