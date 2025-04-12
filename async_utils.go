package dragonscale

import (
	"context"
	"fmt"
	"time"

	"github.com/ZanzyTHEbar/dragonscale-genkit/internal/statemachine"
)

// AsyncExecutionStatus represents the status information for an async execution.
type AsyncExecutionStatus struct {
	ExecutionID  string                    `json:"execution_id"`
	Query        string                    `json:"query"`
	CurrentState statemachine.ProcessState `json:"current_state"`
	StartTime    time.Time                 `json:"start_time"`
	Duration     time.Duration             `json:"duration"`
	IsComplete   bool                      `json:"is_complete"`
	HasError     bool                      `json:"has_error"`
	ErrorMessage string                    `json:"error_message,omitempty"`
	ErrorStage   string                    `json:"error_stage,omitempty"`
}

// GetAsyncStatus retrieves the current status of an async execution.
func (d *DragonScale) GetAsyncStatus(executionID string) (*AsyncExecutionStatus, error) {
	d.asyncExecutionsMutex.RLock()
	defer d.asyncExecutionsMutex.RUnlock()

	pCtx, exists := d.asyncExecutions[executionID]
	if !exists {
		return nil, fmt.Errorf("execution with ID '%s' not found", executionID)
	}

	status := &AsyncExecutionStatus{
		ExecutionID:  executionID,
		Query:        pCtx.Query,
		CurrentState: pCtx.CurrentState,
		StartTime:    pCtx.StartTime,
		Duration:     pCtx.GetTotalDuration(),
		IsComplete:   pCtx.CurrentState == statemachine.StateComplete,
		HasError:     pCtx.CurrentState == statemachine.StateError,
	}

	if pCtx.LastError != nil {
		status.ErrorMessage = pCtx.LastError.Error()
		status.ErrorStage = pCtx.ErrorStage
	}

	return status, nil
}

// GetAsyncResult retrieves the result of a completed async execution.
// Returns error if the execution is not complete or encountered an error.
func (d *DragonScale) GetAsyncResult(executionID string) (string, error) {
	d.asyncExecutionsMutex.RLock()
	defer d.asyncExecutionsMutex.RUnlock()

	pCtx, exists := d.asyncExecutions[executionID]
	if !exists {
		return "", fmt.Errorf("execution with ID '%s' not found", executionID)
	}

	// Check if execution is complete
	if pCtx.CurrentState != statemachine.StateComplete {
		if pCtx.CurrentState == statemachine.StateError {
			return "", fmt.Errorf("execution failed: %v", pCtx.LastError)
		}
		return "", fmt.Errorf("execution is still in progress (current state: %s)", pCtx.CurrentState)
	}

	return pCtx.FinalAnswer, nil
}

// CancelAsyncProcess cancels an ongoing async execution.
// Returns true if the execution was successfully cancelled, false if it was already complete or not found.
func (d *DragonScale) CancelAsyncProcess(executionID string) (bool, error) {
	d.asyncExecutionsMutex.Lock()
	defer d.asyncExecutionsMutex.Unlock()

	pCtx, exists := d.asyncExecutions[executionID]
	if !exists {
		return false, fmt.Errorf("execution with ID '%s' not found", executionID)
	}

	// Check if execution is already complete
	if pCtx.CurrentState == statemachine.StateComplete || pCtx.CurrentState == statemachine.StateError {
		return false, nil
	}

	// Retrieve and call the cancel function
	if cancelFn, ok := pCtx.StateData["cancel"].(context.CancelFunc); ok {
		cancelFn()

		// Update state to cancelled
		pCtx.CurrentState = statemachine.StateError
		pCtx.LastError = fmt.Errorf("execution cancelled by user")
		pCtx.ErrorStage = "cancelled"

		// Publish cancellation event if event bus is available
		if d.config.EnableEventBus && d.eventBus != nil {
			cancelEvent := eventbus.NewEvent(
				eventbus.EventQueryAsyncProcessingCancelled,
				pCtx.Query,
				"DragonScale.CancelAsyncProcess",
				map[string]interface{}{
					"execution_id": executionID,
					"duration_ms":  pCtx.GetTotalDuration().Milliseconds(),
				},
			)
			d.eventBus.Publish(context.Background(), cancelEvent)
		}

		return true, nil
	}

	return false, fmt.Errorf("cannot cancel execution: cancel function not found")
}

// ListAsyncExecutions returns a list of all async execution IDs and their current states.
func (d *DragonScale) ListAsyncExecutions() map[string]string {
	d.asyncExecutionsMutex.RLock()
	defer d.asyncExecutionsMutex.RUnlock()

	result := make(map[string]string)
	for id, pCtx := range d.asyncExecutions {
		result[id] = string(pCtx.CurrentState)
	}

	return result
}

// CleanupCompletedExecutions removes completed or errored executions older than the specified duration.
// This helps prevent memory leaks from storing too many completed executions.
func (d *DragonScale) CleanupCompletedExecutions(olderThan time.Duration) int {
	d.asyncExecutionsMutex.Lock()
	defer d.asyncExecutionsMutex.Unlock()

	now := time.Now()
	count := 0

	for id, pCtx := range d.asyncExecutions {
		// Only cleanup completed or errored executions
		if (pCtx.CurrentState == statemachine.StateComplete || pCtx.CurrentState == statemachine.StateError) &&
			now.Sub(pCtx.StateStartTimes[pCtx.CurrentState]) > olderThan {
			delete(d.asyncExecutions, id)
			count++
		}
	}

	return count
}
