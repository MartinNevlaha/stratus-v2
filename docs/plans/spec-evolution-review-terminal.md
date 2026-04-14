# Plan: Evolution Proposal → Review in Terminal

## Goal
Pridať "Review in Terminal" tlačidlo pre Evolution hypotézy s `decision === 'proposal_created'`. Klik → prepne Overview tab, prefillne `/spec ...` do terminálu. User stlačí Enter.

## Scope (only)
- Iba `proposal_created` riadky (tie majú `wiki_page_id`)
- `auto_applied` / `rejected` / `inconclusive` zostanú bez tlačidla — nie sú akčné

## Design

### Helper (`frontend/src/lib/evolutionFlow.ts`, new)
```ts
import type { EvolutionHypothesis } from './types'
import { sanitizeTerminalPayload } from './findingFlow'

export function buildEvolutionCommand(h: EvolutionHypothesis): string {
  const wikiRef = h.wiki_page_id ? ` [wiki:${h.wiki_page_id}]` : ''
  const raw = `/spec Review evolution proposal — ${h.category}: ${h.description} (baseline ${h.baseline_value} → proposed ${h.proposed_value}, confidence ${(h.confidence*100).toFixed(0)}%)${wikiRef}`
  return sanitizeTerminalPayload(raw)
}
```

### Evolution.svelte
- Nový stĺpec **Actions** v tabuľke hypotéz
- Pre riadky s `decision === 'proposal_created'` renderuj `<button>Review in Terminal</button>`
- Pre ostatné decisions: prázdna bunka (alebo `—`)
- Click handler:
  ```ts
  requestTerminalPrefill(buildEvolutionCommand(h))
  setActiveTab('overview')
  ```

### Tests (`evolutionFlow.test.ts`)
- `buildEvolutionCommand` format snapshot
- Žiaden CR/LF v output-e
- Obsahuje category, baseline→proposed, confidence %, wiki ref keď je prítomný
- Funguje aj keď `wiki_page_id` je null

## Reuse
- `sanitizeTerminalPayload` z findingFlow.ts
- `requestTerminalPrefill` + `setActiveTab` zo store.svelte.ts
- Terminal `$effect` consumer už existuje — nič netreba

## Tasks
1. Vytvor `frontend/src/lib/evolutionFlow.ts` s `buildEvolutionCommand`
2. Vytvor `frontend/src/lib/evolutionFlow.test.ts` (Vitest)
3. Pridaj stĺpec Actions + tlačidlo do hypotéz tabuľky v `Evolution.svelte`
4. `npm run check` + `vitest run` + `npm run build`
