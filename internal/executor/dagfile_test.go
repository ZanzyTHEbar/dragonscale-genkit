package executor

import (
	"testing"
)

func TestDAGFile_Validate_TableDriven(t *testing.T) {
	tests := []struct {
		name    string
		dag     DAGFile
		wantErr bool
	}{
		{
			"valid dag",
			DAGFile{
				Tasks: []DAGTask{
					{ID: "a", DependsOn: []string{}},
					{ID: "b", DependsOn: []string{"a"}},
				},
			},
			false,
		},
		{
			"duplicate id",
			DAGFile{
				Tasks: []DAGTask{
					{ID: "a"}, {ID: "a"},
				},
			},
			true,
		},
		{
			"missing dependency",
			DAGFile{
				Tasks: []DAGTask{
					{ID: "a", DependsOn: []string{"b"}},
				},
			},
			true,
		},
		{
			"cycle",
			DAGFile{
				Tasks: []DAGTask{
					{ID: "a", DependsOn: []string{"b"}},
					{ID: "b", DependsOn: []string{"a"}},
				},
			},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.dag.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDAGFile_ToExecutionPlan_Basic(t *testing.T) {
	dag := DAGFile{
		Tasks: []DAGTask{
			{ID: "a", Tool: "noop", Args: map[string]interface{}{"x": 1}},
			{ID: "b", Tool: "noop", Args: map[string]interface{}{"y": "$a.output"}, DependsOn: []string{"a"}},
		},
	}
	plan := dag.ToExecutionPlan()
	if len(plan.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(plan.Tasks))
	}
	if plan.Tasks[0].ID != "a" || plan.Tasks[1].ID != "b" {
		t.Errorf("unexpected task IDs: %+v", plan.Tasks)
	}
	if plan.Tasks[1].Args["y"].Type != "dependencyOutput" {
		t.Errorf("expected dependencyOutput for y, got %v", plan.Tasks[1].Args["y"].Type)
	}
}

func TestDAGFile_ToExecutionPlan_ExpressionArg(t *testing.T) {
	dag := DAGFile{
		Tasks: []DAGTask{
			{ID: "a", Tool: "noop", Args: map[string]interface{}{"x": 1}},
			{ID: "b", Tool: "noop", Args: map[string]interface{}{"y": "$a.output + 2"}, DependsOn: []string{"a"}},
		},
	}
	plan := dag.ToExecutionPlan()
	if plan.Tasks[1].Args["y"].Type != "literal" {
		t.Errorf("expected literal for y (expressions currently treated as literal), got %v", plan.Tasks[1].Args["y"].Type)
	}
	if plan.Tasks[1].Args["y"].Value != "$a.output + 2" {
		t.Errorf("expected value to be the expression string, got %v", plan.Tasks[1].Args["y"].Value)
	}
}
