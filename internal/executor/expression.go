package executor

import "github.com/Knetic/govaluate"

// LoadAndValidateDAG loads and validates a DAG file, returning an ExecutionPlan (does not execute).
// Use this to inspect or modify the plan before running.
// Example:
//   plan, err := executor.LoadAndValidateDAG("mydag.yaml")
//   if err != nil { ... }
//   results, err := exec.ExecutePlan(ctx, plan)

////////////////////////////////////////////////////////////////////////////////
// Minimal Expression Evaluator for ArgumentSourceExpression
////////////////////////////////////////////////////////////////////////////////

// ExpressionFunctionRegistry allows registration of custom functions for expression evaluation.
type ExpressionFunctionRegistry struct {
	functions map[string]govaluate.ExpressionFunction
}

var globalExprFuncRegistry = &ExpressionFunctionRegistry{functions: make(map[string]govaluate.ExpressionFunction)}

// RegisterExpressionFunction allows users to register a custom function for expressions.
func RegisterExpressionFunction(name string, fn govaluate.ExpressionFunction) {
	globalExprFuncRegistry.functions[name] = fn
}

// getWhitelistedFunctions returns only whitelisted functions for security.
func getWhitelistedFunctions() map[string]govaluate.ExpressionFunction {
	// TODO: Add built-in safe functions here, e.g., math, string ops, etc.
	whitelist := map[string]govaluate.ExpressionFunction{}
	for k, v := range globalExprFuncRegistry.functions {
		whitelist[k] = v
	}
	return whitelist
}

// ValidateExpression checks if an expression is valid at DAG load time.
func ValidateExpression(expr string) error {
	_, err := govaluate.NewEvaluableExpressionWithFunctions(expr, getWhitelistedFunctions())
	return err
}
