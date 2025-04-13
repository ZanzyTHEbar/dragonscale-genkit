package dragonscale

import (
	"context"
	"fmt"
	"time"

	"github.com/ZanzyTHEbar/dragonscale-genkit/internal/eventbus"
)

//! The pushdown automaton approach was specifically chosen because:
//!
//! TODO: It allows tracking the execution history (through the state stack)
//! TODO: It supports branching and alternative paths through the workflow
//! It can easily be extended to support retries, fallbacks, or other complex patterns
//! It provides better visibility into the execution progress

// ProcessState represents the current state of a process execution.
type ProcessState string

const (
	// StateInit is the initial state of the process
	StateInit ProcessState = "init"
	// StatePlanning represents the planning phase
	StatePlanning ProcessState = "planning"
	// StateExecution represents the execution phase
	StateExecution ProcessState = "execution"
	// StateRetrieval represents the context retrieval phase
	StateRetrieval ProcessState = "retrieval"
	// StateSynthesis represents the answer synthesis phase
	StateSynthesis ProcessState = "synthesis"
	// StateError represents an error state
	StateError ProcessState = "error"
	// StateComplete represents the completed state
	StateComplete ProcessState = "complete"
	// StateCancelled represents the cancelled state
	StateCancelled ProcessState = "cancelled"
	// StateUnknown is used when the status of an async execution cannot be determined.
	StateUnknown ProcessState = "unknown" // Added StateUnknown
)

// ProcessContext contains the data needed for process execution.
// It acts as the "tape" in the pushdown automaton.
type ProcessContext struct {
	// Input parameters
	Query string

	// Intermediate results
	PlannerInput     interface{}
	ExecutionPlan    interface{}
	ExecutionResults map[string]interface{}
	RetrievedContext string
	FinalAnswer      string

	// Error handling
	LastError  error
	ErrorStage string

	// State management
	CurrentState ProcessState
	StateStack   []ProcessState
	StateData    map[string]interface{}

	// Timestamp tracking
	StartTime       time.Time
	EndTime         time.Time
	StateStartTimes map[ProcessState]time.Time
}

// NewProcessContext creates a new process context with the given query.
func NewProcessContext(query string) *ProcessContext {
	return &ProcessContext{
		Query:           query,
		CurrentState:    StateInit,
		StateStack:      []ProcessState{},
		StateData:       make(map[string]interface{}),
		StartTime:       time.Now(),
		StateStartTimes: make(map[ProcessState]time.Time),
	}
}

// PushState pushes the current state onto the stack and sets a new current state.
func (pc *ProcessContext) PushState(state ProcessState) {
	pc.StateStack = append(pc.StateStack, pc.CurrentState)
	pc.CurrentState = state
	pc.StateStartTimes[state] = time.Now()
}

// PopState pops the top state from the stack and sets it as the current state.
// Returns false if the stack is empty.
func (pc *ProcessContext) PopState() bool {
	if len(pc.StateStack) == 0 {
		return false
	}
	lastIdx := len(pc.StateStack) - 1
	pc.CurrentState = pc.StateStack[lastIdx]
	pc.StateStack = pc.StateStack[:lastIdx]
	pc.StateStartTimes[pc.CurrentState] = time.Now()
	return true
}

// IsTerminal checks if the current state is a terminal state (Complete, Error, Cancelled).
func (pc *ProcessContext) IsTerminal() bool {
	return pc.CurrentState == StateComplete || pc.CurrentState == StateError || pc.CurrentState == StateCancelled
}

// SetError sets the last error and error stage, transitioning to StateError.
func (pc *ProcessContext) SetError(err error, stage string) {
	pc.LastError = err
	pc.ErrorStage = stage
	pc.CurrentState = StateError
	pc.StateStartTimes[StateError] = time.Now()
}

// SetCancelled sets the state to Cancelled and records the cancellation error.
func (pc *ProcessContext) SetCancelled(err error, stage string) {
	pc.LastError = err
	pc.ErrorStage = stage // Record the stage where cancellation was detected
	pc.CurrentState = StateCancelled
	pc.StateStartTimes[StateCancelled] = time.Now()
}

// Complete marks the process as complete and sets the end time.
func (pc *ProcessContext) Complete() {
	pc.CurrentState = StateComplete
	pc.EndTime = time.Now()
	pc.StateStartTimes[StateComplete] = pc.EndTime
}

// GetStateDuration returns the duration spent in the given state.
func (pc *ProcessContext) GetStateDuration(state ProcessState) time.Duration {
	startTime, ok := pc.StateStartTimes[state]
	if (!ok) {
		return 0
	}

	if state == pc.CurrentState {
		return time.Since(startTime)
	}

	// For past states, we'd need to track end times for each state
	// This is a simplified implementation
	return 0
}

// GetTotalDuration returns the total duration of the process so far.
func (pc *ProcessContext) GetTotalDuration() time.Duration {
	if pc.CurrentState == StateComplete {
		return pc.EndTime.Sub(pc.StartTime)
	}
	return time.Since(pc.StartTime)
}

// StateTransition defines a transition function for the state machine.
type StateTransition func(ctx context.Context, eventBus eventbus.EventBus, pCtx *ProcessContext) (ProcessState, error)

// StateMachine represents a finite state machine for process execution.
type StateMachine struct {
	transitions map[ProcessState]StateTransition
	eventBus    eventbus.EventBus
}

// NewStateMachine creates a new state machine with the provided transitions.
func NewStateMachine(eventBus eventbus.EventBus) *StateMachine {
	return &StateMachine{
		transitions: make(map[ProcessState]StateTransition),
		eventBus:    eventBus,
	}
}

// RegisterTransition registers a state transition function.
func (sm *StateMachine) RegisterTransition(state ProcessState, transition StateTransition) {
	sm.transitions[state] = transition
}

// Execute runs the state machine until completion or error.
func (sm *StateMachine) Execute(ctx context.Context, pCtx *ProcessContext) (string, error) {
	for !pCtx.IsTerminal() { // Use the new IsTerminal method
		// Check for context cancellation before executing the next state
		select {
		case <-ctx.Done():
			// Context was cancelled
			err := ctx.Err()
			currentStage := string(pCtx.CurrentState)
			pCtx.SetCancelled(err, currentStage) // Use SetCancelled method
			return "", err // Return the cancellation error
		default:
			// Context is still active, proceed
		}

		transition, exists := sm.transitions[pCtx.CurrentState]
		if !exists {
			err := fmt.Errorf("no transition defined for state: %s", pCtx.CurrentState)
			currentStage := string(pCtx.CurrentState)
			pCtx.SetError(err, currentStage) // Use SetError
			return "", err
		}

		// Execute the transition function for the current state
		nextState, err := transition(ctx, sm.eventBus, pCtx)

		if err != nil {
			currentStage := string(pCtx.CurrentState)
			// Check if the error is due to context cancellation (might be caught within the transition)
			if err == context.Canceled || err == context.DeadlineExceeded {
				pCtx.SetCancelled(err, currentStage) // Use SetCancelled
			} else {
				// SetError is usually called within the transition for specific errors,
				// but if a transition returns a non-cancellation error without setting state,
				// we set it here.
				if !pCtx.IsTerminal() { // Avoid overwriting if already set to Error/Cancelled
					pCtx.SetError(err, currentStage)
				}
			}
			// The loop will continue and check IsTerminal() again
			continue // Go to the top of the loop to check terminal state
		}

		// Update the current state if it wasn't changed by SetError/SetCancelled
		if !pCtx.IsTerminal() {
			pCtx.CurrentState = nextState
			pCtx.StateStartTimes[nextState] = time.Now()
		}
	}

	// Return the final answer and any error encountered (including cancellation)
	return pCtx.FinalAnswer, pCtx.LastError
}

// createCancelledTransition handles the cancelled state.
func createCancelledTransition(_ DragonScaleComponents) StateTransition {
	return func(ctx context.Context, eb eventbus.EventBus, pCtx *ProcessContext) (ProcessState, error) {
		// This is a terminal state. The error (context.Canceled or DeadlineExceeded)
		// should already be set in pCtx.LastError by the Execute loop or a transition.
		return StateCancelled, pCtx.LastError // Remain in Cancelled state, return the cancellation error
	}
}
