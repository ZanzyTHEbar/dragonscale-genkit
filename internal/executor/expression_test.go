package executor

import (
	"testing"

	"github.com/Knetic/govaluate"
)

func TestRegisterExpressionFunction_AndWhitelist(t *testing.T) {
	called := false
	RegisterExpressionFunction("customAdd", func(args ...interface{}) (interface{}, error) {
		called = true
		return args[0].(float64) + args[1].(float64), nil
	})
	funcs := getWhitelistedFunctions()
	if _, ok := funcs["customAdd"]; !ok {
		t.Error("customAdd not found in whitelist")
	}
	// Use the function in an expression
	eval, err := govaluate.NewEvaluableExpressionWithFunctions("customAdd(2, 3)", funcs)
	if err != nil {
		t.Fatalf("failed to parse expression: %v", err)
	}
	res, err := eval.Evaluate(nil)
	if err != nil {
		t.Fatalf("failed to evaluate: %v", err)
	}
	if res != 5.0 {
		t.Errorf("expected 5.0, got %v", res)
	}
	if !called {
		t.Error("custom function was not called")
	}
}

func TestValidateExpression_SuccessAndFailure(t *testing.T) {
	if err := ValidateExpression("1 + 2"); err != nil {
		t.Errorf("expected valid expression, got %v", err)
	}
	if err := ValidateExpression("1 + "); err == nil {
		t.Error("expected error for invalid expression, got nil")
	}
}
