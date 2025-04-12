package tools

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/ZanzyTHEbar/dragonscale-genkit"
	"github.com/ZanzyTHEbar/dragonscale-genkit/internal/adapters"
)

// SetupTools creates and returns a map of all available tools.
func SetupTools() map[string]dragonscale.Tool {
	return map[string]dragonscale.Tool{
		"search": adapters.NewGoToolAdapter(
			"search",
			PerformSearch,
			adapters.WithDescription("Performs a web search for a given query."),
			adapters.WithCategory("Web"),
			adapters.WithParameters(map[string]string{
				"arg0": "Search query string",
			}),
			adapters.WithReturns("Search results as a string."),
			adapters.WithExamples([]string{
				"search \"golang concurrency patterns\"",
				"search \"weather in New York\"",
			}),
			adapters.WithValidator(validateSearchInput),
		),
		"calculate": adapters.NewGoToolAdapter(
			"calculate",
			PerformCalculation,
			adapters.WithDescription("Calculates a mathematical expression."),
			adapters.WithCategory("Math"),
			adapters.WithParameters(map[string]string{
				"arg0": "Mathematical expression to evaluate (e.g., '5*9')",
			}),
			adapters.WithReturns("Calculation result as a float."),
			adapters.WithExamples([]string{
				"calculate \"5*9\"",
				"calculate \"1+1\"",
			}),
			adapters.WithValidator(validateCalculationInput),
		),
	}
}

// PerformSearch simulates a web search.
// It expects an argument named "arg0" containing the query string.
func PerformSearch(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	query, ok := input["arg0"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid or missing query argument (expected string at key 'arg0')")
	}
	log.Printf("TOOL: Searching for '%s'...", query)

	// Simulate network delay
	time.Sleep(time.Duration(500+rand.Intn(500)) * time.Millisecond)

	// Simulate search results
	result := fmt.Sprintf("Simulated search results for query: %s", query)

	// Tools should return a map, conventionally with an "output" key
	output := make(map[string]interface{})
	output["output"] = result
	return output, nil
}

// PerformCalculation simulates a calculation.
// It expects an argument named "arg0" containing the expression string.
func PerformCalculation(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	expression, ok := input["arg0"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid or missing expression argument (expected string at key 'arg0')")
	}
	log.Printf("TOOL: Calculating '%s'...", expression)

	// Simulate calculation time
	time.Sleep(time.Duration(100+rand.Intn(200)) * time.Millisecond)

	// Simulate calculation result (very basic)
	var resultValue float64
	switch expression {
	case "5*9":
		resultValue = 45
	case "1+1":
		resultValue = 2
	case "1/0":
		return nil, fmt.Errorf("division by zero error")
	default:
		resultValue = 123.45 // Default placeholder
	}

	result := fmt.Sprintf("Calculation result: %.2f", resultValue)

	output := make(map[string]interface{})
	output["output"] = result
	return output, nil
}

// Validator functions for tools

// validateSearchInput validates the input for the search tool.
func validateSearchInput(input map[string]interface{}) error {
	query, ok := input["arg0"]
	if !ok {
		return fmt.Errorf("missing search query (expected at key 'arg0')")
	}

	queryStr, ok := query.(string)
	if !ok {
		return fmt.Errorf("search query must be a string, got %T", query)
	}

	if len(queryStr) == 0 {
		return fmt.Errorf("search query cannot be empty")
	}

	if len(queryStr) > 1000 {
		return fmt.Errorf("search query too long (max 1000 characters)")
	}

	return nil
}

// validateCalculationInput validates the input for the calculation tool.
func validateCalculationInput(input map[string]interface{}) error {
	expr, ok := input["arg0"]
	if !ok {
		return fmt.Errorf("missing expression (expected at key 'arg0')")
	}

	exprStr, ok := expr.(string)
	if !ok {
		return fmt.Errorf("expression must be a string, got %T", expr)
	}

	if len(exprStr) == 0 {
		return fmt.Errorf("expression cannot be empty")
	}

	if len(exprStr) > 100 {
		return fmt.Errorf("expression too long (max 100 characters)")
	}

	// Could add more sophisticated validation for valid expressions here

	return nil
}
