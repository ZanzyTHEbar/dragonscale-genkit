# Prompt Package (`internal/prompt`)

This package provides a centralized registry for managing and interacting with Genkit prompts within the application.
It acts as a higher-level interface over the core Genkit Go SDK's prompt functionalities, ensuring consistent access and execution patterns.

## Core Component: `Registry`

The primary component is the `Registry` struct. It holds an initialized Genkit instance and provides methods to interact with prompts.

### Initialization

A `Registry` instance is created using the `NewRegistry` function:

```go
func NewRegistry(ctx context.Context, opts ...genkit.GenkitOption) (*Registry, error)
```

-   It requires a `context.Context`.
-   It accepts variadic `genkit.GenkitOption` arguments. These are crucial for configuring the underlying Genkit instance.
-   **Important:** When calling `NewRegistry` from your application setup, you **must** pass the necessary options, such as:
    -   `ai.WithPlugins(...)`: To register the AI model plugins (e.g., `googlegenai.GoogleAI{}`).
    -   `ai.WithPromptDir("./path/to/prompts")`: To specify the directory where your `.prompt` files are located (defaults to `./prompts` if not provided).
    -   Other options like tracing or flow state storage configurations.

Example:

```go
import (
    "context"
    "log"

    "github.com/firebase/genkit/go/ai"
    "github.com/firebase/genkit/go/plugins/googleai"
    "path/to/your/internal/prompt"
)

func main() {
    ctx := context.Background()

    // Configure Genkit options
    opts := []genkit.GenkitOption{
        ai.WithPlugins(&googleai.GoogleAI{}),
        ai.WithPromptDir("./myprompts"), // Optional: if not using default "prompts/"
    }

    promptRegistry, err := prompt.NewRegistry(ctx, opts...)
    if err != nil {
        log.Fatalf("Failed to initialize prompt registry: %v", err)
    }

    // ... use promptRegistry ...
}
```

### Retrieving Prompts

Use `GetPrompt` to retrieve a prompt that has been loaded by Genkit (either from a `.prompt` file or defined programmatically):

```go
func (r *Registry) GetPrompt(name string) (*ai.Prompt, error)
```

-   It returns a pointer to the `ai.Prompt` object or an error if the prompt with the given `name` is not found.

### Executing Prompts

Use `ExecutePrompt` to render and execute a prompt with specific input data:

```go
func (r *Registry) ExecutePrompt(ctx context.Context, promptName string, input map[string]interface{}, execOpts ...ai.PromptExecuteOption) (*ai.ModelResponse, error) // Corrected return type
```

-   It looks up the prompt by `promptName`.
-   It takes `input` data (a map) for variable substitution in the template.
-   It accepts optional `ai.PromptExecuteOption` arguments (e.g., `ai.WithTemperature(0.8)`).
-   It returns the `*ai.ModelResponse` from the model or an error.

### Rendering Prompts

Use `RenderPrompt` if you need to see the rendered prompt content *before* execution, perhaps for logging, modification, or passing to a different system component:

```go
func (r *Registry) RenderPrompt(ctx context.Context, promptName string, input map[string]interface{}, renderOpts ...ai.PromptExecuteOption) (*ai.GenerateActionOptions, error)
```

-   It looks up the prompt by `promptName`.
-   It takes `input` data.
-   It currently primarily uses the `ai.WithInput` option for rendering, as per Genkit examples. Other `renderOpts` might be ignored by the standard `Render` method.
-   It returns `*ai.GenerateActionOptions`, which contains the rendered messages, model name, config, etc.

### Programmatic Definitions

The `Registry` also provides wrappers around Genkit's functions for defining prompts, partials, and helpers directly in code:

-   `DefinePrompt(name string, opts ...ai.PromptOption) (*ai.Prompt, error)`
-   `DefinePartial(name, template string) error`
-   `DefineHelper(name string, helperFunc interface{}) error`

These are useful if you need to create prompts dynamically rather than solely relying on `.prompt` files.

## Integration with Genkit

This package is designed to work seamlessly with the Genkit Go SDK. It leverages Genkit for:

-   Loading and parsing `.prompt` files (including YAML front matter and Handlebars templates).
-   Managing partials (`_*.prompt` files or defined via `DefinePartial`).
-   Handling Handlebars rendering, including variable substitution and partial expansion.
-   Executing the rendered prompt against the configured AI models via registered plugins.

By using this `Registry`, you get a consistent interface while relying on the robust underlying mechanisms provided by Genkit.
