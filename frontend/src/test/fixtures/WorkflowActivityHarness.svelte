<script lang="ts">
  import { untrack } from 'svelte'
  import WorkflowActivity from '../../components/WorkflowActivity.svelte'
  import type { WorkflowState } from '../../lib/types'
  let { initial }: { initial: WorkflowState } = $props()
  let wf = $state(untrack(() => initial))
  export function setWorkflow(next: WorkflowState) { wf = next }
</script>
<WorkflowActivity {wf} />
