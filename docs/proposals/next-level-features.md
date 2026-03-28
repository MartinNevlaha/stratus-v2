# Next-Level Feature Proposals

Návrhy na posunutie Stratus z reaktívneho workflow orchestrátora na **proaktívneho AI development partnera**.

---

## Prehľad

| # | Feature | Dopad | Komplexita | Priorita |
|---|---------|-------|------------|---------|
| 1 | GitHub Integration Bridge | Vysoký | Stredná | 🔴 Vysoká |
| 2 | Workflow Risk Scoring | Vysoký | Nízka | 🔴 Vysoká |
| 3 | Adaptive Agent Evolution | Vysoký | Nízka | 🔴 Vysoká |
| 4 | Semantic Change Summary | Stredný | Stredná | 🟡 Stredná |
| 5 | Ambient Codebase Guardian | Vysoký | Stredná | 🟡 Stredná |
| 6 | Cross-Project Knowledge Federation | Vysoký | Vysoká | 🟢 Neskôr |

---

## Feature 1: GitHub Integration Bridge

### Problém
Stratus žije mimo prirodzeného dev cyklu. Vývojár musí manuálne spúšťať workflows. CI zlyhá → vývojár si to všimne → ručne spustí bug workflow. Stratená automatizácia.

### Riešenie
Webhook receiver, ktorý napojí Stratus na GitHub/GitLab eventy a automaticky spúšťa workflows.

### Správanie

| GitHub Event | Stratus Akcia |
|---|---|
| `pull_request.opened` | Auto-start `code-review` workflow s PR URL a diff |
| `issues.opened` + label `bug` | Auto-start `bug` workflow s popisom z issue |
| GitHub Actions `workflow_run.completed` (failed) | Auto-start `bug` workflow s linkom na failed run a log snippetom |
| Stratus workflow `complete` | POST komentár na GitHub PR/issue so sumárom |

### Architektonické body
```
POST /api/integrations/github/webhook
  → verify HMAC signature (X-Hub-Signature-256)
  → map event type → workflow type + params
  → POST /api/workflows (existujúci endpoint)

POST /api/integrations/github/comment
  → po workflow complete, POST GitHub API /repos/.../issues/.../comments

.stratus.json:
  "github": {
    "webhook_secret": "...",
    "token": "ghp_...",
    "auto_comment": true
  }

stratus init --github  → vypíše inštrukcie pre GitHub webhook setup
```

Dashboard: nový tab "Integrations" so stavom webhookov a históriou triggerovaných eventov.

### Prečo teraz?
Uzatvára cyklus repo → Stratus → repo. Žiaden iný lokálny AI tool toto nemá. Relatívne izolovaná implementácia bez dopadov na core.

---

## Feature 2: Workflow Risk Scoring & Smart Auto-Config

### Problém
Používateľ musí sám rozhodnúť: simple vs. complex spec? swarm vs. single-agent? Toto rozhodnutie vyžaduje znalosť histórie projektu. Zlé rozhodnutie = zbytočná práca alebo nedostatočné pokrytie.

### Riešenie
Pred spustením workflow Stratus analyzuje task description + historické dáta a navrhne optimálnu konfiguráciu.

### Nový endpoint
```
POST /api/workflows/analyze
Body: { "description": "Add OAuth login via Google", "files_hint": ["auth/", "api/routes_auth.go"] }

Response:
{
  "recommended_type": "spec",
  "recommended_complexity": "complex",
  "recommended_strategy": "swarm",
  "risk_score": 0.73,
  "risk_factors": [
    "touches auth module (high-risk domain)",
    "no existing tests for OAuth flow",
    "3 domains affected: backend, frontend, database"
  ],
  "estimated_duration_minutes": 45,
  "similar_past_workflows": [
    { "id": "wf_abc", "description": "Add JWT auth", "outcome": "success", "duration_min": 38 }
  ],
  "suggested_domains": ["backend", "database", "qa"]
}
```

### Logika risk score
```
risk_score =
  (count_affected_domains * 0.20) +
  (keyword_score * 0.30)    // auth/payment/migration/security keywords
  (test_coverage * 0.20)    // inverted — nízke pokrytie = vyšší risk
  (historical_failure_rate * 0.30)  // z workflow_metrics
```

Dashboard: modal pred spustením zobrazí odporúčania s možnosťou override.

### Prečo teraz?
Využíva existujúce `workflow_metrics` dáta. Malý nový endpoint, veľký UX dopad. Implementovateľné bez DB zmien.

---

## Feature 3: Adaptive Agent Evolution

### Problém
Learning pipeline vytvára `.claude/rules/*.md` súbory a governance docs. Agenti sa ale nenaučia nové pravidlá automaticky — treba ich manuálne editovať. Learning loop nie je uzatvorený.

### Riešenie
Keď je `proposal` accepted, Stratus automaticky propaguje novú rule do systémových promptov relevantných agentov.

### Správanie
Pri `POST /api/learning/proposals/{id}/decide` s `decision: accept`:

1. Zistí doménu z proposal metadata (`domain` field)
2. Nájde relevantných agentov podľa doménovej mapy (config):
   ```json
   {
     "backend": ["delivery-backend-engineer", "delivery-implementation-expert"],
     "frontend": ["delivery-frontend-engineer"],
     "qa": ["delivery-qa-engineer"],
     "security": ["delivery-backend-engineer", "delivery-code-reviewer"],
     "all": ["delivery-code-reviewer", "delivery-governance-checker"]
   }
   ```
3. Pridá rule do sekcie `## Learned Rules` v `.claude/agents/{agent}.md` (smart diff — neduplikuje)
4. Uloží audit trail do novej tabuľky `agent_evolutions`

### DB zmena
```sql
CREATE TABLE agent_evolutions (
    id INTEGER PRIMARY KEY,
    proposal_id TEXT NOT NULL,
    agent_name TEXT NOT NULL,
    rule_added TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### API
```
GET /api/agents/evolutions          -- história zmien agentov
GET /api/agents/evolutions/{agent}  -- zmeny pre konkrétneho agenta
```

Dashboard: v Agents tabe badge "X rules learned" pri každom agentovi.

### Prečo teraz?
Minimálna implementácia (hook do existujúceho `decide` endpointu). Uzatvára existujúci learning loop bez nových systémových komponentov.

---

## Feature 4: Semantic Change Summary

### Problém
Keď workflow skončí, výsledok je git diff. Diff hovorí *čo* sa zmenilo na úrovni kódu, nie *čo sa sémanticky zmenilo* v produkte — aké schopnosti pribudli, čo sa rozbilo, aké downstream riziká existujú.

### Riešenie
Po `phase → complete` transition Stratus vygeneruje štruktúrovaný sémantický sumár zmien.

### Implementácia
Uložiť `base_commit` SHA do `workflows` tabuľky pri vytvorení.

Po complete:
1. `git diff {base_commit}..HEAD` → zoznam zmenených súborov + štatistiky
2. FTS search governance docs pre každý zmenený modul → nájde relevantné pravidlá
3. Vexor similarity search → nájde podobné minulé zmeny a ich dôsledky
4. Uloží JSON sumár do `workflows.change_summary`

### Výstup
```json
{
  "capabilities_added": ["OAuth 2.0 login via Google provider"],
  "capabilities_modified": ["Session management now uses JWT instead of cookies"],
  "capabilities_removed": [],
  "downstream_risks": [
    "Mobile clients need to update token handling",
    "Existing sessions will be invalidated on deploy"
  ],
  "governance_compliance": ["Follows auth-pattern rule from .claude/rules/auth.md"],
  "test_coverage_delta": "+12% in auth package",
  "files_changed": 8,
  "lines_added": 234,
  "lines_removed": 89
}
```

### API
```
GET /api/workflows/{id}/summary       -- JSON
GET /api/workflows/{id}/summary.md    -- Markdown export
```

Dashboard: workflow detail zobrazí "Change Summary" kartu.

---

## Feature 5: Ambient Codebase Guardian

### Problém
Stratus reaguje len keď ho vývojár explicitne zavolá. Medzitým môže test coverage klesnúť, governance pravidlá byť porušené novými súbormi, worflows zaseknuté — a nikto to nevie.

### Riešenie
Background goroutine bežiaca vedľa `stratus serve`, ktorá sleduje stav codebase a proaktívne upozorňuje.

### Kontroly (každých N minút, konfigurovateľné)

| Kontrola | Trigger | Akcia |
|---|---|---|
| **Test coverage drift** | pokrytie kleslo o >X% od baseline | Dashboard alert + "Start spec workflow" button |
| **Governance violations** | nový súbor porušuje existujúce rule (FTS match) | Alert s konkrétnym pravidlom |
| **Tech debt accumulation** | TODO/FIXME/HACK count trend rastie | Dashboard trend widget |
| **Stale workflow** | workflow v rovnakej fáze >2h | Alert "Workflow may be stuck" |
| **Memory health** | events count > threshold | Navrhne `/learn` run |

### Architektonické body
```go
// Nový package: guardian/
type Guardian struct {
    db     *db.DB
    config GuardianConfig  // intervals, thresholds
    hub    *api.Hub        // WebSocket broadcast
}

func (g *Guardian) Run(ctx context.Context)  // ticker loop, spúšťa sa v cmd/stratus/main.go
```

### DB zmena
```sql
CREATE TABLE guardian_alerts (
    id INTEGER PRIMARY KEY,
    type TEXT NOT NULL,       -- coverage_drift | governance_violation | stale_workflow | ...
    severity TEXT NOT NULL,   -- info | warning | critical
    message TEXT NOT NULL,
    metadata JSON,
    dismissed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### API
```
GET /api/guardian/alerts               -- aktívne alerty
PUT /api/guardian/alerts/{id}/dismiss  -- zavrieť alert
GET /api/guardian/config               -- zobraziť thresholds
PUT /api/guardian/config               -- update thresholds
```

Dashboard: "Guardian" widget na Overview tabe s badge count. Každý alert má direct "Start Workflow" button.

---

## Feature 6: Cross-Project Knowledge Federation

### Problém
Tímy na mikroslužbách duplikujú governance docs, agent konfigurácie a patterns v každom repo. Keď sa naučí niečo v service-A, service-B o tom nevie.

### Riešenie
Viacero repozitárov môže zdieľať jeden Stratus federation node pre governance a patterns.

### Setup
```bash
# Na zdieľanom node (napr. team server):
stratus serve --federation-mode

# V každom repo:
stratus init --federation http://team-stratus:41777 --federation-token <token>
```

### Čo sa zdieľa (pull pri `stratus refresh`)
- Governance docs z federation node (read-only, mergované do lokálneho FTS indexu)
- Accepted proposals → propagujú sa do všetkých projektov v federation
- Agent evolutions z Feature 3 → synchronizované cez federation
- Aggregated (anonymized) metrics pre porovnanie tímovej produktivity

### Čo zostáva lokálne (nikdy nezdieľané)
- Workflow state a history
- Memory events (môžu obsahovať sensitive code)
- Swarm missions
- File reservations

### Nové API endpointy (federation node)
```
GET  /api/federation/export           -- všetky zdieľané docs + proposals + agent rules
POST /api/federation/register         -- registrácia nového projektu
GET  /api/federation/nodes            -- zoznam registrovaných projektov
```

### .stratus.json rozšírenie
```json
{
  "federation": {
    "endpoint": "http://team-stratus:41777",
    "token": "...",
    "sync_interval_minutes": 30,
    "push_proposals": true,
    "pull_governance": true
  }
}
```

---

## Implementačné poradie

1. **Feature 2** (Risk Scoring) — najrýchlejšia implementácia, okamžitý UX dopad
2. **Feature 3** (Adaptive Agent Evolution) — uzatvára existujúci learning loop
3. **Feature 1** (GitHub Integration) — najvyšší visibility, izolovaná implementácia
4. **Feature 4** (Semantic Change Summary) — vyžaduje `base_commit` tracking
5. **Feature 5** (Ambient Guardian) — nový systémový komponent, background goroutine
6. **Feature 6** (Federation) — najkomplexnejšia, správna až po vyspelom lokálnom core
