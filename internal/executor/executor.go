// filepath: /mnt/dragonnet/common/Projects/Personal/General/genkit/internal/executor/executor.go
package executor

import (
	"context"
	"fmt"
	"log"
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
	var execErr error
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
			// Cancel all running tasks and break the loop
			errMutex.Lock()
			if execErr == nil {
				execErr = execCtx.Err()
			}
			errMutex.Unlock()
			break // Exit the outer loop
		default:
			// Continue execution
		}

		// Check if we should break due to cancellation detected above
		errMutex.Lock()
		isCancelled := execErr != nil
		errMutex.Unlock()
		if isCancelled {
			break
		}

		// Launch tasks while workers are available and tasks are ready
		for priorityQueue.Len() > 0 && activeWorkers < e.maxWorkers {
			taskNode := heap.Pop(&priorityQueue).(*TaskNode)
			task := taskNode.task

			if task.GetStatus() != dragonscale.TaskStatusReady { // Skip if already picked up or status changed
				continue
			}

			activeWorkers++
			wg.Add(1)
			// Pass the executor context (execCtx) to the task execution goroutine
			go e.executeTask(execCtx, task, plan, &wg, completionChannel, &errMutex, &execErr)
		}

		// Wait for a task to complete if no more tasks can be launched immediately or if we need to check for errors
		if activeWorkers == e.maxWorkers || (priorityQueue.Len() == 0 && activeWorkers > 0) || isCancelled {
			select {
			case completedTask := <-completionChannel:
				activeWorkers--

				if completedTask.GetStatus() == dragonscale.TaskStatusFailed {
					log.Printf("Task failed (task_id: %s, error: %v, error_context: %s)",
						completedTask.ID,
						completedTask.Error,
						completedTask.ErrorContext)

					errMutex.Lock()
					if execErr == nil { // Keep the first error
						execErr = fmt.Errorf("task %s failed: %w", completedTask.ID, completedTask.Error)
					}
					errMutex.Unlock()

					// Check if we should retry the task (only if the DAG hasn't been cancelled)
					errMutex.Lock()
					shouldRetry := execErr == nil && completedTask.RetryCount < e.maxRetries
					errMutex.Unlock()

					if shouldRetry {
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
						log.Printf("Not retrying task %s because DAG execution has failed or been cancelled.", completedTask.ID)
					} else {
						log.Printf("Task %s failed and reached max retries.", completedTask.ID)
					}
					// If not retrying, the error is already set, and the loop will eventually break.

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
					execErr = execCtx.Err()
				}
				errMutex.Unlock()
				break // Break the inner select
			}
		}

		// Check again if we should break the outer loop due to error/cancellation
		errMutex.Lock()
		isErrorOrCancelled := execErr != nil
		errMutex.Unlock()
		if isErrorOrCancelled {
			break
		}
	}

	wg.Wait() // Wait for any remaining tasks launched in the last iteration

	// Check final status
	errMutex.Lock()
	finalError := execErr
	errMutex.Unlock()

	if finalError != nil {
		log.Printf("DAG execution finished with error: %v", finalError)
		// Ensure all remaining tasks are marked appropriately (e.g., cancelled)
		for _, task := range plan.TaskMap {
			if task.GetStatus() == dragonscale.TaskStatusPending || task.GetStatus() == dragonscale.TaskStatusReady || task.GetStatus() == dragonscale.TaskStatusRunning {
				task.UpdateStatus(dragonscale.TaskStatusCancelled, finalError)
				task.SetErrorContext("DAG execution failed or was cancelled")
			}
		}
		return nil, finalError
	}

	allCompleted := true
	for _, task := range plan.TaskMap {
		if task.GetStatus() != dragonscale.TaskStatusCompleted {
			allCompleted = false
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
		// Return an error if not all tasks completed successfully
		return nil, fmt.Errorf("DAG execution finished, but not all tasks completed successfully")
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
		task.UpdateStatus(dragonscale.TaskStatusCancelled, ctx.Err())
		task.SetErrorContext("Task cancelled before execution started")
		return
	default:
	}

	task.UpdateStatus(dragonscale.TaskStatusRunning, nil)
	log.Printf("Starting task execution (task_id: %s, tool: %s, retry_count: %d)",
		task.ID,
		task.ToolName,
		task.RetryCount)

	// Get the tool from the registry
	tool, exists := e.toolRegistry[task.ToolName]
	if !exists {
		err := fmt.Errorf("tool '%s' not found in registry", task.ToolName)
		task.UpdateStatus(dragonscale.TaskStatusFailed, err)
		task.SetErrorContext("Tool not found in registry")
		return
	}

	// Resolve arguments using the new structured approach
	resolvedArgs, err := e.resolveArguments(ctx, task, plan)
	if err != nil {
		err := fmt.Errorf("argument resolution failed for task %s: %w", task.ID, err)
		task.UpdateStatus(dragonscale.TaskStatusFailed, err)
		task.SetErrorContext("Argument resolution failed")
		return
	}

	// Execute the tool with retry logic
	var result map[string]interface{}
	var toolErr error
	retryAttempt := task.RetryCount // Start from current retry count

	for retryAttempt <= e.maxRetries {
		// Check for cancellation before each attempt
		select {
		case <-ctx.Done():
			toolErr = ctx.Err() // Use context error
			task.SetErrorContext("Context cancelled during execution/retry wait")
			break // Exit retry loop
		default:
		}
		if toolErr != nil { // Break if cancelled
			break
		}

		// Execute the tool with timeout
		execTimeout := time.Minute * 5 // TODO: Make this configurable via DragonScale Config
		execCtx, cancel := context.WithTimeout(ctx, execTimeout)

		// Execute the tool directly
		result, toolErr = tool.Execute(execCtx, resolvedArgs)
		cancel() // Cancel the timeout context immediately after execution

		if toolErr == nil {
			// Success! Break out of retry loop
			break
		}

		// Handle error type
		if execCtx.Err() == context.DeadlineExceeded {
			// Execution timed out
			task.SetErrorContext(fmt.Sprintf("Execution timed out after %v", execTimeout))
			// Keep toolErr as the timeout error
		} else if ctx.Err() != nil {
			// Parent context was cancelled (this might be redundant with the check at the start of the loop)
			task.SetErrorContext("Parent context cancelled during execution")
			toolErr = ctx.Err() // Ensure we store the cancellation error
			break // No point retrying if parent context is gone
		} else {
			// Other execution error
			task.SetErrorContext(fmt.Sprintf("Execution failed: %v", toolErr))
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

		// Wait before retrying, respecting context cancellation
		select {
		case <-ctx.Done():
			toolErr = ctx.Err() // Parent context cancelled while waiting
			task.SetErrorContext("Context cancelled while waiting to retry")
			break // Exit retry loop
		case <-time.After(e.retryDelay):
			// Ready to retry
		}
		if toolErr != nil { // Break if cancelled during wait
			break
		}
	}

	// Final status update based on toolErr
	if toolErr != nil {
		// Determine final status (Failed or Cancelled)
		finalStatus := dragonscale.TaskStatusFailed
		if toolErr == context.Canceled || toolErr == context.DeadlineExceeded {
			// If the error is due to cancellation or timeout, mark as Cancelled?
			// Or keep as Failed but use ErrorContext? Let's keep Failed for now.
			// finalStatus = dragonscale.TaskStatusCancelled
		}

		task.UpdateStatus(finalStatus, toolErr)
		log.Printf("Task execution finished with error after %d attempts (task_id: %s, tool: %s, error: %v)",
			retryAttempt, // Use retryAttempt which reflects the number of tries
			task.ID,
			task.ToolName,
			toolErr)
	} else {
		// Validate result is not nil (optional, depends on tool contract)
		if result == nil {
			err := fmt.Errorf("tool execution returned a nil result map")
			task.UpdateStatus(dragonscale.TaskStatusFailed, err)
			task.SetErrorContext("Tool returned nil result")
			return
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
		switch argSource.Type {
		case dragonscale.ArgumentSourceLiteral:
			// Use the literal value directly
			// TODO: Consider type conversion if schema specifies a non-string type for the literal?
			// For now, assume literals are passed as intended type (often string).
			resolved[argName] = argSource.Value

		case dragonscale.ArgumentSourceDependencyOutput:
			// Fetch the result from the dependency task
			depTaskID := argSource.DependencyTaskID
			outputFieldName := argSource.OutputFieldName

			// Fetch the dependent task to ensure it completed
			// No need for GetTask method here, access TaskMap directly after locking
			plan.StateMutex.RLock()
			depTask, exists := plan.TaskMap[depTaskID]
			plan.StateMutex.RUnlock()

			if !exists {
				return nil, fmt.Errorf("dependency task '%s' for argument '%s' not found in plan", depTaskID, argName)
			}

			// Only completed tasks can provide valid results
			if depTask.GetStatus() != dragonscale.TaskStatusCompleted {
				// Wait for the dependency to complete? No, the scheduler ensures this.
				// If we are here, the dependency should have completed.
				// This indicates a potential logic error in the scheduler or status updates.
				return nil, fmt.Errorf(
					"internal error: dependency task '%s' for argument '%s' has status %s, expected %s",
					depTaskID,
					argName,
					depTask.GetStatus(),
					dragonscale.TaskStatusCompleted,
				)
			}

			// Fetch the full result map of the dependency task from the plan's results
			depResultRaw, ok := plan.GetResult(depTaskID) // Use safe GetResult method
			if !ok {
				// If it completed but result wasn't found, that's an internal error
				return nil, fmt.Errorf(
					"internal error: failed to find result map for completed task '%s' needed for argument '%s'",
					depTaskID,
					argName,
				)
			}

			// Assert that the result is a map[string]interface{}
			depResultMap, ok := depResultRaw.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf(
					"result for dependency task '%s' is not of expected type map[string]interface{} (got %T)",
					depTaskID,
					depResultRaw,
				)
			}

			// Extract the specific field needed for the argument
			value, fieldExists := depResultMap[outputFieldName]
			if !fieldExists {
				// Check if the user intended to pass the *entire* result map
				if outputFieldName == "" || outputFieldName == "*" { // Convention for whole map
					resolved[argName] = depResultMap
				} else {
					return nil, fmt.Errorf(
						"output field '%s' not found in result map of dependency task '%s' for argument '%s'",
						outputFieldName,
						depTaskID,
						argName,
					)
				}
			} else {
				resolved[argName] = value
			}

		default:
			return nil, fmt.Errorf("unknown argument source type '%s' for argument '%s'", argSource.Type, argName)
		}
	}

	return resolved, nil
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
