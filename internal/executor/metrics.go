package executor

import (
	"sync"
	"time"
)

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

// Create a copy without the muxtex
func (m *ExecutorMetrics) Copy() ExecutorMetrics {
	m.mu.Lock()
	defer m.mu.Unlock()

	return ExecutorMetrics{
		TasksExecuted:    m.TasksExecuted,
		TasksSuccessful:  m.TasksSuccessful,
		TasksFailed:      m.TasksFailed,
		TotalDuration:    m.TotalDuration,
		LongestTaskTime:  m.LongestTaskTime,
		ShortestTaskTime: m.ShortestTaskTime,
		TotalRetries:     m.TotalRetries,
	}
}
