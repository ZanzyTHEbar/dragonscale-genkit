package dragonscale

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ZanzyTHEbar/dragonscale-genkit/internal/eventbus"
)

func TestStateMachine_EventBus_EmitsEvents(t *testing.T) {
	bus := eventbus.NewChannelEventBus(
		eventbus.WithBufferSize(10),
		eventbus.WithWorkerCount(1),
		eventbus.WithRetries(1, 10*time.Millisecond),
	)
	defer bus.Close()

	var mu sync.Mutex
	emitted := make(map[eventbus.EventType]bool)
	handler := func(ctx context.Context, evt eventbus.Event) error {
		if evt == nil {
			t.Error("event is nil")
			return nil
		}

		mu.Lock()

		if _, ok := emitted[evt.Type()]; !ok {
			t.Logf("event emitted: %v", evt.Type())
			emitted[evt.Type()] = true
		}

		mu.Unlock()
		return nil
	}

	_, err := bus.Subscribe([]eventbus.EventType{
		eventbus.EventContextRetrievalStarted,
		eventbus.EventContextRetrievalFailure,
		eventbus.EventContextRetrievalSuccess,
		eventbus.EventTaskExecutionStarted,
		eventbus.EventTaskExecutionFailure,
		eventbus.EventTaskExecutionSuccess,
	}, handler) // Subscribe to all events
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	err = bus.Publish(context.Background(), eventbus.NewEmptyEvent(eventbus.EventContextRetrievalStarted))
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	ds := &DragonScale{
		planner:   &dummyPlanner{},
		executor:  &dummyExecutor{},
		retriever: &dummyRetriever{},
		solver:    &dummySolver{},
		cache:     &dummyCache{},
		tools:     map[string]Tool{"noop": &dummyToolForStateMachine{}},
		config:    Config{MaxConcurrentExecutions: 1, MaxRetries: 1, RetryDelay: time.Millisecond, ExecutionTimeout: time.Second, EnableEventBus: true},
		eventBus:  bus,
	}
	stateMachine := ds.createStateMachine()
	pCtx := NewProcessContext("test query")
	_, err = stateMachine.Execute(context.Background(), pCtx)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// Wait briefly for events to be processed
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if len(emitted) == 0 {
		t.Error("expected at least one event to be emitted")
	}
	mu.Unlock()
}
