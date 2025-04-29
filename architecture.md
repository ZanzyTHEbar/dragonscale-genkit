# DragonScale Genkit Architecture

This document provides a visual representation of the DragonScale Genkit architecture using UML diagrams.

## System Architecture Overview

DragonScale Genkit is a modular, extensible workflow automation framework powered by AI. It provides a structured approach to building AI-powered applications that can plan, execute, and solve complex tasks using tools and context retrieval.

```mermaid
flowchart TB
    subgraph "Core Components"
        DS[DragonScale] --> Planner
        DS --> Executor
        DS --> Retriever
        DS --> Solver
        DS --> Cache
        DS --> Tools
        DS --> EventBus[Event Bus]
    end
    
    subgraph "Process Workflow"
        Query[User Query] --> SM[State Machine]
        SM --> Planning[Planning State]
        SM --> Execution[Execution State]
        SM --> Retrieval[Retrieval State]
        SM --> Synthesis[Synthesis State]
        SM --> Complete[Complete State]
        SM --> Error[Error State]
        SM --> Cancelled[Cancelled State]
        
        Planning --> Execution
        Execution --> Retrieval
        Retrieval --> Synthesis
        Synthesis --> Complete
    end
    
    subgraph "Error Handling"
        Error1[Error] --> DSError[DragonScaleError]
        DSError --> ConfigErr[ConfigurationError]
        DSError --> ToolNotFoundErr[ToolNotFoundError]
        DSError --> ToolExecErr[ToolExecutionError]
        DSError --> PlannerErr[PlannerError]
        DSError --> ExecutorErr[ExecutorError]
        DSError --> RetrieverErr[RetrieverError]
        DSError --> SolverErr[SolverError]
        DSError --> CacheErr[CacheError]
        DSError --> ValidationErr[ValidationError]
        DSError --> InternalErr[InternalError]
        DSError --> ArgResolutionErr[ArgResolutionError]
        DSError --> ToolValidationErr[ToolValidationError]
    end
    
    Query -.-> DS
    DS -.-> SM
```

## Core Interfaces

The system is built around key interfaces that enable modularity and extensibility.

```mermaid
classDiagram
    class Planner {
        <<interface>>
        +GeneratePlan(ctx, input)
    }
    
    class Tool {
        <<interface>>
        +Execute(ctx, input)
        +Schema()
        +Validate(input)
        +Name()
    }
    
    class Executor {
        <<interface>>
        +ExecutePlan(ctx, plan)
    }
    
    class Retriever {
        <<interface>>
        +RetrieveContext(ctx, query, dag)
    }
    
    class Solver {
        <<interface>>
        +Synthesize(ctx, query, executionResults, retrievedContext)
    }
    
    class Cache {
        <<interface>>
        +Get(ctx, key)
        +Set(ctx, key, value)
    }
    
    DragonScale --> "1" Planner : uses
    DragonScale --> "1" Executor : uses
    DragonScale --> "1" Retriever : uses
    DragonScale --> "1" Solver : uses
    DragonScale --> "1" Cache : uses
    DragonScale --> "n" Tool : uses
```

## State Machine and Process Flow

The process workflow is handled by a state machine that manages transitions between states.

```mermaid
stateDiagram-v2
    [*] --> StateInit
    StateInit --> StatePlanning
    StatePlanning --> StateExecution
    StateExecution --> StateRetrieval
    StateRetrieval --> StateSynthesis
    StateSynthesis --> StateComplete
    
    StateInit --> StateError
    StatePlanning --> StateError
    StateExecution --> StateError
    StateRetrieval --> StateError
    StateSynthesis --> StateError
    
    StateInit --> StateCancelled
    StatePlanning --> StateCancelled
    StateExecution --> StateCancelled
    StateRetrieval --> StateCancelled
    StateSynthesis --> StateCancelled
    
    StateComplete --> [*]
    StateError --> [*]
    StateCancelled --> [*]
```

## Execution Model

The system executes tasks in a Directed Acyclic Graph (DAG) model.

```mermaid
classDiagram
    class ExecutionPlan {
        +Tasks: Task[]
        +TaskMap: Map
        +Results: Map
        +GetResult(taskID)
        +SetResult(taskID, result)
        +GetTask(taskID)
    }
    
    class Task {
        +ID: string
        +Description: string
        +ToolName: string
        +Args: Map of ArgumentSource
        +DependsOn: string[]
        +Category: string
        +GetStatus()
        +UpdateStatus(status, error)
        +Duration()
        +SetErrorContext(context)
        +Retry()
    }
    
    class ArgumentSource {
        +Type: ArgumentSourceType
        +Value: any
        +DependencyTaskID: string
        +OutputFieldName: string
        +Expression: string
        +Required: boolean
        +DefaultValue: any
        +Description: string
    }
    
    ExecutionPlan "1" --> "n" Task : contains
    Task --> "n" ArgumentSource : uses
```

## Error Handling System

The project implements a comprehensive error handling system.

```mermaid
classDiagram
    class DragonScaleError {
        +Code: ErrorCode
        +Scope: string
        +Message: string
        +Cause: error
        +Error()
        +Unwrap()
    }
    
    class ConfigurationError {
        +DragonScaleError
    }
    
    class ToolNotFoundError {
        +DragonScaleError
        +ToolName: string
    }
    
    class ToolExecutionError {
        +DragonScaleError
        +ToolName: string
    }
    
    class PlannerError {
        +DragonScaleError
    }
    
    class ExecutorError {
        +DragonScaleError
    }
    
    class RetrieverError {
        +DragonScaleError
    }
    
    class SolverError {
        +DragonScaleError
    }
    
    class InternalError {
        +DragonScaleError
    }
    
    class ArgResolutionError {
        +DragonScaleError
        +TaskID: string
        +ArgumentName: string
    }
    
    class ToolValidationError {
        +DragonScaleError
        +ToolName: string
    }
    
    DragonScaleError <|-- ConfigurationError
    DragonScaleError <|-- ToolNotFoundError
    DragonScaleError <|-- ToolExecutionError
    DragonScaleError <|-- PlannerError
    DragonScaleError <|-- ExecutorError
    DragonScaleError <|-- RetrieverError
    DragonScaleError <|-- SolverError
    DragonScaleError <|-- InternalError
    DragonScaleError <|-- ArgResolutionError
    DragonScaleError <|-- ToolValidationError
```

## Component Implementations

The internal directory contains implementations of the core interfaces.

```mermaid
classDiagram
    class GenkitPlannerAdapter {
        +GeneratePlan(ctx, input)
    }
    
    class DAGExecutor {
        +ExecutePlan(ctx, plan)
        +executeTask(ctx, task, plan, wg, completionChan, errMutex, execErr)
        +resolveArguments(ctx, task, plan)
        +resolveDependencyOutput(ctx, task, plan, argName, argSource)
        +findReadyTasks(plan)
        +calculatePriority(task, plan)
    }
    
    class VectorRetriever {
        +RetrieveContext(ctx, query, dag)
    }
    
    class GenkitSolverAdapter {
        +Synthesize(ctx, query, executionResults, retrievedContext)
    }
    
    class GoToolAdapter {
        +Execute(ctx, input)
        +Schema()
        +Validate(input)
        +Name()
    }
    
    class MemoryCache {
        +Get(ctx, key)
        +Set(ctx, key, value)
    }
    
    class FilePersistentCache {
        +Get(ctx, key)
        +Set(ctx, key, value)
        +persist()
        +load()
    }
    
    class ChannelEventBus {
        +Subscribe(eventType, handler)
        +Publish(ctx, event)
        +Close()
    }
    
    Planner <|.. GenkitPlannerAdapter
    Executor <|.. DAGExecutor
    Retriever <|.. VectorRetriever
    Solver <|.. GenkitSolverAdapter
    Tool <|.. GoToolAdapter
    Cache <|.. MemoryCache
    Cache <|.. FilePersistentCache
```

## Usage Flow

The typical usage flow in an application using DragonScale Genkit.

```mermaid
sequenceDiagram
    participant App as Application
    participant DS as DragonScale
    participant P as Planner
    participant E as Executor
    participant R as Retriever
    participant S as Solver
    participant T as Tools
    
    App->>DS: Process(query)
    DS->>P: GeneratePlan(query)
    P-->>DS: ExecutionPlan
    DS->>E: ExecutePlan(plan)
    E->>T: Execute Task 1
    E->>T: Execute Task 2
    T-->>E: Task Results
    E-->>DS: Execution Results
    DS->>R: RetrieveContext(query, plan)
    R-->>DS: Context
    DS->>S: Synthesize(query, results, context)
    S-->>DS: Final Answer
    DS-->>App: Response
```

## Argument Resolution System

The system includes a robust argument resolution system for task execution.

```mermaid
classDiagram
    class ArgumentSource {
        +Type: ArgumentSourceType
        +Value: any
        +DependencyTaskID: string
        +OutputFieldName: string
        +Expression: string
        +SourceMap: map
        +Required: boolean
        +DefaultValue: any
        +Description: string
    }
    
    class ArgumentSourceType {
        <<enumeration>>
        LITERAL
        DEPENDENCY_OUTPUT
        EXPRESSION
        MERGED
    }
    
    ArgumentSource --> ArgumentSourceType : uses
    
    class DAGExecutor {
        +resolveArguments(ctx, task, plan)
        +resolveDependencyOutput(ctx, task, plan, argName, argSource)
    }
    
    DAGExecutor --> ArgumentSource : resolves
```
