package dragonscale

import (
	"sync"
	"time"
)

// TaskStatus represents the possible states of a task.
type TaskStatus string

const (
	// TaskStatusPending indicates the task is waiting for dependencies.
	TaskStatusPending TaskStatus = "pending"
	// TaskStatusReady indicates the task is ready to be executed.
	TaskStatusReady TaskStatus = "ready"
	// TaskStatusRunning indicates the task is currently executing.
	TaskStatusRunning TaskStatus = "running"
	// TaskStatusCompleted indicates the task has completed successfully.
	TaskStatusCompleted TaskStatus = "completed"
	// TaskStatusFailed indicates the task has failed.
	TaskStatusFailed TaskStatus = "failed"
	// TaskStatusCancelled indicates the task was cancelled.
	TaskStatusCancelled TaskStatus = "cancelled"
)

// Task represents a single unit of work in the execution plan.
type Task struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	ToolName    string   `json:"tool_name"`
	Args        []string `json:"args"` // Arguments, potentially including placeholders like "$task1_output"
	DependsOn   []string `json:"depends_on"`
	Category    string   `json:"category,omitempty"` // Optional category for grouping/filtering tasks

	// Internal execution state (not serialized typically)
	status        TaskStatus    `json:"-"`
	Result        interface{}   `json:"-"`
	Error         error         `json:"-"`
	ErrorContext  string        `json:"-"` // Additional context about the error
	mutex         sync.Mutex    `json:"-"` // Protects Status, Result, Error
	ResultChannel chan struct{} `json:"-"` // Used to signal completion

	// Execution metrics
	StartTime  time.Time `json:"-"` // When the task started execution
	EndTime    time.Time `json:"-"` // When the task completed or failed
	RetryCount int       `json:"-"` // Number of times this task has been retried
}

// ExecutionPlan represents the Directed Acyclic Graph (DAG) of tasks.
type ExecutionPlan struct {
	Tasks      []Task                 `json:"tasks"`
	TaskMap    map[string]*Task       `json:"-"` // Populated for quick lookup during execution
	Results    map[string]interface{} `json:"-"` // Stores results of completed tasks
	StateMutex sync.RWMutex           `json:"-"` // Protects TaskMap and Results during execution
}

// PlannerInput contains the information needed by the Planner to generate a plan.
type PlannerInput struct {
	Query        string            `json:"query"`
	ToolSchema   map[string]string `json:"tool_schema"`             // Map tool name to description/schema
	CurrentState *ExecutionPlan    `json:"current_state,omitempty"` // For replanning
	Reason       string            `json:"reason,omitempty"`        // For replanning
}

// NewExecutionPlan creates a new execution plan and initializes internal maps.
func NewExecutionPlan(tasks []Task) *ExecutionPlan {
	plan := &ExecutionPlan{
		Tasks:   tasks,
		TaskMap: make(map[string]*Task, len(tasks)),
		Results: make(map[string]interface{}),
	}
	for i := range tasks {
		task := &tasks[i] // Get pointer to task in slice
		task.status = TaskStatusPending
		task.ResultChannel = make(chan struct{})
		plan.TaskMap[task.ID] = task
	}
	return plan
}

// GetResult safely retrieves a result for a given task ID.
func (ep *ExecutionPlan) GetResult(taskID string) (interface{}, bool) {
	ep.StateMutex.RLock()
	defer ep.StateMutex.RUnlock()
	result, ok := ep.Results[taskID]
	return result, ok
}

// SetResult safely sets the result for a given task ID.
func (ep *ExecutionPlan) SetResult(taskID string, result interface{}) {
	ep.StateMutex.Lock()
	defer ep.StateMutex.Unlock()
	ep.Results[taskID] = result
}

// GetTask safely retrieves a task by ID.
func (ep *ExecutionPlan) GetTask(taskID string) (*Task, bool) {
	ep.StateMutex.RLock()
	defer ep.StateMutex.RUnlock()
	task, ok := ep.TaskMap[taskID]
	return task, ok
}

// UpdateTaskStatus safely updates the status of a task.
// Deprecated: Use UpdateStatus with TaskStatus instead.
func (t *Task) UpdateTaskStatus(status string, err error) {
	var taskStatus TaskStatus
	switch status {
	case "pending":
		taskStatus = TaskStatusPending
	case "ready":
		taskStatus = TaskStatusReady
	case "running":
		taskStatus = TaskStatusRunning
	case "completed":
		taskStatus = TaskStatusCompleted
	case "failed":
		taskStatus = TaskStatusFailed
	case "cancelled":
		taskStatus = TaskStatusCancelled
	default:
		taskStatus = TaskStatus(status)
	}
	t.UpdateStatus(taskStatus, err)
}

// GetStatus safely retrieves the task's current status.
func (t *Task) GetStatus() TaskStatus {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.status
}

// UpdateStatus safely updates the task's status and related information.
func (t *Task) UpdateStatus(newStatus TaskStatus, err error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	oldStatus := t.status
	t.status = newStatus

	// Record timing information
	now := time.Now()
	if newStatus == TaskStatusRunning && oldStatus != TaskStatusRunning {
		t.StartTime = now
	}
	if (newStatus == TaskStatusCompleted || newStatus == TaskStatusFailed || newStatus == TaskStatusCancelled) &&
		(oldStatus != TaskStatusCompleted && oldStatus != TaskStatusFailed && oldStatus != TaskStatusCancelled) {
		t.EndTime = now
	}

	// Update error information
	if err != nil {
		t.Error = err
	}

	// Signal completion for dependent tasks
	if newStatus == TaskStatusCompleted || newStatus == TaskStatusFailed || newStatus == TaskStatusCancelled {
		close(t.ResultChannel)
	}
}

// Duration returns the execution duration of the task.
func (t *Task) Duration() time.Duration {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	// If the task hasn't started, return 0
	if t.StartTime.IsZero() {
		return 0
	}

	// If the task is still running, calculate against current time
	if t.EndTime.IsZero() {
		return time.Since(t.StartTime)
	}

	// Otherwise return the total execution time
	return t.EndTime.Sub(t.StartTime)
}

// SetErrorContext sets additional context for an error.
func (t *Task) SetErrorContext(context string) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.ErrorContext = context
}

// Retry increments the retry count and updates the status.
func (t *Task) Retry() {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.RetryCount++
	t.status = TaskStatusReady

	// Create a new result channel for the retry
	if t.ResultChannel == nil || isClosed(t.ResultChannel) {
		t.ResultChannel = make(chan struct{})
	}
}

// isClosed checks if a channel is closed safely.
func isClosed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
