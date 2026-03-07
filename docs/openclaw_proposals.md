# OpenClaw Proposal Engine

## Overview

The OpenClaw Proposal Engine transforms detected system patterns into **structured improvement proposals**. These proposals represent suggested improvements to the system, such as:

- Changing agent routing for degraded workflows
- Adding extra review gates to problematic workflows
- Investigating repeated failures in specific workflow types
- Disabling or reducing confidence in underperforming agents
- Introducing retry/escalation logic for repeated tool failures

**Important:** The Proposal Engine is **advisory only**. It does NOT execute actions automatically. Proposals must be reviewed and approved by human operators before any changes are made.

---

## Proposal Lifecycle

1. **Detection** - Pattern Detection Engine identifies system issues
2. **Generation** - Proposal Engine generates proposals from patterns
3. **Deduplication** - Duplicate proposals are suppressed (24h window)
4. **Persistence** - Unique proposals are saved to database
5. **Review** - Human operators review proposals (future)
6. **Approval/Rejection** - Decisions are recorded (future)
7. **Execution** - Approved proposals are executed (future)

Current implementation covers steps 1-4.

---

## Proposal Types

| Type | Description | Example Use Case |
|------|-------------|------------------|
| `routing.change` | Route work away from problematic entity | Critical failure rate in workflow |
| `review_gate.add` | Add quality checkpoint | High rejection rate |
| `workflow.investigate` | Investigate root cause | Elevated failure rate |
| `agent.deprioritize` | Reduce agent priority | Severe performance drop |
| `retry_policy.adjust` | Modify retry logic | Duration spike |

---

## Generation Rules

The engine uses **deterministic, rule-based generation** (no LLM). Each pattern type maps to specific proposal types based on evidence.

### Workflow Failure Cluster → Proposal

| Failure Rate | Proposal Type | Reasoning |
|--------------|---------------|-----------|
| ≥ 70% | `routing.change` | Critical failure rate |
| 50-69% | `review_gate.add` | High failure rate suggests quality issues |
| 30-49% | `workflow.investigate` | Elevated failure rate needs investigation |

### Agent Performance Drop → Proposal

| Performance Drop | Proposal Type | Reasoning |
|------------------|---------------|-----------|
| ≥ 30% | `agent.deprioritize` | Severe performance degradation |
| 20-29% | `routing.change` | Moderate performance drop |

### Review Rejection Spike → Proposal

| Rejection Rate | Proposal Type | Reasoning |
|----------------|---------------|-----------|
| ≥ 50% | `workflow.investigate` | Critical rejection rate |
| 40-49% | `review_gate.add` | Elevated rejection rate |

### Workflow Duration Spike → Proposal

| Duration Multiplier | Proposal Type | Reasoning |
|---------------------|---------------|-----------|
| ≥ 3x | `workflow.investigate` | Critical performance bottleneck |
| 2-3x | `retry_policy.adjust` | Possible transient issues |

---

## Deduplication Strategy

### Deduplication Criteria

A proposal is considered a duplicate if **ALL** of these match:

1. **Same proposal type** (e.g., both `routing.change`)
2. **Same source pattern type** (e.g., both from `workflow.failure_cluster`)
3. **Same affected entity** (extracted from evidence)
4. **Within 24-hour time window** (configurable)

### Affected Entity Extraction

- For workflow patterns: `evidence.affected_workflow`
- For agent patterns: `evidence.agent_id`

### Example

```
Proposal A (created 2 hours ago):
  type: routing.change
  pattern: workflow.failure_cluster
  evidence: {affected_workflow: "spec-complex"}

Proposal B (created now):
  type: routing.change
  pattern: workflow.failure_cluster
  evidence: {affected_workflow: "spec-complex"}

Result: B is duplicate of A (not saved)
```

### Time Window

- **Default:** 24 hours
- **Configurable:** via `EngineConfig.DedupWindowHours`
- **Rationale:** Prevents spam while allowing re-detection if issue persists

---

## Confidence Scoring

### Formula

```
confidence = base_confidence 
           + frequency_boost 
           + severity_boost 
           + evidence_boost
           + recency_boost
```

### Components

| Component | Condition | Boost |
|-----------|-----------|-------|
| **Base** | Pattern confidence from detection | 0.0 - 1.0 |
| **Frequency** | Pattern seen ≥ 5 times | +0.10 |
| | Pattern seen ≥ 3 times | +0.05 |
| **Severity** | Critical severity | +0.10 |
| | High severity | +0.05 |
| **Evidence Volume** | Total events ≥ 20 | +0.10 |
| | Total events ≥ 10 | +0.05 |
| **Recency** | Last seen < 6 hours ago | +0.05 |

### Confidence Ranges

| Range | Evidence Quality | Action |
|-------|------------------|--------|
| 0.40 - 0.60 | Weak | Review carefully |
| 0.61 - 0.75 | Medium | Standard review |
| 0.76 - 0.95 | Strong | High priority |

### Confidence Cap

Maximum confidence is **0.95** (never 100%). This maintains humility in advisory systems and leaves room for human judgment.

---

## Risk Level Determination

Risk levels help operators prioritize proposal review.

### Rules

| Severity | Confidence | Risk Level |
|----------|------------|------------|
| Critical | ≥ 0.70 | **High** |
| Critical | < 0.70 | Low |
| High | Any | **Medium** |
| Medium | Any | Low |
| Low | Any | Low |

### Rationale

- **High risk:** Requires careful review before execution (critical impact)
- **Medium risk:** Standard review process (significant impact)
- **Low risk:** Can be fast-tracked (minor impact)

---

## Proposal Model

### Structure

```go
type Proposal struct {
    ID              string         // UUID v4
    Type            ProposalType   // e.g., routing.change
    Status          ProposalStatus // detected, drafted, approved, rejected, archived
    Title           string         // Human-readable title
    Description     string         // Detailed description
    Confidence      float64        // 0.0 - 0.95
    RiskLevel       RiskLevel      // low, medium, high
    SourcePatternID string         // ID of source pattern
    Evidence        map[string]any // Evidence from pattern
    Recommendation  map[string]any // Machine-readable suggested action
    CreatedAt       time.Time      // Timestamp
    UpdatedAt       time.Time      // Last update
}
```

### Recommendation Payload

The `Recommendation` field contains machine-readable suggested action data for future automation.

#### Example 1: Routing Change

```json
{
  "workflow_type": "spec-complex",
  "suggested_action": "reroute_to_alternate",
  "alternate_workflow": "spec-simple",
  "reason": "75% failure rate over last 20 runs",
  "priority": "high",
  "estimated_impact": "reduce_failures_by_60%"
}
```

#### Example 2: Agent Deprioritization

```json
{
  "agent_id": "mobile-dev-specialist",
  "suggested_action": "deprioritize_for_workflow",
  "workflow_type": "mobile-implementation",
  "reason": "success rate dropped by 24% in last week",
  "current_success_rate": 0.62,
  "previous_success_rate": 0.86,
  "alternative_agent": "generalist-dev"
}
```

#### Example 3: Review Gate Addition

```json
{
  "workflow_type": "bug-fix",
  "suggested_action": "add_review_gate",
  "gate_phase": "before_fix",
  "reason": "40% rejection rate suggests unclear requirements",
  "estimated_reduction": "reduce_rejections_by_50%"
}
```

---

## Usage

### Automatic Generation (Scheduler)

Proposals are generated automatically during scheduled OpenClaw analysis:

```text
Scheduler tick
  → Pattern Detection
  → Proposal Generation  ← NEW
  → Persistence
```

Configure interval in OpenClaw config (default: 1 hour).

### Manual Generation (API)

Trigger proposal generation via API:

```bash
POST /api/openclaw/proposals/generate
```

Response:

```json
{
  "status": "generation_triggered",
  "message": "OpenClaw proposal generation started in background"
}
```

### Query Proposals

List recent proposals:

```bash
GET /api/openclaw/proposals?limit=50&status=detected&min_confidence=0.6
```

Query parameters:
- `limit` (int): Max results (default: 50, max: 200)
- `status` (string): Filter by status (optional)
- `type` (string): Filter by proposal type (optional)
- `min_confidence` (float): Minimum confidence (optional)

Get single proposal:

```bash
GET /api/openclaw/proposals/{id}
```

---

## Database Schema

### Table: `openclaw_proposals`

```sql
CREATE TABLE openclaw_proposals (
    id                TEXT PRIMARY KEY,
    type              TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'detected',
    title             TEXT NOT NULL,
    description       TEXT NOT NULL,
    confidence        REAL NOT NULL,
    risk_level        TEXT NOT NULL DEFAULT 'medium',
    source_pattern_id TEXT NOT NULL,
    evidence          TEXT NOT NULL DEFAULT '{}',
    recommendation    TEXT NOT NULL DEFAULT '{}',
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL
);

CREATE INDEX idx_openclaw_proposals_type ON openclaw_proposals(type);
CREATE INDEX idx_openclaw_proposals_status ON openclaw_proposals(status);
CREATE INDEX idx_openclaw_proposals_pattern ON openclaw_proposals(source_pattern_id);
CREATE INDEX idx_openclaw_proposals_created ON openclaw_proposals(created_at DESC);
```

---

## Architecture

### Package Structure

```
/internal/openclaw/proposals/
├── models.go          # Proposal model, types, enums
├── generators.go      # Pattern → Proposal generators
├── engine.go          # Proposal generation engine
├── store.go           # Persistence layer
└── engine_test.go     # Tests
```

### Flow Diagram

```
┌─────────────────┐
│  Pattern Store  │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  Proposal Engine│
│  - Load patterns│
│  - Generate     │
│  - Deduplicate  │
│  - Validate     │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Proposal Store  │
│   (Database)    │
└─────────────────┘
```

### Integration Points

1. **OpenClaw Scheduler** - Triggers proposal generation after pattern detection
2. **OpenClaw Engine** - Manages proposal engine lifecycle
3. **API Server** - Exposes proposals via REST endpoints
4. **Database** - Persists proposals for querying

---

## Configuration

### EngineConfig

```go
type EngineConfig struct {
    DedupWindowHours int     // Deduplication window (default: 24)
    MinConfidence    float64 // Minimum confidence to save (default: 0.40)
    MaxProposals     int     // Max proposals per run (default: 100)
}
```

### Default Values

```go
DedupWindowHours: 24
MinConfidence:    0.40
MaxProposals:     100
```

---

## Logging

### Structured Logs

The engine produces structured logs for observability:

#### Generation Start

```
INFO openclaw proposals: generation started
```

#### Proposal Generated

```
INFO proposal generated id=550e8400... type=routing.change risk=high confidence=0.88 pattern_id=42
```

#### Proposal Deduplicated

```
INFO proposal deduplicated type=routing.change pattern_id=42 similar_id=abc123...
```

#### Generation Complete

```
INFO openclaw proposals: generation complete generated=5 saved=3 deduplicated=2 duration_ms=234
```

---

## Testing

### Test Coverage

| Test | Purpose |
|------|---------|
| `TestGenerateProposalFromWorkflowFailureCluster` | Verify workflow failure → proposal mapping |
| `TestGenerateProposalFromAgentPerformanceDrop` | Verify agent performance → proposal mapping |
| `TestProposalDeduplication` | Verify duplicate suppression |
| `TestProposalConfidenceScoring` | Verify confidence calculation |
| `TestProposalStoreSaveAndLoad` | Verify persistence |
| `TestMultipleGenerators` | Verify multiple generators work together |
| `TestRiskLevelDetermination` | Verify risk level assignment |

### Running Tests

```bash
cd internal/openclaw/proposals
go test -v
```

---

## Future Enhancements

The following features are **intentionally out of scope** for this implementation:

- ❌ Proposal approval workflow
- ❌ Automated execution engine
- ❌ A/B testing framework
- ❌ LLM-based proposal generation
- ❌ Proposal rollback mechanism
- ❌ Impact measurement system
- ❌ Proposal prioritization beyond risk level
- ❌ Multi-criteria decision support
- ❌ Historical proposal analysis

These may be added in future iterations based on operational feedback.

---

## Troubleshooting

### No Proposals Generated

**Possible causes:**

1. **No patterns detected** - Check pattern detection is working
2. **Confidence too low** - Lower `MinConfidence` threshold
3. **All proposals deduplicated** - Check logs for deduplication messages

**Solution:** Check OpenClaw logs for details.

### Too Many Proposals

**Possible causes:**

1. **Deduplication window too short** - Increase `DedupWindowHours`
2. **Sensitivity too high** - Adjust pattern detection thresholds

**Solution:** Tune configuration parameters.

### Proposals Missing Expected Fields

**Possible causes:**

1. **Generator error** - Check logs for generator failures
2. **Pattern evidence incomplete** - Verify pattern detection includes required fields

**Solution:** Review pattern detection implementation.

---

## Security & Safety

### Advisory Only

The proposal engine **never executes changes automatically**. All proposals are advisory and require human review.

### No Side Effects

Proposal generation has no side effects:
- Does not modify workflows
- Does not change agent routing
- Does not affect running processes

### Fail-Safe Design

- **Deduplication errors:** Fail open (allow proposal if dedup check fails)
- **Database errors:** Log and continue with other proposals
- **Generator errors:** Log and continue with other generators

---

## Performance

### Metrics

- **Generation time:** < 500ms for 100 patterns
- **Memory usage:** Minimal (streaming processing)
- **Database load:** Light (batch inserts)

### Scalability

- **Pattern volume:** Handles 1000+ patterns per run
- **Proposal volume:** Stores unlimited proposals (prune old proposals as needed)
- **Concurrent runs:** Thread-safe (mutex protected)

---

## Summary

The OpenClaw Proposal Engine provides:

✅ **Structured proposals** from detected patterns  
✅ **Deterministic generation** (no LLM, fully reproducible)  
✅ **Intelligent deduplication** (24-hour window)  
✅ **Confidence scoring** (evidence-based)  
✅ **Risk assessment** (helps prioritize review)  
✅ **Machine-readable recommendations** (future automation)  
✅ **Full observability** (structured logging + API)  
✅ **Advisory only** (no automatic execution)  

The engine transforms detected system issues into clear, reviewable, actionable recommendations while maintaining safety and observability.
