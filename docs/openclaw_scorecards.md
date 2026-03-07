# OpenClaw Scorecards

## Purpose

Scorecards provide long-term operational performance summaries for agents and workflows in Stratus. They enable OpenClaw to reason about system quality in a durable way, answering questions like:

- Which agents perform best for which workflow types?
- Which workflows have poor completion quality?
- Which agents cause the most rework?
- Which workflow categories are degrading over time?

Scorecards are derived from historical events and outcomes, providing a stronger basis for:
- Future routing decisions
- Proposal confidence
- Anomaly detection
- Optimization recommendations

## Scorecard Types

### Agent Scorecard

Represents how a single agent performs over a defined time window.

| Field | Type | Description |
|-------|------|-------------|
| `agent_name` | string | Unique identifier for the agent |
| `window` | string | Time window: `7d` or `30d` |
| `total_runs` | int | Total number of agent executions |
| `success_rate` | float | Ratio of completed to total runs |
| `failure_rate` | float | Ratio of failed to total runs |
| `review_pass_rate` | float | Ratio of passed reviews to total reviews |
| `rework_rate` | float | Ratio of retry cycles to total runs |
| `avg_cycle_time_ms` | int | Average execution time in milliseconds |
| `regression_rate` | float | Ratio of failures after success to total successes |
| `confidence_score` | float | Reliability score (0.0-1.0) |
| `trend` | string | Direction: `improving`, `degrading`, or `stable` |

### Workflow Scorecard

Represents how a workflow type performs over a defined time window.

| Field | Type | Description |
|-------|------|-------------|
| `workflow_type` | string | Type of workflow (spec, bug, e2e) |
| `window` | string | Time window: `7d` or `30d` |
| `total_runs` | int | Total number of workflow executions |
| `completion_rate` | float | Ratio of completed to total runs |
| `failure_rate` | float | Ratio of failed to total runs |
| `review_rejection_rate` | float | Ratio of failed reviews to total reviews |
| `rework_rate` | float | Ratio of phase backtracks to total runs |
| `avg_duration_ms` | int | Average workflow duration in milliseconds |
| `confidence_score` | float | Reliability score (0.0-1.0) |
| `trend` | string | Direction: `improving`, `degrading`, or `stable` |

## Metric Definitions

### Agent Metrics

| Metric | Formula | Description |
|--------|---------|-------------|
| Success Rate | `completed / (completed + failed)` | Percentage of successful agent executions |
| Failure Rate | `failed / total_runs` | Percentage of failed agent executions |
| Review Pass Rate | `review_passed / (review_passed + review_failed)` | Percentage of reviews that pass |
| Rework Rate | `retry_cycles / total_runs` | Percentage of executions requiring retry |
| Regression Rate | `regressions / successes` | Percentage of successes followed by failure |
| Avg Cycle Time | `sum(cycle_times) / count` | Mean time from spawned to completed |
| Confidence Score | See below | Reliability of the scorecard data |

### Workflow Metrics

| Metric | Formula | Description |
|--------|---------|-------------|
| Completion Rate | `completed / total_runs` | Percentage of successfully completed workflows |
| Failure Rate | `failed / total_runs` | Percentage of failed workflows |
| Review Rejection Rate | `review_failed / total_reviews` | Percentage of rejected reviews |
| Rework Rate | `phase_backtracks / total_runs` | Percentage of backward phase transitions |
| Avg Duration | `sum(durations) / count` | Mean time from start to completion/failure |
| Confidence Score | See below | Reliability of the scorecard data |

## Time Window Logic

Scorecards support two time windows:

| Window | Duration | Use Case |
|--------|----------|----------|
| `7d` | 7 days | Recent performance, quick feedback |
| `30d` | 30 days | Long-term trends, stable assessment |

Each window computes metrics independently, allowing comparison between recent and long-term performance.

## Approximated Metrics

Some metrics cannot be computed directly from current event data and require approximation:

### Rework Rate (Agent)

**Approximation**: Count retry cycles (failed→spawned pairs) within the same workflow instance.

**Assumption**: A failed agent execution followed by a new spawn for the same workflow indicates rework.

**Limitations**: 
- May miss rework that spans multiple workflow instances
- Cannot distinguish intentional retries from rework

### Rework Rate (Workflow)

**Approximation**: Count phase transitions that move backwards in the workflow order.

**Phase Order**: `plan → discovery → design → governance → accept → implement → verify → learn → complete`

**Assumption**: Moving backwards indicates rework is needed.

**Limitations**:
- Some workflows may have intentional backward transitions
- Does not capture rework within a phase

### Regression Rate (Agent)

**Approximation**: Count failures that occur after a successful completion for the same agent in the same workflow.

**Assumption**: A success followed by a failure for the same agent indicates regression.

**Limitations**:
- May count legitimate failures in new work as regression
- Does not distinguish between code regressions and environment issues

## Confidence Scoring

The confidence score reflects the reliability of the scorecard metrics based on:

1. **Sample Size** (primary factor)
   - `< 5 samples`: 0.3 (low confidence)
   - `5-10 samples`: 0.4-0.6 (medium confidence)
   - `10-20 samples`: 0.6-0.8 (good confidence)
   - `20-50 samples`: 0.8-0.9 (high confidence)
   - `> 50 samples`: 0.9-0.95 (very high confidence)

2. **Consistency Bonus**
   - Additional 0.1-0.2 for consistent metrics across the window

3. **Maximum Score**: 0.95 (never 100% confident)

## Trend Indicators

Trend direction is computed by comparing the current window to the previous equivalent window:

| Trend | Condition |
|-------|-----------|
| `improving` | 2+ key metrics improved by ≥5% |
| `degrading` | 2+ key metrics degraded by ≥5% |
| `stable` | Changes < 5% on key metrics |

### Key Metrics by Scorecard Type

**Agent**: success_rate, failure_rate, review_pass_rate

**Workflow**: completion_rate, failure_rate, rework_rate

## API Endpoints

### List Agent Scorecards

```
GET /api/openclaw/scorecards/agents
```

Query Parameters:
- `window` (string): `7d` or `30d` (default: `7d`)
- `sortBy` (string): Column to sort by
- `sortDirection` (string): `ASC` or `DESC` (default: `DESC`)
- `limit` (int): Max results (default: 50, max: 200)

Response:
```json
{
  "scorecards": [...],
  "window": "7d",
  "count": 5,
  "highlights": {
    "best_agent": {...},
    "most_degraded_agent": {...}
  }
}
```

### Get Single Agent Scorecard

```
GET /api/openclaw/scorecards/agents/:name
```

Query Parameters:
- `window` (string): `7d` or `30d` (default: `7d`)

### List Workflow Scorecards

```
GET /api/openclaw/scorecards/workflows
```

Query Parameters: Same as agent scorecards

### Get Single Workflow Scorecard

```
GET /api/openclaw/scorecards/workflows/:type
```

Query Parameters:
- `window` (string): `7d` or `30d` (default: `7d`)

### Trigger Scorecard Computation

```
POST /api/openclaw/scorecards/compute
```

Triggers background computation of all scorecards for both windows.

### Get Scorecard Highlights

```
GET /api/openclaw/scorecards/highlights
```

Query Parameters:
- `window` (string): `7d` or `30d` (default: `7d`)

Response includes:
- Best performing agent
- Most degraded agent
- Slowest workflow
- Highest rework workflow
- Metric definitions
- Approximation notes

## Dashboard Integration

The OpenClaw dashboard displays scorecards with:

1. **Agent Scorecards Table**: Shows all agents with key metrics and trends
2. **Workflow Scorecards Table**: Shows all workflow types with key metrics
3. **Highlights Section**: Top performers and problematic areas
4. **Window Selector**: Switch between 7-day and 30-day views
5. **Compute Button**: Manually trigger scorecard computation

## Architecture

```
openclaw/events → scorecards/engine.go → scorecards/store.go → database
                      ↓
                 calculators.go (metric computation)
                      ↓
                 models.go (data structures)
```

## Future Enhancements

- Per-workflow-type agent scorecards
- Seasonal adjustments for time-based patterns
- Integration with routing decisions
- Automated alerting on degrading trends
- Historical scorecard comparison
