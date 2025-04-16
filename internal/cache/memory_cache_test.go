package cache

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryCache_SetAndGet(t *testing.T) {
	cache := NewInMemoryCache(2 * time.Second)
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
	cache := NewInMemoryCache(1 * time.Second)
	ctx := context.Background()
	key := "baz"
	value := "qux"

	err := cache.Set(ctx, key, value)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	time.Sleep(1100 * time.Millisecond)
	_, err = cache.Get(ctx, key)
	if err == nil {
		t.Errorf("expected error for expired item, got nil")
	}
}
