package dragonscale

import "sync"

// Task represents a single unit of work in the execution plan.
type Task struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	ToolName    string   `json:"tool_name"`
	Args        []string `json:"args"` // Arguments, potentially including placeholders like "$task1_output"
	DependsOn   []string `json:"depends_on"`

	// Internal execution state (not serialized typically)
	Status        string      `json:"-"` // e.g., "pending", "ready", "running", "completed", "failed"
	Result        interface{} `json:"-"`
	Error         error       `json:"-"`
	mutex       sync.Mutex  `json:"-"` // Protects Status, Result, Error
	ResultChannel chan struct{} `json:"-"` // Used to signal completion
}

// ExecutionPlan represents the Directed Acyclic Graph (DAG) of tasks.
type ExecutionPlan struct {
	Tasks     []Task             `json:"tasks"`
	TaskMap   map[string]*Task   `json:"-"` // Populated for quick lookup during execution
	Results   map[string]interface{} `json:"-"` // Stores results of completed tasks
	StateMutex sync.RWMutex      `json:"-"` // Protects TaskMap and Results during execution
}

// PlannerInput contains the information needed by the Planner to generate a plan.
type PlannerInput struct {
	Query        string              `json:"query"`
	ToolSchema   map[string]string   `json:"tool_schema"` // Map tool name to description/schema
	CurrentState *ExecutionPlan      `json:"current_state,omitempty"` // For replanning
	Reason       string              `json:"reason,omitempty"`        // For replanning
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
		task.Status = "pending"
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
func (t *Task) UpdateStatus(status string, err error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.Status = status
	t.Error = err
	if status == "completed" || status == "failed" {
		close(t.ResultChannel) // Signal completion/failure
	}
}

// GetStatus safely reads the status of a task.
func (t *Task) GetStatus() string {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.Status
}
