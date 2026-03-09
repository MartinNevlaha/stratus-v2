<script lang="ts">
  import { onMount } from 'svelte'
  import { Chart, type ChartConfiguration, type ChartType, registerables } from 'chart.js'
  
  Chart.register(...registerables)
  
  interface Props {
    type: ChartType
    data: any
    options?: any
  }
  
  let { type, data, options = {} }: Props = $props()
  
  let canvas: HTMLCanvasElement
  let chart: Chart | null = null
  
  function createChart() {
    if (chart) {
      chart.destroy()
    }
    if (canvas && data) {
      chart = new Chart(canvas, {
        type,
        data,
        options
      })
    }
  }
  
  onMount(() => {
    createChart()
    return () => {
      if (chart) {
        chart.destroy()
      }
    }
  })
  
  $effect(() => {
    if (chart && data) {
      chart.data = data
      chart.options = options
      chart.update()
    }
  })
</script>

<canvas bind:this={canvas}></canvas>
