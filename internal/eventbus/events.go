package eventbus

import (
	"time"

	"context"
)

// EventType represents the type of an event
type EventType string

// Standard event types
const (
	// Plan generation events
	EventPlanGenerationStarted EventType = "plan_generation_started"
	EventPlanGenerationSuccess EventType = "plan_generation_success"
	EventPlanGenerationFailure EventType = "plan_generation_failure"

	// Task execution events
	EventTaskExecutionStarted  EventType = "task_execution_started"
	EventTaskExecutionSuccess  EventType = "task_execution_success"
	EventTaskExecutionFailure  EventType = "task_execution_failure"
	EventTaskExecutionRetry    EventType = "task_execution_retry"
	EventTaskExecutionCanceled EventType = "task_execution_canceled"

	// DAG execution events
	EventDAGExecutionStarted  EventType = "dag_execution_started"
	EventDAGExecutionProgress EventType = "dag_execution_progress"
	EventDAGExecutionSuccess  EventType = "dag_execution_success"
	EventDAGExecutionFailure  EventType = "dag_execution_failure"

	// Context retrieval events
	EventContextRetrievalStarted EventType = "context_retrieval_started"
	EventContextRetrievalSuccess EventType = "context_retrieval_success"
	EventContextRetrievalFailure EventType = "context_retrieval_failure"

	// Response synthesis events
	EventSynthesisStarted EventType = "synthesis_started"
	EventSynthesisSuccess EventType = "synthesis_success"
	EventSynthesisFailure EventType = "synthesis_failure"

	// Query processing events
	EventQueryProcessingStarted  EventType = "query_processing_started"
	EventQueryProcessingProgress EventType = "query_processing_progress"
	EventQueryProcessingSuccess  EventType = "query_processing_success"
	EventQueryProcessingFailure  EventType = "query_processing_failure"
	
	// Async query processing events
	EventQueryAsyncProcessingStarted    EventType = "query_async_processing_started"
	EventQueryAsyncProcessingProgress   EventType = "query_async_processing_progress"
	EventQueryAsyncProcessingSuccess    EventType = "query_async_processing_success"
	EventQueryAsyncProcessingFailure    EventType = "query_async_processing_failure"
	EventQueryAsyncProcessingCancelled  EventType = "query_async_processing_cancelled"

	// System events
	EventSystemError   EventType = "system_error"
	EventSystemWarning EventType = "system_warning"
	EventSystemInfo    EventType = "system_info"
)

// EventHandler is a function that handles events
type EventHandler func(context.Context, Event) error

// Event represents something that has happened within the system
type Event interface {
	// Type returns the event type
	Type() EventType

	// Payload returns the event data
	Payload() interface{}

	// Metadata returns additional information about the event
	Metadata() map[string]interface{}

	// Timestamp returns when the event occurred
	Timestamp() int64

	// Source returns information about what generated the event
	Source() string
}

// EventBus is the central event dispatch system
type EventBus interface {
	// Publish sends an event to all subscribed handlers
	Publish(ctx context.Context, event Event) error

	// Subscribe registers a handler for specific event types
	// Returns a subscription ID that can be used to unsubscribe
	Subscribe(eventTypes []EventType, handler EventHandler) (string, error)

	// SubscribeAll registers a handler for all event types
	// Returns a subscription ID that can be used to unsubscribe
	SubscribeAll(handler EventHandler) (string, error)

	// Unsubscribe removes a subscription by ID
	Unsubscribe(subscriptionID string) error

	// Close shuts down the event bus, cleaning up resources
	Close() error
}

// BaseEvent is a simple implementation of the Event interface
type BaseEvent struct {
	eventType  EventType
	payload    interface{}
	metadata   map[string]interface{}
	timestamp  int64
	sourceInfo string
}

// NewEvent creates a new BaseEvent
func NewEvent(
	eventType EventType,
	payload interface{},
	source string,
	metadata map[string]interface{},
) *BaseEvent {
	if metadata == nil {
		metadata = make(map[string]interface{})
	}

	return &BaseEvent{
		eventType:  eventType,
		payload:    payload,
		metadata:   metadata,
		timestamp:  time.Now().UnixNano(),
		sourceInfo: source,
	}
}

// Type returns the event type
func (e *BaseEvent) Type() EventType {
	return e.eventType
}

// Payload returns the event data
func (e *BaseEvent) Payload() interface{} {
	return e.payload
}

// Metadata returns additional information about the event
func (e *BaseEvent) Metadata() map[string]interface{} {
	return e.metadata
}

// Timestamp returns when the event occurred
func (e *BaseEvent) Timestamp() int64 {
	return e.timestamp
}

// Source returns information about what generated the event
func (e *BaseEvent) Source() string {
	return e.sourceInfo
}

// WithMetadata adds or updates metadata and returns the same event
// This allows for fluent method chaining
func (e *BaseEvent) WithMetadata(key string, value interface{}) *BaseEvent {
	e.metadata[key] = value
	return e
}

// AddMetadata adds multiple metadata entries at once and returns the same event
func (e *BaseEvent) AddMetadata(data map[string]interface{}) *BaseEvent {
	for k, v := range data {
		e.metadata[k] = v
	}
	return e
}
