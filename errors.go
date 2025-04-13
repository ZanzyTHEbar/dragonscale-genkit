// filepath: /mnt/dragonnet/common/Projects/Personal/General/genkit/errors.go
package dragonscale

import "fmt"

// Error codes for specific failure types
const (
	ErrCodeValidation     = "VALIDATION_ERROR"
	ErrCodeToolNotFound   = "TOOL_NOT_FOUND"
	ErrCodeToolExecution  = "TOOL_EXECUTION_ERROR"
	ErrCodeArgResolution  = "ARGUMENT_RESOLUTION_ERROR"
	ErrCodePlanGeneration = "PLAN_GENERATION_ERROR"
	ErrCodeDAGExecution   = "DAG_EXECUTION_ERROR"
	ErrCodeSynthesis      = "SYNTHESIS_ERROR"
	ErrCodeRetrieval      = "RETRIEVAL_ERROR"
	ErrCodeConfiguration  = "CONFIGURATION_ERROR"
	ErrCodeCancelled      = "EXECUTION_CANCELLED"
	ErrCodeTimeout        = "EXECUTION_TIMEOUT"
	ErrCodeCache          = "CACHE_ERROR"
	ErrCodeInternal       = "INTERNAL_ERROR"
)

// DragonScaleError is a custom error type for DragonScale specific errors.
type DragonScaleError struct {
	Code    string // A machine-readable error code (e.g., ErrCodeToolNotFound)
	Message string // A human-readable message
	Stage   string // The stage where the error occurred (e.g., "planning", "execution")
	Cause   error  // The underlying error, if any
}

// Error implements the error interface.
func (e *DragonScaleError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s:%s] %s: %v", e.Stage, e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s:%s] %s", e.Stage, e.Code, e.Message)
}

// Unwrap returns the underlying cause of the error, allowing for error chaining.
func (e *DragonScaleError) Unwrap() error {
	return e.Cause
}

// NewError creates a new DragonScaleError.
func NewError(code, stage, message string, cause error) *DragonScaleError {
	return &DragonScaleError{
		Code:    code,
		Stage:   stage,
		Message: message,
		Cause:   cause,
	}
}

// Specific error constructors

func NewValidationError(stage, message string, cause error) *DragonScaleError {
	return NewError(ErrCodeValidation, stage, message, cause)
}

func NewToolNotFoundError(stage, toolName string) *DragonScaleError {
	return NewError(ErrCodeToolNotFound, stage, fmt.Sprintf("tool '%s' not found", toolName), nil)
}

func NewToolExecutionError(stage, toolName string, cause error) *DragonScaleError {
	return NewError(ErrCodeToolExecution, stage, fmt.Sprintf("execution failed for tool '%s'", toolName), cause)
}

func NewArgResolutionError(stage, taskID, argName string, cause error) *DragonScaleError {
	msg := fmt.Sprintf("failed to resolve argument '%s' for task '%s'", argName, taskID)
	return NewError(ErrCodeArgResolution, stage, msg, cause)
}

func NewPlanGenerationError(cause error) *DragonScaleError {
	return NewError(ErrCodePlanGeneration, "planning", "failed to generate execution plan", cause)
}

func NewDAGExecutionError(cause error) *DragonScaleError {
	return NewError(ErrCodeDAGExecution, "execution", "DAG execution failed", cause)
}

func NewSynthesisError(cause error) *DragonScaleError {
	return NewError(ErrCodeSynthesis, "synthesis", "failed to synthesize final answer", cause)
}

func NewRetrievalError(cause error) *DragonScaleError {
	return NewError(ErrCodeRetrieval, "retrieval", "failed to retrieve context", cause)
}

func NewConfigurationError(message string, cause error) *DragonScaleError {
	return NewError(ErrCodeConfiguration, "initialization", message, cause)
}

func NewCancelledError(stage string, cause error) *DragonScaleError {
	msg := "execution cancelled"
	if cause != nil && cause.Error() != "" && cause.Error() != "context canceled" { // Add more detail if cause isn't just context.Canceled
		msg = fmt.Sprintf("execution cancelled: %v", cause)
	}
	return NewError(ErrCodeCancelled, stage, msg, cause)
}

func NewTimeoutError(stage string, cause error) *DragonScaleError {
	return NewError(ErrCodeTimeout, stage, "execution timed out", cause)
}

func NewCacheError(stage, operation string, cause error) *DragonScaleError {
	return NewError(ErrCodeCache, stage, fmt.Sprintf("cache operation '%s' failed", operation), cause)
}

func NewInternalError(stage, message string, cause error) *DragonScaleError {
	return NewError(ErrCodeInternal, stage, message, cause)
}
