package tools

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"
)

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
