// filepath: /mnt/dragonnet/common/Projects/Personal/General/genkit/internal/executor/executor.go
package executor

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"container/heap"

	"github.com/ZanzyTHEbar/dragonscale-genkit"
	"github.com/ZanzyTHEbar/errbuilder-go"
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

// NewDAGExecutor creates a new executor with the given options.
func NewDAGExecutor(toolRegistry map[string]dragonscale.Tool, maxWorkers int, options ...ExecutorOption) *DAGExecutor {
	ctx, cancel := context.WithCancel(context.Background())

	e := &DAGExecutor{
		toolRegistry: toolRegistry,
		maxWorkers:   maxWorkers,
		maxRetries:   3,               // Default to 3 retries
		retryDelay:   time.Second * 2, // Default 2-second delay
		ctx:          ctx,
		cancelFunc:   cancel,
	}

	// Apply options
	for _, option := range options {
		option(e)
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
			break
		default:
			// Continue execution
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
			go e.executeTask(ctx, task, plan, &wg, completionChannel, &errMutex, &execErr)
		}

		// Wait for a task to complete if no more tasks can be launched immediately
		if activeWorkers == e.maxWorkers || (priorityQueue.Len() == 0 && activeWorkers > 0) {
			completedTask := <-completionChannel
			activeWorkers--

			if completedTask.GetStatus() == dragonscale.TaskStatusFailed {
				// Handle potential replanning trigger here
				log.Printf("Task failed, stopping DAG execution for now (task_id: %s, error: %v, error_context: %s)",
					completedTask.ID,
					completedTask.Error,
					completedTask.ErrorContext)

				errMutex.Lock()
				if execErr == nil { // Keep the first error
					execErr = fmt.Errorf("task %s failed: %w", completedTask.ID, completedTask.Error)
				}
				errMutex.Unlock()

				// Check if we should retry the task
				if completedTask.RetryCount < e.maxRetries {
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
					continue
				}

				//TODO: we could flag specific tasks for replanning or implement a circuit breaker pattern for frequent failures
			}

			// Check for newly ready tasks upon completion
			newlyReady := e.findNewlyReadyTasks(completedTask, plan)
			for _, nextTask := range newlyReady {
				if nextTask.GetStatus() == dragonscale.TaskStatusPending { // Ensure we only add pending tasks
					node := &TaskNode{task: nextTask, priority: e.calculatePriority(nextTask, plan)}
					heap.Push(&priorityQueue, node)
					nextTask.UpdateStatus(dragonscale.TaskStatusReady, nil)
				}
			}
		}

		// If an error occurred, break the loop after draining current tasks
		errMutex.Lock()
		isError := execErr != nil
		errMutex.Unlock()
		if isError && activeWorkers == 0 { // Stop adding new tasks if an error occurred and wait for running ones to finish
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
		return nil, finalError
	}

	allCompleted := true
	for _, task := range plan.TaskMap {
		if task.GetStatus() != dragonscale.TaskStatusCompleted {
			allCompleted = false
			log.Printf("DAG execution finished, but not all tasks completed (task_id: %s, status: %s, error: %v, retry_count: %d)",
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
		// TODO: Decide if this constitutes an error
	}

	return plan.Results, nil
}

func (e *DAGExecutor) executeTask(ctx context.Context, task *dragonscale.Task, plan *dragonscale.ExecutionPlan, wg *sync.WaitGroup, completionChan chan<- *dragonscale.Task, errMutex *sync.Mutex, execErr *error) {
	defer wg.Done()
	defer func() {
		// Update metrics before signaling completion
		e.updateTaskMetrics(task)
		completionChan <- task
	}() // Signal completion regardless of outcome

	// Check for cancellation using errbuilder
	if err := errbuilder.WrapIfContextDone(ctx, nil); err != nil {
		task.UpdateStatus(dragonscale.TaskStatusCancelled, err)
		task.SetErrorContext("Task cancelled due to context cancellation")
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
		var errs errbuilder.ErrorMap
		errs.Set("tool", task.ToolName)
		errs.Set("task_id", task.ID)
		err := errbuilder.NotFoundErr(errbuilder.NewErrDetails(errs).Errors)
		task.UpdateStatus(dragonscale.TaskStatusFailed, err)
		task.SetErrorContext("Tool not found in registry")
		return
	}
	
	// Simple validation - no need to use assert-lib for this case
	if tool == nil {
		var errs errbuilder.ErrorMap
		errs.Set("validation", "Tool must not be nil")
		err := errbuilder.ValidationErr(errbuilder.NewErrDetails(errs).Errors)
		task.UpdateStatus(dragonscale.TaskStatusFailed, err)
		task.SetErrorContext("Tool validation failed")
		return
	}

	// Resolve arguments
	resolvedArgs, err := e.resolveArguments(ctx, task.Args, plan)
	if err != nil {
		var errs errbuilder.ErrorMap
		errs.Set("task_id", task.ID)
		errs.Set("cause", err.Error())
		err := errbuilder.GenericErr("argument resolution failed", err)
		task.UpdateStatus(dragonscale.TaskStatusFailed, err)
		task.SetErrorContext("Argument resolution failed")
		return
	}

	// Execute the tool with retry logic
	var result map[string]interface{}
	retryAttempt := 0
	maxRetries := e.maxRetries

	// If this is already a retry (from a previous failure), account for that
	if task.RetryCount > 0 {
		retryAttempt = task.RetryCount
		maxRetries = e.maxRetries + task.RetryCount // Adjust max retries based on previous attempts
	}

	for retryAttempt <= maxRetries {
		// Execute the tool with timeout
		var execErr error
		execTimeout := time.Minute * 5 // Default timeout - could be made configurable

		// Create a timeout context for the execution
		execCtx, cancel := context.WithTimeout(ctx, execTimeout)
		defer cancel()

		// Execute the tool directly
		result, err = tool.Execute(execCtx, resolvedArgs)

		if err == nil {
			// Success! Break out of retry loop
			break
		}

		// Handle based on error type
		if ctx.Err() != nil {
			// Parent context was cancelled, no point retrying
			var errs errbuilder.ErrorMap
			errs.Set("cause", err.Error())
			errs.Set("context_error", ctx.Err().Error())
			execErr = errbuilder.GenericErr("context cancelled", err)
			task.SetErrorContext("Parent context cancelled during execution")
			break
		} else if execCtx.Err() != nil && execCtx.Err() == context.DeadlineExceeded {
			// Execution timed out
			var errs errbuilder.ErrorMap
			errs.Set("task_id", task.ID)
			errs.Set("tool", task.ToolName)
			errs.Set("timeout", execTimeout.String())
			errs.Set("cause", err.Error())
			execErr = errbuilder.GenericErr("execution timeout", err)
			task.SetErrorContext("Execution timed out")
		} else {
			// Other error
			var errs errbuilder.ErrorMap
			errs.Set("task_id", task.ID)
			errs.Set("tool", task.ToolName)
			errs.Set("cause", err.Error())
			execErr = errbuilder.GenericErr("tool execution", err)
			task.SetErrorContext(fmt.Sprintf("Execution failed: %v", err))
		}

		// Decide whether to retry
		retryAttempt++
		if retryAttempt > maxRetries {
			// No more retries
			break
		}

		// Log the retry
		log.Printf("Task execution failed, retrying (task_id: %s, tool: %s, error: %v, retry: %d, max_retries: %d)",
			task.ID,
			task.ToolName,
			execErr,
			retryAttempt,
			maxRetries)

		// Wait before retrying
		select {
		case <-ctx.Done():
			// Parent context was cancelled while waiting to retry
			execErr = ctx.Err()
			task.SetErrorContext("Context cancelled while waiting to retry")
			break
		case <-time.After(e.retryDelay):
			// Ready to retry
		}
	}

	if err != nil {
		// Create a structured error with details about the failure
		var errs errbuilder.ErrorMap
		errs.Set("task_id", task.ID)
		errs.Set("tool", task.ToolName)
		errs.Set("retry_count", retryAttempt)
		errs.Set("max_retries", maxRetries)
		finalErr := errbuilder.GenericErr("task execution failed after retries", err)
		task.UpdateStatus(dragonscale.TaskStatusFailed, finalErr)
		log.Printf("Task execution failed after retries (task_id: %s, tool: %s, error: %v, retries: %d)",
			task.ID,
			task.ToolName,
			finalErr,
			retryAttempt)
	} else {
		if result == nil {
			var errs errbuilder.ErrorMap
			errs.Set("validation", "Tool execution result cannot be nil")
			err := errbuilder.ValidationErr(errbuilder.NewErrDetails(errs).Errors)
			task.UpdateStatus(dragonscale.TaskStatusFailed, err)
			task.SetErrorContext("Result validation failed")
			return
		}
		
		// Store result(s) - assuming a standard output key for simplicity, might need refinement
		outputResult := result["output"] // Adjust based on actual Tool output structure
		task.Result = outputResult
		plan.SetResult(task.ID+"_output", outputResult) // Store result with a specific key convention
		task.UpdateStatus(dragonscale.TaskStatusCompleted, nil)
		log.Printf("Task execution completed (task_id: %s, tool: %s, result: %v, duration: %v)",
			task.ID,
			task.ToolName,
			outputResult,
			task.Duration())
	}
}

// findReadyTasks identifies tasks whose dependencies are met.
func (e *DAGExecutor) findReadyTasks(plan *dragonscale.ExecutionPlan) []*dragonscale.Task {
	ready := []*dragonscale.Task{}
	for _, task := range plan.TaskMap {
		if task.GetStatus() != dragonscale.TaskStatusPending {
			continue
		}
		dependenciesMet := true
		for _, depID := range task.DependsOn {
			depTask, exists := plan.GetTask(depID)
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
			depTask, exists := plan.GetTask(depID)
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

// resolveArguments replaces placeholders like $taskID_output with actual results.
func (e *DAGExecutor) resolveArguments(ctx context.Context, args []string, plan *dragonscale.ExecutionPlan) (map[string]interface{}, error) {
	resolved := make(map[string]interface{}, len(args))
	// Regex to find placeholders like $taskID_output or ${taskID_output}
	placeholderRegex := regexp.MustCompile(`\$\{?(\w+)_output\}?`) // Matches $taskID_output or ${taskID_output}

	for i, arg := range args {
		matches := placeholderRegex.FindAllStringSubmatch(arg, -1)

		// If no placeholders found, pass the argument as is
		if len(matches) == 0 {
			resolved[fmt.Sprintf("arg%d", i)] = arg
			continue
		}

		replacedArg := arg
		for _, match := range matches {
			if len(match) < 2 {
				continue // Should not happen with the regex
			}
			placeholder := match[0]
			dependencyTaskID := match[1]
			resultKey := dependencyTaskID + "_output" // Construct the key used in SetResult

			// Fetch the dependent task to check its status
			depTask, exists := plan.GetTask(dependencyTaskID)
			if !exists {
				return nil, fmt.Errorf("dependency task '%s' not found in plan", dependencyTaskID)
			}

			// Only completed tasks can provide valid results
			if depTask.GetStatus() != dragonscale.TaskStatusCompleted {
				return nil, fmt.Errorf(
					"dependency '%s' required for arg '%s' has not completed successfully (status: %s)",
					dependencyTaskID,
					arg,
					depTask.GetStatus())
			}

			// Fetch the result from the plan's results map
			value, ok := plan.GetResult(resultKey)
			if !ok {
				// If it completed but result wasn't found, that's an internal error
				return nil, fmt.Errorf(
					"failed to find result '%s' from completed task '%s' needed for arg '%s'",
					resultKey,
					dependencyTaskID,
					arg)
			}

			// If the entire argument is the placeholder, assign the value directly
			if arg == placeholder {
				resolved[fmt.Sprintf("arg%d", i)] = value // Assign directly if arg is just the placeholder
				replacedArg = ""                          // Mark as handled
				break                                     // Assume only one placeholder if it matches the whole arg
			} else {
				// Otherwise, do string replacement (best effort)
				// Convert value to string if it's not already
				var valueStr string
				switch v := value.(type) {
				case string:
					valueStr = v
				case []byte:
					valueStr = string(v)
				default:
					valueStr = fmt.Sprintf("%v", v)
				}
				replacedArg = strings.Replace(replacedArg, placeholder, valueStr, 1)
			}
		}

		// If we did string replacements and haven't set the value directly
		if replacedArg != "" {
			resolved[fmt.Sprintf("arg%d", i)] = replacedArg
		}
	}

	return resolved, nil
}

// calculatePriority assigns a priority to a task (lower is higher priority).
// Placeholder implementation - a real implementation might use Critical Path Method.
func (e *DAGExecutor) calculatePriority(task *dragonscale.Task, plan *dragonscale.ExecutionPlan) int {
	// Simple heuristic: prioritize tasks with more downstream dependencies?
	// Or prioritize based on estimated duration (if available)?
	// Placeholder: Tasks with no dependencies get higher priority (priority 0).
	// Tasks with dependencies get priority 1.
	if len(task.DependsOn) == 0 {
		return 0
	}
	// TODO: Could implement CPM here by traversing the graph
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

// updateTaskMetrics updates metrics based on a completed task
func (e *DAGExecutor) updateTaskMetrics(task *dragonscale.Task) {
	duration := task.Duration()

	e.metrics.mu.Lock()
	defer e.metrics.mu.Unlock()

	e.metrics.TasksExecuted++
	e.metrics.TotalDuration += duration
	e.metrics.TotalRetries += task.RetryCount

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
}
