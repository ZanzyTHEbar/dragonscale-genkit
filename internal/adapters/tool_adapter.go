package adapters

import (
	"context"
	"fmt"
)

// GoToolAdapter adapts a standard Go function to the dragonscale.Tool interface.
type GoToolAdapter struct {
	toolFunc func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error)
	schema   map[string]interface{}
}

// NewGoToolAdapter creates a new adapter for a Go function.
func NewGoToolAdapter(
	toolFunc func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error),
	schema map[string]interface{}) *GoToolAdapter {
	if schema == nil {
		schema = make(map[string]interface{}) // Ensure schema is not nil
	}
	return &GoToolAdapter{
		toolFunc: toolFunc,
		schema:   schema,
	}
}

// Execute implements the dragonscale.Tool interface.
func (a *GoToolAdapter) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	if a.toolFunc == nil {
		return nil, fmt.Errorf("tool function is nil")
	}
	return a.toolFunc(ctx, input)
}

// Schema implements the dragonscale.Tool interface.
func (a *GoToolAdapter) Schema() map[string]interface{} {
	return a.schema
}
