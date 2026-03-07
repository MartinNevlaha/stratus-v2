# OpenClaw Event Ingestion Layer

The OpenClaw Event Ingestion Layer provides real-time observability into the Stratus system by capturing and persisting events from various system components.

## Overview

OpenClaw observes system behavior through events emitted by:
- Workflow lifecycle (start, complete, fail, abort, phase transitions)
- Agent lifecycle (spawn, complete, fail)
- Proposal lifecycle (create, accept, reject)
- Review lifecycle (start, pass, fail)

## Architecture

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   Orchestrator  │     │   API Routes    │     │    Swarm/Other  │
│   (workflow)    │     │   (proposals)   │     │                 │
└────────┬────────┘     └────────┬────────┘     └────────┬────────┘
         │                       │                       │
         │                       │                       │
         └───────────────────────┼───────────────────────┘
                                 │
                                 ▼
                    ┌────────────────────────┐
                    │      EventBus          │
                    │   (InMemoryBus)        │
                    └───────────┬────────────┘
                                │
                    ┌───────────┼───────────┐
                    │           │           │
                    ▼           ▼           ▼
            ┌───────────┐ ┌───────────┐ ┌───────────┐
            │ OpenClaw  │ │  Future   │ │  Future   │
            │  Engine   │ │  Handler  │ │  Handler  │
            └─────┬─────┘ └───────────┘ └───────────┘
                  │
                  ▼
          ┌───────────────┐
          │  EventStore   │
          │   (SQLite)    │
          └───────────────┘
```

## Event Model

### Event Structure

```go
type Event struct {
    ID        string         // Unique identifier (UUID)
    Type      EventType      // Event type (e.g., workflow.started)
    Timestamp time.Time      // UTC timestamp
    Source    string         // Component that emitted the event
    Payload   map[string]any // Event-specific data
}
```

### Event Types

#### Workflow Events

| Type | Description | Payload |
|------|-------------|---------|
| `workflow.started` | New workflow created | `workflow_id`, `type`, `complexity`, `title` |
| `workflow.completed` | Workflow finished successfully | `workflow_id`, `type` |
| `workflow.failed` | Workflow failed | `workflow_id`, `error` |
| `workflow.aborted` | Workflow manually aborted | `workflow_id`, `type` |
| `workflow.phase_transition` | Phase changed | `workflow_id`, `from_phase`, `to_phase` |

#### Agent Events

| Type | Description | Payload |
|------|-------------|---------|
| `agent.spawned` | New agent started | `agent_id`, `type` |
| `agent.completed` | Agent finished successfully | `agent_id` |
| `agent.failed` | Agent failed | `agent_id`, `error` |

#### Proposal Events

| Type | Description | Payload |
|------|-------------|---------|
| `proposal.created` | New proposal created | `proposal_id`, `type`, `title` |
| `proposal.accepted` | Proposal accepted | `proposal_id`, `applied` |
| `proposal.rejected` | Proposal rejected | `proposal_id` |

#### Review Events

| Type | Description | Payload |
|------|-------------|---------|
| `review.started` | Review started | `review_id` |
| `review.passed` | Review passed | `review_id` |
| `review.failed` | Review failed | `review_id`, `reason` |

## Event Bus

The `InMemoryBus` provides a lightweight, in-process event distribution mechanism:

```go
// Create a bus with a buffer size of 100
bus := events.NewInMemoryBus(100)

// Subscribe to events (returns a subscription ID for later unsubscription)
subID := bus.Subscribe(func(ctx context.Context, event events.Event) {
    log.Printf("Received event: %s", event.Type)
})

// Publish events (non-blocking)
event := events.NewEvent(events.EventWorkflowStarted, "orchestration", map[string]any{
    "workflow_id": "wf-123",
})
bus.Publish(context.Background(), event)

// Unsubscribe when done
bus.Unsubscribe(subID)

// Clean shutdown
bus.Close()
```

### Features

- **Non-blocking publish**: Events are buffered and dispatched asynchronously
- **Multiple subscribers**: All subscribers receive each event
- **Unsubscribe support**: Subscribers can be removed at runtime
- **Thread-safe**: Safe for concurrent use
- **Panic recovery**: Panicking handlers don't affect other subscribers
- **Graceful shutdown**: Drains remaining events on close

### Event Ordering

Events are **not guaranteed** to be delivered in the order they were published. This is by design for performance:

- Events are dispatched to handlers in separate goroutines
- Multiple handlers process events concurrently
- For time-based analysis, use the event's `Timestamp` field

If strict ordering is required for a specific use case, consider:
1. Using the `Timestamp` field to sort events after retrieval
2. Implementing a sequencer pattern in the consumer

## Event Store

Events are persisted to SQLite for historical analysis:

```go
// Create a store
store := events.NewDBStore(db.SQL())

// Save an event
store.SaveEvent(ctx, event)

// Query recent events
events, err := store.GetRecentEvents(ctx, 100)

// Query by type
events, err := store.GetEventsByType(ctx, events.EventWorkflowStarted, 50)
```

### Database Schema

```sql
CREATE TABLE openclaw_events (
    id         TEXT PRIMARY KEY,
    type       TEXT NOT NULL,
    timestamp  TEXT NOT NULL,
    source     TEXT NOT NULL,
    payload    TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX idx_openclaw_events_type ON openclaw_events(type);
CREATE INDEX idx_openclaw_events_timestamp ON openclaw_events(timestamp DESC);
```

## Integration

### OpenClaw Engine

The OpenClaw engine subscribes to the event bus to observe system behavior:

```go
engine := openclaw.NewEngineWithEvents(database, cfg, eventBus)
```

The engine's `HandleEvent` method:
1. Logs the event using structured logging
2. Persists the event to the database

### Emitting Events

#### From Coordinator

```go
c.emitEvent(events.EventWorkflowStarted, "orchestration", map[string]any{
    "workflow_id": id,
    "type":        string(wtype),
})
```

#### From API Routes

```go
s.emitEvent(events.EventProposalCreated, "api", map[string]any{
    "proposal_id": id,
    "type":        p.Type,
})
```

## Usage Example

```go
package main

import (
    "context"
    "log/slog"
    
    "github.com/MartinNevlaha/stratus-v2/openclaw/events"
)

func main() {
    // Create event bus
    bus := events.NewInMemoryBus(100)
    defer bus.Close()
    
    // Subscribe to workflow events
    bus.Subscribe(func(ctx context.Context, event events.Event) {
        if event.Type.Category() == "workflow" {
            slog.Info("workflow event",
                "type", event.Type,
                "workflow_id", event.Payload["workflow_id"])
        }
    })
    
    // Emit a workflow started event
    evt := events.NewEvent(events.EventWorkflowStarted, "example", map[string]any{
        "workflow_id": "wf-001",
        "type":        "spec",
    })
    bus.Publish(context.Background(), evt)
}
```

## Future Enhancements

The event ingestion layer provides the foundation for:

1. **Pattern Detection**: Analyze event sequences to identify recurring patterns
2. **Anomaly Detection**: Flag unusual event patterns
3. **Predictive Analysis**: Predict failures before they occur
4. **Automation**: Trigger actions based on event patterns
5. **Metrics**: Aggregate events into performance metrics

## Configuration

Events are enabled by default when using `NewEngineWithEvents`. No additional configuration is required.

### Context and Timeouts

Event emission uses a 5-second timeout to prevent blocking:

```go
// In coordinator and API server
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
eventBus.Publish(ctx, event)
```

If the event buffer is full and the timeout is exceeded, the event is dropped. This ensures system stability under high load.

## Performance

- Events are published asynchronously and don't block the emitter
- Buffer size can be tuned based on event volume (default: 100)
- Database writes are performed in the background by OpenClaw
- Minimal overhead on system performance
- Subscribers process events concurrently
