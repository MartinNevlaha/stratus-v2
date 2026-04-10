# Stratus v2 — Samoevolujuci system, Onboarding a Wiki

Kompletny popis funkcionalit v ludskej reci.

---

## Co to celkovo robi

Stratus ma tri prepojene systemy, ktore spolu tvoria **samoevolujuci znalostny framework**:

1. **Onboarding** — nascanuje existujuci projekt a vygeneruje wiki dokumentaciu
2. **Wiki Engine** — spravuje znalostnu bazu (stranky, linky, staleness, Obsidian vault)
3. **Evolution Loop** — autonomne vylepsuje pravidla a konfiguracii na zaklade dat

Vsetky tri systemy su prepojene cez LLM (OpenAI, Anthropic, Ollama, ZAI) s budget trackingom a fail-open dizajnom — ked LLM nie je dostupne, system funguje dalej, len bez generovania.

---

## 1. Onboarding — Automaticka dokumentacia projektu

### Co to robi

Ked vyvojar pride do existujuceho projektu, nechce travit hodiny citanim kodu aby pochopil architekturu. Stratus **nascanuje cely projekt** a **vygeneruje wiki stranky** popisujuce architekturu, moduly, konvencie a build proces.

### Ako to funguje krok za krokom

```
1. Detekcia: Je to existujuci projekt?
   ├── Pocet git commitov (vaha 0.30)
   ├── Pocet zdrojovych suborov (vaha 0.25)
   ├── Existuju go.mod / package.json? (vaha 0.20)
   ├── Existuje README? (vaha 0.10)
   └── Existuje CI config? (vaha 0.15)
   → Skore >= 0.4 = existujuci projekt

2. Skenovanie (bez LLM, cisto deterministicke):
   ├── Jazyky (Go, TypeScript, Python...) + pocet riadkov
   ├── Entry pointy (main.go, index.ts, app.py)
   ├── Adresarova struktura (hlbka podla depth)
   ├── Konfiguracne subory (go.mod, Makefile, Dockerfile...)
   ├── Git statistiky (pocet commitov, prispievatelia, vek)
   ├── Testovacia struktura (frameworky, pokrytie)
   └── Vzory (monorepo, CLI app, web app, docker)

3. Generovanie stranok (LLM):
   ├── Architecture Overview (celkovy prehlad)
   ├── Module pages (jeden pre kazdy top-level adresar)
   ├── Conventions (konvencie v kode)
   ├── Dependencies (zavislosti)
   └── Build Guide (ako buildovat a deployovat)

4. Prepojenie:
   ├── Cross-referencie medzi strankami (Linker)
   ├── Vault sync do Obsidian (ak je nakonfigurovany)
   └── Ulozenie do SQLite (wiki_pages + FTS5 full-text search)
```

### Tri sposoby spustenia

| Sposob | Prikaz | Kedy pouzit |
|--------|--------|-------------|
| **CLI** | `stratus onboard --depth standard` | Developer v terminali |
| **API** | `POST /api/onboard` | Dashboard alebo automatizacia |
| **Dashboard** | Tlacidlo "Onboard Project" na Wiki stranke | Vizualne rozhranie |

### Hlbka generovania

| Depth | Stranok | Co generuje |
|-------|---------|-------------|
| `shallow` | 3-5 | Architektura, konvencie, build guide |
| `standard` | 8-15 | + moduly pre top-level adresare |
| `deep` | 15-25 | + sub-moduly, zavislosti, testovacia strategia |

### Bezpecnost

- **Ziadny zdrojovy kod sa neposiela do LLM** — len metadata (nazvy suborov, pocty riadkov, struktura)
- Preskakuje `.env`, `*.pem`, `*.key`, `credentials*` a dalsie citlive subory
- Config subory su orezane na 4KB
- README na 2000 znakov
- Output dir je validovany proti path traversal

### Idempotencia

Druhe spustenie `stratus onboard` **neprepisuje existujuce stranky**:
- Publikovane stranky sa preskakuju
- Stale (zastarale) stranky sa regeneruju
- Nove stranky sa pridavaju

---

## 2. Wiki Engine — Znalostna baza

### Co to robi

Wiki Engine je **LLM-pohanana znalostna baza** ktora:
- Generuje a udrzuje wiki stranky
- Sleduje ich aktualnost (staleness)
- Prepaja ich cez cross-referencie
- Synchornizuje do Obsidian vaultu
- Odpoveda na otazky cez syntezny engine

### Typy stranok

| Typ | Popis | Priklady |
|-----|-------|----------|
| `summary` | Prehlady a suhrny | Architecture Overview, Build Guide |
| `entity` | Konkretne moduly/komponenty | API Module, Database Layer |
| `concept` | Abstraktne koncepty | Error Handling, Testing Strategy |
| `answer` | Odpovede na otazky | Synthesized wiki query results |
| `index` | Automaticky index | Knowledge Wiki Index |

### Zivotny cyklus stranky

```
draft → published → stale → (regenerated) → published
                      ↑
                      └── staleness_score > 0.7
```

### Staleness — Ako sa deteguje zastaralost

Kazda stranka ma `staleness_score` od 0.0 do 1.0. Vypocet:

```
staleness = 0.3 * (dni_od_aktualizacie / 30)
          + 0.2 * (je_to_prva_verzia ? 1 : 0)
          + 0.2 * (nema_prichadzajuce_linky ? 1 : 0)
```

**Tri urovne detekcie:**

1. **Casova** — Periodic maintenance (kazdych 24h) prepocitava skore
2. **Git-based** — Pri starte `stratus serve` porovna git diff a zvysi staleness o 0.3 pre dotkute stranky
3. **Workflow-based** — Ked sa dokonci workflow, zvysi staleness o 0.2 pre stranky suvisace so zmenenymi subormi

Ked staleness prekroci 0.7, stranka sa oznaci ako `stale` a pri dalsom ingeste sa regeneruje.

### Linker — Automaticke prepojenia

Linker deteguje vztahy medzi strankami:

| Typ linku | Ako sa deteguje | Sila |
|-----------|-----------------|------|
| `related` | Nadpis stranky A sa nachadza v obsahu stranky B | 0.5 |
| `related` | Stranky zdielaju 2+ rovnakych zdrojov | 0.2 * pocet |
| `contradicts` | Rovnaky prefix nadpisu, rozny obsah | 0.7 |
| `parent`/`child` | Hierarchicka struktura | variabilna |

### Synthesizer — Odpovedanie na otazky

Ked sa agent (alebo uzivatel) spyta otazku cez `wiki_query`:

```
1. Vyhladanie relevatnych stranok cez FTS5
2. Stranky sa poslu ako kontext do LLM
3. LLM vygeneruje odpoved s citaciami [source_type:source_id]
4. Odpoved sa moze ulozit ako nova "answer" stranka
```

### Obsidian Vault Sync

Ak je nakonfigurovany `vault_path` v `.stratus.json`:

```
wiki_pages (SQLite) → VaultSync → .obsidian-vault/
                                   ├── summaries/
                                   │   └── architecture-overview.md
                                   ├── entities/
                                   │   ├── api-module.md
                                   │   └── database-layer.md
                                   ├── concepts/
                                   │   └── project-conventions.md
                                   └── answers/
                                       └── ...
```

Kazdy subor obsahuje:
- YAML frontmatter (metadata, tagy, staleness)
- Obsidian-kompatibilny markdown
- `[[Wikilinks]]` na dalsie stranky
- Callout bloky (`> [!note]`, `> [!warning]`)

---

## 3. Evolution Loop — Autonomne vylepsovanie

### Co to robi

Evolution Loop je **autonomny optimalizacny system** ktory:
- Generuje hypotezy o tom, co by sa dalo vylepsit
- Spusta experimenty (realne alebo simulovane)
- Vyhodnocuje vysledky
- Automaticky aplikuje vylepsenia (alebo vytvara navrhy na schvalenie)

### Ako to funguje

```
1. GENEROVANIE HYPOTEZ
   ├── LLM analyzuje metriky a vzory
   ├── Navrhne konkretne zmeny (JSON format)
   └── Fallback: staticke seed hypotezy (8 preddefinovanych)

2. EXPERIMENTOVANIE
   ├── prompt_tuning: Realne A/B testovanie cez LLM
   │   ├── Baseline prompt → LLM → score
   │   └── Navrhnuty prompt → LLM → score
   └── Ostatne kategorie: Simulovane metriky

3. VYHODNOTENIE
   confidence = sqrt(sampleSize / minSampleSize) * effectRatio
   
   ├── confidence >= 0.85 + interna kategoria → AUTO-APPLY
   ├── confidence >= 0.65 → PROPOSAL (wiki stranka na schvalenie)
   └── inak → REJECTED

4. GUARDIAN MONITORING
   └── Ak sa po zmene zhorsia scorecards o >10% → AUTO-REVERT
```

### Kategorie hypotez

| Kategoria | Co optimalizuje | Sposob aplikacie |
|-----------|----------------|------------------|
| `workflow_routing` | Kam sa smeruju workflows | Auto-apply (interne) |
| `agent_selection` | Ktory agent pre aku ulohu | Auto-apply (interne) |
| `threshold_adjustment` | Prahy pre auto-schvalovanie | Auto-apply (interne) |
| `prompt_tuning` | Prompty pre agentov | Proposal → wiki → schvalenie uzivatelom |

### Priklady hypotez

- "Zvysit confidence threshold pre auto-apply z 0.85 na 0.90" (threshold_adjustment)
- "Preferovat delivery-backend-engineer ked task obsahuje 'API'" (agent_selection)
- "Pridat chain-of-thought prefix do review promptu" (prompt_tuning)
- "Zvysit timeout pre complex spec workflows" (workflow_routing)

### Bezpecnostne zarucky

- **Timeout** — Kazdy run ma casovy limit (default 2 minuty)
- **Budget** — LLM volania su limitovane dennym tokenovym budgetom
- **Guardian** — Monitoruje zmeny a revertuje pri degradacii
- **Priority** — Evolution pouziva `PriorityLow` — ked je budget vyerpany, zastavi sa skor ako wiki alebo user queries

---

## 4. Ako to vsetko spolupracuje

### Kompletny datovy tok

```
┌────────────────────────────────────────────────────────────┐
│                    DEVELOPER ONBOARDING                     │
│                                                             │
│  git clone projekt                                          │
│  stratus init → "Detected existing project (85%)"           │
│  stratus onboard → 12 wiki stranok vygenerovanych           │
│  Obsidian vault synchronizovany                             │
└────────────────────────┬───────────────────────────────────┘
                         │
┌────────────────────────▼───────────────────────────────────┐
│                    AGENT WORKFLOWS                           │
│                                                             │
│  /spec-complex "pridaj payment processing"                  │
│     discovery faza:                                         │
│       → retrieve(query) → vrati KOD + GOVERNANCE + WIKI     │
│       → agent vidi Architecture Overview wiki stranku        │
│       → rozumie architektury PRED implementaciou             │
│                                                             │
│  /bug "login timeout"                                       │
│     analyze faza:                                           │
│       → retrieve("auth module") → wiki entity stranka       │
│       → rychlejsie najde root cause                         │
└────────────────────────┬───────────────────────────────────┘
                         │
┌────────────────────────▼───────────────────────────────────┐
│                    STALENESS DETEKCIA                        │
│                                                             │
│  stratus serve start:                                       │
│    → git diff → zmenene subory → boost staleness +0.3       │
│                                                             │
│  Workflow complete event:                                    │
│    → dotkute subory → boost staleness +0.2                  │
│                                                             │
│  Periodic maintenance (24h):                                │
│    → prepocet staleness score → mark stale ak > 0.7         │
│    → stale stranky sa regeneruju pri dalsom ingeste          │
└────────────────────────┬───────────────────────────────────┘
                         │
┌────────────────────────▼───────────────────────────────────┐
│                    EVOLUTION LOOP                            │
│                                                             │
│  Trigger (manualne alebo scheduled):                        │
│    → Generovanie hypotez (LLM alebo seed)                   │
│    → Experimentovanie (A/B alebo simulacia)                 │
│    → Vyhodnotenie (confidence scoring)                      │
│    → Auto-apply ALEBO proposal → wiki stranka               │
│                                                             │
│  Guardian monitoring:                                       │
│    → Sleduje scorecards po zmenach                          │
│    → Auto-revert ak degradacia > 10%                        │
└────────────────────────────────────────────────────────────┘
```

### Retrieve — Jediny pristupovy bod pre agentov

Ked agent vola `retrieve` (MCP tool), dostane vysledky z **troch zdrojov**:

| Zdroj | Co hlada | Engine |
|-------|----------|--------|
| **Code** | Zdrojovy kod, funkcie, typy | Vexor (semanticky search) |
| **Governance** | Pravidla, ADRs, templates, CLAUDE.md | FTS5 (trigram tokenizer) |
| **Wiki** | Architektura, moduly, konvencie, build guide | FTS5 (porter unicode61) |

V auto rezime (corpus nie je specifikovany):
- Code: az `top_k` vysledkov
- Governance: az `top_k` vysledkov
- Wiki: orezane na `top_k / 3` (aby neprehlusili ostatne)
- Stale wiki stranky maju **50% penaltu** na skore

### LLM Budget Tracking

Vsetky LLM volania su sledovane:

```
                    ┌─────────────┐
                    │ LLM Provider│
                    │ (OpenAI...)  │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │ BudgetClient│ ← denný limit (default 100K tokenov)
                    └──────┬──────┘
                           │
              ┌────────────┼─────────────┐
              │            │             │
     ┌────────▼───┐ ┌─────▼────┐ ┌──────▼─────┐
     │ wiki_engine│ │evolution │ │ onboarding │
     │ (Medium)   │ │(Low)     │ │ (Medium)   │
     └────────────┘ └──────────┘ └────────────┘
     
     PriorityHigh: user queries, guardian (vzdy prejdu)
     PriorityMedium: wiki, onboarding (zastavia sa pri vycerpanom budgete)
     PriorityLow: evolution (zastavi sa prvy)
```

---

## 5. Konfiguracia

### .stratus.json — Klucove nastavenia

```json
{
  "llm": {
    "provider": "openai",
    "model": "gpt-4o",
    "api_key": "sk-...",
    "temperature": 0.7,
    "max_tokens": 16384
  },
  "wiki": {
    "enabled": true,
    "ingest_on_event": true,
    "staleness_threshold": 0.7,
    "vault_path": "./wiki-vault",
    "vault_sync_on_save": true,
    "onboarding_depth": "standard",
    "onboarding_max_pages": 20
  },
  "evolution": {
    "enabled": true,
    "daily_token_budget": 100000,
    "auto_apply_threshold": 0.85,
    "proposal_threshold": 0.65,
    "max_hypotheses_per_run": 10,
    "timeout_ms": 120000
  }
}
```

### Defaultne hodnoty

| Nastavenie | Default | Popis |
|-----------|---------|-------|
| `wiki.enabled` | `false` | Wiki engine je vypnuty kym sa explicitne nezapne |
| `wiki.staleness_threshold` | `0.7` | Prah pre oznacenie stranky ako stale |
| `wiki.onboarding_depth` | `"standard"` | Hlbka generovania (shallow/standard/deep) |
| `wiki.onboarding_max_pages` | `20` | Maximum stranok na jeden onboarding run |
| `evolution.enabled` | `false` | Evolution loop je vypnuty kym sa nezapne |
| `evolution.auto_apply_threshold` | `0.85` | Minimalna confidence pre auto-apply |
| `evolution.proposal_threshold` | `0.65` | Minimalna confidence pre navrh |
| `evolution.daily_token_budget` | `100000` | Denny limit tokenov pre evolution |

---

## 6. CLI prikazy

```bash
# Inicializacia (deteguje existujuci projekt)
stratus init

# Onboarding — vygeneruj wiki
stratus onboard
stratus onboard --depth deep
stratus onboard --dry-run              # len scan, bez generovania
stratus onboard --output-dir ./docs    # + standalone markdown

# Server (spusti dashboard + API)
stratus serve

# MCP server (pre Claude Code / OpenCode)
stratus mcp-serve
```

---

## 7. API endpointy

### Wiki
- `GET /api/wiki/pages` — Zoznam stranok (filtre: type, status, tag)
- `GET /api/wiki/search?q=...` — Full-text vyhladavanie
- `POST /api/wiki/query` — LLM synteza s citaciami
- `GET /api/wiki/graph` — Graf pre vizualizaciu
- `POST /api/wiki/vault/sync` — Synchronizacia do Obsidian

### Onboarding
- `POST /api/onboard` — Spusti onboarding (async, vrati job_id)
- `GET /api/onboard/status` — Progress (WebSocket broadcast)

### Evolution
- `POST /api/evolution/trigger` — Manualne spustenie
- `GET /api/evolution/runs` — Historia behu
- `GET /api/evolution/config` — Aktualna konfiguracia

### Retrieve (zjednoteny search)
- `GET /api/retrieve?q=...&corpus=wiki` — Hlada v kode + governance + wiki

### LLM
- `GET /api/llm/status` — Stav LLM providera
- `GET /api/llm/usage` — Historia pouzitia tokenov

---

## 8. Dizajnove principy

### Fail-Open
Ked LLM nie je dostupne alebo zlyha, system pokracuje:
- Wiki ingest preskaoci generovanie, vrati prazdny vysledok
- Evolution sa prepne na staticke seed hypotezy
- Onboarding preskaoci zlyhanu stranku, pokracuje s dalsimi
- Vault sync ignoruje chyby jednotlivych stranok

### Zero-External-Dependencies
Vsetko bezi v jednom binarnom subore:
- SQLite databaza (WAL mode pre concurrency)
- Embedded Svelte frontend
- Embedded skills, agenti, pravidla
- LLM je jedina externalna zavislost (a aj ta je volitelna)

### Budget-Aware
Kazde LLM volanie je sledovane a limitovane:
- Denne limity per subsystem
- Priority system (High/Medium/Low)
- Transparentne reportovanie cez API + dashboard

### Staleness-Driven Refresh
Dokumentacia sa neregeneruje zbytocne:
- Len ked staleness prekroci prah
- Casove, git-based, a workflow-based triggery
- Idempotentne operacie (existujuce stranky sa preskakuju)
