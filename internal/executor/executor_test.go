package executor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ZanzyTHEbar/dragonscale-genkit"
)

func TestDAGExecutor_ExecutePlan_SuccessAndFailure(t *testing.T) {
	executor := &DAGExecutor{
		toolRegistry: map[string]dragonscale.Tool{
			"success": &mockTool{name: "success", execFunc: func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
				return map[string]interface{}{"out": 1}, nil
			}},
			"fail": &mockTool{name: "fail", execFunc: func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
				return nil, errors.New("fail")
			}},
		},
		maxWorkers: 2,
	}
	plan := dragonscale.NewExecutionPlan([]dragonscale.Task{
		{ID: "t1", ToolName: "success", Args: map[string]dragonscale.ArgumentSource{}},
		{ID: "t2", ToolName: "fail", Args: map[string]dragonscale.ArgumentSource{}, DependsOn: []string{"t1"}},
	})
	results, err := executor.ExecutePlan(context.Background(), plan)
	if err == nil {
		t.Errorf("expected error for failed task, got nil")
	}
	if results != nil {
		t.Errorf("expected nil results on failure, got %v", results)
	}
}

func TestDAGExecutor_ExecutePlan_Retry(t *testing.T) {
	callCount := 0
	executor := &DAGExecutor{
		toolRegistry: map[string]dragonscale.Tool{
			"flaky": &mockTool{name: "flaky", execFunc: func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
				callCount++
				if callCount < 2 {
					return nil, errors.New("fail once")
				}
				return map[string]interface{}{"out": 42}, nil
			}},
		},
		maxWorkers: 1,
		maxRetries: 1,
		retryDelay: 10 * time.Millisecond,
	}
	plan := dragonscale.NewExecutionPlan([]dragonscale.Task{
		{ID: "t1", ToolName: "flaky", Args: map[string]dragonscale.ArgumentSource{}},
	})
	results, err := executor.ExecutePlan(context.Background(), plan)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if results["t1"] == nil {
		t.Errorf("expected result for t1, got nil")
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
}

func TestDAGExecutor_Concurrency_Metrics(t *testing.T) {
	executor := &DAGExecutor{
		toolRegistry: map[string]dragonscale.Tool{
			"sleep": &mockTool{name: "sleep", execFunc: func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
				time.Sleep(30 * time.Millisecond)
				return map[string]interface{}{"ok": true}, nil
			}},
		},
		maxWorkers: 3,
	}
	plan := dragonscale.NewExecutionPlan([]dragonscale.Task{
		{ID: "t1", ToolName: "sleep", Args: map[string]dragonscale.ArgumentSource{}},
		{ID: "t2", ToolName: "sleep", Args: map[string]dragonscale.ArgumentSource{}},
		{ID: "t3", ToolName: "sleep", Args: map[string]dragonscale.ArgumentSource{}},
	})
	start := time.Now()
	results, err := executor.ExecutePlan(context.Background(), plan)
	elapsed := time.Since(start)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
	metrics := executor.GetMetrics()
	if metrics.TasksExecuted != 3 || metrics.TasksSuccessful != 3 {
		t.Errorf("unexpected metrics: %+v", metrics.Copy())
	}
	if elapsed > 60*time.Millisecond {
		t.Errorf("expected concurrent execution, took too long: %v", elapsed)
	}
}

func TestDAGExecutor_ExecutePlan_Cancellation(t *testing.T) {
	executor := &DAGExecutor{
		toolRegistry: map[string]dragonscale.Tool{
			"block": &mockTool{name: "block", execFunc: func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(200 * time.Millisecond):
					return map[string]interface{}{"ok": true}, nil
				}
			}},
		},
		maxWorkers: 1,
	}
	plan := dragonscale.NewExecutionPlan([]dragonscale.Task{
		{ID: "t1", ToolName: "block", Args: map[string]dragonscale.ArgumentSource{}},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	results, err := executor.ExecutePlan(ctx, plan)
	if err == nil {
		t.Error("expected error due to cancellation, got nil")
	}
	if results != nil {
		t.Errorf("expected nil results on cancellation, got %v", results)
	}
}
