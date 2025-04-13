# DragonScale Genkit

[![Go Reference](https://pkg.go.dev/badge/github.com/ZanzyTHEbar/dragonscale-genkit.svg)](https://pkg.go.dev/github.com/ZanzyTHEbar/dragonscale-genkit)
[![Go Report Card](https://goreportcard.com/badge/github.com/ZanzyTHEbar/dragonscale-genkit)](https://goreportcard.com/report/github.com/ZanzyTHEbar/dragonscale-genkit)
[![License](https://img.shields.io/github/license/ZanzyTHEbar/dragonscale-genkit)](https://github.com/ZanzyTHEbar/dragonscale-genkit/blob/main/LICENSE)

> [!IMPORTANT]\
> This project is in early development and may not be stable.
> Please use it at your own risk.

> [!NOTE]\
> This project is not affiliated with or endorsed by Google, Firebase, or any other third-party services.

DragonScale is a modular, extensible workflow automation framework powered by AI. It provides a structured approach to building AI-powered applications that can plan, execute, and solve complex tasks using tools and context retrieval.

## Overview

DragonScale orchestrates AI workflows through a powerful pipeline:

1. **Planning**: AI generates structured execution plans from natural language queries
2. **Execution**: Tools are executed according to the plan in a DAG (Directed Acyclic Graph)
3. **Context Retrieval**: Relevant information is retrieved to enhance the response
4. **Synthesis**: Final answers are crafted based on execution results and context

## Features

- 🔄 **Modular Architecture**: Easily swap components like planners, executors, and tools
- 🛠️ **Extensible Tools**: Create custom tools with simple interfaces
- 🧠 **Context-Aware**: Retrieve relevant information to improve response quality
- 💾 **Caching**: Optimize performance with built-in caching
- 🧩 **Composable**: Build complex workflows from simple components
- 🔌 **Firebase Genkit Integration**: Leverages Google's Genkit for AI capabilities

## Installation

### Prerequisites

- Go 1.24 or later

### Using go get

```bash
go get github.com/ZanzyTHEbar/dragonscale-genkit
```

### Using as a module in your project

```go
import "github.com/ZanzyTHEbar/dragonscale-genkit"
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"
    
    "github.com/ZanzyTHEbar/dragonscale-genkit/internal/adapters"
    "github.com/ZanzyTHEbar/dragonscale-genkit/internal/cache"
    "github.com/ZanzyTHEbar/dragonscale-genkit/internal/executor"
    "github.com/ZanzyTHEbar/dragonscale-genkit/internal/tools"
    "github.com/ZanzyTHEbar/dragonscale-genkit"
    "github.com/firebase/genkit/go/genkit"
)

func main() {
    // Initialize Genkit with your preferred settings
    ctx := context.Background()
    g, err := genkit.Init(ctx)
    if err != nil {
        log.Fatalf("Failed to initialize Genkit: %v", err)
    }
    
    // Create components
    memCache := cache.NewMemoryCache()
    plannerFlow := createPlannerFlow(g)
    plannerAdapter := adapters.NewGenkitPlannerAdapter(plannerFlow, memCache)
    
    solverFlow := createSolverFlow(g)
    solverAdapter := adapters.NewGenkitSolverAdapter(solverFlow)
    
    executorInstance := executor.NewExecutor()
    
    // Define tools
    weatherTool := tools.NewWeatherTool()
    calculatorTool := tools.NewCalculatorTool()
    availableTools := map[string]dragonscale.Tool{
        "weather": weatherTool,
        "calculator": calculatorTool,
    }
    
    // Initialize DragonScale
    ds, err := dragonscale.New(ctx, g,
        dragonscale.WithPlanner(plannerAdapter),
        dragonscale.WithExecutor(executorInstance),
        dragonscale.WithSolver(solverAdapter),
        dragonscale.WithCache(memCache),
        dragonscale.WithTools(availableTools),
    )
    if err != nil {
        log.Fatalf("Failed to initialize DragonScale: %v", err)
    }
    
    // Process a query
    response, err := ds.Process(ctx, "What's the weather in New York and what's 25 + 17?")
    if err != nil {
        log.Fatalf("Query processing failed: %v", err)
    }
    
    fmt.Println("Response:", response)
}

// Helper functions to create flows (implementation depends on your specific setup)
```

## Architecture

DragonScale follows a modular architecture with clearly defined interfaces:

### Core Components

- **Planner**: Converts natural language queries into structured execution plans
- **Executor**: Runs tools according to the execution plan
- **Retriever**: Fetches relevant context to enhance responses
- **Solver**: Synthesizes final answers from execution results and context
- **Cache**: Stores frequently accessed data to improve performance

### Example Flow

```
Query: "What's the weather in Paris and calculate the exchange rate from 100 USD to EUR"

1. Planner: Generate execution plan with two parallel tasks
   - Task 1: Call weather tool with location="Paris"
   - Task 2: Call exchange rate tool with amount=100, from="USD", to="EUR"

2. Executor: Run both tasks in parallel, collecting results

3. Retriever: Get additional context about Paris weather patterns

4. Solver: Synthesize results into a human-friendly response
```

## Detailed Usage

### Setting Up a Custom Configuration

```go
// Configure DragonScale with custom settings
config := dragonscale.Config{
    MaxConcurrentExecutions: 10,
    MaxRetries: 5,
    RetryDelay: time.Second * 3,
    ExecutionTimeout: time.Minute * 10,
    EnableRetrieval: true,
}

ds, err := dragonscale.New(ctx, g,
    dragonscale.WithConfig(config),
    // Other options...
)
```

### Creating Custom Tools

```go
// Define a custom tool that satisfies the dragonscale.Tool interface
type TranslationTool struct{}

func (t *TranslationTool) Execute(ctx context.Context, params map[string]interface{}) (interface{}, error) {
    text, ok := params["text"].(string)
    if !ok {
        return nil, fmt.Errorf("missing required parameter: text")
    }
    
    targetLang, ok := params["target_language"].(string)
    if !ok {
        return nil, fmt.Errorf("missing required parameter: target_language")
    }
    
    // Implement your translation logic here
    translatedText := translateText(text, targetLang)
    
    return map[string]interface{}{
        "translated_text": translatedText,
    }, nil
}

func (t *TranslationTool) Schema() map[string]interface{} {
    return map[string]interface{}{
        "name": "translate",
        "description": "Translates text to a target language",
        "parameters": map[string]interface{}{
            "text": map[string]interface{}{
                "type": "string",
                "description": "Text to translate",
            },
            "target_language": map[string]interface{}{
                "type": "string",
                "description": "Target language code (e.g., 'fr', 'es', 'de')",
            },
        },
        "returns": map[string]interface{}{
            "translated_text": map[string]interface{}{
                "type": "string",
                "description": "Translated text in the target language",
            },
        },
    }
}

// In your main function
translationTool := &TranslationTool{}
ds.RegisterTool("translate", translationTool)
```

### Using Custom Adapters

DragonScale supports custom implementations of its core interfaces. You can create custom adapters that wrap your existing AI services:

```go
// Implement a custom planner that uses your own LLM service
type CustomPlanner struct {
    llmClient *YourLLMClient
}

func (p *CustomPlanner) GeneratePlan(ctx context.Context, input dragonscale.PlannerInput) (*dragonscale.ExecutionPlan, error) {
    // Use your custom LLM client to generate a plan
    prompt := fmt.Sprintf("Plan the execution of the following query: %s\nTool Schemas: %v", 
        input.Query, input.ToolSchema)
    
    response, err := p.llmClient.Generate(ctx, prompt)
    if err != nil {
        return nil, err
    }
    
    // Parse response into an ExecutionPlan
    plan, err := ParseExecutionPlan(response)
    if err != nil {
        return nil, err
    }
    
    return plan, nil
}

// In your main function
customPlanner := &CustomPlanner{llmClient: NewYourLLMClient()}
ds, err := dragonscale.New(ctx, g,
    dragonscale.WithPlanner(customPlanner),
    // Other options...
)
```

## Common Use Cases

### 1. AI Assistants with Tool Use

Build AI assistants that can leverage external tools and APIs to answer complex queries. DragonScale handles:

- **Planning**: Breaking down complex tasks into steps
- **Tool Execution**: Calling the right tools in the right sequence
- **Answer Generation**: Creating coherent responses from tool outputs

### 2. Multi-step Data Processing

Create sophisticated data pipelines that leverage AI for decision making:

- **Data Extraction**: Use tools to extract data from various sources
- **Analysis**: Process data with specialized tools
- **Synthesis**: Generate reports from analysis results

### 3. Automation Workflows

Automate complex business processes:

- **Scheduling**: Plan task execution based on dependencies
- **Integration**: Connect to various services and APIs
- **Monitoring**: Track workflow progress and handle failures

### 4. Knowledge Base Augmentation

Enhance responses with relevant knowledge:

- **Query Analysis**: Understand information needs
- **Context Retrieval**: Fetch relevant documents or data
- **Integrated Responses**: Combine retrieved context with execution results

## Advanced Features

### Error Handling and Retries

DragonScale includes robust error handling and retry mechanisms:

```go
// Configure retry behavior
config := dragonscale.Config{
    MaxRetries: 3,
    RetryDelay: time.Second * 2,
}

// DragonScale automatically retries failed tool executions
```

### Concurrency Control

Control the parallelism of your tool executions:

```go
// Limit concurrent executions
config := dragonscale.Config{
    MaxConcurrentExecutions: 5,
}
```

### Future: Asynchronous Processing

DragonScale plans to support asynchronous processing for long-running tasks:

```go
// Start an asynchronous process (future feature)
executionID, err := ds.ProcessAsync(ctx, "Generate a comprehensive report on Q1 sales")
if err != nil {
    log.Fatalf("Failed to start async processing: %v", err)
}

// Later, check the status or get results using the execution ID
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Acknowledgments

- Firebase Genkit for providing the foundational AI capabilities
