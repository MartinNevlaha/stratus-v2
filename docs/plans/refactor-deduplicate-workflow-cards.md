# Refactor: Deduplicate Workflow Card Markup

## Problem

`Overview.svelte` (lines 117-283) and `PastWorkflows.svelte` (lines 104-130) render workflow cards with near-identical markup and duplicated logic:

- **Duplicated functions**: `displayType()`, `handleDelete()`, `cancelDelete()`
- **Duplicated CSS**: `.workflow-card`, `.workflow-header`, `.wf-type`, `.wf-id`, `.wf-phase`, `.wf-title`, `.wf-actions`, `.btn-delete`, `.confirm-label`, `.btn-confirm`, `.btn-cancel`, `.cs-stats`, `.cs-stat`
- **Duplicated template**: type badge, ID, phase, title, delete confirmation, change_summary stats

`Overview.svelte` is 482 lines, violating the 150-line component standard.

## Approach

Extract a shared `WorkflowCard.svelte` component using Svelte 5 `$props()` and **snippets** (the idiomatic Svelte 5 way to pass variable content regions — preferred over slots for Svelte 5).

### Component Design: `WorkflowCard.svelte`

**Props:**
```typescript
interface Props {
  wf: WorkflowState
  onDelete: (id: string) => void
  past?: boolean           // adds .past class (opacity), shows timestamp
  showAbortedPhase?: boolean // past cards show "aborted" in phase span
}
```

**Snippets:**
- `{@render children?.()}` — callers pass extra content (active: PhaseTimeline, plans, designs, tasks, SwarmDetail, resume buttons; past: nothing extra)

**Self-contained:** The component owns `displayType()` and the CSS for all card-related classes. Delete confirmation state (`confirmDelete`, `confirmTimer`) is internal — the `onDelete` prop is called only on final confirmation.

### Caller Changes

**`Overview.svelte`:**
- Remove `displayType()`, `handleDelete()`, `cancelDelete()`, `confirmDelete`, `confirmTimer` (all moved into WorkflowCard)
- Simplify `handleDelete` to just call `deleteWorkflow` and refresh — or pass as lambda
- Replace the `{#each activeWfs}` block with `<WorkflowCard>` + snippet for extra sections
- Remove duplicated CSS classes

**`PastWorkflows.svelte`:**
- Remove `displayType()`, `handleDelete()`, `cancelDelete()`, `confirmDelete`, `confirmTimer`
- Replace workflow card block with `<WorkflowCard past={true} showAbortedPhase={true}>`
- Remove duplicated CSS classes (keep `.workflow-card.past` override or pass via prop)

## Constraints (from governance)

- Use Svelte 5 idioms (`$props()`, typed interfaces, snippets over slots)
- No `any` types
- Do not restyle or reformat adjacent code
- Do not add features not currently used by either caller
- Surgical: touch only what's needed for dedup
- Keep existing CSS class names

## Tasks

1. Create `WorkflowCard.svelte` component with shared card rendering, delete logic, and CSS
2. Refactor `Overview.svelte` to use `WorkflowCard` with snippet for active-only sections
3. Refactor `PastWorkflows.svelte` to use `WorkflowCard` in past mode
4. Verify: typecheck + visual parity
