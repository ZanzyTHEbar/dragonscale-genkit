// filepath: /mnt/dragonnet/common/Projects/Personal/General/genkit/internal/executor/executor.go
package executor

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"container/heap"

	"github.com/ZanzyTHEbar/dragonscale-genkit"
)

// Define TaskNode for the priority queue
type TaskNode struct {
	task     *dragonscale.Task
	priority int // Lower value means higher priority (e.g., based on critical path)
	index    int // Index in the heap
}

// PriorityQueue implements heap.Interface and holds TaskNodes.
type PriorityQueue []*TaskNode

func (pq PriorityQueue) Len() int { return len(pq) }

func (pq PriorityQueue) Less(i, j int) bool {
	// Min-heap based on priority
	return pq[i].priority < pq[j].priority
}

func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *PriorityQueue) Push(x interface{}) {
	n := len(*pq)
	node := x.(*TaskNode)
	node.index = n
	*pq = append(*pq, node)
}

func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	node := old[n-1]
	old[n-1] = nil  // avoid memory leak
	node.index = -1 // for safety
	*pq = old[0 : n-1]
	return node
}

// DAGExecutor handles the execution of a task graph.
type DAGExecutor struct {
	toolRegistry map[string]dragonscale.Tool
	maxWorkers   int           // Max concurrent tasks
	maxRetries   int           // Max retries per task
	retryDelay   time.Duration // Delay between retries

	// Statistics and metrics
	metrics ExecutorMetrics

	// Controls for graceful shutdown
	ctx        context.Context
	cancelFunc context.CancelFunc
}

// ExecutorMetrics tracks statistics about DAG execution.
type ExecutorMetrics struct {
	TasksExecuted    int
	TasksSuccessful  int
	TasksFailed      int
	TotalDuration    time.Duration
	LongestTaskTime  time.Duration
	ShortestTaskTime time.Duration
	TotalRetries     int

	mu sync.Mutex // Protects metrics updates
}

// ExecutorOption represents an option for configuring the DAGExecutor.
type ExecutorOption func(*DAGExecutor)

// WithMaxRetries sets the maximum number of retries for failed tasks.
func WithMaxRetries(retries int) ExecutorOption {
	return func(e *DAGExecutor) {
		e.maxRetries = retries
	}
}

// WithRetryDelay sets the delay between task retries.
func WithRetryDelay(delay time.Duration) ExecutorOption {
	return func(e *DAGExecutor) {
		e.retryDelay = delay
	}
}

// NewExecutor creates a new executor with default settings.
// It requires the tool registry to be passed during initialization.
func NewExecutor(toolRegistry map[string]dragonscale.Tool, options ...ExecutorOption) *DAGExecutor {
	ctx, cancel := context.WithCancel(context.Background())

	e := &DAGExecutor{
		toolRegistry: toolRegistry,      // Store the provided tool registry
		maxWorkers:   5,               // Default max workers
		maxRetries:   3,               // Default to 3 retries
		retryDelay:   time.Second * 2, // Default 2-second delay
		ctx:          ctx,
		cancelFunc:   cancel,
	}

	// Apply options
	for _, option := range options {
		option(e)
	}

	// Validate that toolRegistry is not nil or empty
	if e.toolRegistry == nil || len(e.toolRegistry) == 0 {
		// Log a warning or handle as appropriate, depending on whether an empty registry is valid
		log.Println("Warning: DAGExecutor initialized with an empty or nil tool registry.")
		// Depending on requirements, you might return an error here:
		// return nil, fmt.Errorf("tool registry cannot be nil or empty")
	}

	return e
}

// ExecutePlan runs the execution plan.
func (e *DAGExecutor) ExecutePlan(ctx context.Context, plan *dragonscale.ExecutionPlan) (map[string]interface{}, error) {
	// Create a child context that can be cancelled
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel() // Ensure all resources are released when we're done

	startTime := time.Now()
	log.Printf("Starting DAG execution (total_tasks: %d)", len(plan.Tasks))

	priorityQueue := make(PriorityQueue, 0)
	heap.Init(&priorityQueue)

	activeWorkers := 0
	var wg sync.WaitGroup
	var execErr error // This will hold the *first* critical error encountered
	var errMutex sync.Mutex
	completionChannel := make(chan *dragonscale.Task, len(plan.Tasks)) // Channel to receive completed tasks

	// Reset metrics for this execution
	e.resetMetrics()

	// Initial population of the ready queue
	readyTasks := e.findReadyTasks(plan)
	for _, task := range readyTasks {
		node := &TaskNode{task: task, priority: e.calculatePriority(task, plan)} // Add priority calculation
		heap.Push(&priorityQueue, node)
		task.UpdateStatus(dragonscale.TaskStatusReady, nil)
	}

	// Main execution loop
	for priorityQueue.Len() > 0 || activeWorkers > 0 {
		// Check if context was cancelled
		select {
		case <-execCtx.Done():
			log.Printf("DAG execution cancelled (reason: %v)", execCtx.Err())
			errMutex.Lock()
			if execErr == nil {
				// Wrap context error in ExecutorError
				execErr = dragonscale.NewExecutorError("execution", "context cancelled", execCtx.Err())
			}
			errMutex.Unlock()
			break // Exit the outer loop
		default:
			// Continue execution
		}

		// Check if we should break due to cancellation or error detected above or during task completion
		errMutex.Lock()
		isErrorOrCancelled := execErr != nil
		errMutex.Unlock()
		if isErrorOrCancelled {
			break
		}

		// Launch tasks while workers are available and tasks are ready
		for priorityQueue.Len() > 0 && activeWorkers < e.maxWorkers {
			// ... (rest of the launching logic remains the same) ...
			taskNode := heap.Pop(&priorityQueue).(*TaskNode)
			task := taskNode.task

			if task.GetStatus() != dragonscale.TaskStatusReady { // Skip if already picked up or status changed
				continue
			}

			activeWorkers++
			wg.Add(1)
			// Pass the executor context (execCtx) to the task execution goroutine
			go e.executeTask(execCtx, task, plan, &wg, completionChannel, &errMutex, &execErr) // Pass execErr pointer
		}

		// Wait for a task to complete if no more tasks can be launched immediately or if we need to check for errors
		// Only wait if there are active workers OR if the queue is empty but we are still waiting for workers to finish
		if activeWorkers == e.maxWorkers || (priorityQueue.Len() == 0 && activeWorkers > 0) {
			select {
			case completedTask := <-completionChannel:
				activeWorkers--

				if completedTask.GetStatus() == dragonscale.TaskStatusFailed {
					log.Printf("Task failed (task_id: %s, error: %v, error_context: %s)",
						completedTask.ID,
						completedTask.Error, // This should be a specific error type already
						completedTask.ErrorContext)

					errMutex.Lock()
					if execErr == nil { // Keep the first error
						// Wrap the specific task error in an ExecutorError
						execErr = dragonscale.NewExecutorError("execution", fmt.Sprintf("task %s failed", completedTask.ID), completedTask.Error)
					}
					errMutex.Unlock()

					// Check if we should retry the task (only if the DAG hasn't been cancelled by a previous error)
					errMutex.Lock()
					shouldRetry := execErr == nil && completedTask.RetryCount < e.maxRetries
					errMutex.Unlock()

					if shouldRetry {
						// ... (retry logic remains the same) ...
						log.Printf("Retrying failed task (task_id: %s, retry: %d, max_retries: %d)",
							completedTask.ID,
							completedTask.RetryCount+1,
							e.maxRetries)

						// Reset the task for retry
						completedTask.Retry()
						node := &TaskNode{
							task:     completedTask,
							priority: e.calculatePriority(completedTask, plan) - 5, // Higher priority for retries
						}
						heap.Push(&priorityQueue, node)
						continue // Continue the loop to potentially launch the retry
					} else if execErr != nil {
						log.Printf("Not retrying task %s because DAG execution has already failed or been cancelled.", completedTask.ID)
					} else {
						log.Printf("Task %s failed and reached max retries.", completedTask.ID)
						// If max retries reached and execErr was still nil, set execErr now
						errMutex.Lock()
						if execErr == nil {
							execErr = dragonscale.NewExecutorError("execution", fmt.Sprintf("task %s failed after max retries", completedTask.ID), completedTask.Error)
						}
						errMutex.Unlock()
					}
					// If not retrying, the error is now set, and the outer loop will break on the next iteration.

				} else if completedTask.GetStatus() == dragonscale.TaskStatusCompleted {
					// Check for newly ready tasks upon successful completion (only if DAG is not cancelled)
					errMutex.Lock()
					if execErr == nil {
						newlyReady := e.findNewlyReadyTasks(completedTask, plan)
						for _, nextTask := range newlyReady {
							if nextTask.GetStatus() == dragonscale.TaskStatusPending { // Ensure we only add pending tasks
								node := &TaskNode{task: nextTask, priority: e.calculatePriority(nextTask, plan)}
								heap.Push(&priorityQueue, node)
								nextTask.UpdateStatus(dragonscale.TaskStatusReady, nil)
							}
						}
					}
					errMutex.Unlock()
				}
				// Handle other completion statuses if necessary (e.g., Cancelled)

			case <-execCtx.Done():
				// Context cancelled while waiting for a task to complete
				log.Printf("DAG execution cancelled while waiting for task completion (reason: %v)", execCtx.Err())
				errMutex.Lock()
				if execErr == nil {
					// Wrap context error in ExecutorError
					execErr = dragonscale.NewExecutorError("execution", "context cancelled while waiting", execCtx.Err())
				}
				errMutex.Unlock()
				// The outer loop will break on the next iteration
			}
		}

		// Check again if we should break the outer loop due to error/cancellation
		errMutex.Lock()
		isErrorOrCancelled = execErr != nil
		errMutex.Unlock()
		if isErrorOrCancelled {
			break
		}
	}

	wg.Wait() // Wait for any remaining tasks launched in the last iteration

	// Check final status
	errMutex.Lock()
	finalError := execErr // Use the captured first error
	errMutex.Unlock()

	if finalError != nil {
		log.Printf("DAG execution finished with error: %v", finalError)
		// Ensure all remaining tasks are marked appropriately (e.g., cancelled)
		for _, task := range plan.TaskMap {
			if task.GetStatus() == dragonscale.TaskStatusPending || task.GetStatus() == dragonscale.TaskStatusReady || task.GetStatus() == dragonscale.TaskStatusRunning {
				// Use the captured finalError as the cancellation reason
				task.UpdateStatus(dragonscale.TaskStatusCancelled, finalError)
				task.SetErrorContext("DAG execution failed or was cancelled")
			}
		}
		// Return the specific ExecutorError captured earlier
		return nil, finalError
	}

	// Check if all tasks actually completed successfully
	allCompleted := true
	var firstNonCompletedTask *dragonscale.Task
	for _, task := range plan.TaskMap {
		if task.GetStatus() != dragonscale.TaskStatusCompleted {
			allCompleted = false
			if firstNonCompletedTask == nil {
				firstNonCompletedTask = task // Store the first non-completed task for error reporting
			}
			log.Printf("DAG execution finished, but task did not complete successfully (task_id: %s, status: %s, error: %v, retry_count: %d)",
				task.ID,
				task.GetStatus(),
				task.Error,
				task.RetryCount)
		}
	}

	// Log execution metrics
	execDuration := time.Since(startTime)
	log.Printf("DAG execution metrics (total_tasks: %d, successful_tasks: %d, failed_tasks: %d, total_duration: %v, total_retries: %d)",
		len(plan.Tasks),
		e.metrics.TasksSuccessful,
		e.metrics.TasksFailed,
		execDuration,
		e.metrics.TotalRetries)

	if allCompleted {
		log.Printf("DAG execution finished successfully")
	} else {
		log.Printf("DAG execution finished with non-completed tasks")
		// Return a specific ExecutorError if not all tasks completed successfully
		errMsg := "DAG execution finished, but not all tasks completed successfully"
		var underlyingErr error
		if firstNonCompletedTask != nil {
			errMsg = fmt.Sprintf("%s (e.g., task %s status: %s)", errMsg, firstNonCompletedTask.ID, firstNonCompletedTask.GetStatus())
			underlyingErr = firstNonCompletedTask.Error // Include the error of the first non-completed task, if any
		}
		return nil, dragonscale.NewExecutorError("execution", errMsg, underlyingErr)
	}

	return plan.Results, nil
}

func (e *DAGExecutor) executeTask(ctx context.Context, task *dragonscale.Task, plan *dragonscale.ExecutionPlan, wg *sync.WaitGroup, completionChan chan<- *dragonscale.Task, errMutex *sync.Mutex, execErr *error) {
	defer wg.Done()
	defer func() {
		// Update metrics before signaling completion
		e.updateTaskMetrics(task)
		// Non-blocking send to completion channel in case the main loop already exited
		select {
		case completionChan <- task:
		default:
			log.Printf("Warning: Could not send completion signal for task %s (channel likely closed or full)", task.ID)
		}
	}() // Signal completion regardless of outcome

	// Check for cancellation at the start
	select {
	case <-ctx.Done():
		// Use the specific context error, potentially wrapped later if it causes DAG failure
		task.UpdateStatus(dragonscale.TaskStatusCancelled, ctx.Err())
		task.SetErrorContext("Task cancelled before execution started")
		return
	default:
	}

	// Check if the DAG execution has already failed
	errMutex.Lock()
	dagFailed := *execErr != nil
	errMutex.Unlock()
	if dagFailed {
		task.UpdateStatus(dragonscale.TaskStatusCancelled, *execErr) // Use the existing DAG error
		task.SetErrorContext("Task cancelled because DAG execution failed")
		return
	}

	task.UpdateStatus(dragonscale.TaskStatusRunning, nil)
	log.Printf("Starting task execution (task_id: %s, tool: %s, retry_count: %d)",
		task.ID,
		task.ToolName,
		task.RetryCount)

	// Get the tool from the registry
	tool, exists := e.toolRegistry[task.ToolName]
	if !exists {
		// Use the specific ToolNotFoundError
		err := dragonscale.NewToolNotFoundError("execution", task.ToolName)
		task.UpdateStatus(dragonscale.TaskStatusFailed, err)
		task.SetErrorContext("Tool not found in registry")
		// Signal potential DAG failure (though ExecutePlan will also catch this)
		// errMutex.Lock()
		// if *execErr == nil { *execErr = dragonscale.NewExecutorError("execution", "tool not found", err) }
		// errMutex.Unlock()
		return
	}

	// Resolve arguments using the new structured approach
	resolvedArgs, err := e.resolveArguments(ctx, task, plan)
	if err != nil {
		// Use the specific ArgResolutionError (already returned by resolveArguments)
		task.UpdateStatus(dragonscale.TaskStatusFailed, err)
		task.SetErrorContext("Argument resolution failed")
		// Signal potential DAG failure
		// errMutex.Lock()
		// if *execErr == nil { *execErr = dragonscale.NewExecutorError("execution", "argument resolution failed", err) }
		// errMutex.Unlock()
		return
	}

	// Execute the tool with retry logic
	var result map[string]interface{}
	var toolErr error
	retryAttempt := task.RetryCount // Start from current retry count

	for retryAttempt <= e.maxRetries {
		// Check for cancellation before each attempt (including DAG failure)
		errMutex.Lock()
		dagFailed = *execErr != nil
		errMutex.Unlock()

		select {
		case <-ctx.Done():
			toolErr = ctx.Err() // Use context error
			task.SetErrorContext("Context cancelled during execution/retry wait")
			break // Exit retry loop
		default:
			if dagFailed {
				toolErr = *execErr // Use the DAG error that caused cancellation
				task.SetErrorContext("DAG execution failed during task execution/retry wait")
				break // Exit retry loop
			}
		}
		if toolErr != nil { // Break if cancelled or DAG failed
			break
		}

		// Execute the tool with timeout
		execTimeout := time.Minute * 5 // TODO: Make this configurable via DragonScale Config
		execCtx, cancel := context.WithTimeout(ctx, execTimeout)

		// Execute the tool directly
		result, toolErr = tool.Execute(execCtx, resolvedArgs) // tool.Execute should return specific errors
		cancel() // Cancel the timeout context immediately after execution

		if toolErr == nil {
			// Success! Break out of retry loop
			break
		}

		// Handle error type
		if execCtx.Err() == context.DeadlineExceeded {
			// Execution timed out
			task.SetErrorContext(fmt.Sprintf("Execution timed out after %v", execTimeout))
			// Keep toolErr as the timeout error (which is context.DeadlineExceeded)
			// Wrap it for clarity?
			toolErr = dragonscale.NewToolExecutionError("execution", task.ToolName, fmt.Errorf("tool execution timed out: %w", context.DeadlineExceeded))
		} else if ctx.Err() != nil && toolErr == ctx.Err() {
			// Parent context was cancelled (this might be redundant with the check at the start of the loop)
			task.SetErrorContext("Parent context cancelled during execution")
			// toolErr is already ctx.Err()
			break // No point retrying if parent context is gone
		} else if dagFailed && toolErr == *execErr {
			// DAG execution failed elsewhere
			task.SetErrorContext("DAG execution failed during task execution")
			// toolErr is already *execErr
			break // No point retrying
		} else {
			// Other execution error from the tool itself (should be specific type)
			task.SetErrorContext(fmt.Sprintf("Execution failed: %v", toolErr))
			// Ensure it's wrapped if it's not already a specific error type?
			// Adapters *should* be returning specific types. If not, wrap it here.
			// We'll assume adapters do their job for now. If toolErr is not a DragonScaleError,
			// it might indicate an issue in the adapter or tool implementation.
			// Let's wrap it defensively with ToolExecutionError if it's not already one.
			if !dragonscale.IsDragonScaleError(toolErr) {
				toolErr = dragonscale.NewToolExecutionError("execution", task.ToolName, toolErr)
			}
		}

		// Decide whether to retry
		retryAttempt++
		if retryAttempt > e.maxRetries {
			// No more retries
			break
		}

		// Log the retry
		log.Printf("Task execution failed, retrying (task_id: %s, tool: %s, error: %v, retry: %d, max_retries: %d)",
			task.ID,
			task.ToolName,
			toolErr,
			retryAttempt,
			e.maxRetries)

		// Wait before retrying, respecting context cancellation and DAG failure
		select {
		case <-ctx.Done():
			toolErr = ctx.Err() // Parent context cancelled while waiting
			task.SetErrorContext("Context cancelled while waiting to retry")
			break // Exit retry loop
		case <-time.After(e.retryDelay):
			// Check for DAG failure *after* waiting
			errMutex.Lock()
			dagFailed = *execErr != nil
			errMutex.Unlock()
			if dagFailed {
				toolErr = *execErr
				task.SetErrorContext("DAG execution failed while waiting to retry")
				break // Exit retry loop
			}
			// Ready to retry
		}
		if toolErr != nil { // Break if cancelled or DAG failed during wait
			break
		}
	}

	// Final status update based on toolErr
	if toolErr != nil {
		// Determine final status (Failed or Cancelled)
		finalStatus := dragonscale.TaskStatusFailed
		// If the error is due to cancellation or timeout, mark as Failed but use ErrorContext.
		// The task didn't *complete*, even if cancellation was external.
		// if toolErr == context.Canceled || toolErr == context.DeadlineExceeded || (execErr != nil && toolErr == *execErr) {
		// finalStatus = dragonscale.TaskStatusCancelled // Or keep Failed? Let's stick with Failed.
		// }

		// Ensure the final error stored on the task is a specific DragonScale error type.
		// If toolErr came from context or wasn't wrapped properly before, wrap it now.
		var finalTaskError error
		if dsErr, ok := toolErr.(*dragonscale.DragonScaleError); ok {
			finalTaskError = dsErr // Already a specific error
		} else if toolErr == context.Canceled || toolErr == context.DeadlineExceeded || ctx.Err() == toolErr {
			// Wrap context errors
			finalTaskError = dragonscale.NewToolExecutionError("execution", task.ToolName, fmt.Errorf("task interrupted: %w", toolErr))
		} else {
			// Wrap other potential errors defensively
			finalTaskError = dragonscale.NewToolExecutionError("execution", task.ToolName, toolErr)
		}

		task.UpdateStatus(finalStatus, finalTaskError)
		log.Printf("Task execution finished with error after %d attempts (task_id: %s, tool: %s, error: %v)",
			retryAttempt, // Use retryAttempt which reflects the number of tries
			task.ID,
			task.ToolName,
			finalTaskError) // Log the final specific error
	} else {
		// Validate result is not nil (optional, depends on tool contract)
		if result == nil {
			// Use specific InternalError
			err := dragonscale.NewInternalError("execution", "tool execution returned a nil result map", nil)
			task.UpdateStatus(dragonscale.TaskStatusFailed, err)
			task.SetErrorContext("Tool returned nil result")
			// Signal potential DAG failure
			// errMutex.Lock()
			// if *execErr == nil { *execErr = dragonscale.NewExecutorError("execution", "tool returned nil result", err) }
			// errMutex.Unlock()
			return // Return early as task failed
		}

		// Store the *entire* result map in the plan, keyed by the task ID.
		task.Result = result // Store the full result on the task itself for potential direct access/debugging
		plan.SetResult(task.ID, result) // Store the full result map in the plan's shared results
		task.UpdateStatus(dragonscale.TaskStatusCompleted, nil)
		log.Printf("Task execution completed successfully (task_id: %s, tool: %s, duration: %v)",
			task.ID,
			task.ToolName,
			task.Duration())
	}
}

// findReadyTasks identifies tasks whose dependencies are met.
func (e *DAGExecutor) findReadyTasks(plan *dragonscale.ExecutionPlan) []*dragonscale.Task {
	ready := []*dragonscale.Task{}
	plan.StateMutex.RLock() // Lock for reading task statuses
	defer plan.StateMutex.RUnlock()

	for _, task := range plan.TaskMap {
		if task.GetStatus() != dragonscale.TaskStatusPending {
			continue
		}
		dependenciesMet := true
		for _, depID := range task.DependsOn {
			depTask, exists := plan.TaskMap[depID] // Access TaskMap directly within lock
			if !exists || depTask.GetStatus() != dragonscale.TaskStatusCompleted {
				dependenciesMet = false
				break
			}
		}
		if dependenciesMet {
			ready = append(ready, task)
		}
	}
	return ready
}

// findNewlyReadyTasks finds tasks that become ready after a specific task completes.
func (e *DAGExecutor) findNewlyReadyTasks(completedTask *dragonscale.Task, plan *dragonscale.ExecutionPlan) []*dragonscale.Task {
	newlyReady := []*dragonscale.Task{}
	plan.StateMutex.RLock() // Lock for reading task statuses
	defer plan.StateMutex.RUnlock()

	// Iterate through all tasks to find those dependent on the completed one
	for _, task := range plan.TaskMap {
		if task.GetStatus() != dragonscale.TaskStatusPending {
			continue
		}
		isDependent := false
		for _, depID := range task.DependsOn {
			if depID == completedTask.ID {
				isDependent = true
				break
			}
		}
		if !isDependent {
			continue
		}

		// Check if ALL dependencies for this task are now met
		allDepsMet := true
		for _, depID := range task.DependsOn {
			depTask, exists := plan.TaskMap[depID] // Access TaskMap directly within lock
			if !exists || depTask.GetStatus() != dragonscale.TaskStatusCompleted {
				allDepsMet = false
				break
			}
		}

		if allDepsMet {
			newlyReady = append(newlyReady, task)
		}
	}
	return newlyReady
}

// resolveArguments resolves arguments based on the Task's Args map and dependency results.
func (e *DAGExecutor) resolveArguments(ctx context.Context, task *dragonscale.Task, plan *dragonscale.ExecutionPlan) (map[string]interface{}, error) {
	resolved := make(map[string]interface{}, len(task.Args))

	for argName, argSource := range task.Args {
		// Determine if the argument is required (default to true for backward compatibility)
		isRequired := true
		if argSource.SourceMap != nil {
			if req, exists := argSource.SourceMap["required"].(bool); exists {
				isRequired = req
			}
		} else {
			isRequired = argSource.Required
		}

		// Try to resolve the argument
		var value interface{}
		var err error

		switch argSource.Type {
		case dragonscale.ArgumentSourceLiteral:
			// Use the literal value directly
			value = argSource.Value

		case dragonscale.ArgumentSourceDependencyOutput:
			// Fetch the result from the dependency task
			value, err = e.resolveDependencyOutput(ctx, task, plan, argName, argSource)

		case dragonscale.ArgumentSourceExpression:
			// For future implementation - evaluate expressions
			err = dragonscale.NewInternalError("execution", fmt.Sprintf(
				"expression-based argument sources are not yet implemented (argument '%s' for task '%s')",
				argName, task.ID), nil)

		case dragonscale.ArgumentSourceMerged:
			// For future implementation - merge multiple sources
			err = dragonscale.NewInternalError("execution", fmt.Sprintf(
				"merged argument sources are not yet implemented (argument '%s' for task '%s')",
				argName, task.ID), nil)

		default:
			err = dragonscale.NewArgResolutionError("execution", task.ID, argName, fmt.Errorf(
				"unknown argument source type '%s'", argSource.Type))
		}

		// Handle resolution errors
		if err != nil {
			// If not required and has default, use the default value
			if !isRequired && argSource.DefaultValue != nil {
				resolved[argName] = argSource.DefaultValue
				continue
			}

			// Otherwise if required, return the error
			if isRequired {
				return nil, err
			}

			// If not required and no default, skip this argument
			continue
		}

		// Store the resolved value
		resolved[argName] = value
	}

	return resolved, nil
}

// resolveDependencyOutput resolves an argument that depends on output from another task.
func (e *DAGExecutor) resolveDependencyOutput(ctx context.Context, task *dragonscale.Task, 
	plan *dragonscale.ExecutionPlan, argName string, argSource dragonscale.ArgumentSource) (interface{}, error) {
	
	depTaskID := argSource.DependencyTaskID
	outputFieldName := argSource.OutputFieldName

	// Fetch the dependent task to ensure it completed
	plan.StateMutex.RLock()
	depTask, exists := plan.TaskMap[depTaskID]
	plan.StateMutex.RUnlock()

	if !exists {
		return nil, dragonscale.NewArgResolutionError("execution", task.ID, argName, fmt.Errorf(
			"dependency task '%s' not found in plan", depTaskID))
	}

	// Only completed tasks can provide valid results
	if depTask.GetStatus() != dragonscale.TaskStatusCompleted {
		return nil, dragonscale.NewInternalError("execution", fmt.Sprintf(
			"dependency task '%s' for argument '%s' has status %s, expected %s",
			depTaskID, argName, depTask.GetStatus(), dragonscale.TaskStatusCompleted), nil)
	}

	// Fetch the full result map of the dependency task from the plan's results
	depResultRaw, ok := plan.GetResult(depTaskID)
	if !ok {
		return nil, dragonscale.NewInternalError("execution", fmt.Sprintf(
			"failed to find result map for completed task '%s' needed for argument '%s'",
			depTaskID, argName), nil)
	}

	// Handle different result types based on the tool's output structure
	switch result := depResultRaw.(type) {
	case map[string]interface{}:
		// Standard map result format
		// If outputFieldName is empty or "*", return the entire result map
		if outputFieldName == "" || outputFieldName == "*" {
			return result, nil
		}
		
		// Check if the requested field exists
		value, exists := result[outputFieldName]
		if !exists {
			return nil, dragonscale.NewArgResolutionError("execution", task.ID, argName, fmt.Errorf(
				"output field '%s' not found in result map of dependency task '%s'",
				outputFieldName, depTaskID))
		}
		return value, nil
		
	case []interface{}:
		// Array result format
		// If outputFieldName is empty or "*", return the entire array
		if outputFieldName == "" || outputFieldName == "*" {
			return result, nil
		}
		
		// Try to interpret outputFieldName as an index if it's numeric
		index, err := strconv.Atoi(outputFieldName)
		if err != nil || index < 0 || index >= len(result) {
			return nil, dragonscale.NewArgResolutionError("execution", task.ID, argName, fmt.Errorf(
				"invalid array index '%s' for result of dependency task '%s' (array length: %d)",
				outputFieldName, depTaskID, len(result)))
		}
		return result[index], nil
		
	default:
		// Simple scalar result
		// If outputFieldName is empty, return the raw value
		if outputFieldName == "" || outputFieldName == "*" {
			return result, nil
		}
		
		return nil, dragonscale.NewArgResolutionError("execution", task.ID, argName, fmt.Errorf(
			"cannot extract field '%s' from non-map result of dependency task '%s' (type: %T)",
			outputFieldName, depTaskID, result))
	}
}

// calculatePriority assigns a priority to a task (lower is higher priority).
// Placeholder implementation - a real implementation might use Critical Path Method.
func (e *DAGExecutor) calculatePriority(task *dragonscale.Task, plan *dragonscale.ExecutionPlan) int {
	// Simple heuristic: prioritize tasks with no dependencies.
	if len(task.DependsOn) == 0 {
		return 0
	}
	// TODO: Implement Critical Path Method (CPM) or other heuristics.
	// For now, all dependent tasks have the same lower priority.
	return 1
}

// resetMetrics resets the execution metrics for a new run.
func (e *DAGExecutor) resetMetrics() {
	e.metrics.mu.Lock()
	defer e.metrics.mu.Unlock()

	e.metrics = ExecutorMetrics{
		ShortestTaskTime: time.Hour * 24, // Set to a large value initially
	}
}

// updateTaskMetrics updates metrics based on a completed or failed task
func (e *DAGExecutor) updateTaskMetrics(task *dragonscale.Task) {
	duration := task.Duration()

	e.metrics.mu.Lock()
	defer e.metrics.mu.Unlock()

	// Only count metrics if the task actually ran (or attempted to run)
	if task.GetStatus() == dragonscale.TaskStatusCompleted || task.GetStatus() == dragonscale.TaskStatusFailed || task.GetStatus() == dragonscale.TaskStatusCancelled {
		e.metrics.TasksExecuted++ // Count executed/attempted tasks
		e.metrics.TotalDuration += duration
		e.metrics.TotalRetries += task.RetryCount // Total retries across all attempts for this task instance

		if duration > e.metrics.LongestTaskTime {
			e.metrics.LongestTaskTime = duration
		}

		if duration < e.metrics.ShortestTaskTime && duration > 0 {
			e.metrics.ShortestTaskTime = duration
		}

		if task.GetStatus() == dragonscale.TaskStatusCompleted {
			e.metrics.TasksSuccessful++
		} else if task.GetStatus() == dragonscale.TaskStatusFailed {
			e.metrics.TasksFailed++
		}
		// Note: Cancelled tasks are counted in TasksExecuted but not Successful/Failed
	}
}
