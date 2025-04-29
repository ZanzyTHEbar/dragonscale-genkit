// filepath: /mnt/dragonnet/common/Projects/Personal/General/genkit/internal/executor/executor.go
package executor

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"container/heap"

	"github.com/Knetic/govaluate"
	"github.com/ZanzyTHEbar/dragonscale-genkit"
	"github.com/sourcegraph/conc/pool"
	"golang.org/x/sync/errgroup"
)

// Define TaskNode for the priority queue
type TaskNode struct {
	task     *dragonscale.Task
	priority int // Lower value means higher priority (e.g., based on critical path)
	index    int // Index in the heap
}

// DAGExecutor handles the execution of a task graph.
type DAGExecutor struct {
	toolRegistry map[string]dragonscale.Tool
	maxWorkers   int           // Max concurrent tasks
	maxRetries   int           // Max retries per task
	retryDelay   time.Duration // Delay between retries
	execTimeout  time.Duration // Per-task execution timeout

	// Statistics and metrics
	metrics ExecutorMetrics

	// Controls for graceful shutdown
	ctx        context.Context
	cancelFunc context.CancelFunc
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

// WithExecTimeout sets the per-task execution timeout.
func WithExecTimeout(timeout time.Duration) ExecutorOption {
	return func(e *DAGExecutor) {
		e.execTimeout = timeout
	}
}

// NewExecutor creates a new executor with default settings.
// It requires the tool registry to be passed during initialization.
func NewExecutor(toolRegistry map[string]dragonscale.Tool, options ...ExecutorOption) *DAGExecutor {
	ctx, cancel := context.WithCancel(context.Background())

	e := &DAGExecutor{
		toolRegistry: toolRegistry,    // Store the provided tool registry
		maxWorkers:   5,               // Default max workers
		maxRetries:   3,               // Default to 3 retries
		retryDelay:   time.Second * 2, // Default 2-second delay
		execTimeout:  time.Minute * 5, // Default 5-minute timeout
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
		// TODO: Depending on requirements, you might return an error here:
		// return nil, fmt.Errorf("tool registry cannot be nil or empty")
	}

	return e
}

// ExecutePlan runs the execution plan.
func (e *DAGExecutor) ExecutePlan(ctx context.Context, plan *dragonscale.ExecutionPlan) (map[string]interface{}, error) {
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	startTime := time.Now()
	log.Printf("Starting DAG execution (total_tasks: %d)", len(plan.Tasks))

	priorityQueue := make(PriorityQueue, 0)
	heap.Init(&priorityQueue)

	execErrChan := make(chan error, 1) // buffered to avoid goroutine leak
	completionChannel := make(chan *dragonscale.Task, len(plan.Tasks))

	e.resetMetrics()

	readyTasks := e.findReadyTasks(plan)
	for _, task := range readyTasks {
		priority, err := e.calculatePriority(task, plan)
		if (err != nil) {
			log.Printf("Error calculating priority for task %s: %v", task.ID, err)
			continue
		}
		node := &TaskNode{task: task, priority: priority}
		heap.Push(&priorityQueue, node)
		task.UpdateStatus(dragonscale.TaskStatusReady, nil)
	}

	g, gctx := errgroup.WithContext(execCtx)
	workerPool := pool.New().WithMaxGoroutines(e.maxWorkers)

	for priorityQueue.Len() > 0 {
		if gctx.Err() != nil {
			break
		}
		taskNode := heap.Pop(&priorityQueue).(*TaskNode)
		task := taskNode.task
		if task.GetStatus() != dragonscale.TaskStatusReady {
			continue
			}
		workerPool.Go(func() {
			e.executeTaskWithCancel(gctx, task, plan, completionChannel, execErrChan, cancel)
		})
	}

	workerPool.Wait()
	g.Wait() // Wait for errgroup (not strictly needed here, but for symmetry)
	close(completionChannel)

	var execErr error
	select {
	case err := <-execErrChan:
		execErr = err
	default:
		execErr = nil
	}

	// Handle completion and errors as before
	// Check final status
	if execErr != nil {
		log.Printf("DAG execution finished with error: %v", execErr)
		// Ensure all remaining tasks are marked appropriately (e.g., cancelled)
		plan.StateMutex.Lock()
		for _, task := range plan.TaskMap {
			if task.GetStatus() == dragonscale.TaskStatusPending || task.GetStatus() == dragonscale.TaskStatusReady || task.GetStatus() == dragonscale.TaskStatusRunning {
				// Use the captured finalError as the cancellation reason
				task.UpdateStatus(dragonscale.TaskStatusCancelled, execErr)
				task.SetErrorContext("DAG execution failed or was cancelled")
			}
		}
		plan.StateMutex.Unlock()
		// Return the specific ExecutorError captured earlier
		return nil, execErr

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

// Refactored: handleTaskCompletion now takes a pointer to the priorityQueue and a **error for execErr
func (e *DAGExecutor) handleTaskCompletion(priorityQueue *PriorityQueue, completedTask *dragonscale.Task, plan *dragonscale.ExecutionPlan, errMutex *sync.Mutex, execErr *error) {
	e.updateTaskMetrics(completedTask)
	switch completedTask.GetStatus() {
	case dragonscale.TaskStatusCancelled:
		// TODO: No-op for now, could log if needed
	case dragonscale.TaskStatusFailed:
		log.Printf("Task failed (task_id: %s, error: %v, error_context: %s)",
			completedTask.ID,
			completedTask.Error,
			completedTask.ErrorContext)
		errMutex.Lock()
		if *execErr == nil {
			*execErr = dragonscale.NewExecutorError("execution", fmt.Sprintf("task %s failed", completedTask.ID), completedTask.Error)
		}
		errMutex.Unlock()
		// Check if we should retry the task (only if the DAG hasn't been cancelled by a previous error)
		errMutex.Lock()
		shouldRetry := *execErr == nil && completedTask.RetryCount < e.maxRetries
		errMutex.Unlock()
		if shouldRetry {
			log.Printf("Retrying failed task (task_id: %s, retry: %d, max_retries: %d)",
				completedTask.ID,
				completedTask.RetryCount+1,
				e.maxRetries)
			completedTask.Retry()
			priority, err := e.calculatePriority(completedTask, plan)
			if err != nil {
				log.Printf("Error calculating priority for retry of task %s: %v", completedTask.ID, err)
				return
			}
			node := &TaskNode{
				task:     completedTask,
				priority: priority - 5, // Higher priority for retries
			}
			heap.Push(priorityQueue, node)
			completedTask.UpdateStatus(dragonscale.TaskStatusReady, nil)
			completedTask.SetErrorContext("")
		} else if *execErr != nil {
			log.Printf("Not retrying task %s because DAG execution has already failed or been cancelled.", completedTask.ID)
		} else {
			log.Printf("Task %s failed and reached max retries.", completedTask.ID)
			errMutex.Lock()
			if *execErr == nil {
				*execErr = dragonscale.NewExecutorError("execution", fmt.Sprintf("task %s failed after max retries", completedTask.ID), completedTask.Error)
			}
			errMutex.Unlock()
		}
	case dragonscale.TaskStatusCompleted:
		errMutex.Lock()
		if *execErr != nil {
			errMutex.Unlock()
			return
		}
		newlyReady := e.findNewlyReadyTasks(completedTask, plan)
		for _, nextTask := range newlyReady {
			if nextTask.GetStatus() == dragonscale.TaskStatusPending {
				priority, err := e.calculatePriority(nextTask, plan)
				if err != nil {
					log.Printf("Error calculating priority for task %s: %v", nextTask.ID, err)
					continue
				}
				node := &TaskNode{task: nextTask, priority: priority}
				heap.Push(priorityQueue, node)
				nextTask.UpdateStatus(dragonscale.TaskStatusReady, nil)
			}
		}
		return
	}
	// TODO: Other statuses: Pending, Ready, Running: No-op for now, implement callbacks?
	errMutex.Unlock()
}

func (e *DAGExecutor) executeTaskWithCancel(ctx context.Context, task *dragonscale.Task, plan *dragonscale.ExecutionPlan, completionChan chan<- *dragonscale.Task, execErrChan chan<- error, cancel context.CancelFunc) {
	defer func() {
		e.updateTaskMetrics(task)
		select {
		case completionChan <- task:
		default:
			log.Printf("Warning: Could not send completion signal for task %s (channel likely closed or full)", task.ID)
		}
	}()

	// Check for cancellation at the start
	select {
	case <-ctx.Done():
		task.UpdateStatus(dragonscale.TaskStatusCancelled, ctx.Err())
		task.SetErrorContext("Task cancelled before execution started")
		return
	default:
	}

	// If an error is sent to execErrChan, cancel the context for all tasks
	sendErr := func(err error) {
		select {
		case execErrChan <- err:
			cancel()
		default:
			// already sent, do nothing
		}
	}

	// Check if the DAG execution has already failed
	if ctx.Err() != nil {
		task.UpdateStatus(dragonscale.TaskStatusCancelled, ctx.Err())
		task.SetErrorContext("Task cancelled because DAG execution failed")
		return
	}

	task.UpdateStatus(dragonscale.TaskStatusRunning, nil)
	log.Printf("Starting task execution (task_id: %s, tool: %s, retry_count: %d)",
		task.ID,
		task.ToolName,
		task.RetryCount)

	tool, exists := e.toolRegistry[task.ToolName]
	if !exists {
		err := dragonscale.NewToolNotFoundError("execution", task.ToolName)
		task.UpdateStatus(dragonscale.TaskStatusFailed, err)
		task.SetErrorContext("Tool not found in registry")
		sendErr(err)
		return
	}

	resolvedArgs, err := e.resolveArguments(ctx, task, plan)
	if err != nil {
		task.UpdateStatus(dragonscale.TaskStatusFailed, err)
		task.SetErrorContext("Argument resolution failed")
		sendErr(err)
		return
	}

	var result map[string]interface{}
	var toolErr error
	retryAttempt := task.RetryCount

	for retryAttempt <= e.maxRetries {
		if ctx.Err() != nil {
			toolErr = ctx.Err()
			task.SetErrorContext("Context cancelled during execution/retry wait")
			break
		}

		execTimeout := e.execTimeout
		if execTimeout <= 0 {
			execTimeout = time.Minute * 5
		}
		execCtx, cancelTimeout := context.WithTimeout(ctx, execTimeout)
		result, toolErr = tool.Execute(execCtx, resolvedArgs)
		cancelTimeout()

		if toolErr == nil {
			break
		}

		if execCtx.Err() == context.DeadlineExceeded {
			task.SetErrorContext(fmt.Sprintf("Execution timed out after %v", execTimeout))
			toolErr = dragonscale.NewToolExecutionError("execution", task.ToolName, fmt.Errorf("tool execution timed out: %w", context.DeadlineExceeded))
		} else if ctx.Err() != nil && toolErr == ctx.Err() {
			task.SetErrorContext("Parent context cancelled during execution")
			break
		} else {
			task.SetErrorContext(fmt.Sprintf("Execution failed: %v", toolErr))
			if !dragonscale.IsDragonScaleError(toolErr) {
				toolErr = dragonscale.NewToolExecutionError("execution", task.ToolName, toolErr)
			}
		}

		retryAttempt++
		if retryAttempt > e.maxRetries {
			break
		}
		log.Printf("Task execution failed, retrying (task_id: %s, tool: %s, error: %v, retry: %d, max_retries: %d)",
			task.ID,
			task.ToolName,
			toolErr,
			retryAttempt,
			e.maxRetries)
		select {
		case <-ctx.Done():
			toolErr = ctx.Err()
			task.SetErrorContext("Context cancelled while waiting to retry")
			break
		case <-time.After(e.retryDelay):
		}
		if ctx.Err() != nil {
			break
		}
	}

	if toolErr != nil {
		finalStatus := dragonscale.TaskStatusFailed
		var finalTaskError error
		if dsErr, ok := toolErr.(*dragonscale.DragonScaleError); ok {
			finalTaskError = dsErr
		} else if toolErr == context.Canceled || toolErr == context.DeadlineExceeded || ctx.Err() == toolErr {
			finalTaskError = dragonscale.NewToolExecutionError("execution", task.ToolName, fmt.Errorf("task interrupted: %w", toolErr))
		} else {
			finalTaskError = dragonscale.NewToolExecutionError("execution", task.ToolName, toolErr)
		}
		task.UpdateStatus(finalStatus, finalTaskError)
		log.Printf("Task execution finished with error after %d attempts (task_id: %s, tool: %s, error: %v)",
			retryAttempt,
			task.ID,
			task.ToolName,
			finalTaskError)
		sendErr(finalTaskError)
		return
	} else {
		if result == nil {
			err := dragonscale.NewInternalError("execution", "tool execution returned a nil result map", nil)
			task.UpdateStatus(dragonscale.TaskStatusFailed, err)
			task.SetErrorContext("Tool returned nil result")
			sendErr(err)
			return
		}
		task.Result = result
		plan.SetResult(task.ID, result)
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
			// Minimal expression evaluator: supports arithmetic and references to dependency results.
			expr := argSource.Expression
			if expr == "" {
				err = dragonscale.NewInternalError("execution", fmt.Sprintf(
					"empty expression for argument '%s' in task '%s'", argName, task.ID), nil)
				break
			}
			// For security and simplicity, only allow expressions of the form: "$dep.field + 2" or "$dep + 2"
			// Parse variables: $dep or $dep.field
			// Only support +, -, *, / and parentheses for now.
			evalValue, evalErr := e.evaluateSimpleExpression(ctx, expr, task, plan)
			if evalErr != nil {
				err = dragonscale.NewInternalError("execution", fmt.Sprintf(
					"failed to evaluate expression for argument '%s' in task '%s': %v", argName, task.ID, evalErr), nil)
			} else {
				value = evalValue
				err = nil
			}

		case dragonscale.ArgumentSourceMerged:
			// TODO: For future implementation - merge multiple sources
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

// calculatePriority assigns a priority to a task using the Critical Path Method (CPM).
// Tasks on the critical path (longest path to completion) receive the highest priority (lowest int value).
// This implementation computes, for each task, the length of the longest path from this task to any leaf (task with no dependents).
// Tasks with longer critical paths are prioritized to minimize total execution time and resource idling.
// The result is negated so that tasks on the critical path have lower (higher-priority) values for the min-heap.
//
// Returns (priority, error). If a cycle is detected, returns an error instead of panicking.
func (e *DAGExecutor) calculatePriority(task *dragonscale.Task, plan *dragonscale.ExecutionPlan) (int, error) {
	if plan == nil || plan.TaskMap == nil {
		return 0, nil
	}

	// Build dependents map once per plan for efficiency.
	if plan.Dependents == nil {
		dependents := make(map[string][]string, len(plan.TaskMap))
		for _, t := range plan.TaskMap {
			for _, depID := range t.DependsOn {
				dependents[depID] = append(dependents[depID], t.ID)
			}
		}
		plan.Dependents = dependents
	}
	dependents := plan.Dependents

	// Use a local cache to avoid recomputation within this call.
	pathLenCache := make(map[string]int, len(plan.TaskMap))

	// Recursive DFS to compute the longest path from this task to any leaf.
	var dfs func(tid string, visited map[string]bool) (int, error)
	dfs = func(tid string, visited map[string]bool) (int, error) {
		if v, ok := pathLenCache[tid]; ok {
			return v, nil
		}
		if visited[tid] {
			return 0, fmt.Errorf("cycle detected in task graph at task '%s': not a DAG", tid)
		}
		visited[tid] = true
		defer func() { visited[tid] = false }()

		maxLen := 0
		for _, dep := range dependents[tid] {
			l, err := dfs(dep, visited)
			if err != nil {
				return 0, err
			}
			l = 1 + l
			if l > maxLen {
				maxLen = l
			}
		}
		pathLenCache[tid] = maxLen
		return maxLen, nil
	}

	criticalPathLength, err := dfs(task.ID, make(map[string]bool))
	if err != nil {
		return 0, err
	}
	// Negate so that longer paths (more critical) get higher priority (lower int)
	return -criticalPathLength, nil
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

// GetMetrics returns a copy of the current execution metrics.
func (e *DAGExecutor) GetMetrics() ExecutorMetrics {
	e.metrics.mu.Lock()
	defer e.metrics.mu.Unlock()
	return e.metrics.Copy()
}

// evaluateSimpleExpression evaluates a robust, secure arithmetic/logical expression with variables.
// Supports $dep, $dep.field, $dep.field[0], and custom functions. Returns structured errors.
func (e *DAGExecutor) evaluateSimpleExpression(
	ctx context.Context,
	expr string,
	task *dragonscale.Task,
	plan *dragonscale.ExecutionPlan,
) (interface{}, error) {
	varRe := regexp.MustCompile(`\$([a-zA-Z0-9_]+)((?:\.[a-zA-Z0-9_]+|\[[0-9]+\])*)`)
	variables := map[string]interface{}{}
	replaced := varRe.ReplaceAllStringFunc(expr, func(matched string) string {
		matches := varRe.FindStringSubmatch(matched)
		depID := matches[1]
		accessors := matches[2]
		depResult, ok := plan.GetResult(depID)
		if !ok {
			variables[matched] = nil
			return matched
		}
		val := depResult
		accRe := regexp.MustCompile(`(\.[a-zA-Z0-9_]+|\[[0-9]+\])`)
		accMatches := accRe.FindAllString(accessors, -1)
		for _, acc := range accMatches {
			if strings.HasPrefix(acc, ".") {
				field := acc[1:]
				if m, ok := val.(map[string]interface{}); ok {
					v, ok := m[field]
					if !ok {
						variables[matched] = nil
						return matched
					}
					val = v
				} else {
					variables[matched] = nil
					return matched
				}
			} else if strings.HasPrefix(acc, "[") && strings.HasSuffix(acc, "]") {
				idxStr := acc[1 : len(acc)-1]
				idx, err := strconv.Atoi(idxStr)
				if err != nil {
					variables[matched] = nil
					return matched
				}
				if arr, ok := val.([]interface{}); ok {
					if idx < 0 || idx >= len(arr) {
						variables[matched] = nil
						return matched
					}
					val = arr[idx]
				} else {
					variables[matched] = nil
					return matched
				}
			}
		}
		varName := depID
		for _, acc := range accMatches {
			varName += strings.ReplaceAll(acc, ".", "_")
		}
		variables[varName] = val
		return varName
	})

	functions := getWhitelistedFunctions()
	evalExpr, err := govaluate.NewEvaluableExpressionWithFunctions(replaced, functions)
	if err != nil {
		return nil, dragonscale.NewInternalError("expression", fmt.Sprintf("failed to parse expression: %s", expr), err)
	}
	result, err := evalExpr.Evaluate(variables)
	if err != nil {
		return nil, dragonscale.NewInternalError("expression", fmt.Sprintf("failed to evaluate expression: %s", expr), err)
	}
	return result, nil
}
