// Package dragonscale provides the core runtime for AI-powered workflow automation.
package dragonscale

import (
	"fmt"
)

// ErrorCode represents a specific category of error within DragonScale.
type ErrorCode string

const (
	// ErrCodeConfiguration indicates an error related to setup or configuration.
	ErrCodeConfiguration ErrorCode = "configuration_error"
	// ErrCodeToolNotFound indicates a requested tool could not be found.
	ErrCodeToolNotFound ErrorCode = "tool_not_found"
	// ErrCodeToolExecution indicates an error during the execution of a tool.
	ErrCodeToolExecution ErrorCode = "tool_execution_error"
	// ErrCodePlanner indicates an error during the planning phase.
	ErrCodePlanner ErrorCode = "planner_error"
	// ErrCodeExecutor indicates an error during the execution phase (DAG execution).
	ErrCodeExecutor ErrorCode = "executor_error"
	// ErrCodeRetriever indicates an error during the context retrieval phase.
	ErrCodeRetriever ErrorCode = "retriever_error"
	// ErrCodeSolver indicates an error during the final answer synthesis phase.
	ErrCodeSolver ErrorCode = "solver_error"
	// ErrCodeCache indicates an error related to the cache component.
	ErrCodeCache ErrorCode = "cache_error"
	// ErrCodeValidation indicates an input validation error.
	ErrCodeValidation ErrorCode = "validation_error"
	// ErrCodeInternal indicates an unexpected internal error.
	ErrCodeInternal ErrorCode = "internal_error"
	// ErrCodeCancelled indicates the operation was cancelled.
	ErrCodeCancelled ErrorCode = "cancelled"
	// ErrCodeTimeout indicates the operation timed out.
	ErrCodeTimeout ErrorCode = "timeout"
)

// DragonScaleError is the base error type for all errors originating from the DragonScale library.
type DragonScaleError struct {
	Code    ErrorCode // The specific error code.
	Scope   string    // The component or operation where the error occurred (e.g., "planner", "tool:search").
	Message string    // A human-readable description of the error.
	Cause   error     // The underlying error that caused this one, if any.
}

// Error implements the standard error interface.
func (e *DragonScaleError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s:%s] %s: %v", e.Code, e.Scope, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s:%s] %s", e.Code, e.Scope, e.Message)
}

// Unwrap provides compatibility with errors.Is and errors.As.
func (e *DragonScaleError) Unwrap() error {
	return e.Cause
}

// IsDragonScaleError checks if an error is a DragonScaleError or derived type.
func IsDragonScaleError(err error) bool {
	_, ok := err.(*DragonScaleError)
	if ok {
		return true
	}
	
	// Check for specific error types derived from DragonScaleError
	_, okConfig := err.(*ConfigurationError)
	_, okToolNotFound := err.(*ToolNotFoundError)
	_, okToolExecution := err.(*ToolExecutionError)
	_, okPlanner := err.(*PlannerError)
	_, okExecutor := err.(*ExecutorError)
	_, okRetriever := err.(*RetrieverError)
	_, okSolver := err.(*SolverError)
	_, okCache := err.(*CacheError)
	_, okInternal := err.(*InternalError)
	_, okArgResolution := err.(*ArgResolutionError)
	_, okToolValidation := err.(*ToolValidationError)
	
	return okConfig || okToolNotFound || okToolExecution || okPlanner || 
	       okExecutor || okRetriever || okSolver || okCache || okInternal || 
	       okArgResolution || okToolValidation
}

// NewError creates a new DragonScaleError.
func NewError(code ErrorCode, scope, message string, cause error) *DragonScaleError {
	return &DragonScaleError{
		Code:    code,
		Scope:   scope,
		Message: message,
		Cause:   cause,
	}
}

// --- Specific Error Types ---

// ConfigurationError indicates an error related to setup or configuration.
type ConfigurationError struct {
	DragonScaleError
}

// NewConfigurationError creates a new ConfigurationError.
func NewConfigurationError(scope, message string, cause error) *ConfigurationError {
	return &ConfigurationError{
		DragonScaleError: DragonScaleError{
			Code:    ErrCodeConfiguration,
			Scope:   scope,
			Message: message,
			Cause:   cause,
		},
	}
}

// ToolNotFoundError indicates a requested tool could not be found.
type ToolNotFoundError struct {
	DragonScaleError
	ToolName string
}

// NewToolNotFoundError creates a new ToolNotFoundError.
func NewToolNotFoundError(scope, toolName string) *ToolNotFoundError {
	return &ToolNotFoundError{
		DragonScaleError: DragonScaleError{
			Code:    ErrCodeToolNotFound,
			Scope:   scope, // e.g., "executor", "lookup"
			Message: fmt.Sprintf("tool '%s' not found", toolName),
		},
		ToolName: toolName,
	}
}

// ToolExecutionError indicates an error during the execution of a tool.
type ToolExecutionError struct {
	DragonScaleError
	ToolName string
}

// NewToolExecutionError creates a new ToolExecutionError.
func NewToolExecutionError(toolName string, message string, cause error) *ToolExecutionError {
	return &ToolExecutionError{
		DragonScaleError: DragonScaleError{
			Code:    ErrCodeToolExecution,
			Scope:   fmt.Sprintf("tool:%s", toolName),
			Message: message,
			Cause:   cause,
		},
		ToolName: toolName,
	}
}

// PlannerError indicates an error during the planning phase.
type PlannerError struct {
	DragonScaleError
}

// NewPlannerError creates a new PlannerError.
func NewPlannerError(message string, cause error) *PlannerError {
	return &PlannerError{
		DragonScaleError: DragonScaleError{
			Code:    ErrCodePlanner,
			Scope:   "planner",
			Message: message,
			Cause:   cause,
		},
	}
}

// ExecutorError indicates an error during the execution phase (DAG execution).
type ExecutorError struct {
	DragonScaleError
}

// NewExecutorError creates a new ExecutorError.
func NewExecutorError(scope, message string, cause error) *ExecutorError {
	return &ExecutorError{
		DragonScaleError: DragonScaleError{
			Code:    ErrCodeExecutor,
			Scope:   scope,
			Message: message,
			Cause:   cause,
		},
	}
}

// RetrieverError indicates an error during the context retrieval phase.
type RetrieverError struct {
	DragonScaleError
}

// NewRetrieverError creates a new RetrieverError.
func NewRetrieverError(message string, cause error) *RetrieverError {
	return &RetrieverError{
		DragonScaleError: DragonScaleError{
			Code:    ErrCodeRetriever,
			Scope:   "retriever",
			Message: message,
			Cause:   cause,
		},
	}
}

// SolverError indicates an error during the final answer synthesis phase.
type SolverError struct {
	DragonScaleError
}

// NewSolverError creates a new SolverError.
func NewSolverError(message string, cause error) *SolverError {
	return &SolverError{
		DragonScaleError: DragonScaleError{
			Code:    ErrCodeSolver,
			Scope:   "solver",
			Message: message,
			Cause:   cause,
		},
	}
}

// CacheError indicates an error related to the cache component.
type CacheError struct {
	DragonScaleError
	Operation string // e.g., "get", "set", "delete"
	Key       string
}

// NewCacheError creates a new CacheError.
func NewCacheError(operation, key, message string, cause error) *CacheError {
	return &CacheError{
		DragonScaleError: DragonScaleError{
			Code:    ErrCodeCache,
			Scope:   fmt.Sprintf("cache:%s", operation),
			Message: message,
			Cause:   cause,
		},
		Operation: operation,
		Key:       key,
	}
}

// InternalError indicates an unexpected internal error.
type InternalError struct {
	DragonScaleError
}

// NewInternalError creates a new InternalError.
func NewInternalError(scope, message string, cause error) *InternalError {
	return &InternalError{
		DragonScaleError: DragonScaleError{
			Code:    ErrCodeInternal,
			Scope:   scope,
			Message: message,
			Cause:   cause,
		},
	}
}

// ArgResolutionError indicates an error resolving arguments for a task.
type ArgResolutionError struct {
	DragonScaleError
	TaskID    string
	ArgumentName string
}

// NewArgResolutionError creates a new ArgResolutionError.
func NewArgResolutionError(scope, taskID, argName string, cause error) *ArgResolutionError {
	return &ArgResolutionError{
		DragonScaleError: DragonScaleError{
			Code:    ErrCodeValidation,
			Scope:   scope,
			Message: fmt.Sprintf("failed to resolve argument '%s' for task '%s'", argName, taskID),
			Cause:   cause,
		},
		TaskID:       taskID,
		ArgumentName: argName,
	}
}

// ToolValidationError indicates an error during tool argument validation.
type ToolValidationError struct {
	DragonScaleError
	ToolName string
}

// NewToolValidationError creates a new ToolValidationError.
func NewToolValidationError(toolName, message string, cause error) *ToolValidationError {
	return &ToolValidationError{
		DragonScaleError: DragonScaleError{
			Code:    ErrCodeValidation,
			Scope:   fmt.Sprintf("tool:%s", toolName),
			Message: message,
			Cause:   cause,
		},
		ToolName: toolName,
	}
}
