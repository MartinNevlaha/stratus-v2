import {
  Chart,
  ChartConfiguration,
  ChartType,
  registerables,
} from 'chart.js';

Chart.register(...registerables);

export function createLineChart(
  labels: string[],
  datasets: any[]
): ChartConfiguration {
  return {
    type: 'line',
    data: {
      labels,
      datasets,
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
    },
  };
}

export function createBarChart(
  labels: string[],
  datasets: any[]
): ChartConfiguration {
  return {
    type: 'bar',
    data: {
      labels,
      datasets,
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
    },
  };
}

export function createPieChart(
  labels: string[],
  datasets: any[]
): ChartConfiguration {
  return {
    type: 'pie',
    data: {
      labels,
      datasets,
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
    },
  };
}

function getChartColors(): string[] {
  return [
    '#3b82f6',
    '#10b981',
    '#6b7280',
    '#f59e0b',
    '#ef4444',
    '#8b5cf6',
    '#ec4899',
    '#6366f1',
    '#9333ea',
  ];
}

export function createBarChart(
  labels: string[],
  datasets: ChartData[]
): ChartConfiguration {
  return {
    type: 'bar',
    data: {
      labels,
      datasets,
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
    },
  };
}

export function createPieChart(
  labels: string[],
  datasets: ChartData[]
): ChartConfiguration {
  return {
    type: 'pie',
    data: {
      labels,
      datasets,
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
    },
  };
}

export function getChartColors(): string[] {
  return [
    '#3b82f6',
    '#10b981',
    '#6b7280',
    '#f59e0b',
    '#ef4444',
    '#8b5cf6',
    '#ec4899',
    '#6366f1',
    '#9333ea',
    '#78716c',
    '#22c55e',
  ];
}
