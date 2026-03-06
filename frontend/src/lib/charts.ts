import {
  Chart,
  ChartConfiguration,
  ChartType,
  DefaultDataPoint,
  registerables,
} from 'chart.js';
import type { ChartData } from 'chart.js/dist/types/index';
import { onMount } from 'svelte';

export function createLineChart(
  labels: string[],
  datasets: ChartData[]
): ChartConfiguration {
export function createBarChart(
  labels: string[],
  datasets: ChartData[]
): ChartConfiguration {
export function createPieChart(
  labels: string[],
  datasets: ChartData[]
): ChartConfiguration;

const chartRegistry: Map<string, ChartType> = new Map();

onMount(() => {
  register(Chart, 'line', LineController);
  register(Chart, 'bar', BarController);
  register(Chart, 'pie', PieController);
  register(Chart, 'doughnut', DoughnutController);
  register(Chart, 'radar', RadarController);
});

function getChartColors(): string[] {
  return [
    '#3b82f6',
    '#10b9818',
    '#6b728a7',
    '#f59e0b0',
    '#ef4444',
    '#8b5cf6',
    '#ec4899',
    '#6366f1',
    '#9333ea',
    '#78716c',
    '#22c55e5',
  ];
}
