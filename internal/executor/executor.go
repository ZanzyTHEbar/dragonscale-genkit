package executor

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"container/heap"

	"github.com/ZanzyTHEbar/dragonscale-genkit/pkg/dragonscale"
	"github.com/firebase/genkit/go/genkit"
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
	maxWorkers   int // Max concurrent tasks
}

// NewDAGExecutor creates a new executor.
func NewDAGExecutor(toolRegistry map[string]dragonscale.Tool, maxWorkers int) *DAGExecutor {
	return &DAGExecutor{
		toolRegistry: toolRegistry,
		maxWorkers:   maxWorkers,
	}
}

// ExecutePlan runs the execution plan.
func (e *DAGExecutor) ExecutePlan(ctx context.Context, plan *dragonscale.ExecutionPlan) (map[string]interface{}, error) {
	genkit.Logger(ctx).Info("Starting DAG execution", "total_tasks", len(plan.Tasks))

	priorityQueue := make(PriorityQueue, 0)
	heap.Init(&priorityQueue)

	activeWorkers := 0
	var wg sync.WaitGroup
	var execErr error
	var errMutex sync.Mutex
	completionChannel := make(chan *dragonscale.Task, len(plan.Tasks)) // Channel to receive completed tasks

	// Initial population of the ready queue
	readyTasks := e.findReadyTasks(plan)
	for _, task := range readyTasks {
		node := &TaskNode{task: task, priority: e.calculatePriority(task, plan)} // Add priority calculation
		heap.Push(&priorityQueue, node)
		task.UpdateStatus("ready", nil)
	}

	// Main execution loop
	for priorityQueue.Len() > 0 || activeWorkers > 0 {
		// Launch tasks while workers are available and tasks are ready
		for priorityQueue.Len() > 0 && activeWorkers < e.maxWorkers {
			taskNode := heap.Pop(&priorityQueue).(*TaskNode)
			task := taskNode.task

			if task.GetStatus() != "ready" { // Skip if already picked up or status changed
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

			if completedTask.GetStatus() == "failed" {
				// Handle potential replanning trigger here
				genkit.Logger(ctx).Error("Task failed, stopping DAG execution for now", "task_id", completedTask.ID, "error", completedTask.Error)
				errMutex.Lock()
				if execErr == nil { // Keep the first error
					execErr = fmt.Errorf("task %s failed: %w", completedTask.ID, completedTask.Error)
				}
				errMutex.Unlock()
				// Potentially break or implement more sophisticated failure handling
				// break // Simple stop on first failure
			}

			// Check for newly ready tasks upon completion
			newlyReady := e.findNewlyReadyTasks(completedTask, plan)
			for _, nextTask := range newlyReady {
				if nextTask.GetStatus() == "pending" { // Ensure we only add pending tasks
					node := &TaskNode{task: nextTask, priority: e.calculatePriority(nextTask, plan)}
					heap.Push(&priorityQueue, node)
					nextTask.UpdateStatus("ready", nil)
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
		genkit.Logger(ctx).Error("DAG execution finished with error", "error", finalError)
		return nil, finalError
	}

	allCompleted := true
	for _, task := range plan.TaskMap {
		if task.GetStatus() != "completed" {
			allCompleted = false
			genkit.Logger(ctx).Warn("DAG execution finished, but not all tasks completed", "task_id", task.ID, "status", task.GetStatus())
			// This might indicate a cycle or an unhandled state
		}
	}

	if allCompleted {
		genkit.Logger(ctx).Info("DAG execution finished successfully")
	} else {
		genkit.Logger(ctx).Warn("DAG execution finished with non-completed tasks")
		// Decide if this constitutes an error
	}

	return plan.Results, nil
}

func (e *DAGExecutor) executeTask(ctx context.Context, task *dragonscale.Task, plan *dragonscale.ExecutionPlan, wg *sync.WaitGroup, completionChan chan<- *dragonscale.Task, errMutex *sync.Mutex, execErr *error) {
	defer wg.Done()
	defer func() { completionChan <- task }() // Signal completion regardless of outcome

	task.UpdateStatus("running", nil)
	genkit.Logger(ctx).Info("Starting task execution", "task_id", task.ID, "tool", task.ToolName)

	// Get the tool from the registry
	tool, exists := e.toolRegistry[task.ToolName]
	if !exists {
		err := fmt.Errorf("tool '%s' not found in registry", task.ToolName)
		task.UpdateStatus("failed", err)
		return
	}

	// Resolve arguments
	resolvedArgs, err := e.resolveArguments(ctx, task.Args, plan)
	if err != nil {
		task.UpdateStatus("failed", fmt.Errorf("failed to resolve args for task %s: %w", task.ID, err))
		return
	}

	// Execute the tool
	var result map[string]interface{}
	err = genkit.Run(ctx, fmt.Sprintf("Tool: %s (Task: %s)", task.ToolName, task.ID), func() error {
		var toolErr error
		result, toolErr = tool.Execute(ctx, resolvedArgs)
		return toolErr
	})

	if err != nil {
		task.UpdateStatus("failed", err)
		genkit.Logger(ctx).Error("Task execution failed", "task_id", task.ID, "tool", task.ToolName, "error", err)
	} else {
		// Store result(s) - assuming a standard output key for simplicity, might need refinement
		outputResult := result["output"] // Adjust based on actual Tool output structure
		task.Result = outputResult
		plan.SetResult(task.ID+"_output", outputResult) // Store result with a specific key convention
		task.UpdateStatus("completed", nil)
		genkit.Logger(ctx).Info("Task execution completed", "task_id", task.ID, "tool", task.ToolName, "result", outputResult)
	}

}

// findReadyTasks identifies tasks whose dependencies are met.
func (e *DAGExecutor) findReadyTasks(plan *dragonscale.ExecutionPlan) []*dragonscale.Task {
	ready := []*dragonscale.Task{}
	for _, task := range plan.TaskMap {
		if task.GetStatus() != "pending" {
			continue
		}
		dependenciesMet := true
		for _, depID := range task.DependsOn {
			depTask, exists := plan.GetTask(depID)
			if !exists || depTask.GetStatus() != "completed" {
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
		if task.GetStatus() != "pending" {
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
			if !exists || depTask.GetStatus() != "completed" {
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
	// Using a simple string replacement approach here for clarity, regex is more robust.
	placeholderRegex := regexp.MustCompile(`\$\{?(\w+)_output\}?`) // Matches $taskID_output or ${taskID_output}

	for i, arg := range args {
		matches := placeholderRegex.FindAllStringSubmatch(arg, -1)
		replacedArg := arg
		for _, match := range matches {
			if len(match) < 2 {
				continue // Should not happen with the regex
			}
			placeholder := match[0]
			resultKey := match[1] + "_output" // Construct the key used in SetResult

			// Fetch the result from the plan's results map
			value, ok := plan.GetResult(resultKey)
			if !ok {
				// Check if the dependent task hasn't finished or failed
				depTaskID := match[1]
				depTask, exists := plan.GetTask(depTaskID)
				if !exists {
					return nil, fmt.Errorf("dependency task '%s' not found in plan", depTaskID)
				}
				// Wait briefly for the dependency to finish if it's running? Risky. Better to rely on scheduler.
				if depTask.GetStatus() != "completed" {
					return nil, fmt.Errorf("dependency '%s' required for arg '%s' has not completed successfully (status: %s)", resultKey, arg, depTask.GetStatus())
				}
				// If it completed but result wasn't found, that's an internal error
				return nil, fmt.Errorf("failed to find result for dependency '%s' needed for arg '%s'", resultKey, arg)
			}

			// Replace placeholder with the actual value (handle type conversion if necessary)
			// Simple string replacement might be brittle if results aren't strings.
			// If the entire argument is the placeholder, assign the value directly.
			if arg == placeholder {
				resolved[fmt.Sprintf("arg%d", i)] = value // Assign directly if arg is just the placeholder
				replacedArg = ""                          // Mark as handled
				break                                     // Assume only one placeholder if it matches the whole arg
			} else {
				// Otherwise, do string replacement (best effort)
				replacedArg = strings.Replace(replacedArg, placeholder, fmt.Sprintf("%v", value), 1)
			}
		}
		if replacedArg != "" { // If not handled by direct assignment
			resolved[fmt.Sprintf("arg%d", i)] = replacedArg // Store the potentially modified string
		}
	}

	// TODO: Refine argument passing. Currently passes args as map["arg0":"val0", "arg1":"val1"].
	// Tools might need named arguments based on their schema.
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
	// Could implement CPM here by traversing the graph
	return 1
}
