package events

import (
	"context"
	"sync"
	"sync/atomic"
)

type Handler func(ctx context.Context, event Event)

type SubscriptionID int64

type EventBus interface {
	Publish(ctx context.Context, event Event) error
	Subscribe(handler Handler) SubscriptionID
	Unsubscribe(id SubscriptionID) bool
	Close()
}

type InMemoryBus struct {
	mu        sync.RWMutex
	handlers  map[SubscriptionID]Handler
	nextSubID atomic.Int64
	buffer    chan Event
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closed    atomic.Bool
}

func NewInMemoryBus(bufferSize int) *InMemoryBus {
	if bufferSize <= 0 {
		bufferSize = 100
	}
	ctx, cancel := context.WithCancel(context.Background())
	bus := &InMemoryBus{
		handlers: make(map[SubscriptionID]Handler),
		buffer:   make(chan Event, bufferSize),
		ctx:      ctx,
		cancel:   cancel,
	}
	bus.wg.Add(1)
	go bus.dispatch()
	return bus
}

func (b *InMemoryBus) Publish(ctx context.Context, event Event) error {
	if b.closed.Load() {
		return context.Canceled
	}
	select {
	case b.buffer <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-b.ctx.Done():
		return b.ctx.Err()
	default:
		select {
		case b.buffer <- event:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-b.ctx.Done():
			return b.ctx.Err()
		}
	}
}

func (b *InMemoryBus) Subscribe(handler Handler) SubscriptionID {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := SubscriptionID(b.nextSubID.Add(1))
	b.handlers[id] = handler
	return id
}

func (b *InMemoryBus) Unsubscribe(id SubscriptionID) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.handlers[id]; exists {
		delete(b.handlers, id)
		return true
	}
	return false
}

func (b *InMemoryBus) Close() {
	if !b.closed.CompareAndSwap(false, true) {
		return
	}
	b.cancel()
	close(b.buffer)
	b.wg.Wait()
}

func (b *InMemoryBus) dispatch() {
	defer b.wg.Done()
	for {
		select {
		case event, ok := <-b.buffer:
			if !ok {
				return
			}
			b.deliver(event)
		case <-b.ctx.Done():
			return
		}
	}
}

func (b *InMemoryBus) deliver(event Event) {
	b.mu.RLock()
	handlers := make([]Handler, 0, len(b.handlers))
	for _, h := range b.handlers {
		handlers = append(handlers, h)
	}
	b.mu.RUnlock()

	for _, h := range handlers {
		if b.closed.Load() {
			return
		}
		go func(handler Handler) {
			defer func() {
				if r := recover(); r != nil {
				}
			}()
			handler(b.ctx, event)
		}(h)
	}
}
