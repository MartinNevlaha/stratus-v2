# Prečo subagent „zamrzne" a prečo sa `TeammateIdle` hook zacyklí

Analýza z 2026-08-11 (session `bug-reparse-fk-violation`, kde sa naživo objavili
oba javy naraz). Popis stavu, nie oprava — nič som nemenil.

Sú to **dve rôzne poruchy**, ktoré sa navzájom maskujú. Prvá je pôvodná choroba,
druhá je defekt v lieku na ňu.

---

## 1. Pôvodná choroba: report ako obyčajný text

Named teammate dokončí prácu a napíše report ako **obyčajný assistant text**
namiesto `SendMessage`. Ten text sa zahodí — ku koordinátorovi nikdy nedôjde.

Výsledok je deadlock:

- lead čaká na správu, ktorá nikdy nepríde,
- teammate čaká na ďalšiu prácu,
- láme to až človek („agent zamrzol, treba naňho kliknúť").

Namerané 2026-08-05 (71 sedení, 424 idle udalostí): 89 idle bez `SendMessage`
(21 %), z toho **44 po reálnej práci** = stratený report. Najhorší prípad
`task13-api-owui` 07-24: **466 minút**. Pravidlo v `delivery-*.md` („plain text
output is discarded") pridané 08-03 znížilo výskyt z 13 % na 1 %, ale
nezlikvidovalo ho.

Na to vznikol hook — zapojený v `.claude/settings.json:67-71`:

```json
"TeammateIdle": [{ "hooks": [{ "command": "stratus hook teammate_idle" }] }]
```

Implementácia: `~/Documents/projects/stratus-v2/hooks/teammate_idle.go`
(137 riadkov, commit `41adbf8`; nasadená binárka `~/go/bin/stratus` zo 7. 8.,
záloha `stratus.bak-before-teammate-idle`).

Logika: pri prechode do idle prečíta `transcript_path`; ak od poslednej
prichádzajúcej správy bola **reálna práca** a **žiadne `SendMessage`**, pošle
agentovi nudge.

### Pasca v návratovej hodnote stop-like hooku

Overené v binárke CC 2.1.222 — a je to dôležité, lebo tá nesprávna možnosť
chorobu zhoršuje:

| návratová hodnota | efekt |
|---|---|
| `{"continue": false}` | `preventContinuation` → agenta **TVRDO ZASTAVÍ** |
| `{"decision": "block", "reason": "…"}` + exit 0 | `blockingError` → CC vloží `reason` do konverzácie agenta ako správu a agent **pokračuje** |

Nudge je tá druhá. Preto je `hooks.Nudge()` (`hooks/handler.go:69-75`) samostatná
cesta, nie vetva `Block()` — zdieľaný `hooks.Block()` emituje presne
`continue:false`.

---

## 2. Defekt v lieku: hook sa zacyklí

Jadro problému, `hooks/teammate_idle.go:113`:

```go
if strings.Contains(text, "<teammate-message") {
    seg = idleSegment{} // new assignment — start counting again
}
```

Hook si drží len **posledný úsek** transcriptu od poslednej prichádzajúcej
správy a pri každej takej správe resetuje **všetky tri** príznaky štruktúry
`idleSegment`:

```go
type idleSegment struct {
    sentMessage   bool   // reportoval cez SendMessage
    alreadyNudged bool   // už raz dostal nudge
    workActions   int    // počet volaní „pracovných" nástrojov
}
```

Rozhodnutie padne na `teammate_idle.go:56-60`:

```go
if seg.sentMessage || seg.workActions == 0 || seg.alreadyNudged {
    return Decision{Continue: true}
}
return Decision{Nudge: true, Reason: teammateIdleNudgeReason}
```

**Dôsledok:** akákoľvek správa od leada sa počíta ako „nové zadanie" — aj
obyčajné potvrdenie „ďakujem, mám to". Reset zmaže aj fakt, že agent **už
reportoval**, aj fakt, že **už raz dostal nudge**. Slučka potom vyzerá takto:

```
agent reportuje cez SendMessage        → sentMessage = true, nudge nepríde
lead odpíše ack                        → seg = {} … sentMessage aj alreadyNudged PREČ
agent spraví jeden kontrolný Bash      → workActions = 1
agent ide do idle                      → nudge (akoby nikdy nič neposlal)
agent pošle „som hotový" znova         → lead odpíše → reset → nudge → …
```

Garancia „max jeden nudge na zadanie" teda platí len dovtedy, kým lead
neodpovie.

### Dva prispievajúce faktory

**`Bash` je v `workTools`** (`teammate_idle.go:25-31`):

```go
var workTools = map[string]bool{
    "Edit": true, "Write": true, "MultiEdit": true,
    "NotebookEdit": true, "Bash": true,
}
```

`Read`/`Grep`/`Glob` sú zámerne vynechané (ack na stale echo nikomu nič
nedlhuje), ale agent, ktorý už reportoval a potom si len niečo overí príkazom
(`docker exec … psql`), je klasifikovaný ako „vyprodukoval prácu a mlčí".

**Hook nemá pojem „hotový".** Nedokáže rozlíšiť „dokončil a reportoval" od
„pracoval a mlčí". Terminálny stav vie určiť jedine lead cez `shutdown_request`
alebo `TaskStop`.

### ⚠ Nie je to preklep — je to zafixované testom

`hooks/teammate_idle_test.go:116`
`TestTeammateIdle_NewAssignmentAfterNudge_NudgesAgain` toto chovanie zakotvil
ako **zámer**. Nejde teda o omyl v implementácii, ale o návrhové rozhodnutie,
ktoré **nerozlišuje zadanie od potvrdenia**. Akákoľvek oprava musí premisu toho
testu prepísať, nie ho len opraviť.

---

## Ako to opraviť

### Odporúčané: zastropovať nudge cez celý transcript

Najrobustnejšie a najmenej invazívne — počítať nudge cez **celý** súbor, nie cez
posledný úsek, a zastropovať ich:

```go
// namiesto seg.alreadyNudged v trailing segmente
nudgeCount := <počet výskytov teammateIdleNudgeMarker v celom transcripte>
if nudgeCount >= 2 { return Decision{Continue: true} }
```

Zacyklenie zmizne bez toho, aby bolo treba odlíšiť ack od zadania — čo je
heuristika, ktorá by bola aj tak nespoľahlivá.

**Cena:** agent, ktorý dva nudge zignoruje, ďalší nedostane. To je akceptovateľné
— idle notifikáciu aj tak vidí lead aj človek.

### Doplnkovo: `sentMessage` prežije reset

Brať `SendMessage` kdekoľvek **po poslednom nudge markeri** ako „reportoval".
Dnes ho reset zmaže, čo je hlavný zdroj falošných nudge.

### Nasadenie

Zmena v `stratus-v2` nestačí — treba **rebuild binárky** do `~/go/bin/stratus`
(aktuálna je zo 7. 8. 2026). Hook je fail-open, takže pokazený build agentov
nezasekne, ale nudge prestane fungovať potichu.

---

## Čo funguje už dnes, bez zmeny kódu

**Hotového agenta hneď ukončiť** — `shutdown_request`, resp. `TaskStop`. Hook
nikdy nevie, že je agent hotový; to je práca leada.

Bez toho hook generuje duplicitné „som hotový" správy a — čo je horšia škoda než
samotný spam — **maskuje tým skutočné zamrznutia iných agentov**. Doložené
2026-08-06: `sa-v4` dostal 4 nudge po 3 korektne doručených správach.

---

## Súvisiace záznamy v pamäti

- `bug_teammate_idle_silent_report_deadlock.md` — merania, pasca v návratovej
  hodnote, dodatok z 08-06
- `audit_claude_agents_tools_allowlist_2026_07_30.md` — `tools:` v agentoch je
  striktný allowlist (preto reviewer nemá `Bash` a nemôže spúšťať SQL)
- `trap_agent_dispatch_blocked_means_unregistered_workflow.md` — iná porucha,
  ktorá sa mýli s touto
