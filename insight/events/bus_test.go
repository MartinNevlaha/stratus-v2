package events

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEventBusPublish(t *testing.T) {
	bus := NewInMemoryBus(10)
	defer bus.Close()

	done := make(chan Event, 1)
	bus.Subscribe(func(ctx context.Context, event Event) {
		select {
		case done <- event:
		default:
		}
	})

	evt := NewEvent(EventWorkflowStarted, "test", map[string]any{"key": "value"})
	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	select {
	case received := <-done:
		if received.Type != EventWorkflowStarted {
			t.Errorf("expected type %s, got %s", EventWorkflowStarted, received.Type)
		}
		if received.Source != "test" {
			t.Errorf("expected source test, got %s", received.Source)
		}
		if received.ID == "" {
			t.Error("expected event ID to be set")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestEventBusMultipleSubscribers(t *testing.T) {
	bus := NewInMemoryBus(10)
	defer bus.Close()

	var count atomic.Int32
	done := make(chan struct{}, 3)

	bus.Subscribe(func(ctx context.Context, event Event) {
		count.Add(1)
		done <- struct{}{}
	})
	bus.Subscribe(func(ctx context.Context, event Event) {
		count.Add(1)
		done <- struct{}{}
	})
	bus.Subscribe(func(ctx context.Context, event Event) {
		count.Add(1)
		done <- struct{}{}
	})

	evt := NewEvent(EventWorkflowCompleted, "test", nil)
	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	for i := 0; i < 3; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for subscriber %d", i)
		}
	}

	if count.Load() != 3 {
		t.Errorf("expected 3 events received, got %d", count.Load())
	}
}

func TestEventBusNonBlocking(t *testing.T) {
	bus := NewInMemoryBus(10)
	defer bus.Close()

	var received atomic.Int32
	done := make(chan struct{}, 5)

	bus.Subscribe(func(ctx context.Context, event Event) {
		received.Add(1)
		done <- struct{}{}
	})

	start := time.Now()
	for i := 0; i < 5; i++ {
		evt := NewEvent(EventWorkflowStarted, "test", map[string]any{"index": i})
		if err := bus.Publish(context.Background(), evt); err != nil {
			t.Fatalf("Publish failed: %v", err)
		}
	}
	elapsed := time.Since(start)

	if elapsed > 30*time.Millisecond {
		t.Errorf("Publish should be non-blocking, took %v", elapsed)
	}

	for i := 0; i < 5; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for event %d", i)
		}
	}

	if received.Load() != 5 {
		t.Errorf("expected 5 events, got %d", received.Load())
	}
}

func TestEventBusConcurrentPublish(t *testing.T) {
	bus := NewInMemoryBus(100)
	defer bus.Close()

	var count atomic.Int32
	done := make(chan struct{}, 100)

	bus.Subscribe(func(ctx context.Context, event Event) {
		count.Add(1)
		done <- struct{}{}
	})

	var wg sync.WaitGroup
	numGoroutines := 10
	eventsPerGoroutine := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				evt := NewEvent(EventWorkflowStarted, "concurrent", nil)
				bus.Publish(context.Background(), evt)
			}
		}()
	}

	wg.Wait()

	expected := numGoroutines * eventsPerGoroutine
	for i := 0; i < expected; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for event %d, got %d", i, count.Load())
		}
	}

	if count.Load() != int32(expected) {
		t.Errorf("expected %d events, got %d", expected, count.Load())
	}
}

func TestEventBusPanicRecovery(t *testing.T) {
	bus := NewInMemoryBus(10)
	defer bus.Close()

	var count atomic.Int32
	done := make(chan struct{}, 1)

	bus.Subscribe(func(ctx context.Context, event Event) {
		panic("intentional panic")
	})

	bus.Subscribe(func(ctx context.Context, event Event) {
		count.Add(1)
		done <- struct{}{}
	})

	evt := NewEvent(EventWorkflowStarted, "test", nil)
	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for second handler")
	}

	if count.Load() != 1 {
		t.Errorf("expected second handler to receive event despite first panicking, got %d", count.Load())
	}
}

func TestEventBusClosedPublish(t *testing.T) {
	bus := NewInMemoryBus(10)
	bus.Close()

	evt := NewEvent(EventWorkflowStarted, "test", nil)
	err := bus.Publish(context.Background(), evt)
	if err == nil {
		t.Error("expected error when publishing to closed bus")
	}
}

func TestEventBusUnsubscribe(t *testing.T) {
	bus := NewInMemoryBus(10)
	defer bus.Close()

	var count atomic.Int32
	done := make(chan struct{}, 1)

	subID := bus.Subscribe(func(ctx context.Context, event Event) {
		count.Add(1)
		select {
		case done <- struct{}{}:
		default:
		}
	})

	evt := NewEvent(EventWorkflowStarted, "test", nil)
	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for first event")
	}

	if count.Load() != 1 {
		t.Fatalf("expected 1 event before unsubscribe, got %d", count.Load())
	}

	if !bus.Unsubscribe(subID) {
		t.Fatal("Unsubscribe should return true for valid subscription")
	}

	if bus.Unsubscribe(subID) {
		t.Fatal("Unsubscribe should return false for already removed subscription")
	}

	if err := bus.Publish(context.Background(), evt); err != nil {
		t.Fatalf("Publish after unsubscribe failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if count.Load() != 1 {
		t.Errorf("expected count to remain 1 after unsubscribe, got %d", count.Load())
	}
}
