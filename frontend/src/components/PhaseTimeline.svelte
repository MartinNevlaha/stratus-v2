<script lang="ts">
  interface Props {
    type: 'spec' | 'bug'
    complexity?: 'simple' | 'complex'
    currentPhase: string
  }

  let { type, complexity = 'simple', currentPhase }: Props = $props()

  const specSimplePhases  = ['plan', 'implement', 'verify', 'learn', 'complete']
  const specComplexPhases = ['discovery', 'design', 'plan', 'implement', 'verify', 'learn', 'complete']
  const bugPhases         = ['analyze', 'fix', 'review', 'complete']

  let phases = $derived(
    type === 'bug'           ? bugPhases :
    complexity === 'complex' ? specComplexPhases :
    specSimplePhases
  )

  function phaseStatus(phase: string): 'done' | 'current' | 'pending' {
    const idx = phases.indexOf(phase)
    const cur = phases.indexOf(currentPhase)
    if (idx < cur) return 'done'
    if (idx === cur) return 'current'
    return 'pending'
  }
</script>

<div class="timeline">
  {#each phases as phase, i}
    {@const status = phaseStatus(phase)}
    <div class="step" class:done={status === 'done'} class:current={status === 'current'}>
      <div class="dot"></div>
      <span class="label">{phase}</span>
    </div>
    {#if i < phases.length - 1}
      <div class="line" class:done={status === 'done'}></div>
    {/if}
  {/each}
</div>

<style>
  .timeline {
    display: flex;
    align-items: center;
    gap: 0;
    flex-wrap: wrap;
  }

  .step {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    min-width: 60px;
  }

  .dot {
    width: 12px;
    height: 12px;
    border-radius: 50%;
    background: #30363d;
    border: 2px solid #30363d;
    transition: all 0.2s;
  }

  .done .dot { background: #3fb950; border-color: #3fb950; }
  .current .dot { background: #58a6ff; border-color: #58a6ff; box-shadow: 0 0 8px #58a6ff80; }

  .label {
    font-size: 11px;
    color: #8b949e;
    text-transform: capitalize;
  }

  .done .label, .current .label { color: #c9d1d9; }

  .line {
    flex: 1;
    height: 2px;
    background: #30363d;
    min-width: 20px;
    margin-bottom: 16px;
    transition: background 0.2s;
  }

  .line.done { background: #3fb950; }
</style>
