# Wiki Knowledge Graph — Improvements

**Workflow:** `spec-wiki-graph-markdown`
**Date:** 2026-04-14

## Súčasný stav

- **Graf:** `frontend/src/routes/Wiki.svelte:771–838` — d3-force + natívne SVG (900×560), node click → `selectedPage` (`:455–462`).
- **Render obsahu:** raw text v `.page-detail` (`:644–652`) — markdown sa ukladá do DB, ale vo FE sa nerenderuje.
- **Žiadna markdown knižnica** v `frontend/package.json`.
- **Node payload:** `id, title, page_type, status, staleness_score` (+ edges: `link_type, strength`).
- **API:** `GET /api/wiki/graph?type=&limit=` → `api/routes_wiki.go:280–301`.
- **DB:** `wiki_pages.content TEXT` (markdown), `wiki_links.link_type` (related/parent/child/contradicts/supersedes/cites).

## Príležitosti na zlepšenie

### 1. Markdown preview on node click (PRIORITA — požadované užívateľom)

- Pridať `marked` (+ `DOMPurify` kvôli XSS) do `frontend/package.json`.
- Nový komponent `WikiPagePanel.svelte` — sticky pravý side-panel (400–500 px), otvorí sa pri kliknutí na node v grafe.
- Obsah: rendered markdown (nadpisy, listy, tabuľky, code blocks, linky), breadcrumb `parent → current`, zoznam `links_to/from` ako klikateľné chips.
- Reuse `selectedPage` state, len swap raw text za `{@html sanitize(marked(content))}`.
- Bonus: syntax highlighting code blokov cez `highlight.js` (voliteľne, +~30 KB).

### 2. Edge-type legenda + vizuálne rozlíšenie

- Momentálne sú všetky hrany šedé, líšia sa len hrúbkou (strength).
- Mapovať `link_type` → farba + dash: related (šedá dashed), parent (modrá), child (zelená), contradicts (červená), supersedes (oranžová), cites (fialová).
- Legenda nad grafom + hover na hranu = tooltip s typom a strength.

### 3. Search/filter nad grafom

- Textový input + checkboxy `page_type` a `status`.
- Real-time dimming nie-matchujúcich nodov (reuse existujúci hover/dim pattern na `:1387`).
- Pre obsahový search využiť existujúci FTS5 `searchWiki()` endpoint.

### 4. Staleness tooltip + badge

- Pri hover na node: tooltip so `staleness_score`, `updated_at`, `generated_by`.
- Stale nody (> 0.7) dostanú jemný pulz/badge vedľa labelu.

### 5. Layout mód: force vs. hierarchický (voliteľné, druhá vlna)

- Toggle medzi d3-force a hierarchickým (parent/child ako strom cez d3-hierarchy).
- Pomôže pri wiki s výraznou parent-child štruktúrou.

## Navrhovaný scope pre túto iteráciu (simple spec)

Zameraná iterácia — len **najvyššia priorita** + lacné vizuálne vylepšenia, aby bol PR zvládnuteľný:

1. **Markdown rendering** v detail view (pravý side-panel na graph click + aj v existujúcom browse view).
2. **Edge-type farby + legenda** (čistý CSS/SVG, žiadna nová dependency).
3. **Staleness tooltip** pri hover node.

Search/filter a layout toggle → samostatný spec neskôr.

## Tasks

1. **Frontend: pridať `marked` + `DOMPurify`** do `package.json`, vytvoriť util `lib/markdown.ts` (render + sanitize).
2. **Frontend: nový `WikiPagePanel.svelte`** — sticky right panel, otvára sa pri `selectedPage != null`, renderuje markdown, breadcrumb, linky.
3. **Frontend: integrácia panelu do `Wiki.svelte`** — graph node click otvorí panel, existujúci browse view tiež použije markdown render namiesto raw text.
4. **Frontend: edge-type farby + legenda** v SVG rendereri (`Wiki.svelte:790–798` + nová `.graph-legend`).
5. **Frontend: staleness tooltip** on hover — rozšíriť existujúci hover pattern o floating tooltip s `staleness_score`, `updated_at`.
6. **Rebuild + smoke test** — `cd frontend && npm install && npm run build`, potom `go build`, overiť `./stratus serve` → wiki tab.

## Risks / gotchas

- **XSS:** markdown z DB = nutné sanitizovať (`DOMPurify`), inak XSS cez `{@html}`.
- **Svelte 5 runes:** nový panel state cez `$state`, `$derived`.
- **Embed rebuild:** FE build musí byť commitnutý do `cmd/stratus/static/` pred `go build`.
- **Bundle size:** `marked` (~10 KB) + `DOMPurify` (~20 KB) = akceptovateľné.
