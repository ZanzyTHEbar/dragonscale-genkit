package dragonscale

import (
	"context"
	"fmt"
	"time"

	"github.com/ZanzyTHEbar/dragonscale-genkit/internal/eventbus"
)

//! The pushdown automaton approach was specifically chosen because:
//!
//! It allows tracking the execution history (through the state stack) - Implemented!
//! TODO: It supports branching and alternative paths through the workflow (stack enables this)
//! It can easily be extended to support retries, fallbacks, or other complex patterns (stack enables this)
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
	StateStack   []ProcessState // Stores the history of states visited
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
		StateStack:      []ProcessState{}, // Initialize empty stack
		StateData:       make(map[string]interface{}),
		StartTime:       time.Now(),
		StateStartTimes: make(map[ProcessState]time.Time),
	}
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

// GetHistory returns a copy of the state execution history.
func (pc *ProcessContext) GetHistory() []ProcessState {
	history := make([]ProcessState, len(pc.StateStack))
	copy(history, pc.StateStack)
	return history
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
	// TODO: This is a simplified implementation
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
	// Record the initial state's start time
	pCtx.StateStartTimes[pCtx.CurrentState] = time.Now()

	for !pCtx.IsTerminal() { // Use the new IsTerminal method
		// Check for context cancellation before executing the next state
		select {
		case <-ctx.Done():
			// Context was cancelled
			err := ctx.Err()
			currentStage := string(pCtx.CurrentState)
			pCtx.SetCancelled(err, currentStage) // Use SetCancelled method
			// Push the final cancelled state onto the history stack before returning
			pCtx.StateStack = append(pCtx.StateStack, pCtx.CurrentState)
			return "", err // Return the cancellation error
		default:
			// Context is still active, proceed
		}

		// Find the transition for the current state
		transition, exists := sm.transitions[pCtx.CurrentState]
		if !exists {
			err := fmt.Errorf("no transition defined for state: %s", pCtx.CurrentState)
			currentStage := string(pCtx.CurrentState)
			pCtx.SetError(err, currentStage) // Use SetError
			// Push the final error state onto the history stack before returning
			pCtx.StateStack = append(pCtx.StateStack, pCtx.CurrentState)
			return "", err
		}

		// Push the current state onto the stack *before* executing the transition
		// This records the history of states entered.
		pCtx.StateStack = append(pCtx.StateStack, pCtx.CurrentState)

		// Execute the transition function for the current state
		nextState, err := transition(ctx, sm.eventBus, pCtx)

		if err != nil {
			currentStage := string(pCtx.CurrentState) // Stage where error occurred during transition
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
			// The loop will continue and check IsTerminal() again. The error state
			// will be pushed onto the stack in the next iteration before exiting.
			continue // Go to the top of the loop to check terminal state
		}

		// Update the current state if it wasn't changed to a terminal state by the transition
		if !pCtx.IsTerminal() {
			pCtx.CurrentState = nextState
			pCtx.StateStartTimes[nextState] = time.Now() // Record start time for the new state
		}
	}

	// Push the final terminal state (Complete, Error, Cancelled) onto the history stack
	// This ensures the history always includes the final state.
	// Note: Error/Cancelled states might have already been pushed if they occurred
	// during context check or transition lookup failure, but appending again is harmless
	// if the state hasn't changed (e.g., if SetError was called inside the transition).
	// A simple check prevents duplicate appends if the state was already terminal *before* the loop check.
	if len(pCtx.StateStack) == 0 || pCtx.StateStack[len(pCtx.StateStack)-1] != pCtx.CurrentState {
		pCtx.StateStack = append(pCtx.StateStack, pCtx.CurrentState)
	}

	// Return the final answer and any error encountered (including cancellation)
	return pCtx.FinalAnswer, pCtx.LastError
}
