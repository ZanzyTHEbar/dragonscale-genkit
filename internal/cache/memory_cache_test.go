package cache

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestInMemoryCache_SetAndGet(t *testing.T) {
	cache := NewInMemoryCache(1 * time.Second)
	ctx := context.Background()
	key := "foo"
	value := "bar"

	err := cache.Set(ctx, key, value)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	got, err := cache.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got != value {
		t.Errorf("expected %v, got %v", value, got)
	}
}

func TestInMemoryCache_Expiration(t *testing.T) {
	cache := NewInMemoryCache(50 * time.Millisecond)
	ctx := context.Background()
	key := "baz"
	value := "qux"

	err := cache.Set(ctx, key, value)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	time.Sleep(60 * time.Millisecond)
	_, err = cache.Get(ctx, key)
	if err == nil {
		t.Errorf("expected error for expired item, got nil")
	}
}

func TestInMemoryCache_Concurrency(t *testing.T) {
	cache := NewInMemoryCache(1 * time.Second)
	ctx := context.Background()
	key := "concurrent"
	value := "val"
	setErr := make(chan error, 1)
	getErr := make(chan error, 1)

	go func() {
		setErr <- cache.Set(ctx, key, value)
	}()
	go func() {
		_, err := cache.Get(ctx, key)
		getErr <- err
	}()

	if err := <-setErr; err != nil {
		t.Errorf("Set failed: %v", err)
	}
	if err := <-getErr; err != nil && !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected Get error: %v", err)
	}
}
