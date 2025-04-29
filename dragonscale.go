// Package dragonscale provides the core runtime for AI-powered workflow automation.
package dragonscale

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ZanzyTHEbar/dragonscale-genkit/internal/eventbus"
	"github.com/firebase/genkit/go/genkit"
	"github.com/google/uuid"
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
	eventBus  eventbus.EventBus

	// Available tools
	tools map[string]Tool

	// Configuration
	config Config
	
	// Async processing
	asyncExecutions     map[string]*ProcessContext
	asyncExecutionsMutex sync.RWMutex
}

// DragonScaleComponents holds references to the core components needed for state transitions.
type DragonScaleComponents struct {
	Planner    Planner
	Executor   Executor
	Retriever  Retriever
	Solver     Solver
	Tools      map[string]Tool
	Config     Config
	
	// Function to retrieve tool schemas
	GetSchemas func() map[string]map[string]interface{}
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
	
	// Event bus configuration
	EnableEventBus bool
	EventBusBufferSize int
	EventBusWorkerCount int
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
		EnableEventBus:          true,
		EventBusBufferSize:      100,
		EventBusWorkerCount:     5,
	}
}

// Option is a function that configures a DragonScale instance.
type Option func(*DragonScale)

// WithConfig sets the configuration for
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
		// Use NewConfigurationError for validation failures during initialization
		return nil, NewConfigurationError("initialization", "genkit instance is required", nil)
	}

	// Create with default configuration
	ds := &DragonScale{
		config:           DefaultConfig(),
		tools:            make(map[string]Tool),
		asyncExecutions:  make(map[string]*ProcessContext),
	}

	// Apply options
	for _, option := range options {
		option(ds)
	}

	// Validate the configuration
	if err := ds.validateConfiguration(); err != nil {
		// Wrap validation error using NewConfigurationError
		// The error from validateConfiguration is already a DragonScaleError (or ConfigurationError)
		// so we can return it directly.
		return nil, err
	}

	// Initialize event bus if enabled but not provided
	if ds.config.EnableEventBus && ds.eventBus == nil {
		// Create a default channel-based event bus
		ds.eventBus = eventbus.NewChannelEventBus(
			eventbus.WithBufferSize(ds.config.EventBusBufferSize),
			eventbus.WithWorkerCount(ds.config.EventBusWorkerCount),
		)
		log.Printf("Initialized default channel-based event bus")
	}

	return ds, nil
}

// validateConfiguration checks if the essential components are configured.
// Returns a DragonScaleError if validation fails.
func (d *DragonScale) validateConfiguration() error { // Return type is now error
	if d.planner == nil {
		return NewConfigurationError("validation", "planner is required", nil)
	}
	if d.executor == nil {
		return NewConfigurationError("validation", "executor is required", nil)
	}
	if d.solver == nil {
		return NewConfigurationError("validation", "solver is required", nil)
	}
	if d.cache == nil {
		return NewConfigurationError("validation", "cache is required", nil)
	}
	if len(d.tools) == 0 {
		return NewConfigurationError("validation", "at least one tool is required", nil)
	}
	if d.config.MaxConcurrentExecutions <= 0 {
		return NewConfigurationError("validation", "MaxConcurrentExecutions must be positive", nil)
	}
	if d.config.MaxRetries < 0 {
		return NewConfigurationError("validation", "MaxRetries cannot be negative", nil)
	}
	if d.config.RetryDelay < 0 {
		return NewConfigurationError("validation", "RetryDelay cannot be negative", nil)
	}
	if d.config.ExecutionTimeout <= 0 {
		return NewConfigurationError("validation", "ExecutionTimeout must be positive", nil)
	}
	return nil
}

// RegisterTool adds a new tool to the DragonScale runtime.
func (d *DragonScale) RegisterTool(name string, tool Tool) error {
	if _, exists := d.tools[name]; exists {
		// Use NewConfigurationError for registration issues
		return NewConfigurationError("registration", fmt.Sprintf("tool with name '%s' already exists", name), nil)
	}

	d.tools[name] = tool
	return nil
}

// GetToolSchemas returns a map of tool names to their full schemas,
// suitable for use in planner prompts.
func (d *DragonScale) GetToolSchemas() map[string]map[string]interface{} { // Updated return type
	schemas := make(map[string]map[string]interface{}) // Updated type

	for name, tool := range d.tools {
		schemas[name] = tool.Schema() // Get the full schema map
	}

	return schemas
}

// Process handles an end-to-end query execution through the DragonScale runtime
// using a pushdown automaton state machine approach (State Machine with a stack).
func (d *DragonScale) Process(ctx context.Context, query string) (string, error) {
	// Create a state machine for processing
	stateMachine := d.createStateMachine()
	
	// Create an initial process context with the query
	processContext := NewProcessContext(query)
	
	// Execute the state machine until completion or error
	return stateMachine.Execute(ctx, processContext)
}

// createStateMachine builds a state machine with all necessary transitions
// for the DragonScale processing workflow.
func (d *DragonScale) createStateMachine() *StateMachine {
	// Determine if event bus should be used
	var eventBus eventbus.EventBus
	if d.config.EnableEventBus {
		eventBus = d.eventBus
	}
	
	// Build components structure to pass to state machine
	components := DragonScaleComponents{
		Planner:   d.planner,
		Executor:  d.executor,
		Retriever: d.retriever,
		Solver:    d.solver,
		Tools:     make(map[string]Tool),
		// Pass the config to the state machine
		Config:    d.config,
		GetSchemas: func() map[string]map[string]interface{} { // Updated return type
			return d.GetToolSchemas() // Call the updated method
		},
	}
	
	// Add tools
	for name, tool := range d.tools {
		components.Tools[name] = tool
	}
	
	// Create and return the state machine
	return CreateProcessStateMachine(components, eventBus)
}

// ProcessAsync starts an asynchronous query execution.
// It returns a unique execution ID that can be used to check the status or get the result.
// The provided context's cancellation signal will be propagated to the async execution.
func (d *DragonScale) ProcessAsync(ctx context.Context, query string) (string, error) {
	// Generate a unique execution ID
	executionID := uuid.New().String()

	// Create a state machine for processing
	stateMachine := d.createStateMachine()

	// Create an initial process context with the query
	processContext := NewProcessContext(query)

	// Store the process context in our map
	d.asyncExecutionsMutex.Lock()
	d.asyncExecutions[executionID] = processContext
	d.asyncExecutionsMutex.Unlock()

	// Create a new background context derived from the parent context (ctx)
	// This ensures cancellation of the parent context propagates down.
	asyncCtx, cancel := context.WithCancel(ctx) // Changed from context.Background()

	// Store the cancel function in the state data for potential explicit cancellation via API later
	processContext.StateData["cancel"] = cancel
	// Store execution ID for logging/debugging within transitions/state machine
	processContext.StateData["execution_id"] = executionID

	// Check if event bus is available
	if d.config.EnableEventBus && d.eventBus != nil {
		// Publish event for async processing started
		startEvent := eventbus.NewEvent(
			eventbus.EventQueryAsyncProcessingStarted,
			query,
			"DragonScale.ProcessAsync",
			map[string]interface{}{
				"timestamp":    time.Now().Format(time.RFC3339),
				"execution_id": executionID,
			},
		)
		// Use the original context (ctx) for publishing the initial event
		d.eventBus.Publish(ctx, startEvent)
	}

	// Start a goroutine to execute the state machine using the derived context (asyncCtx)
	go func() {
		defer cancel() // Ensure context is cancelled when goroutine exits

		// Execute the state machine with the derived context
		result, err := stateMachine.Execute(asyncCtx, processContext)

		// Update the process context with the final result
		d.asyncExecutionsMutex.Lock()
		if pCtx, exists := d.asyncExecutions[executionID]; exists {
			pCtx.FinalAnswer = result
			if err != nil {
				currentStage := string(pCtx.CurrentState) // Get stage before potentially changing it
				// Check if the error was due to context cancellation
				if err == context.Canceled || err == context.DeadlineExceeded {
					pCtx.SetCancelled(err, currentStage) // Use SetCancelled method
				} else {
					// If the state machine didn't already set the error state,
					// set it here. The state machine Execute should handle this,
					// but this is a safeguard.
					if !pCtx.IsTerminal() {
						pCtx.SetError(err, currentStage)
					}
				}
			} else {
				// Only mark as complete if no error occurred and not already terminal
				if !pCtx.IsTerminal() {
					pCtx.Complete()
				}
			}
		}
		d.asyncExecutionsMutex.Unlock()

		// Publish completion event if event bus is available
		if d.config.EnableEventBus && d.eventBus != nil {
			eventType := eventbus.EventQueryAsyncProcessingSuccess
			metadata := map[string]interface{}{
				"execution_id": executionID,
				"duration_ms":  processContext.GetTotalDuration().Milliseconds(), // Use processContext directly
			}

			if err != nil {
				eventType = eventbus.EventQueryAsyncProcessingFailure
				metadata["error"] = err.Error()
				metadata["error_stage"] = processContext.ErrorStage // Use processContext directly
				// Add context cancellation info if applicable
				if err == context.Canceled {
					metadata["reason"] = "Context Canceled"
				} else if err == context.DeadlineExceeded {
					metadata["reason"] = "Context Deadline Exceeded"
				}
			}

			completionEvent := eventbus.NewEvent(
				eventType,
				query,
				"DragonScale.ProcessAsync",
				metadata,
			)
			// Use background context for publishing completion event,
			// as the original context might be done.
			d.eventBus.Publish(context.Background(), completionEvent)
		}
	}()

	return executionID, nil
}

// GetToolByName returns a tool by its name, or an error if not found.
func (d *DragonScale) GetToolByName(name string) (Tool, error) {
	if tool, exists := d.tools[name]; exists {
		return tool, nil
	}
	// Use NewToolNotFoundError
	return nil, NewToolNotFoundError("lookup", name)
}

// ListTools returns a list of all registered tool names.
func (d *DragonScale) ListTools() []string {
	names := make([]string, 0, len(d.tools))
	for name := range d.tools {
		names = append(names, name)
	}
	return names
}
