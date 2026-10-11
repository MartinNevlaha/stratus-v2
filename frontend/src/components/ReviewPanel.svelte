<script lang="ts">
  import { relativeTime } from '$lib/ui'
  import type { WorkflowAgent } from '$lib/types'

  let { agents }: { agents: WorkflowAgent[] } = $props()

  // Reviews = agents that reported a verdict, oldest first.
  let reviews = $derived(agents.filter(a => a.verdict))
  let latest = $derived(reviews.length > 0 ? reviews[reviews.length - 1] : null)
  // Each FAIL sent the work back to fix, so FAILs are the review ↔ fix loops.
  let loops = $derived(reviews.filter(r => r.verdict === 'FAIL').length)
</script>

{#if latest}
  <div class="review">
    <div class="head">
      <span class="label">Review</span>
      <span class="badge" class:pass={latest.verdict === 'PASS'} class:fail={latest.verdict === 'FAIL'}>{latest.verdict}</span>
      <span class="when">{relativeTime(latest.ended_at ?? latest.last_activity)}</span>
      <span class="loops" title="Number of FAIL verdicts, i.e. review → fix loops">
        {loops} {loops === 1 ? 'loop' : 'loops'} · {reviews.length} {reviews.length === 1 ? 'review' : 'reviews'}
      </span>
    </div>
    <div class="counts">
      <span class="sev must" class:zero={latest.must_fix === 0}>{latest.must_fix} must_fix</span>
      <span class="sev should" class:zero={latest.should_fix === 0}>{latest.should_fix} should_fix</span>
      <span class="sev sugg" class:zero={latest.suggestion === 0}>{latest.suggestion} suggestion</span>
    </div>
  </div>
{/if}

<style>
  .review { display: flex; flex-direction: column; gap: 6px; background: #161b22; border: 1px solid #30363d; border-radius: 6px; padding: 8px 10px; }
  .head { display: flex; align-items: center; gap: 8px; font-size: 12px; }
  .label { color: #8b949e; font-weight: 600; }
  .badge { font-size: 11px; font-weight: 600; padding: 1px 8px; border-radius: 10px; background: #21262d; color: #8b949e; }
  .badge.pass { color: #3fb950; background: #1f2d1f; }
  .badge.fail { color: #f85149; background: #3d1f1f; }
  .when { color: #484f58; font-size: 11px; }
  .loops { margin-left: auto; color: #8b949e; font-size: 11px; }
  .counts { display: flex; gap: 6px; flex-wrap: wrap; }
  .sev { font-size: 11px; padding: 1px 8px; border-radius: 4px; }
  .sev.must { color: #f85149; background: #3d1f1f; }
  .sev.should { color: #d29922; background: #2d2200; }
  .sev.sugg { color: #58a6ff; background: #1f3056; }
  .sev.zero { color: #484f58; background: #21262d; }
</style>
