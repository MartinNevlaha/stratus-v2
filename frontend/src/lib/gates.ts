import type { MissionHandoffGate } from './types'

// Names an older server implied; used only when it sends no expected_gates.
export const legacyGateNames = ['build', 'vet', 'placeholder_scan']

export const gateLabel = (name: string): string => (name === 'placeholder_scan' ? 'placeholder' : name)

export const expectedGateNames = (expected?: string[]): string[] => (expected?.length ? expected : legacyGateNames)

// The backend records a single info row named "checks" when a project has no automated checks configured.
export const noChecksConfigured = (g: MissionHandoffGate): boolean => g.name === 'checks' && g.verdict === 'info'

export type GateChip = { text: string; cls: 'pass' | 'fail' | 'info'; outdated: boolean }

// An outdated row wins over its verdict: it no longer describes the current tree.
export function gateChip(g: MissionHandoffGate): GateChip {
  const cls = g.verdict === 'pass' ? 'pass' : g.verdict === 'fail' ? 'fail' : 'info'
  if (!g.current) return { text: 'zastaralé', cls, outdated: true }
  if (noChecksConfigured(g)) return { text: 'žiadne kontroly', cls, outdated: false }
  if (cls === 'info' && g.content.includes('not applicable')) return { text: 'nepoužiteľné', cls, outdated: false }
  return { text: cls === 'pass' ? '✓' : cls === 'fail' ? '✕' : '·', cls, outdated: false }
}

export const noChecksHint = 'Pridaj "checks" do .stratus.json (pozri docs/usage/managed-teams.md).'
