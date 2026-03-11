<script lang="ts">
  import { onMount } from 'svelte'
  import { Chart, type ChartType, registerables } from 'chart.js'
  
  Chart.register(...registerables)
  
  interface Props {
    type: ChartType
    data: any
    options?: any
  }
  
  let { type, data, options = {} }: Props = $props()
  
  let canvas: HTMLCanvasElement
  let chart: Chart | null = null
  let mounted = false
  
  function deepClone(obj: any): any {
    return JSON.parse(JSON.stringify(obj))
  }
  
  function createChart() {
    if (chart) {
      chart.destroy()
      chart = null
    }
    if (canvas && data) {
      chart = new Chart(canvas, { type, data: deepClone(data), options: deepClone(options) })
    }
  }
  
  function updateChart() {
    if (chart && data) {
      chart.data = deepClone(data)
      chart.options = deepClone(options)
      chart.update()
    } else if (mounted && canvas && data) {
      createChart()
    }
  }
  
  onMount(() => {
    mounted = true
    createChart()
    return () => {
      if (chart) {
        chart.destroy()
      }
    }
  })
  
  $effect(() => {
    if (mounted && data) {
      updateChart()
    }
  })
</script>

<canvas bind:this={canvas}></canvas>
