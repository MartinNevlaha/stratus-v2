# Insight Dashboard

## Overview

The Insight Dashboard provides a comprehensive interface for viewing and managing Insight's autonomous improvement proposals. It offers visibility into detected patterns, generated proposals, and their lifecycle management.

**Important:** Insight is **advisory only**. No automatic execution or system changes occur without human review and approval.

---

## Dashboard Features

### Summary Cards

The dashboard displays real-time summary metrics for the last 24 hours:

- **Recent Proposals**: Total proposals generated in the last 24 hours
- **Recent Patterns**: Total patterns detected in the last 24 hours
- **Approved**: Proposals that have been approved
- **Rejected**: Proposals that have been rejected
- **Critical/High Patterns**: Number of high-severity patterns requiring attention

### Proposals Table

The proposals table shows all recent proposals with the following information:

- **Title**: Human-readable proposal title
- **Type**: Proposal type (routing.change, workflow.investigate, etc.)
- **Status**: Current lifecycle status (detected, drafted, approved, rejected, archived)
- **Risk**: Risk level (high, medium, low)
- **Confidence**: Confidence score (0-100%)
- **Created**: When the proposal was created

**Filtering:**
- **Status**: Filter by proposal status
- **Type**: Filter by proposal type
- **Risk**: Filter by risk level

**Interaction:**
- Click any row to view full proposal details

### Proposal Detail View

When a proposal is selected, a detail panel shows:

**Core Information:**
- Title and description
- Type and status badges
- Risk level and confidence score
- Creation and update timestamps

**Evidence Section:**
- JSON display of all evidence supporting the proposal
- Shows affected workflows, agents, failure rates, etc.

**Recommendation Section:**
- Machine-readable suggested action
- Action type, target entity, and expected impact

**Decision Reason:**
- If approved or rejected, shows the reason for the decision
- Only displayed when a reason was provided

**Actions:**
Available actions depend on current status:
- **Detected → Drafted**: Mark as drafted for review
- **Drafted → Approved**: Approve the proposal (optional reason)
- **Drafted → Rejected**: Reject the proposal (reason required)
- **Approved/Rejected → Archived**: Archive the proposal

### Patterns Section

Displays recently detected patterns:

- **Pattern Name**: Identifier for the pattern
- **Type**: Pattern type (workflow, agent, performance, etc.)
- **Severity**: Critical, high, medium, or low
- **Description**: Human-readable pattern description
- **Confidence**: Detection confidence (0-100%)
- **Frequency**: Number of times this pattern has been observed
- **Last Seen**: Most recent occurrence

**Filtering:**
- **Severity**: Filter by pattern severity

### Analysis History

Shows recent Insight analysis runs:

- **Analysis Type**: Type of analysis performed
- **Patterns Found**: Number of patterns detected
- **Proposals Created**: Number of proposals generated
- **Execution Time**: How long the analysis took
- **Scope**: What was analyzed

---

## API Endpoints

### GET /api/insight/dashboard

Returns dashboard summary metrics.

**Response:**
```json
{
  "recent_proposals": 12,
  "recent_patterns": 8,
  "proposals_by_status": {
    "detected": 5,
    "drafted": 3,
    "approved": 2,
    "rejected": 1,
    "archived": 1
  },
  "patterns_by_severity": {
    "critical": 1,
    "high": 3,
    "medium": 3,
    "low": 1
  },
  "top_affected_workflows": ["spec-complex", "bug-fix"],
  "top_affected_agents": ["mobile-dev-specialist"],
  "time_window_hours": 24
}
```

### GET /api/insight/proposals

List proposals with optional filtering.

**Query Parameters:**
- `status` (string): Filter by status
- `type` (string): Filter by proposal type
- `risk` (string): Filter by risk level
- `min_confidence` (float): Minimum confidence (0-1)
- `limit` (int): Max results (default: 50, max: 200)
- `offset` (int): Pagination offset

**Response:**
```json
{
  "proposals": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "type": "routing.change",
      "status": "approved",
      "title": "Reroute spec-complex workflow",
      "description": "75% failure rate detected",
      "confidence": 0.88,
      "risk_level": "high",
      "source_pattern_id": "pattern-42",
      "evidence": { /* ... */ },
      "recommendation": { /* ... */ },
      "decision_reason": "Strong evidence",
      "created_at": "2026-03-06T12:00:00Z",
      "updated_at": "2026-03-06T14:30:00Z"
    }
  ],
  "count": 1
}
```

### GET /api/insight/proposals/:id

Get full details for a single proposal.

**Response:** Single proposal object with all fields.

### PATCH /api/insight/proposals/:id/status

Update proposal lifecycle status.

**Request:**
```json
{
  "status": "approved",
  "reason": "Strong evidence supports this change"
}
```

**Response:** Updated proposal object.

**Errors:**
- `400 Bad Request`: Invalid status or invalid transition
- `404 Not Found`: Proposal doesn't exist

### GET /api/insight/patterns

List detected patterns.

**Query Parameters:**
- `type` (string): Filter by pattern type
- `severity` (string): Filter by severity
- `min_confidence` (float): Minimum confidence (0-1)
- `limit` (int): Max results (default: 100, max: 500)

**Response:**
```json
{
  "patterns": [ /* ... */ ],
  "count": 10
}
```

---

## Proposal Lifecycle

### Statuses

1. **detected**: Initial state when proposal is generated from a pattern
2. **drafted**: Proposal has been reviewed and is ready for decision
3. **approved**: Proposal has been accepted (may be executed in future)
4. **rejected**: Proposal has been declined
5. **archived**: Proposal is no longer active

### Valid Transitions

```
detected → drafted
drafted → approved OR rejected
approved → archived
rejected → archived
```

**Rules:**
- Invalid transitions are rejected with a 400 error
- All status changes are logged with timestamps
- Decision reasons are optional for approve, required for reject
- Once archived, a proposal cannot be unarchived

### Example Workflow

1. **Pattern Detected**: Insight detects workflow failure cluster
2. **Proposal Generated**: System creates "routing.change" proposal with status `detected`
3. **Draft**: Reviewer marks proposal as `drafted` for team review
4. **Decision**: Team reviews evidence and either:
   - Approves with optional reason → status becomes `approved`
   - Rejects with required reason → status becomes `rejected`
5. **Archive**: After implementation or when no longer relevant → status becomes `archived`

---

## UI Components

### Status Badges

Color-coded badges indicate proposal status:

- **detected**: Gray - Initial state
- **drafted**: Blue - Under review
- **approved**: Green - Accepted
- **rejected**: Red - Declined
- **archived**: Dark gray - Inactive

### Risk Level Badges

- **high**: Red - Critical impact, careful review needed
- **medium**: Orange - Significant impact, standard review
- **low**: Green - Minor impact, can be fast-tracked

### Severity Badges

- **critical**: Red - Immediate attention required
- **high**: Orange - High priority
- **medium**: Yellow - Normal priority
- **low**: Gray - Low priority

### Confidence Indicator

Visual bar showing confidence score (0-100%):
- **Green** (80-100%): Strong evidence
- **Yellow** (60-79%): Medium evidence
- **Red** (0-59%): Weak evidence

---

## Best Practices

### Reviewing Proposals

1. **Check Confidence**: Higher confidence = stronger evidence
2. **Review Evidence**: Always examine the evidence JSON
3. **Understand Impact**: Check risk level and affected entities
4. **Read Recommendation**: Understand suggested action
5. **Add Decision Reason**: Document why you approved/rejected

### Filtering Strategies

- **High Priority**: Filter by `risk=high` or `status=detected`
- **Review Queue**: Filter by `status=drafted`
- **Learning**: Review `status=rejected` proposals with reasons
- **Pattern Analysis**: Use severity filter to find critical patterns

### Common Workflows

**Quick Review:**
1. Filter by `status=detected`
2. Review confidence and evidence
3. Mark as drafted or leave for later

**Team Review:**
1. Filter by `status=drafted`
2. Discuss in team meeting
3. Approve or reject with detailed reasons

**Audit Trail:**
1. Filter by `status=approved` or `status=rejected`
2. Review decision reasons
3. Archive when implemented

---

## Troubleshooting

### No Proposals Showing

**Possible causes:**
- No patterns detected in last 24 hours
- All proposals have been archived
- Filters are too restrictive

**Solution:**
- Clear all filters
- Check pattern detection is running
- Run manual analysis with "Run Analysis" button

### Cannot Update Status

**Possible causes:**
- Invalid transition (e.g., approved → drafted)
- Proposal doesn't exist
- Missing required reason for reject

**Solution:**
- Check valid transitions in lifecycle section
- Verify proposal ID
- Provide reason when rejecting

### Proposals Keep Getting Deduplicated

**Possible causes:**
- Same issue persisting within 24-hour window
- Deduplication working as designed

**Solution:**
- Check existing proposals before running new analysis
- Resolve underlying issue to prevent re-detection
- View existing proposal for same pattern

---

## Logging and Auditability

### Status Changes

All status changes are logged with:
- Timestamp
- Previous status
- New status
- Decision reason (if provided)
- User context (future enhancement)

### API Errors

All API errors are logged:
- Endpoint
- Error type
- Error message
- Timestamp

### Invalid Transitions

Invalid lifecycle transitions are logged:
- Proposal ID
- Current status
- Requested status
- Timestamp

---

## Security and Safety

### Advisory Only

- **No automatic execution**: All proposals require human approval
- **No side effects**: Viewing proposals doesn't change system state
- **Explicit actions**: All status changes require deliberate user action

### Fail-Safe Design

- Invalid transitions are rejected (not auto-corrected)
- Database errors don't crash the system
- Frontend errors are displayed clearly
- API errors include helpful messages

---

## Future Enhancements

The following features are **intentionally out of scope** for this implementation:

- ❌ Automated proposal execution
- ❌ Real-time streaming updates
- ❌ Advanced analytics charts
- ❌ Bulk operations
- ❌ Proposal rollback
- ❌ Impact measurement
- ❌ Multi-user collaboration
- ❌ Notifications

These may be added in future iterations based on operational feedback.

---

## Summary

The Insight Dashboard provides:

✅ **Visibility** into detected patterns and generated proposals  
✅ **Transparency** through evidence and confidence display  
✅ **Control** via explicit lifecycle management  
✅ **Auditability** through decision reasons and logging  
✅ **Safety** by remaining advisory-only  
✅ **Clarity** through clean UI and helpful error messages  

The dashboard makes Insight's intelligence **visible, understandable, and actionable** while maintaining safety and human oversight.
