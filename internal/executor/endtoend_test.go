package executor

import (
	"context"
	"os"
	"testing"

	"github.com/ZanzyTHEbar/dragonscale-genkit"
)

type mockTool struct {
	name     string
	execFunc func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error)
}

func (m *mockTool) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	return m.execFunc(ctx, input)
}
func (m *mockTool) Schema() map[string]interface{} {
	return map[string]interface{}{"description": "mock"}
}
func (m *mockTool) Validate(input map[string]interface{}) error { return nil }
func (m *mockTool) Name() string                                { return m.name }

func TestEndToEnd_YAMLToResult_Success(t *testing.T) {
	dagYAML := `
tasks:
  - id: a
    tool: noop
    args:
      x: 1
  - id: b
    tool: noop
    args:
      y: $a.x
    dependsOn: [a]
`
	tmpFile, err := os.CreateTemp("", "dag-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(dagYAML)); err != nil {
		t.Fatalf("failed to write dag yaml: %v", err)
	}
	tmpFile.Close()

	exec := &DAGExecutor{
		toolRegistry: map[string]dragonscale.Tool{
			"noop": &mockTool{name: "noop", execFunc: func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
				return map[string]interface{}{"x": 1, "y": 2}, nil
			}},
		},
		maxWorkers: 2,
	}
	plan, err := LoadAndValidateDAG(tmpFile.Name())
	if err != nil {
		t.Fatalf("LoadAndValidateDAG failed: %v", err)
	}
	results, err := exec.ExecutePlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("ExecutePlan failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestEndToEnd_YAMLToResult_Error(t *testing.T) {
	dagYAML := `
tasks:
  - id: a
    tool: missingtool
    args:
      x: 1
`
	tmpFile, err := os.CreateTemp("", "dag-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.Write([]byte(dagYAML)); err != nil {
		t.Fatalf("failed to write dag yaml: %v", err)
	}
	tmpFile.Close()

	exec := &DAGExecutor{
		toolRegistry: map[string]dragonscale.Tool{},
		maxWorkers:   1,
	}
	plan, err := LoadAndValidateDAG(tmpFile.Name())
	if err != nil {
		t.Fatalf("LoadAndValidateDAG failed: %v", err)
	}
	_, err = exec.ExecutePlan(context.Background(), plan)
	if err == nil {
		t.Error("expected error for missing tool, got nil")
	}
}
