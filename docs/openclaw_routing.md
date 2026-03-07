# OpenClaw Routing Intelligence

## Purpose

The Routing Intelligence Engine analyzes historical performance data to generate **routing recommendations** for Stratus. It provides advisory insights about which agents should be assigned to which workflows based on observed success patterns.

**Important**: Routing recommendations are **advisory only**. OpenClaw does NOT automatically change routing behavior. All routing decisions require human review and approval.

## Capabilities

The Routing Intelligence Engine can answer questions such as:

- Which agents perform best for a given workflow type?
- Which agents should be avoided for specific tasks?
- Should a workflow be routed differently based on performance history?
- Are fallback agents required for unstable workflows?

## Recommendation Types

### 1. Best Agent Recommendation

Identifies the agent with the best historical performance for a workflow type.

**Trigger Conditions**:
- Agent success rate is 15%+ above the average for the workflow
- Agent success rate exceeds 70%
- Minimum 5 observations

**Example Evidence**:
```json
{
  "agent_success_rate": 0.84,
  "avg_success_rate": 0.62,
  "improvement": 0.22,
  "observations": 50,
  "trend": "stable"
}
```

### 2. Agent Deprioritization

Flags agents whose performance has degraded significantly.

**Trigger Conditions**:
- Agent success rate < 50%
- Trend is "degrading"
- Minimum 5 observations

**Example Evidence**:
```json
{
  "agent_success_rate": 0.43,
  "failure_rate": 0.57,
  "rework_rate": 0.30,
  "trend": "degrading"
}
```

### 3. Fallback Needed

Identifies workflows that would benefit from fallback routing.

**Trigger Conditions**:
- Workflow failure rate >= 30%
- Only 1 agent configured for the workflow

**Example Evidence**:
```json
{
  "workflow_failure_rate": 0.41,
  "workflow_completion_rate": 0.59,
  "agent_count": 1,
  "rework_rate": 0.25
}
```

### 4. Workflow Instability

Flags workflows showing signs of instability requiring investigation.

**Trigger Conditions**:
- Rework rate >= 35%
- OR (Rework rate >= 25% AND Review rejection rate >= 30%)

**Example Evidence**:
```json
{
  "rework_rate": 0.45,
  "review_rejection_rate": 0.35,
  "completion_rate": 0.55,
  "failure_rate": 0.30
}
```

## Confidence Scoring

Confidence is calculated based on multiple factors:

### Base Confidence (Observation Count)

| Observations | Base Factor |
|--------------|-------------|
| < 5          | 0.30        |
| 5-9          | 0.50        |
| 10-29        | 0.70        |
| 30-49        | 0.80        |
| 50+          | 0.90        |

### Adjustments

| Factor | Adjustment |
|--------|------------|
| Trend = improving | +0.10 |
| Trend = degrading | -0.10 |
| Metric consistency | ±0.10 |

### Final Confidence

- Maximum: 0.95
- Minimum: 0.25

### Confidence Levels

| Level | Range |
|-------|-------|
| High | >= 0.75 |
| Medium | 0.45 - 0.74 |
| Low | < 0.45 |

## Risk Levels

Risk levels indicate the severity of acting (or not acting) on a recommendation:

| Level | When |
|-------|------|
| Low | Best agent recommendation with high confidence |
| Medium | Default for most recommendations |
| High | Severe performance issues (>50% failure) |
| Critical | Critical workflow instability (>50% failure + fallback needed) |

## Evidence Model

All recommendations include evidence with core metrics:

```go
type Evidence struct {
    // Core metrics (always present)
    Observations   int     // Number of data points
    
    // Type-specific metrics
    AgentSuccessRate     float64 // For best_agent, deprioritize
    WorkflowFailureRate  float64 // For fallback_needed
    ReworkRate           float64 // For instability
    ReviewRejectionRate  float64 // For instability
    Trend                string  // improving, degrading, stable
    
    // Comparative metrics
    AvgSuccessRate       float64 // Average for comparison
    Improvement          float64 // Delta from average
}
```

## Deduplication

Recommendations are deduplicated within a 24-hour window based on:

- Workflow type
- Recommendation type
- Recommended agent (if applicable)
- Current agent (if applicable)

This prevents spam while allowing recommendations to refresh when conditions change.

## API Endpoints

### List Recommendations

```
GET /api/openclaw/routing/recommendations
```

Query Parameters:
- `workflow` - Filter by workflow type
- `type` - Filter by recommendation type
- `min_confidence` - Minimum confidence (0.0-1.0)
- `limit` - Maximum results (default: 50)

Response:
```json
{
  "recommendations": [
    {
      "id": "uuid",
      "workflow_type": "mobile-implementation",
      "recommendation_type": "best_agent",
      "recommended_agent": "mobile-dev-specialist",
      "confidence": 0.82,
      "risk_level": "low",
      "reason": "Agent mobile-dev-specialist has 84% success rate, 22% above average",
      "evidence": {...},
      "observations": 50,
      "created_at": "2026-03-06T12:00:00Z"
    }
  ],
  "count": 1
}
```

### Get Single Recommendation

```
GET /api/openclaw/routing/recommendations/{id}
```

### Trigger Analysis

```
POST /api/openclaw/routing/analyze
```

Triggers routing analysis in the background. Analysis:
1. Loads agent and workflow scorecards (7-day window)
2. Runs all analyzers
3. Deduplicates against recent recommendations
4. Persists new recommendations

## Dashboard Integration

The OpenClaw dashboard includes a **Routing Recommendations** section showing:

- Workflow type
- Recommendation type (color-coded badge)
- Recommended/current agent
- Confidence (with color coding)
- Risk level
- Reason summary
- Creation timestamp

Clicking a row expands to show:
- Full evidence JSON
- Key metrics grid

## Configuration

```go
type RoutingConfig struct {
    MinObservations         int     // Minimum data points (default: 5)
    HighConfidenceThresh    float64 // High confidence threshold (default: 0.75)
    MediumConfidenceThresh  float64 // Medium confidence threshold (default: 0.45)
    DeprioritizeThreshold   float64 // Success rate for deprioritization (default: 0.50)
    FallbackFailureThresh   float64 // Failure rate for fallback (default: 0.30)
    InstabilityReworkThresh float64 // Rework rate for instability (default: 0.35)
    DedupWindowHours        int     // Deduplication window (default: 24)
}
```

## Implementation Files

| File | Purpose |
|------|---------|
| `internal/openclaw/routing/models.go` | Data models and confidence calculation |
| `internal/openclaw/routing/analyzer.go` | Analyzer implementations |
| `internal/openclaw/routing/engine.go` | Engine orchestration |
| `internal/openclaw/routing/store.go` | Database persistence |
| `db/schema.go` | Database schema |
| `db/openclaw.go` | Database operations |
| `api/routes_openclaw.go` | HTTP handlers |
| `openclaw/engine.go` | Main engine integration |

## Usage Examples

### Best Agent for Workflow

```
Recommendation: "Agent mobile-dev-specialist is recommended for mobile-implementation"
Confidence: 82% (High)
Risk: Low
Evidence: 84% success rate vs 62% average, 50 observations
```

### Deprioritize Agent

```
Recommendation: "Agent failing-agent should be deprioritized for general workflows"
Confidence: 68% (Medium)
Risk: High
Evidence: 43% success rate with degrading trend, 50 observations
```

### Add Fallback Routing

```
Recommendation: "Workflow unstable-workflow needs fallback routing"
Confidence: 72% (Medium)
Risk: High
Evidence: 41% failure rate with single agent, 50 observations
```

### Investigate Instability

```
Recommendation: "Workflow buggy-workflow shows instability requiring investigation"
Confidence: 65% (Medium)
Risk: Medium
Evidence: 45% rework rate, 35% review rejection, 50 observations
```

## Non-Goals

The Routing Intelligence Engine does NOT:

- Automatically change routing behavior
- Implement a policy engine
- Execute autonomous routing changes
- Use machine learning for predictions
- A/B test routing strategies

All recommendations require human review and manual implementation.
