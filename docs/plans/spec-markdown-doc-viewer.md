# Spec: Markdown viewer pre plan/design dokumenty v Overview

## Kontext

Delivery agenti dnes produkujú markdown obsah (plán, design document) ktorý sa ukladá do `WorkflowState.plan_content` a `WorkflowState.design_content` v SQLite databáze a vracia cez `GET /api/workflows/{id}`. V `frontend/src/routes/Overview.svelte` (riadky 486–488 pre plan, 507–509 pre design) sa však tento obsah renderuje ako surový text v `<pre>` blokoch — headings, zoznamy, code bloky a tabuľky vyzerajú ako mono-text bez formátovania.

Frontend už má plne funkčnú markdown pipeline — `frontend/src/lib/markdown.ts:renderMarkdown()` používa `marked` (v12) + `DOMPurify` a je nasadený vo `Wiki.svelte` aj `WikiPagePanel.svelte` s kompletným `.markdown-content` CSS (headings, code, tabuľky, odkazy). Stačí ho reuse-núť v Overview.

## Rozhodnutie

**Inline rendering v existujúcej `doc-section` — bez novej routy, bez modalu, bez nového API endpointu.**

Používateľ si plán/design rozbalí tak ako dnes (`▶` / `▼` toggle), ale obsah vnútri je renderovaný ako HTML s GitHub-dark markdown štýlmi namiesto holého `<pre>`.

## Zmeny

### 1. `frontend/src/routes/Overview.svelte`

**Import:**
- Pridať `import { renderMarkdown } from '$lib/markdown'` do `<script>` bloku (po riadku 10).

**Markup — plan (riadky 486–488):**
```svelte
{#if expandedPlans.has(wf.id)}
  <div class="doc-content markdown-content">{@html renderMarkdown(wf.plan_content)}</div>
{/if}
```

**Markup — design (riadky 507–509):**
```svelte
{#if expandedDesigns.has(wf.id)}
  <div class="doc-content markdown-content">{@html renderMarkdown(wf.design_content)}</div>
{/if}
```

**CSS — prida ť `.markdown-content` pravidlá pod existujúci `.doc-content` blok (~riadok 1057):**
- Skopírovať pravidlá z `WikiPagePanel.svelte:244–343` (h1–h4, p, ul/ol, li, code, pre, blockquote, a, table, th/td, hr) s `:global(...)` selektormi (lebo obsah je injected cez `{@html}`).
- **Odstrániť** pravidlo `.doc-content pre { ... font-family: monospace; white-space: pre-wrap; ... }` (riadky 1054–1057) — už nebude platiť pre whole content, lebo `<pre>` je teraz len pre code bloky a ich štýl dedí z `.markdown-content :global(pre)`.
- **Ponechať** `.doc-content` container štýly (max-height, overflow, border) — tie platia stále.

### 2. Žiadne backend zmeny

Plan/design content sa už vracia cez `GET /api/workflows/{id}` → `WorkflowState.plan_content` / `design_content`. Žiadny nový endpoint, migrácia ani DB zmena.

### 3. Žiadne testy

Komponent je čistý render — `renderMarkdown` už má pokrytie tam kde ju používa wiki. Neoplatí sa pridávať unit test pre triviálne `{@html}` binding.

## Úlohy (v poradí)

1. **Frontend edit** (`delivery-frontend-engineer`) — upraviť `Overview.svelte` podľa bodu 1 vyššie.
2. **Build + type check** — `cd frontend && npm run check && npm run build`. Vite dev server na `:5173` auto-reloaduje, takže vizuálna kontrola je okamžitá.

## Verifikačné kritériá

- [x] Plan a design v Overview tabe renderuje markdown: `#`/`##` headings, ordered/unordered listy, inline `code` a fenced ``` code bloky, tabuľky, odkazy, blockquotes.
- [x] Tmavý GitHub theme (`#0d1117` bg, `#c9d1d9` text, `#58a6ff` linky) zostáva konzistentný s wiki.
- [x] Existujúce expand/collapse toggles fungujú rovnako ako predtým.
- [x] `npm run check` prechádza bez TypeScript errorov.
- [x] Žiadny regres v iných sekciách (workflow list, change summary, guardian widget).

## Non-goals (explicitne NIE v tomto spec-u)

- Samostatný "Documents" tab / route.
- Modal alebo full-screen viewer.
- Listovanie `docs/**/*.md` súborov na disku.
- Editor / úprava plánov z UI.
- Syntax highlighting pre code bloky (marked default, bez highlightera).

## Karpathy checkpoint

- **Simplicity First:** ~20 riadkov diff, žiadna nová knižnica, žiadny nový endpoint, žiadna nová abstrakcia.
- **Surgical Changes:** len 2 markup bloky + 1 import + presun CSS z wiki vzoru. Nič adjacentné sa nedotýka.
