package prompt

import (
	"context"
	"fmt"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
)

// Registry manages the loading and execution of Genkit prompts.
type Registry struct {
	genkitInstance *genkit.Genkit
	// TODO: We could add caching here later if needed
}

// NewRegistry initializes the Genkit environment and creates a prompt registry.
// It takes Genkit initialization options, such as plugin configurations and the prompt directory.
// Note: Ensure your main package passes appropriate options, e.g., ai.WithPlugins(...), ai.WithPromptDir(...)
func NewRegistry(ctx context.Context, opts ...genkit.GenkitOption) (*Registry, error) { // Corrected type to genkit.GenkitOption
	// Ensure ai.WithPromptDir is included if a custom dir is needed, or rely on default "prompts/"
	g, err := genkit.Init(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Genkit: %w", err)
	}

	return &Registry{
		genkitInstance: g,
	}, nil
}

// GetPrompt retrieves a loaded prompt by its name using Genkit's lookup.
func (r *Registry) GetPrompt(name string) (*ai.Prompt, error) {
	p := genkit.LookupPrompt(r.genkitInstance, name)
	if p == nil {
		return nil, fmt.Errorf("prompt '%s' not found", name)
	}
	return p, nil
}

// ExecutePrompt retrieves a prompt by name, renders it with the given input,
// and executes it using the Genkit instance.
// It returns the generated response from the underlying model.
func (r *Registry) ExecutePrompt(ctx context.Context, promptName string, input map[string]interface{}, execOpts ...ai.PromptExecuteOption) (*ai.ModelResponse, error) { // Corrected return type to *ai.ModelResponse
	p, err := r.GetPrompt(promptName)
	if err != nil {
		return nil, err // Error already indicates prompt name
	}

	// Add the input data to the execution options
	allOpts := append([]ai.PromptExecuteOption{ai.WithInput(input)}, execOpts...)

	resp, err := p.Execute(ctx, allOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute prompt '%s': %w", promptName, err)
	}

	return resp, nil
}

// RenderPrompt retrieves a prompt by name and renders it into GenerateActionOptions
// using the provided input. This allows for inspection or modification before execution.
// Note: Based on Genkit examples, Render seems to primarily use the input option.
// Other options passed via renderOpts might be ignored by the standard Render method.
func (r *Registry) RenderPrompt(ctx context.Context, promptName string, input map[string]interface{}, renderOpts ...ai.PromptExecuteOption) (*ai.GenerateActionOptions, error) {
	p, err := r.GetPrompt(promptName)
	if err != nil {
		return nil, err
	}

	// Create the primary input option
	inputOpt := ai.WithInput(input)

	// Call Render with context and the input option, as suggested by Genkit examples.
	// If other options from renderOpts are needed here, the approach might require
	// manually applying them to the resulting actionOpts or using a different Genkit mechanism.
	actionOpts, err := p.Render(ctx, inputOpt)
	if err != nil {
		return nil, fmt.Errorf("failed to render prompt '%s': %w", promptName, err)
	}

	// TODO: Potentially apply other relevant options from renderOpts to actionOpts here if needed.

	return actionOpts, nil
}

// DefinePrompt allows defining prompts programmatically via the registry.
func (r *Registry) DefinePrompt(name string, opts ...ai.PromptOption) (*ai.Prompt, error) {
	p, err := genkit.DefinePrompt(r.genkitInstance, name, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to define prompt '%s': %w", name, err)
	}
	return p, nil
}

// DefinePartial allows defining partials programmatically via the registry.
func (r *Registry) DefinePartial(name, template string) error {
	err := genkit.DefinePartial(r.genkitInstance, name, template)
	if err != nil {
		return fmt.Errorf("failed to define partial '%s': %w", name, err)
	}
	return nil
}

// DefineHelper allows defining custom Handlebars helpers via the registry.
func (r *Registry) DefineHelper(name string, helperFunc interface{}) error {
	err := genkit.DefineHelper(r.genkitInstance, name, helperFunc)
	if err != nil {
		return fmt.Errorf("failed to define helper '%s': %w", name, err)
	}
	return nil
}
