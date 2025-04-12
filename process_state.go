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

// SetError sets the last error and error stage.
func (pc *ProcessContext) SetError(err error, stage string) {
	pc.LastError = err
	pc.ErrorStage = stage
	pc.CurrentState = StateError
	pc.StateStartTimes[StateError] = time.Now()
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
	if !ok {
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
	for {
		transition, ok := sm.transitions[pCtx.CurrentState]
		if !ok {
			return "", fmt.Errorf("no transition registered for state %s", pCtx.CurrentState)
		}

		nextState, err := transition(ctx, sm.eventBus, pCtx)
		if err != nil {
			// Error already recorded in the process context
			if pCtx.CurrentState == StateError {
				return "", pCtx.LastError
			}

			pCtx.SetError(err, string(pCtx.CurrentState))
			continue
		}

		// State transition
		pCtx.CurrentState = nextState
		pCtx.StateStartTimes[nextState] = time.Now()

		// Check for completion
		if nextState == StateComplete {
			pCtx.Complete()
			return pCtx.FinalAnswer, nil
		}
	}
}
