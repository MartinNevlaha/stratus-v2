# Analytics & Metrics Engine

## Overview

Implement comprehensive analytics to track workflow efficiency, agent performance, and code quality trends.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│              ANALYTICS ENGINE ARCHITECTURE                  │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐ │
│  │  Event Store │───▶│  Aggregator  │───▶│   Dashboard  │ │
│  │  (SQLite)    │    │  (Go)        │    │   (Svelte)   │ │
│  └──────────────┘    └──────────────┘    └──────────────┘ │
│         │                    │                    │        │
│         │                    ▼                    │        │
│         │            ┌──────────────┐            │        │
│         │            │  ML Insights │            │        │
│         │            │  (Python)    │            │        │
│         │            └──────────────┘            │        │
│         │                    │                    │        │
│         └────────────────────┴────────────────────┘        │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

## Metrics to Track

### 1. Workflow Metrics
```go
type WorkflowMetrics struct {
    // Time metrics
    AvgPhaseDuration    map[string]time.Duration  // plan, implement, verify, learn
    AvgTaskCompletion   time.Duration
    TimeToFirstCommit   time.Duration
    
    // Quality metrics
    BugFixRate          float64  // bugs fixed / total bugs
    RegressionRate      float64  // regressions / deployments
    TestCoverageDelta   float64  // coverage change per workflow
    
    // Efficiency metrics
    AgentUtilization    map[string]float64  // agent_id -> utilization %
    DelegationSuccess   float64  // successful delegations / total
    ReviewLoopCount     int      // average fix loops
    
    // Governance metrics
    RuleViolations      int
    ADRAcceptanceRate   float64
    ProposalAcceptRate  float64
}
```

### 2. Agent Performance Metrics
```go
type AgentMetrics struct {
    AgentID             string
    TasksCompleted      int
    AvgTaskTime         time.Duration
    SuccessRate         float64
    QualityScore        float64  // based on review feedback
    SpecializationScore float64  // domain expertise
    
    // Interaction metrics
    CollaborationScore  float64  // how well with other agents
    CommunicationClarity float64
    FollowUpRequired    float64  // % of tasks needing clarification
}
```

### 3. Code Quality Metrics
```go
type CodeQualityMetrics struct {
    // Complexity
    AvgCyclomaticComplexity  float64
    AvgLinesOfCode          int
    TechnicalDebtScore      float64
    
    // Maintainability
    CodeDuplicationRate     float64
    DocumentationCoverage   float64
    DependencyFreshness     float64  // % of up-to-date deps
    
    // Testing
    TestPassRate            float64
    TestExecutionTime       time.Duration
    MutationScore           float64
    
    // Security
    VulnerabilityCount      int
    SecurityHotspots        int
    DependencyVulnerabilities int
}
```

## Database Schema

```sql
-- Metrics storage
CREATE TABLE workflow_metrics (
    id INTEGER PRIMARY KEY,
    workflow_id TEXT NOT NULL,
    metric_type TEXT NOT NULL,
    metric_name TEXT NOT NULL,
    metric_value REAL,
    metadata JSON,
    recorded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (workflow_id) REFERENCES workflows(id)
);

CREATE INDEX idx_metrics_type_name ON workflow_metrics(metric_type, metric_name);
CREATE INDEX idx_metrics_workflow ON workflow_metrics(workflow_id);

-- Aggregated daily metrics
CREATE TABLE daily_metrics (
    id INTEGER PRIMARY KEY,
    date TEXT NOT NULL UNIQUE,
    metrics JSON NOT NULL,  -- {metric_name: {avg, min, max, count}}
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Agent performance snapshots
CREATE TABLE agent_snapshots (
    id INTEGER PRIMARY KEY,
    agent_id TEXT NOT NULL,
    snapshot_date TEXT NOT NULL,
    metrics JSON NOT NULL,
    recommendations JSON,  -- AI-generated improvement suggestions
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(agent_id, snapshot_date)
);
```

## API Endpoints

```bash
# Get aggregated metrics
GET /api/metrics/workflows?period=7d&group_by=type
GET /api/metrics/agents?period=30d
GET /api/metrics/code-quality?period=7d

# Get trends
GET /api/metrics/trends?metric=bug_fix_rate&period=90d

# Export metrics
GET /api/metrics/export?format=csv|json&period=30d

# Real-time metrics stream
WS /api/metrics/live
```

## Dashboard Visualizations

### Overview Dashboard
```
┌────────────────────────────────────────────────────────────┐
│                    ANALYTICS OVERVIEW                      │
├────────────────────────────────────────────────────────────┤
│                                                            │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐    │
│  │ Avg Workflow │  │   Bug Fix    │  │   Agent      │    │
│  │    Time      │  │    Rate      │  │ Utilization  │    │
│  │   2.3 days   │  │    94.2%     │  │    78.5%     │    │
│  │   ↑ 12%      │  │   ↑ 3.1%     │  │   ↓ 2.3%     │    │
│  └──────────────┘  └──────────────┘  └──────────────┘    │
│                                                            │
│  ┌─────────────────────────────────────────────────────┐  │
│  │         Workflow Performance (Last 30 days)         │  │
│  │  [LINE CHART: phases over time]                     │  │
│  └─────────────────────────────────────────────────────┘  │
│                                                            │
│  ┌─────────────────────┐  ┌─────────────────────────┐    │
│  │  Top Performers     │  │  Bottleneck Detection   │    │
│  │  1. backend-eng     │  │  • Review phase: 45%    │    │
│  │  2. frontend-eng    │  │  • Plan phase: 23%      │    │
│  │  3. qa-engineer     │  │  • Learn phase: 18%     │    │
│  └─────────────────────┘  └─────────────────────────┘    │
│                                                            │
└────────────────────────────────────────────────────────────┘
```

## Implementation Plan

### Week 1-2: Core Infrastructure
- [ ] Database schema migrations
- [ ] Metrics collection hooks in orchestration layer
- [ ] Basic aggregation logic
- [ ] API endpoints

### Week 3-4: Dashboard & Visualization
- [ ] Svelte charts components
- [ ] Real-time WebSocket metrics
- [ ] Export functionality
- [ ] Historical data viewer

### Week 5-6: Advanced Analytics
- [ ] Trend analysis algorithms
- [ ] Anomaly detection
- [ ] Predictive insights
- [ ] Automated reporting

## Success Metrics

- **Adoption**: 80% of users view analytics dashboard weekly
- **Actionability**: 50% of insights lead to governance changes
- **Performance**: < 100ms for dashboard load, < 5s for trend analysis
- **Accuracy**: > 90% correlation between metrics and actual outcomes
