# Evolution & Code Quality v Stratus — ako to funguje (ľudsky)

## 1. Karpathy princíp — čo to vlastne je

Andrej Karpathy má jednu myšlienku, ktorá sa ťahá celou jeho prácou:
**systém sa má sám učiť a sám sa vylepšovať na základe dát, nie na
základe toho, že to niekto manuálne doprogramuje.**

V praxi to znamená cyklus:

1. **Hypotéza** — "čo keby sme zmenili X na Y, možno to bude lepšie?"
2. **Experiment** — reálne alebo simulované porovnanie starého vs. nového
3. **Meranie** — získaj číslo (metrika), nie pocit
4. **Rozhodnutie**:
   - ak je dôkaz silný → aplikuj automaticky
   - ak je slabší, ale zaujímavý → navrhni človeku (proposal/wiki)
   - ak je neistý → zahoď (rejected)
5. **Opakuj** — ďalšie kolo začína s už zlepšeným baseline-om

Presne toto robí Stratus Evolution loop. Nie je to "AI, ktorá vymyslí
všetko". Je to **uzavretý slučkový optimalizátor**, kde LLM je len jedna
z komponent (generátor hypotéz + prompt evaluator), a o výsledku
rozhoduje matematika.

---

## 2. Evolution loop — technicky, ale stručne

Kód: `internal/insight/evolution_loop/`

### Fázy jedného runu

```
generate hypotheses  →  for each: run experiment  →  evaluate  →  decide
       │                         │                      │           │
   LLM alebo               simulácia alebo          confidence    auto_apply
   static seedy            reálne A/B s LLM         = f(sample,    proposal_created
                                                       effect)    rejected
                                                                  inconclusive
```

Hlavné súbory:

| Súbor | Čo robí |
|---|---|
| `loop.go` | Orchestruje celý run, drží timeout budget, zapisuje `evolution_runs` |
| `hypothesis.go` | Generuje kandidátov (LLM → fallback na `seedHypotheses`) |
| `experiment.go` | Spustí experiment. Pre `prompt_tuning` + LLM robí reálne A/B; inak simulácia (fixná metrika per kategória, sample=20) |
| `evaluator.go` | Vypočíta confidence a vyberie rozhodnutie |
| `store.go` | Persistencia do SQLite |

### Kategórie a auto-apply

Len **interné** kategórie môžu byť auto-apply (bez človeka):

```go
internalCategories = {workflow_routing, agent_selection, threshold_adjustment}
```

`prompt_tuning` auto-apply **nie je** — vždy vytvorí proposal (wiki stránku),
lebo zmena promptu ovplyvňuje správanie LLM a človek by to mal vidieť.

### Confidence vzorec (toto je kľúč k tomu, prečo sú všetky rejected)

```
sampleFactor = min(1.0, sqrt(sampleSize / minSampleSize))
effectRatio  = (experimentMetric - baselineMetric) / max(baselineMetric, 0.001)
confidence   = clamp(sampleFactor * effectRatio, 0, 1)
```

Rozhodnutia v poradí:

1. `sample < minSample` → **inconclusive**
2. `confidence ≥ AutoApplyThreshold` **a** internal kategória → **auto_applied**
3. `confidence ≥ ProposalThreshold` → **proposal_created** (vytvorí wiki stránku)
4. inak → **rejected**

---

## 3. Prečo boli v tvojom runu všetky 6 hypotéz **rejected**

Toto je ten najdôležitejší kúsok — nie je to bug, je to matematika.

Tvoj run (12.4.2026 19:08):

| Description | Baseline | Experiment | Delta | Confidence |
|---|---|---|---|---|
| Lower routing threshold 0.80→0.75 | 0.800 | 0.920 | +0.120 | **15%** |
| Raise routing threshold 0.80→0.85 | 0.800 | 0.920 | +0.120 | **15%** |
| Prefer specialist agents >3 files | 0.720 | 0.880 | +0.160 | **22%** |
| Reduce auto-apply threshold 0.85→0.80 | 0.850 | 0.950 | +0.100 | **12%** |
| Increase min sample size 10→15 | 0.780 | 0.950 | +0.170 | **22%** |
| Chain-of-thought prompt prefix | 0.680 | 0.750 | +0.070 | **10%** |

Prečo confidence nie je napr. 85%, keď zlepšenie je +12% absolútne?

Lebo confidence **nie je to isté ako zlepšenie**. Confidence je
**relatívny efekt krát sample faktor**. Spočítajme prvý riadok:

```
effectRatio  = (0.92 - 0.80) / 0.80 = 0.15   (t.j. 15% relatívne zlepšenie)
sampleSize   = 20 (simulované, fixné)
minSample    = default (10, resp. 15 — aj tak sampleFactor ≈ 1)
sampleFactor ≈ 1.0
confidence   = 1.0 * 0.15 = 0.15 = 15%
```

Default thresholdy v `EvolutionConfig`:

- `AutoApplyThreshold` ≈ 0.85
- `ProposalThreshold` ≈ 0.70

**15 % < 70 %** → spadá do "rejected" vetvy. Presne to isté sa deje
pri všetkých ostatných — najlepšie dosahujú 22 %, čo je stále hlboko
pod 70 %.

### Čiže zhrnutie:

- Experimenty "uspeli" v zmysle "metrika sa zlepšila"
- Ale sample size 20 a malý relatívny effect robia confidence nízku
- Systém je konzervatívny — radšej zamietne, ako by mal aplikovať šum
- **Toto je zámer**, nie porucha. Je to Karpathyho princíp v praxi:
  neaplikuj zmenu, ktorú nevieš štatisticky obhájiť.

---

## 4. Čo sa deje s výstupmi jednotlivých decisions

| Decision | Kde skončí | Vidí to človek? |
|---|---|---|
| `auto_applied` | `applyFn` callback v `loop.go` — reálne zmení config. `AutoApplied++`. | Len ako counter + záznam v DB |
| `proposal_created` | `wikiFn` callback vytvorí wiki stránku. Hypotéza má `WikiPageID`. `WikiPagesUpdated++`. | **Áno — ako wiki page** |
| `rejected` | Len update riadku v `evolution_hypotheses` s decision="rejected" a reason. | Len v histórii behov (dashboard tabuľka) |
| `inconclusive` | To isté ako rejected — zapíše sa, nič sa nedeje. | To isté |
| `apply_failed` | Pokus o auto-apply zlyhal → status prepíše na `apply_failed`. | Len v DB |

V tvojom konkrétnom behu:

- **Experiments 6** — vygenerovalo sa 6 hypotéz (static seed, nie LLM,
  keďže sumarizujú katalóg v `seedHypotheses`)
- **Wiki pages updated 0** — nikto neprekročil proposal threshold, takže
  `wikiFn` sa nezavolal
- **Completed** — run prešiel celým cyklom bez timeoutu

Dôležité: výstupy **sa nestrácajú**. Každá hypotéza má v SQLite záznam
s metrikou, confidence a reason. Takže aj "rejected" behy sú dáta,
z ktorých sa dá neskôr učiť (napr. "táto kategória nikdy neprejde,
asi je seed zlý").

---

## 5. Ako by sa to mohlo začať aplikovať

Aby si niekedy uvidel auto-apply alebo wiki page:

1. **Zníž `ProposalThreshold`** (napr. na 0.15) v `.stratus.json` →
   15% confidence by už stačilo na proposal → wiki page sa vytvorí
2. **Zvýš simulovaný effect** v `experiment.go` `categoryBaselines`
   (teraz max 0.95 → delta max ~20%)
3. **Zapni reálny LLM klient** pre `prompt_tuning` — tam confidence
   počíta reálny evaluator, nie simulácia (ale proposal_created, nikdy
   auto_applied, lebo prompt_tuning nie je v `internalCategories`)
4. **Zvýš sample size** v experimente — ale keďže `sampleFactor`
   je už clampnutý na 1, toto sám o sebe nepomôže; musí stúpnuť
   effectRatio

---

## 6. Code Quality (Project Code Evolution) — sesterská vetva

Kód: `CodeAnalysisConfig` v `config/config.go` + analyzátory v
`internal/insight/*`. Je to **iný loop**, ale s rovnakou filozofiou:

- Skenuje repo v intervaloch
- Vyberie súbory s najvyšším churn score (`min_churn_score`)
- Pošle ich do LLM s kategóriami: `anti_pattern`, `duplication`,
  `coverage_gap`, `error_handling`, `complexity`
- LLM vráti návrhy s confidence
- Ak confidence ≥ `ConfidenceThreshold` → návrh sa zapíše (nič sa
  neaplikuje automaticky, lebo to je kód, nie config)
- Token budget (`TokenBudgetPerRun`) obmedzuje náklady

Rozdiel oproti Evolution:

| | Evolution loop | Code Quality |
|---|---|---|
| Čo meria | rozhodovacie pravidlá Stratusu | zdrojový kód projektu |
| Auto-apply | áno (pre interné kategórie) | nie, len návrhy |
| Vstup | hypotézy zo seedov/LLM | súbory s vysokým churn |
| Výstup | config changes + wiki | wiki návrhy / issues |

Scorecards (`internal/insight/scorecards/`) počítajú metriky ako
`success_rate`, `review_pass_rate`, `regression_rate` — tie slúžia
ako "baseline metriky", z ktorých by budúce verzie evolution loopu
mali čerpať namiesto fixných simulovaných hodnôt.

---

## 7. TL;DR

- **Evolution** = autonómny A/B slučkový optimalizátor pre rozhodovacie
  pravidlá Stratusu (thresholdy, routing, agent selection, prompty)
- **Karpathy princíp** = nechaj systém generovať hypotézy, merať ich a
  sám sa vylepšovať — ale vždy s prahom dôvery, aby neaplikoval šum
- **Všetko rejected v tvojom runu** = confidence sa počíta ako *relatívny*
  efekt × sample faktor. 15% relatívne zlepšenie je príliš málo na to,
  aby prekonalo 70% proposal threshold. Systém funguje ako má — je
  konzervatívny.
- **Výstupy** idú do SQLite (každá hypotéza), wiki (proposal_created),
  config (auto_applied). Nič sa nestráca, rejected hypotézy sú dáta
  do budúcnosti.
- **Code Quality** je paralelný loop nad zdrojákom — nikdy neaplikuje
  automaticky, len navrhuje.
