package dragonscale

import (
	"context"
	"errors"
	"testing"
	"time"
)

type dummyPlanner struct{}

func (d *dummyPlanner) GeneratePlan(ctx context.Context, input PlannerInput) (*ExecutionPlan, error) {
	return &ExecutionPlan{Tasks: []Task{{ID: "t1", ToolName: "noop", Args: map[string]ArgumentSource{}}}}, nil
}

type dummyExecutor struct{}

func (d *dummyExecutor) ExecutePlan(ctx context.Context, plan *ExecutionPlan) (map[string]interface{}, error) {
	return map[string]interface{}{plan.Tasks[0].ID: "ok"}, nil
}

type dummyRetriever struct{}

func (d *dummyRetriever) RetrieveContext(ctx context.Context, query string, dag *ExecutionPlan) (string, error) {
	return "context", nil
}

type dummySolver struct{}

func (d *dummySolver) Synthesize(ctx context.Context, query string, executionResults map[string]interface{}, retrievedContext string) (string, error) {
	return "answer", nil
}

type dummyCache struct{}

func (d *dummyCache) Get(ctx context.Context, key string) (interface{}, bool) { return nil, false }
func (d *dummyCache) Set(ctx context.Context, key string, value interface{})  {}

// dummyToolForStateMachine implements the Tool interface for use in the state machine tests.
type dummyToolForStateMachine struct{}

func (d *dummyToolForStateMachine) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"ok": true}, nil
}
func (d *dummyToolForStateMachine) Schema() map[string]interface{} {
	return map[string]interface{}{"description": "dummy"}
}
func (d *dummyToolForStateMachine) Validate(input map[string]interface{}) error { return nil }
func (d *dummyToolForStateMachine) Name() string                                { return "noop" }

func TestStateMachine_Execute_Success(t *testing.T) {
	ds := &DragonScale{
		planner:   &dummyPlanner{},
		executor:  &dummyExecutor{},
		retriever: &dummyRetriever{},
		solver:    &dummySolver{},
		cache:     &dummyCache{},
		tools:     map[string]Tool{"noop": &dummyToolForStateMachine{}},
		config:    Config{MaxConcurrentExecutions: 1, MaxRetries: 1, RetryDelay: time.Millisecond, ExecutionTimeout: time.Second},
	}
	stateMachine := ds.createStateMachine()
	pCtx := NewProcessContext("test query")
	final, err := stateMachine.Execute(context.Background(), pCtx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if final == "" {
		t.Error("expected non-empty final answer")
	}
}

func TestStateMachine_Execute_ErrorTransition(t *testing.T) {
	ds := &DragonScale{
		planner:   &dummyPlanner{},
		executor:  &dummyExecutor{},
		retriever: &dummyRetriever{},
		solver:    &dummySolver{},
		cache:     &dummyCache{},
		tools:     map[string]Tool{"noop": &dummyToolForStateMachine{}},
		config:    Config{MaxConcurrentExecutions: 1, MaxRetries: 1, RetryDelay: time.Millisecond, ExecutionTimeout: time.Second},
	}
	stateMachine := ds.createStateMachine()
	pCtx := NewProcessContext("test query")
	// Simulate error state
	pCtx.SetError(errors.New("fail"), "planning")
	final, err := stateMachine.Execute(context.Background(), pCtx)
	if err == nil {
		t.Error("expected error for error state, got nil")
	}
	if final != "" {
		t.Errorf("expected empty final answer, got %v", final)
	}
}

func TestStateMachine_Execute_Cancellation(t *testing.T) {
	ds := &DragonScale{
		planner:   &dummyPlanner{},
		executor:  &dummyExecutor{},
		retriever: &dummyRetriever{},
		solver:    &dummySolver{},
		cache:     &dummyCache{},
		tools:     map[string]Tool{"noop": &dummyToolForStateMachine{}},
		config:    Config{MaxConcurrentExecutions: 1, MaxRetries: 1, RetryDelay: time.Millisecond, ExecutionTimeout: time.Second},
	}
	stateMachine := ds.createStateMachine()
	pCtx := NewProcessContext("test query")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	final, err := stateMachine.Execute(ctx, pCtx)
	if err == nil {
		t.Error("expected error for cancellation, got nil")
	}
	if final != "" {
		t.Errorf("expected empty final answer, got %v", final)
	}
}
