package adapters

import (
	"context"
	"fmt"

	"github.com/ZanzyTHEbar/dragonscale-genkit"
)

// GoToolAdapter adapts a standard Go function to the dragonscale.Tool interface.
type GoToolAdapter struct {
	toolFunc    func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error)
	schema      map[string]interface{}
	name        string
	validator   func(map[string]interface{}) error
	description string
	category    string
}

// ToolOption represents an option for configuring a GoToolAdapter.
type ToolOption func(*GoToolAdapter)

// WithValidator sets a custom validator function for the tool.
func WithValidator(validator func(map[string]interface{}) error) ToolOption {
	return func(adapter *GoToolAdapter) {
		adapter.validator = validator
	}
}

// WithCategory sets the tool's category.
func WithCategory(category string) ToolOption {
	return func(adapter *GoToolAdapter) {
		adapter.category = category
		if adapter.schema != nil {
			adapter.schema["category"] = category
		}
	}
}

// WithDescription sets a detailed description for the tool.
func WithDescription(description string) ToolOption {
	return func(adapter *GoToolAdapter) {
		adapter.description = description
		if adapter.schema != nil {
			adapter.schema["description"] = description
		}
	}
}

// WithParameters sets the parameters description in the schema.
func WithParameters(parameters map[string]string) ToolOption {
	return func(adapter *GoToolAdapter) {
		if adapter.schema != nil {
			adapter.schema["parameters"] = parameters
		}
	}
}

// WithReturns sets the return value description in the schema.
func WithReturns(returns string) ToolOption {
	return func(adapter *GoToolAdapter) {
		if adapter.schema != nil {
			adapter.schema["returns"] = returns
		}
	}
}

// WithExamples adds usage examples to the schema.
func WithExamples(examples []string) ToolOption {
	return func(adapter *GoToolAdapter) {
		if adapter.schema != nil {
			adapter.schema["examples"] = examples
		}
	}
}

// NewGoToolAdapter creates a new adapter for a Go function.
func NewGoToolAdapter(
	name string,
	toolFunc func(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error),
	options ...ToolOption) *GoToolAdapter {

	schema := map[string]interface{}{
		"name": name,
	}

	adapter := &GoToolAdapter{
		toolFunc: toolFunc,
		schema:   schema,
		name:     name,
		validator: func(input map[string]interface{}) error {
			// Default validator just ensures input is not nil
			if input == nil {
				// Use ToolValidationError for default validation failure
				return dragonscale.NewToolValidationError(name, "input cannot be nil", nil)
			}
			return nil
		},
	}

	// Apply all options
	for _, option := range options {
		option(adapter)
	}

	return adapter
}

// Execute implements the dragonscale.Tool interface.
func (a *GoToolAdapter) Execute(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	if a.toolFunc == nil {
		// Use ConfigurationError if the underlying function is nil
		return nil, dragonscale.NewConfigurationError("tool:"+a.name, "tool function is nil", nil)
	}

	// Validate input before execution
	if err := a.Validate(input); err != nil {
		// Error from Validate should already be ToolValidationError, just return it
		// If Validate returns a non-DragonScaleError, wrap it.
		if _, ok := err.(*dragonscale.DragonScaleError); !ok {
			err = dragonscale.NewToolValidationError(a.name, fmt.Sprintf("input validation failed: %v", err), err)
		}
		return nil, err
	}

	// Execute the actual tool function
	result, err := a.toolFunc(ctx, input)
	if err != nil {
		// Wrap execution errors in ToolExecutionError
		return nil, dragonscale.NewToolExecutionError(a.name, "tool execution failed", err)
	}
	return result, nil
}

// Schema implements the dragonscale.Tool interface.
func (a *GoToolAdapter) Schema() map[string]interface{} {
	return a.schema
}

// Validate implements the dragonscale.Tool interface.
func (a *GoToolAdapter) Validate(input map[string]interface{}) error {
	if a.validator != nil {
		err := a.validator(input)
		if err != nil {
			// Ensure the error returned by the custom validator is wrapped
			if _, ok := err.(*dragonscale.DragonScaleError); !ok {
				return dragonscale.NewToolValidationError(a.name, fmt.Sprintf("custom validation failed: %v", err), err)
			}
			return err // Return the already wrapped error
		}
	}
	return nil
}

// Name implements the dragonscale.Tool interface.
func (a *GoToolAdapter) Name() string {
	return a.name
}
