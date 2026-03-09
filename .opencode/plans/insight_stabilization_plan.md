# Insight Stabilization Plan

## Executive Summary

The Insight integration has **critical runtime bugs** that will cause immediate panics on startup. The module cannot function in its current state due to:
1. Nil pointer dereferences
2. Uninitialized channels
3. Disconnected lifecycle management

This plan focuses on fixing these blockers and hardening the implementation for production use.

---

## Issues Discovered

### 🔴 Critical: Will Cause Immediate Panics

#### 1. Nil Engine Reference in Scheduler
**Location:** `scheduler.go:8`, `engine.go:22`

```go
// engine.go:22 - scheduler created with zero values
scheduler: &Scheduler{}

// scheduler.go:8 - engine field is nil
type Scheduler struct {
    engine *Engine  // nil!
}
```

**Impact:** When scheduler tries to access `s.engine.config.Interval` (line 14), it panics with nil pointer dereference.

#### 2. Uninitialized Stop Channel
**Location:** `scheduler.go:10`

```go
type Scheduler struct {
    stopCh chan struct{}  // nil!
}
```

**Impact:** 
- `s.stopCh` is nil → listening on nil channel blocks forever (line 25)
- `close(s.stopCh)` panics when trying to close nil channel (line 33)

#### 3. Disconnected Lifecycle
**Location:** `engine.go:50-57`

```go
go e.scheduler.Start()  // starts scheduler

func (e *Engine) Stop() {
    close(e.stopCh)  // closes ENGINE's stopCh, not scheduler's
}
```

**Impact:** 
- Engine's `stopCh` never used
- Scheduler has its own uninitialized `stopCh`
- Calling `Engine.Stop()` does NOT stop the scheduler
- Goroutine leak: scheduler runs forever

---

### 🟡 High: Runtime Safety Issues

#### 4. No Double-Start Protection
**Location:** `engine.go:27-54`

```go
func (e *Engine) Start() error {
    // No guard against multiple calls
    go e.scheduler.Start()  // Each call spawns new goroutine
}
```

**Impact:** Calling `Start()` twice spawns duplicate scheduler goroutines, leading to:
- Multiple concurrent analyses
- Race conditions on database writes
- Duplicate patterns/proposals

#### 5. Manual Channel Management
**Location:** `scheduler.go:25`, `engine.go:15`

Current approach uses manual `stopCh` channels which are:
- Error-prone (nil channels)
- Not composable
- Hard to test
- No timeout support

---

### 🟠 Medium: Code Quality Issues

#### 6. Inconsistent Logging
**Location:** Throughout module

```go
fmt.Println("Insight engine started")           // engine.go:52
fmt.Println("Insight: No metrics data available") // analysis.go:25
fmt.Printf("warning: failed to save pattern: %v\n", err) // analysis.go:236
```

**Issues:**
- Uses `fmt` instead of structured `log` package
- Inconsistent message formatting
- No log levels
- Rest of codebase uses `log.Printf`

#### 7. No Error Propagation from Scheduler
**Location:** `scheduler.go:24`

```go
case <-s.ticker.C:
    s.engine.RunAnalysis()  // Error ignored!
```

**Impact:** Analysis failures are silently ignored, no visibility into problems.

#### 8. Non-Idempotent Analysis
**Location:** `analysis.go:234-249`

```go
// Always saves new patterns/proposals, no deduplication
for _, pattern := range patterns {
    if err := e.database.SaveInsightPattern(pattern); err != nil {
        fmt.Printf("warning: failed to save pattern: %v\n", err)
    }
}
```

**Impact:** Running analysis multiple times creates duplicate entries in database.

---

### 🔵 Low: Testing & Maintainability

#### 9. No Tests
**Location:** `insight/*_test.go` - doesn't exist

Zero test coverage for:
- Engine lifecycle
- Scheduler execution
- Analysis logic
- Error handling

#### 10. Tight Coupling
**Location:** `analysis.go` directly calls `e.database.*`

Analysis functions are hard to test because they're methods on Engine and directly access database.

---

## Proposed Fixes

### Phase 1: Fix Critical Panics (MUST DO FIRST)

#### Fix 1.1: Wire Engine to Scheduler

**File:** `engine.go`

```go
func NewEngine(database *db.DB, cfg config.InsightConfig) *Engine {
    e := &Engine{
        database:  database,
        config:    cfg,
        stopCh:    make(chan struct{}),
    }
    // Create scheduler WITH engine reference
    e.scheduler = &Scheduler{
        engine: e,  // Wire the dependency!
    }
    return e
}
```

#### Fix 1.2: Initialize Scheduler Stop Channel

**File:** `scheduler.go`

```go
func (s *Scheduler) Start() {
    s.stopCh = make(chan struct{})  // Initialize before use
    // ... rest of code
}
```

Or better: initialize in constructor.

#### Fix 1.3: Connect Engine Stop to Scheduler

**File:** `engine.go`

```go
func (e *Engine) Stop() {
    if e.scheduler != nil {
        e.scheduler.Stop()  // Stop the scheduler!
    }
    close(e.stopCh)
}
```

---

### Phase 2: Replace Channels with Context

#### Fix 2.1: Context-Based Scheduler

**File:** `scheduler.go`

```go
type Scheduler struct {
    engine   *Engine
    ticker   *time.Ticker
    stopCh   chan struct{}  // Remove this
    running  bool
    mu       sync.Mutex
}

func (s *Scheduler) Run(ctx context.Context) error {
    s.mu.Lock()
    if s.running {
        s.mu.Unlock()
        return errors.New("scheduler already running")
    }
    s.running = true
    s.mu.Unlock()
    
    defer func() {
        s.mu.Lock()
        s.running = false
        s.mu.Unlock()
    }()
    
    interval := s.engine.config.Interval
    if interval <= 0 {
        interval = 1
    }
    
    s.ticker = time.NewTicker(time.Duration(interval) * time.Hour)
    defer s.ticker.Stop()
    
    for {
        select {
        case <-s.ticker.C:
            if err := s.engine.RunAnalysis(); err != nil {
                log.Printf("insight: analysis failed: %v", err)
            }
        case <-ctx.Done():
            return ctx.Err()
        }
    }
}
```

#### Fix 2.2: Engine with Context

**File:** `engine.go`

```go
type Engine struct {
    database  *db.DB
    config    config.InsightConfig
    scheduler *Scheduler
    
    ctx       context.Context
    cancel    context.CancelFunc
    running   bool
    mu        sync.Mutex
}

func (e *Engine) Start(ctx context.Context) error {
    e.mu.Lock()
    defer e.mu.Unlock()
    
    if e.running {
        return errors.New("engine already running")
    }
    
    // Initialize state (existing code)
    state, err := e.database.GetInsightState()
    if err != nil {
        return fmt.Errorf("get state: %w", err)
    }
    // ... state initialization ...
    
    // Create cancellable context
    e.ctx, e.cancel = context.WithCancel(ctx)
    e.running = true
    
    go func() {
        defer func() {
            e.mu.Lock()
            e.running = false
            e.mu.Unlock()
        }()
        
        if err := e.scheduler.Run(e.ctx); err != nil && err != context.Canceled {
            log.Printf("insight: scheduler stopped with error: %v", err)
        }
    }()
    
    log.Println("insight: engine started")
    return nil
}

func (e *Engine) Stop() {
    e.mu.Lock()
    defer e.mu.Unlock()
    
    if !e.running {
        return
    }
    
    if e.cancel != nil {
        e.cancel()
    }
    e.running = false
    
    log.Println("insight: engine stopped")
}
```

---

### Phase 3: Add Structured Logging

#### Fix 3.1: Replace All fmt.Println/Printf

**Pattern:**
```go
// Before
fmt.Println("Insight engine started")
fmt.Printf("warning: failed to save pattern: %v\n", err)

// After
log.Println("insight: engine started")
log.Printf("insight: failed to save pattern: %v", err)
```

**Files to update:**
- `engine.go`: lines 52
- `analysis.go`: lines 25, 236, 247, 284, 288

#### Fix 3.2: Add Log Messages for Key Events

```go
// Engine start/stop
log.Println("insight: engine started")
log.Println("insight: engine stopped")

// Analysis lifecycle
log.Println("insight: analysis started")
log.Printf("insight: analysis complete: patterns=%d proposals=%d duration=%dms", 
    len(patterns), len(proposals), executionTime)

// Pattern detection
log.Printf("insight: detected pattern: type=%s name=%s confidence=%.2f",
    pattern.PatternType, pattern.PatternName, pattern.Confidence)

// Proposal generation
log.Printf("insight: generated proposal: type=%s title=%s confidence=%.2f",
    proposal.Type, proposal.Title, proposal.Confidence)

// Errors
log.Printf("insight: get daily metrics failed: %v", err)
log.Printf("insight: save pattern failed: %v", err)
```

---

### Phase 4: Error Handling Improvements

#### Fix 4.1: Propagate Scheduler Errors

Already covered in Phase 2 context-based approach.

#### Fix 4.2: Add Error Wrapping

```go
// Before
if err != nil {
    return fmt.Errorf("get daily metrics: %w", err)
}

// Already good, ensure consistency everywhere
```

---

### Phase 5: Idempotency Guards

#### Fix 5.1: Check for Duplicate Patterns

**File:** `analysis.go` or new database method

Option A: Add unique constraint in database (requires schema change - avoid per requirements)

Option B: Check before inserting (choose this)

```go
func (e *Engine) savePatternIfNew(pattern *db.InsightPattern) error {
    // Check if pattern already exists
    existing, err := e.database.FindPatternByName(pattern.PatternName)
    if err != nil {
        return fmt.Errorf("check existing pattern: %w", err)
    }
    
    if existing != nil {
        // Update frequency instead of creating duplicate
        existing.Frequency++
        existing.LastSeen = time.Now().UTC().Format(time.RFC3339Nano)
        return e.database.UpdateInsightPattern(existing)
    }
    
    // Pattern is new, save it
    return e.database.SaveInsightPattern(pattern)
}
```

#### Fix 5.2: Limit Proposals by Time Window

```go
// Before saving proposals, check recent count
recentProposals, err := e.database.CountProposalsLastHours(24)
if err != nil {
    log.Printf("insight: failed to count recent proposals: %v", err)
} else if recentProposals >= e.config.MaxProposals {
    log.Printf("insight: skipping proposals, max daily limit reached: %d", recentProposals)
    proposals = nil
}
```

---

### Phase 6: Dependency Injection Cleanup

#### Fix 6.1: Constructor Pattern

```go
type Engine struct {
    database  *db.DB
    config    config.InsightConfig
    scheduler *Scheduler
    logger    *log.Logger  // Optional: for testing
    
    ctx       context.Context
    cancel    context.CancelFunc
    running   bool
    mu        sync.Mutex
}

func NewEngine(database *db.DB, cfg config.InsightConfig) *Engine {
    e := &Engine{
        database: database,
        config:   cfg,
    }
    
    e.scheduler = NewScheduler(e)
    
    return e
}

func NewScheduler(engine *Engine) *Scheduler {
    return &Scheduler{
        engine: engine,
    }
}
```

---

### Phase 7: Add Tests

#### Test 7.1: Engine Lifecycle

**File:** `insight/engine_test.go`

```go
package insight_test

import (
    "context"
    "testing"
    "time"
    
    "github.com/MartinNevlaha/stratus-v2/config"
    "github.com/MartinNevlaha/stratus-v2/insight"
)

func TestEngineStartStop(t *testing.T) {
    // Setup mock DB
    db := setupTestDB(t)
    defer db.Close()
    
    cfg := config.InsightConfig{
        Enabled:  true,
        Interval: 1,
    }
    
    engine := insight.NewEngine(db, cfg)
    
    // Test start
    ctx := context.Background()
    if err := engine.Start(ctx); err != nil {
        t.Fatalf("Start failed: %v", err)
    }
    
    // Test double-start protection
    err := engine.Start(ctx)
    if err == nil {
        t.Error("expected error on double start")
    }
    
    // Test stop
    engine.Stop()
    
    // Verify clean shutdown
    time.Sleep(100 * time.Millisecond)
}

func TestEngineContextCancellation(t *testing.T) {
    db := setupTestDB(t)
    defer db.Close()
    
    cfg := config.InsightConfig{
        Enabled:  true,
        Interval: 1,
    }
    
    engine := insight.NewEngine(db, cfg)
    
    ctx, cancel := context.WithCancel(context.Background())
    
    if err := engine.Start(ctx); err != nil {
        t.Fatalf("Start failed: %v", err)
    }
    
    // Cancel context
    cancel()
    
    // Engine should stop
    time.Sleep(100 * time.Millisecond)
    
    // Verify engine is not running
}
```

#### Test 7.2: Analysis Logic

**File:** `insight/analysis_test.go`

```go
func TestAnalyzeMetrics(t *testing.T) {
    db := setupTestDBWithMetrics(t)
    defer db.Close()
    
    cfg := config.InsightConfig{
        Enabled:       true,
        Interval:      1,
        MaxProposals:  5,
        MinConfidence: 0.7,
    }
    
    engine := insight.NewEngine(db, cfg)
    
    err := engine.RunAnalysis()
    if err != nil {
        t.Fatalf("RunAnalysis failed: %v", err)
    }
    
    // Verify patterns were created
    patterns, err := db.ListInsightPatterns("", 0, 100)
    if err != nil {
        t.Fatalf("ListInsightPatterns failed: %v", err)
    }
    
    if len(patterns) == 0 {
        t.Error("expected patterns to be created")
    }
}

func TestAnalyzeMetricsEmptyDB(t *testing.T) {
    db := setupEmptyTestDB(t)
    defer db.Close()
    
    cfg := config.InsightConfig{
        Enabled:  true,
        Interval: 1,
    }
    
    engine := insight.NewEngine(db, cfg)
    
    // Should not crash on empty metrics
    err := engine.RunAnalysis()
    if err != nil {
        t.Fatalf("RunAnalysis failed on empty DB: %v", err)
    }
}
```

---

## Implementation Order

### Immediate (Blocks All Testing)

1. **Fix nil scheduler.engine** (Fix 1.1)
2. **Fix nil scheduler.stopCh** (Fix 1.2)  
3. **Connect Engine.Stop to Scheduler** (Fix 1.3)

### High Priority (Runtime Safety)

4. **Replace channels with context** (Phase 2)
5. **Add double-start protection** (Phase 2)
6. **Add structured logging** (Phase 3)

### Medium Priority (Data Integrity)

7. **Add idempotency guards** (Phase 5)
8. **Improve error handling** (Phase 4)

### Lower Priority (Maintainability)

9. **Refactor dependency injection** (Phase 6)
10. **Add test coverage** (Phase 7)

---

## Remaining Risks After Fixes

### Risk 1: No Graceful Degradation
If database becomes unavailable mid-analysis, the engine continues running but logs errors. Consider adding circuit breaker pattern.

**Mitigation:** Add health check and auto-disable on repeated failures.

### Risk 2: No Backpressure
If analysis takes longer than interval, multiple analyses could run concurrently.

**Mitigation:** Add mutex to ensure only one analysis runs at a time.

### Risk 3: No Metrics on Engine Health
No visibility into whether engine is running, stuck, or degraded.

**Mitigation:** Add `/api/insight/health` endpoint that returns:
- Running status
- Time since last analysis
- Error count
- Goroutine count

### Risk 4: Pattern Accumulation
Patterns accumulate indefinitely without cleanup.

**Mitigation:** Add TTL or max count for old patterns.

---

## Testing Strategy

### Unit Tests
- Mock database interface
- Test analysis logic in isolation
- Test pattern detection thresholds

### Integration Tests
- Use in-memory SQLite
- Test full lifecycle
- Verify database state changes

### Manual Testing Checklist
```bash
# 1. Build and run
go build -o stratus ./cmd/stratus
./stratus serve

# 2. Check logs for startup
# Expected: "insight: engine started"

# 3. Trigger manual analysis
curl -X POST http://localhost:41777/api/insight/trigger

# 4. Check logs for analysis
# Expected: "insight: analysis started"
# Expected: "insight: analysis complete: patterns=N proposals=M"

# 5. Verify no goroutine leaks
# Kill server and check for clean shutdown
# Expected: "insight: engine stopped"

# 6. Restart server
# Expected: No errors, clean startup

# 7. Test double-start protection (hard to trigger manually)
```

---

## Future Improvements (Out of Scope)

These are NOT part of this stabilization but worth noting:

1. **Metrics Export:** Prometheus metrics for monitoring
2. **Circuit Breaker:** Auto-disable on repeated failures
3. **Rate Limiting:** Prevent runaway proposal generation
4. **Pattern Decay:** Reduce confidence of old patterns
5. **ML Model Integration:** Use configured LLM for smarter analysis
6. **Web UI:** Dashboard for viewing patterns/proposals
7. **Proposal Review Workflow:** Human-in-the-loop acceptance

---

## Definition of Done

- [ ] Engine starts without panic
- [ ] Engine stops cleanly (no goroutine leaks)
- [ ] Scheduler stops on context cancellation
- [ ] Double-start returns error
- [ ] All fmt.Println replaced with log.Printf
- [ ] Analysis logs key events
- [ ] Analysis doesn't crash on empty metrics
- [ ] Basic unit tests pass
- [ ] Integration test with real DB passes
- [ ] Manual testing checklist completed
- [ ] Code review approved

---

## Estimated Effort

- Phase 1 (Critical fixes): 2-3 hours
- Phase 2 (Context refactor): 2-3 hours
- Phase 3 (Logging): 1 hour
- Phase 4 (Error handling): 1 hour
- Phase 5 (Idempotency): 2-3 hours
- Phase 6 (DI cleanup): 1 hour
- Phase 7 (Tests): 3-4 hours
- Documentation: 1 hour

**Total: 13-17 hours**

---

## Code Review Checklist

When reviewing the fixes:

- [ ] No new dependencies added
- [ ] No schema changes
- [ ] All context cancellations handled
- [ ] All goroutines have exit conditions
- [ ] All errors logged
- [ ] No silent failures
- [ ] Logs follow consistent format
- [ ] Tests cover happy path and error cases
- [ ] No exported types changed (API compatibility)

---

## Questions for User

Before proceeding with implementation, I have a few clarifying questions:

1. **Idempotency Approach:** For preventing duplicate patterns, should I:
   - Add a `FindPatternByName()` method to check before insert?
   - Or use a simpler approach like checking timestamp proximity?
   
2. **Breaking Change Tolerance:** The `Start()` method signature will change from `Start()` to `Start(ctx context.Context)`. This requires updating the call site in `cmd/stratus/main.go`. Is this acceptable?

3. **Test Database Setup:** For tests, should I:
   - Create a shared test utility that sets up in-memory SQLite?
   - Or use a simpler mock interface for the database?
   
4. **Analysis Mutex:** Should I add mutex protection to prevent concurrent analyses, or is this out of scope for stabilization?

5. **Context Propagation:** The main.go doesn't currently pass a context to `insight.Start()`. Should I:
   - Create a background context: `context.Background()`
   - Or create a cancellable context tied to the signal handler?
