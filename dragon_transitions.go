package dragonscale

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ZanzyTHEbar/dragonscale-genkit/internal/eventbus"
)

// CreateProcessStateMachine builds a complete state machine for the process workflow.
func CreateProcessStateMachine(components DragonScaleComponents, eventBus eventbus.EventBus) *StateMachine {
	sm := NewStateMachine(eventBus)

	// Register all state transitions
	sm.RegisterTransition(StateInit, createInitTransition(components))
	sm.RegisterTransition(StatePlanning, createPlanningTransition(components))
	sm.RegisterTransition(StateExecution, createExecutionTransition(components))
	sm.RegisterTransition(StateRetrieval, createRetrievalTransition(components))
	sm.RegisterTransition(StateSynthesis, createSynthesisTransition(components))
	sm.RegisterTransition(StateError, createErrorTransition(components))
	sm.RegisterTransition(StateComplete, createCompleteTransition(components))

	return sm
}

// createInitTransition handles the initialization state.
func createInitTransition(components DragonScaleComponents) StateTransition {
	return func(ctx context.Context, eb eventbus.EventBus, pCtx *ProcessContext) (ProcessState, error) {
		// Check if event bus should be used
		hasEventBus := eb != nil

		if hasEventBus {
			// Publish event for processing started
			startEvent := eventbus.NewEvent(
				eventbus.EventQueryProcessingStarted,
				pCtx.Query,
				"StateMachine.Init",
				map[string]interface{}{
					"timestamp": time.Now().Format(time.RFC3339),
				},
			)
			eb.Publish(ctx, startEvent)
		}

		// Prepare planner input
		schemas := components.GetSchemas()
		pCtx.PlannerInput = map[string]interface{}{
			"Query":      pCtx.Query,
			"ToolSchema": schemas,
		}

		// Move to planning state
		return StatePlanning, nil
	}
}

// createPlanningTransition handles the planning state.
func createPlanningTransition(components DragonScaleComponents) StateTransition {
	return func(ctx context.Context, eb eventbus.EventBus, pCtx *ProcessContext) (ProcessState, error) {
		hasEventBus := eb != nil
		planner := components.Planner

		// Cast the input to the expected type
		plannerInput, ok := pCtx.PlannerInput.(map[string]interface{})
		if !ok {
			return StateError, fmt.Errorf("invalid planner input type")
		}

		if hasEventBus {
			// Publish planning started event
			planStartEvent := eventbus.NewEvent(
				eventbus.EventPlanGenerationStarted,
				plannerInput,
				"StateMachine.Planning",
				nil,
			)
			eb.Publish(ctx, planStartEvent)
		}

		// Call the planner through reflection or type assertion
		// TODO: This is a simplified example - the actual implementation would need to match your Planner interface
		executionPlan, err := planner.(interface {
			GeneratePlan(context.Context, interface{}) (interface{}, error)
		}).GeneratePlan(ctx, plannerInput)
		if err != nil {
			if hasEventBus {
				// Publish failure events
				failEvent := eventbus.NewEvent(
					eventbus.EventPlanGenerationFailure,
					err.Error(),
					"StateMachine.Planning",
					map[string]interface{}{
						"error": err.Error(),
					},
				)
				eb.Publish(ctx, failEvent)

				queryFailEvent := eventbus.NewEvent(
					eventbus.EventQueryProcessingFailure,
					pCtx.Query,
					"StateMachine.Planning",
					map[string]interface{}{
						"error": err.Error(),
						"stage": "plan_generation",
					},
				)
				eb.Publish(ctx, queryFailEvent)
			}
			return StateError, fmt.Errorf("failed to generate execution plan: %w", err)
		}

		if hasEventBus {
			// Determine task count for metadata
			var taskCount int
			if plan, ok := executionPlan.(interface{ GetTaskCount() int }); ok {
				taskCount = plan.GetTaskCount()
			}

			// Publish success event
			planSuccessEvent := eventbus.NewEvent(
				eventbus.EventPlanGenerationSuccess,
				executionPlan,
				"StateMachine.Planning",
				map[string]interface{}{
					"task_count": taskCount,
				},
			)
			eb.Publish(ctx, planSuccessEvent)
		}

		// Store execution plan
		pCtx.ExecutionPlan = executionPlan

		// Move to execution state
		return StateExecution, nil
	}
}

// createExecutionTransition handles the execution state.
func createExecutionTransition(components DragonScaleComponents) StateTransition {
	return func(ctx context.Context, eb eventbus.EventBus, pCtx *ProcessContext) (ProcessState, error) {
		hasEventBus := eb != nil
		executor := components.Executor

		// Get the execution plan
		executionPlan := pCtx.ExecutionPlan

		if hasEventBus {
			// Determine task count for metadata
			var taskCount int
			if plan, ok := executionPlan.(interface{ GetTaskCount() int }); ok {
				taskCount = plan.GetTaskCount()
			}

			// Publish DAG execution started event
			dagStartEvent := eventbus.NewEvent(
				eventbus.EventDAGExecutionStarted,
				executionPlan,
				"StateMachine.Execution",
				map[string]interface{}{
					"task_count": taskCount,
				},
			)
			eb.Publish(ctx, dagStartEvent)
		}

		// Execute the plan
		executionResults, err := executor.(interface {
			ExecutePlan(context.Context, interface{}) (map[string]interface{}, error)
		}).ExecutePlan(ctx, executionPlan)
		if err != nil {
			if hasEventBus {
				// Publish failure events
				dagFailEvent := eventbus.NewEvent(
					eventbus.EventDAGExecutionFailure,
					err.Error(),
					"StateMachine.Execution",
					map[string]interface{}{
						"error": err.Error(),
					},
				)
				eb.Publish(ctx, dagFailEvent)

				queryFailEvent := eventbus.NewEvent(
					eventbus.EventQueryProcessingFailure,
					pCtx.Query,
					"StateMachine.Execution",
					map[string]interface{}{
						"error": err.Error(),
						"stage": "dag_execution",
					},
				)
				eb.Publish(ctx, queryFailEvent)
			}
			return StateError, fmt.Errorf("DAG execution failed: %w", err)
		}

		if hasEventBus {
			// Publish success event
			dagSuccessEvent := eventbus.NewEvent(
				eventbus.EventDAGExecutionSuccess,
				executionResults,
				"StateMachine.Execution",
				map[string]interface{}{
					"result_count": len(executionResults),
				},
			)
			eb.Publish(ctx, dagSuccessEvent)
		}

		// Store execution results
		pCtx.ExecutionResults = executionResults

		// Determine next state based on configuration
		retriever := components.Retriever

		if components.Config.EnableRetrieval && retriever != nil {
			return StateRetrieval, nil
		}

		// Skip to synthesis if retrieval is disabled or no retriever is available
		return StateSynthesis, nil
	}
}

// createRetrievalTransition handles the context retrieval state.
func createRetrievalTransition(components DragonScaleComponents) StateTransition {
	return func(ctx context.Context, eb eventbus.EventBus, pCtx *ProcessContext) (ProcessState, error) {
		hasEventBus := eb != nil
		retriever := components.Retriever

		if hasEventBus {
			// Publish retrieval started event
			retrievalStartEvent := eventbus.NewEvent(
				eventbus.EventContextRetrievalStarted,
				pCtx.Query,
				"StateMachine.Retrieval",
				nil,
			)
			eb.Publish(ctx, retrievalStartEvent)
		}

		// Retrieve context
		retrievedContext, err := retriever.(interface {
			RetrieveContext(context.Context, string, interface{}) (string, error)
		}).RetrieveContext(ctx, pCtx.Query, pCtx.ExecutionPlan)
		if err != nil {
			// Log error but don't fail the process
			log.Printf("Context retrieval failed: %v", err)

			if hasEventBus {
				// Publish failure event but don't stop processing
				retrievalFailEvent := eventbus.NewEvent(
					eventbus.EventContextRetrievalFailure,
					err.Error(),
					"StateMachine.Retrieval",
					map[string]interface{}{
						"error": err.Error(),
					},
				)
				eb.Publish(ctx, retrievalFailEvent)
			}
		} else if hasEventBus {
			// Publish success event
			retrievalSuccessEvent := eventbus.NewEvent(
				eventbus.EventContextRetrievalSuccess,
				retrievedContext,
				"StateMachine.Retrieval",
				map[string]interface{}{
					"context_length": len(retrievedContext),
				},
			)
			eb.Publish(ctx, retrievalSuccessEvent)
		}

		// Store retrieved context (will be empty string if retrieval failed)
		pCtx.RetrievedContext = retrievedContext

		// Move to synthesis state
		return StateSynthesis, nil
	}
}

// createSynthesisTransition handles the synthesis state.
func createSynthesisTransition(components DragonScaleComponents) StateTransition {
	return func(ctx context.Context, eb eventbus.EventBus, pCtx *ProcessContext) (ProcessState, error) {
		hasEventBus := eb != nil
		solver := components.Solver

		if hasEventBus {
			// Publish synthesis started event
			synthesisStartEvent := eventbus.NewEvent(
				eventbus.EventSynthesisStarted,
				pCtx.Query,
				"StateMachine.Synthesis",
				map[string]interface{}{
					"has_retrieved_context":  pCtx.RetrievedContext != "",
					"execution_result_count": len(pCtx.ExecutionResults),
				},
			)
			eb.Publish(ctx, synthesisStartEvent)
		}

		// Synthesize final answer
		finalAnswer, err := solver.(interface {
			Synthesize(context.Context, string, map[string]interface{}, string) (string, error)
		}).Synthesize(ctx, pCtx.Query, pCtx.ExecutionResults, pCtx.RetrievedContext)
		if err != nil {
			if hasEventBus {
				// Publish failure events
				synthesisFailEvent := eventbus.NewEvent(
					eventbus.EventSynthesisFailure,
					err.Error(),
					"StateMachine.Synthesis",
					map[string]interface{}{
						"error": err.Error(),
					},
				)
				eb.Publish(ctx, synthesisFailEvent)

				queryFailEvent := eventbus.NewEvent(
					eventbus.EventQueryProcessingFailure,
					pCtx.Query,
					"StateMachine.Synthesis",
					map[string]interface{}{
						"error": err.Error(),
						"stage": "synthesis",
					},
				)
				eb.Publish(ctx, queryFailEvent)
			}
			return StateError, fmt.Errorf("failed to synthesize final answer: %w", err)
		}

		if hasEventBus {
			// Publish success events
			synthesisSuccessEvent := eventbus.NewEvent(
				eventbus.EventSynthesisSuccess,
				finalAnswer,
				"StateMachine.Synthesis",
				map[string]interface{}{
					"answer_length": len(finalAnswer),
				},
			)
			eb.Publish(ctx, synthesisSuccessEvent)

			querySuccessEvent := eventbus.NewEvent(
				eventbus.EventQueryProcessingSuccess,
				pCtx.Query,
				"StateMachine.Synthesis",
				map[string]interface{}{
					"final_answer": finalAnswer,
				},
			)
			eb.Publish(ctx, querySuccessEvent)
		}

		// Store final answer
		pCtx.FinalAnswer = finalAnswer

		// Move to complete state
		return StateComplete, nil
	}
}

// createErrorTransition handles error states.
func createErrorTransition(_ DragonScaleComponents) StateTransition {
	return func(ctx context.Context, eb eventbus.EventBus, pCtx *ProcessContext) (ProcessState, error) {
		// At this point, the error is already recorded in the process context
		// We just need to decide what to do next

		// In a more sophisticated implementation, we might:
		// 1. Check for retry conditions
		// 2. Try alternative paths
		// 3. Fall back to a simpler processing method

		// For now, we'll just transition to complete with the error intact
		// The error will be returned when Execute completes
		return StateComplete, pCtx.LastError
	}
}

// createCompleteTransition handles the complete state.
func createCompleteTransition(_ DragonScaleComponents) StateTransition {
	return func(ctx context.Context, eb eventbus.EventBus, pCtx *ProcessContext) (ProcessState, error) {
		// This is a terminal state - nothing to do
		// The state machine's Execute method will handle returning the final result
		return StateComplete, nil
	}
}
