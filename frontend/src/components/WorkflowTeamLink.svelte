<script lang="ts">
  import { requestTeamSelect } from '$lib/store'
  import DesignReviewStatus from './DesignReviewStatus.svelte'
  import { teamStateLabel } from '$lib/teams'
  import type { ManagedTeam } from '$lib/teams'

  let { team, workflowId }: { team: ManagedTeam; workflowId: string } = $props()
</script>

<span class="team-link">
  <span class="team-chip">Tím {team.id.slice(0, 8)} · {teamStateLabel(team.state)}</span>
  <button class="open-team" onclick={() => requestTeamSelect(team.id)} aria-label={`Otvoriť tím ${team.id}`}>Otvoriť tím</button>
  {#if team.design_review?.enabled}<DesignReviewStatus {workflowId} teamId={team.id} />{/if}
</span>

<style>
  .team-link { display: inline-flex; align-items: flex-start; gap: 6px; }
  .team-chip { font-size: 11px; color: #58a6ff; border: 1px solid #58a6ff55; border-radius: 10px; padding: 1px 8px; }
  .open-team { background: none; border: 1px solid #30363d; color: #8b949e; border-radius: 4px; font-size: 11px; padding: 1px 8px; cursor: pointer; }
  .open-team:hover { color: #c9d1d9; border-color: #484f58; }
</style>
