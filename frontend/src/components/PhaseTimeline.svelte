<script lang="ts">
  interface Props {
    type: 'spec' | 'bug' | 'e2e'
    complexity?: 'simple' | 'complex'
    currentPhase: string
  }

  let { type, complexity = 'simple', currentPhase }: Props = $props()

  const specSimplePhases  = ['plan', 'implement', 'verify', 'learn', 'complete']
  const specComplexPhases = ['discovery', 'design', 'plan', 'implement', 'verify', 'learn', 'complete']
  const bugPhases         = ['analyze', 'fix', 'review', 'complete']
  const e2ePhases         = ['setup', 'plan', 'generate', 'heal', 'complete']

  let phases = $derived(
    type === 'e2e'           ? e2ePhases :
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

  function lineStatus(i: number): 'done' | 'active' | 'pending' {
    const cur = phases.indexOf(currentPhase)
    if (i < cur - 1) return 'done'
    if (i === cur - 1) return 'active'
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
      {@const ls = lineStatus(i)}
      <div class="line" class:done={ls === 'done'} class:active={ls === 'active'}></div>
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
  .current .dot {
    background: #58a6ff;
    border-color: #58a6ff;
    animation: dot-pulse 2s ease-in-out infinite;
  }

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

  .line.active {
    background: linear-gradient(90deg, #3fb950, #58a6ff);
    position: relative;
    overflow: hidden;
  }
  .line.active::after {
    content: '';
    position: absolute;
    top: -1px;
    left: -40%;
    width: 40%;
    height: 4px;
    border-radius: 2px;
    background: linear-gradient(90deg, transparent, #58a6ff, #79c0ff, #58a6ff, transparent);
    animation: line-pulse 2s ease-in-out infinite;
  }

  @keyframes dot-pulse {
    0%, 100% { box-shadow: 0 0 6px #58a6ff60; }
    50%      { box-shadow: 0 0 16px #58a6ffa0, 0 0 30px #58a6ff40; }
  }

  @keyframes line-pulse {
    0%   { left: -40%; }
    100% { left: 100%; }
  }

  @media (prefers-reduced-motion: reduce) {
    .current .dot { animation: none; box-shadow: 0 0 8px #58a6ff80; }
    .line.active::after { animation: none; opacity: 0; }
  }
</style>
