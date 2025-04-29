package eventbus

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestChannelEventBus_PublishAndSubscribe(t *testing.T) {
	eb := NewChannelEventBus(
		WithBufferSize(1),
		WithWorkerCount(1),
		WithRetries(1, 10*time.Millisecond),
	)
	defer eb.Close()

	received := make(chan string, 1)
	handler := func(ctx context.Context, event Event) error {
		received <- string(event.Type())
		return nil
	}
	_, err := eb.Subscribe([]EventType{EventTaskExecutionSuccess}, handler)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	evt := NewEvent(EventTaskExecutionSuccess, nil, "test", nil)
	err = eb.Publish(context.Background(), evt)
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	select {
	case typ := <-received:
		if typ != string(EventTaskExecutionSuccess) {
			t.Errorf("expected event type %v, got %v", EventTaskExecutionSuccess, typ)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timeout waiting for event handler")
	}
}

func TestChannelEventBus_HandlerRetry(t *testing.T) {
	eb := NewChannelEventBus(
		WithBufferSize(1),
		WithWorkerCount(1),
		WithRetries(2, 10*time.Millisecond),
	)
	defer eb.Close()

	var mu sync.Mutex
	calls := 0
	handler := func(ctx context.Context, event Event) error {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls < 2 {
			return context.DeadlineExceeded
		}
		return nil
	}
	_, err := eb.Subscribe([]EventType{EventTaskExecutionFailure}, handler)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	err = eb.Publish(context.Background(), NewEvent(EventTaskExecutionFailure, nil, "test", nil))
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if calls < 2 {
		t.Errorf("expected at least 2 calls, got %d", calls)
	}
	mu.Unlock()
}

func TestChannelEventBus_ContextCancellation(t *testing.T) {
	eb := NewChannelEventBus(
		WithBufferSize(1),
		WithWorkerCount(1),
		WithRetries(1, 10*time.Millisecond),
	)
	defer eb.Close()

	ctx, cancel := context.WithCancel(context.Background())
	received := make(chan struct{}, 1)
	handler := func(ctx context.Context, event Event) error {
		received <- struct{}{}
		return nil
	}
	_, err := eb.Subscribe([]EventType{EventTaskExecutionStarted}, handler)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	cancel()
	err = eb.Publish(ctx, NewEvent(EventTaskExecutionStarted, nil, "test", nil))
	if err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	select {
	case <-received:
		t.Error("handler should not be called after context cancellation")
	case <-time.After(50 * time.Millisecond):
		// Success: handler not called
	}
}
