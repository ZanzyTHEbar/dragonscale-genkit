// Package eventbus provides event bus implementations
package eventbus

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ChannelEventBus is an implementation of EventBus using Go channels
type ChannelEventBus struct {
	// subscribers maps event types to a map of subscription IDs to event handlers
	subscribers map[EventType]map[string]EventHandler

	// allSubscribers contains handlers that receive all events regardless of type
	allSubscribers map[string]EventHandler
	// eventChan is the channel where events are published
	eventChan chan eventWithContext

	// done is used to signal graceful shutdown
	done chan struct{}

	// closed indicates if the event bus has been shut down
	closed bool

	// wg keeps track of active goroutines
	wg sync.WaitGroup

	// mutex protects the subscribers and allSubscribers maps
	mutex sync.RWMutex

	// Configuration
	bufferSize    int
	workerCount   int
	maxRetries    int
	retryInterval time.Duration
}

// eventWithContext bundles an event with its context for processing
type eventWithContext struct {
	ctx   context.Context
	event Event
}

// ChannelEventBusOption configures the channel-based event bus
type ChannelEventBusOption func(*ChannelEventBus)

// WithBufferSize sets the event channel buffer size
func WithBufferSize(size int) ChannelEventBusOption {
	return func(eb *ChannelEventBus) {
		eb.bufferSize = size
	}
}

// WithWorkerCount sets the number of event processing workers
func WithWorkerCount(count int) ChannelEventBusOption {
	return func(eb *ChannelEventBus) {
		eb.workerCount = count
	}
}

// WithRetries configures the retry behavior for event handlers
func WithRetries(maxRetries int, retryInterval time.Duration) ChannelEventBusOption {
	return func(eb *ChannelEventBus) {
		eb.maxRetries = maxRetries
		eb.retryInterval = retryInterval
	}
}

// NewChannelEventBus creates a new channel-based event bus
func NewChannelEventBus(options ...ChannelEventBusOption) *ChannelEventBus {
	eb := &ChannelEventBus{
		subscribers:    make(map[EventType]map[string]EventHandler),
		allSubscribers: make(map[string]EventHandler),
		done:           make(chan struct{}),

		// Default configuration
		bufferSize:    100,
		workerCount:   5,
		maxRetries:    3,
		retryInterval: time.Millisecond * 100,
	}

	// Apply options
	for _, option := range options {
		option(eb)
	}

	// Initialize the event channel with the configured buffer size
	eb.eventChan = make(chan eventWithContext, eb.bufferSize)

	// Start the worker pool
	eb.startWorkers()

	return eb
}

// startWorkers initializes the goroutines that process events
func (eb *ChannelEventBus) startWorkers() {
	for i := 0; i < eb.workerCount; i++ {
		eb.wg.Add(1)
		go eb.worker(i)
	}
}

// worker processes events from the event channel
func (eb *ChannelEventBus) worker(id int) {
	defer eb.wg.Done()

	for {
		select {
		case <-eb.done:
			return
		case evt := <-eb.eventChan:
			eb.processEvent(evt)
		}
	}
}

// processEvent handles the event dispatch to all relevant subscribers
func (eb *ChannelEventBus) processEvent(evt eventWithContext) {
	// Skip processing if context is already cancelled
	if evt.ctx.Err() != nil {
		return
	}

	// Process specific event type subscribers
	eb.mutex.RLock()

	// Create copies of the handler maps to avoid holding the lock during execution
	// This prevents deadlocks if handlers try to subscribe/unsubscribe
	typeHandlers := make(map[string]EventHandler)
	if handlers, exists := eb.subscribers[evt.event.Type()]; exists {
		for id, handler := range handlers {
			typeHandlers[id] = handler
		}
	}

	// Create a copy of all-event subscribers
	allHandlers := make(map[string]EventHandler)
	for id, handler := range eb.allSubscribers {
		allHandlers[id] = handler
	}

	eb.mutex.RUnlock()

	// Dispatch to type-specific handlers
	for _, handler := range typeHandlers {
		eb.executeHandler(evt.ctx, evt.event, handler)
	}

	// Dispatch to all-event handlers
	for _, handler := range allHandlers {
		eb.executeHandler(evt.ctx, evt.event, handler)
	}
}

// executeHandler runs a handler with retry logic
func (eb *ChannelEventBus) executeHandler(ctx context.Context, event Event, handler EventHandler) {
	var err error

	// Try to execute with retries
	for attempt := 0; attempt <= eb.maxRetries; attempt++ {
		// Skip if context is cancelled
		if ctx.Err() != nil {
			return
		}

		// Execute the handler
		err = handler(ctx, event)
		if err == nil {
			return // Success!
		}

		// If this was the last attempt, don't sleep
		if attempt == eb.maxRetries {
			break
		}

		// Wait before retrying
		select {
		case <-ctx.Done():
			return // Context cancelled during wait
		case <-time.After(eb.retryInterval):
			// Continue to next attempt
		}
	}

	if err != nil {
		// Log the error but don't stop other handlers
		// TODO: In a real implementation, we might want to publish a system error event
		fmt.Printf("Event handler error (event_type: %s, retries: %d): %v\n",
			event.Type(), eb.maxRetries, err)
	}
}

// Publish sends an event to all subscribed handlers
func (eb *ChannelEventBus) Publish(ctx context.Context, event Event) error {
	if eb.closed {
		return fmt.Errorf("event bus is closed")
	}

	// Create a context that will be cancelled if the original is cancelled
	// This allows us to buffer events but still respect cancellation
	childCtx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-ctx.Done():
			cancel()
		case <-eb.done:
			cancel()
		}
	}()

	// Try to send on the channel, respecting the context
	select {
	case <-ctx.Done():
		cancel() // Clean up
		return ctx.Err()
	case eb.eventChan <- eventWithContext{ctx: childCtx, event: event}:
		// Event successfully queued
		return nil
	}
}

// Subscribe registers a handler for specific event types
func (eb *ChannelEventBus) Subscribe(eventTypes []EventType, handler EventHandler) (string, error) {
	if eb.closed {
		return "", fmt.Errorf("event bus is closed")
	}

	if handler == nil {
		return "", fmt.Errorf("handler cannot be nil")
	}

	if len(eventTypes) == 0 {
		return "", fmt.Errorf("at least one event type is required")
	}

	// Generate a unique subscription ID
	subscriptionID := uuid.New().String()

	eb.mutex.Lock()
	defer eb.mutex.Unlock()

	// Register the handler for each event type
	for _, eventType := range eventTypes {
		if _, exists := eb.subscribers[eventType]; !exists {
			eb.subscribers[eventType] = make(map[string]EventHandler)
		}
		eb.subscribers[eventType][subscriptionID] = handler
	}

	return subscriptionID, nil
}

// SubscribeAll registers a handler for all event types
func (eb *ChannelEventBus) SubscribeAll(handler EventHandler) (string, error) {
	if eb.closed {
		return "", fmt.Errorf("event bus is closed")
	}

	if handler == nil {
		return "", fmt.Errorf("handler cannot be nil")
	}

	// Generate a unique subscription ID
	subscriptionID := uuid.New().String()

	eb.mutex.Lock()
	defer eb.mutex.Unlock()

	// Register the handler for all events
	eb.allSubscribers[subscriptionID] = handler

	return subscriptionID, nil
}

// Unsubscribe removes a subscription by ID
func (eb *ChannelEventBus) Unsubscribe(subscriptionID string) error {
	if eb.closed {
		return fmt.Errorf("event bus is closed")
	}

	eb.mutex.Lock()
	defer eb.mutex.Unlock()

	// Remove from all subscribers if present
	delete(eb.allSubscribers, subscriptionID)

	// Remove from type-specific subscribers
	for eventType, subscribers := range eb.subscribers {
		if _, exists := subscribers[subscriptionID]; exists {
			delete(eb.subscribers[eventType], subscriptionID)
		}
	}

	return nil
}

// Close shuts down the event bus, cleaning up resources
func (eb *ChannelEventBus) Close() error {
	eb.mutex.Lock()
	if eb.closed {
		eb.mutex.Unlock()
		return nil // Already closed
	}

	eb.closed = true
	eb.mutex.Unlock()

	// Signal all workers to stop
	close(eb.done)

	// Wait for all workers to finish
	eb.wg.Wait()

	// Close the event channel
	close(eb.eventChan)

	return nil
}
