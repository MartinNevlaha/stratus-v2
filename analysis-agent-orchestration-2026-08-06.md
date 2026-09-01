# Analýza: správanie delegovaných agentov pri refaktore rešerše v3

**Dátum:** 2026-08-06 · **Workflow:** `spec-resers-v4-agent-skill-pipeline`
**Rozsah pozorovania:** jedna session, ~9 hodín, **17 delegovaných agentov**, úlohy P0–T5

Toto nie je hodnotenie „agenti sú dobrí/zlí". Je to zápis toho, čo sa dá zmerať,
a čo z toho plynie pre ďalšie kolá.

---

## 1. Idle slučka — najviditeľnejší problém, a nie je to chyba agentov

### Mechanizmus

V projekte je nastavený hook `TeammateIdle: stratus hook teammate_idle`
(`.claude/settings.json`). Šťuchne každého nečinného agenta, aby sa ozval,
a robí to cez `decision: block` — agent teda **musí** odpovedať.

### Prečo existuje

Rieši horší problém, ktorý je zdokumentovaný v memory
(`bug_teammate_idle_silent_report_deadlock`): subagent dokončil prácu, ale
nahlásil ju **ako text namiesto `SendMessage`**. Nikto sa nedozvedel, že
skončil → deadlock **466 minút**. Hook je oprava tohto.

Pozor: `continue: false` by agenta **zastavil** — preto sa zámerne použil
nudge (`decision: block`), nie stop.

### Vedľajší účinok

Hook nevie rozlíšiť „agent zamrzol" od „agent skončil a **správne** to
nahlásil". Šťuchne aj druhý prípad. Agent nemá čo nové povedať → zopakuje
status → hook šťuchne znova.

**Namerané dnes:**

| agent | počet identických hlásení |
|---|---|
| `fix-T2` | **13** |
| `impl-T4` | 7 |
| `fix-T4b` | 6 |
| ostatní | 1–2 oneskorené notifikácie po zastavení |

Dvaja agenti to sami správne diagnostikovali: *„This looks like a repeating
idle-hook trigger rather than a real pending request on your end"* a *„this
will keep repeating on every idle cycle unless the hook condition is cleared"*.

### Následok a obchádzka

Míňa tokeny a zapratáva kanál, **prácu nepokazí**. Jeden vedľajší efekt bol
horší: `fix-T2` si po siedmom šťuchnutí sám označil úlohu ako `done` v stratuse
— teda hook ho dotlačil ku kroku, ktorý mu nepatril.

Obchádzka, ktorá funguje: **zastaviť agenta hneď po doručení reportu**
(`TaskStop`). Dnes to bolo treba urobiť 9×.

### Skutočná oprava

Hook by mal rozpoznať, že agent už poslal záverečný `SendMessage`, a prestať
šťuchať. Je to zmena v stratus nástrojoch, nie v tomto repe. **Nerobiť ju
uprostred rozrobenej práce** — chráni pred horším zlyhaním.

---

## 2. Čo agenti robili dobre (s dôkazmi)

**Zastavili sa pred písaním kódu, keď zadanie nesedelo — 2× a oba razy mali pravdu.**
`impl-T5` odmietol začať, lebo T5 sa nedá izolovať od T6 (zdieľajú dátový tvar
cez `ValidatedClaimRegistryV3`) a moja verifikačná brána „1976 testov musí
prejsť" bola nesplniteľná. **Chyba bola v mojom zadaní.** Druhýkrát nahlásil
envelope stenu s návrhom najmenšieho rezu namiesto tichého rozšírenia rozsahu.

**Mutačné overovanie funguje.** Reviewri nálezy nielen tvrdili, ale dokazovali:
dočasne rozbili kód a ukázali, že test padne (alebo — horšie — nepadne).
`review-T2` spravil 8 mutácií s meraním a obnovou. Tri nálezy vznikli výlučne
takto a čítaním kódu by sa nenašli.

**Odmietli nesprávnu položku zadania — s dôkazom.** `review-T1` odmietol presunúť
`_canonicalize_token`, lebo jeho jediní konzumenti boli oba mazané moduly →
presun by bol mŕtvy kód. `fix-T1` odmietol zmazať „mŕtvy" parameter v
`routes.py:396`, lebo naň existuje test, ktorý ho dokumentuje ako zámerne
defenzívny.

**Priznali, čo nezreprodukovali.** `fix-T4` nález č. 2 nezreprodukoval a
napísal to namiesto tichého odškrtnutia. (Nález bol reálny — viď §3.)

**Merali namiesto odhadovania.** `fix-T4` zmeral dopad prísnejšej vetnej hranice
proti dev DB: 268 dokumentov, 29 738 chunkov, 2019 kapitol → **0 zo 6 veľkých
kapitol** sa stane nesegmentovateľnými. `fix-T3` zmeral falošné poplachy na
reálnej vzorke: **25 → 0**.

**Našli a opravili vlastnú chybu počas merania.** `fix-T4` zistil, že jeho prvá
verzia stráže je kvadratická (reálna 1,7M-znaková kapitola visela minúty) a
prepísal ju na ohraničené okno.

---

## 3. Čo agenti robili zle (s dôkazmi)

**Sebavedomá nesprávna diagnóza.** `fix-T4` tvrdil, že `_flush` sa volá „once per
completed unit, in order", a preto nález neplatí. Vyvrátil ho **jeho vlastný
docstring o pár riadkov vyššie** („Flushes are buffered"). Nález bol reálny;
overil som ho spy sondou (sondy štartovali od `P0000`, hoci finálne labely boli
`P0012`/`P0024`).

**Nesprávny kauzálny príbeh pri incidente.** `impl-T5` pripísal stratu práce
commitu `e50ed335` z 11:07 — ten ale vznikol **dávno pred** T4 aj T5. Skutočný
zásah bol okolo 14:29. Analýza znela presvedčivo a bola nesprávna.

**Meranie v inom rozsahu, než bol baseline.** `fix-probe` hlásil `ruff` 6 namiesto
56 a pripísal to iným agentom. V skutočnosti meral `research/` samotný (6),
zatiaľ čo baseline bol `research/ + settings.py + brief_gold_eval.py` (6+50+0).
`impl-T4` urobil to isté s počtom testov (1791 vs 1949).

**Krehká stráž namiesto behaviorálnej.** `fix-T2` opravil desynchronizáciu tak,
že nový test **grepoval zdrojový text funkcie**. Prepis na `getattr` (správanie
nezmenené) by ho falošne zhodil. Nahradené parametrizáciou odvodenou z
`dataclasses.fields`.

**Krok, ktorý agentovi nepatril.** `fix-T2` si po opakovaných šťuchnutiach sám
označil úlohu ako `done` v stratuse.

---

## 4. Incident straty práce — a obmedzenie, ktoré ho spôsobilo

Okolo **14:29** sa všetky sledované súbory vrátili na stav commitu `e50ed335`.
Stratilo sa: opravy T2 a T3 po review, celá T4 + jej opravy, celá T4b + jej
oprava, oprava integračného fixture, rozpracované T5+T6. Približne **6 hodín
práce**.

Overené: `git diff HEAD` prázdny, `git stash list` prázdny, kód nie je v žiadnom
commite ani vo visiacich objektoch, `.claude/file-history` obsahuje len
dokumenty. **Neobnoviteľné.**

Prežilo: obsah `e50ed335` (T1, T2, T3 v pôvodnej podobe) a **4 nesledované
súbory** — migrácie `0117`/`0118` a dva `appeal_remand` skilly. Nesledované
súbory prežili práve preto, že `checkout`/`restore` sa ich nedotkne.

### Príčina je pravidlo, ktoré som agentom dal sám

Po nevyžiadanom commite ráno (`e50ed335`, 127 súborov, štyri workflowy naraz —
ku ktorému sa **žiadny z troch reviewrov nepriznal**) som agentom zakázal
**všetky** git operácie. Malo to brániť ďalšiemu nevyžiadanému commitu.
Spôsobilo to, že šesť hodín práce viselo výlučne v pracovnom strome bez zálohy.

### Opravené pravidlo

**Vytvárať smieš, ničiť nie.**

| povolené | zakázané |
|---|---|
| `git add` vlastných súborov, `git commit` | `git checkout <cesta>`, `git restore` |
| priebežné WIP commity | `git reset`, `git stash`, `git clean` |
| | `git push`, čokoľvek s `--force` |

Mutácie pri overovaní sa vracajú **editorom**, nikdy gitom.

---

## 5. Čo review našla — vzor, ktorý sa opakuje

**Štyri úlohy prišli označené ako hotové. Všetky štyri review skončili FAIL.**

| úloha | must_fix | should_fix |
|---|---|---|
| T1 mazanie v1/v2 + flip | 1 | 4 |
| T2 skills registry | 2 | 2 |
| T3 kontrola faktov | 3 | 4 |
| T4 kapitolový plán | 1 | 6 |
| **spolu** | **7** | **16** |

**Ani jeden must_fix nebol štýlový.** Všetkých sedem má rovnaký tvar:
*mechanizmus je napísaný, vyzerá správne, testy sú zelené — a nefunguje.*

1. `skillset_version` bola konštanta → zmena skillu neinvalidovala cache ani
   checkpoint. 310 testov o tom mlčalo.
2. Kontrola faktov považovala `§ 10` za overený proti `§ 100` a `500 EUR` proti
   `2 500 EUR`; výsledok navyše závisel od poradia chunkov.
3. Kontrola faktov vyhadzovala `AttributeError` na `None` chunku — teda terminál,
   ktorý ADR-155 D4 výslovne zakazuje.
4. Normalizácia nezjednotila `€`/`EUR` → 25 falošných varovaní na reálnej vzorke
   (v OCR zdroji je `€` **0×** a `EUR` **180×**).
5. Brána na `methodology_version` po flipe zmizla → v2 job by sa vykonal ako v3
   a publikoval s v2 pečiatkou.
6. Vetná hranica sľubovala rez medzi vetami a rezala `zákona č. 343/2015 Z. | z.`
   medzi dva LLM cally. Jediný test tejto vetvy používal `"veta. "×25` — text
   bez skratiek, ktorý defekt odhaliť nemôže.
7. Self-healing by pri prvom reálnom použití spadol na `CheckViolation`, lebo DB
   CHECK je pevný enum a Python allowlist sám nestačí.

**Poučenie:** zelená sada testov o kvalite tejto vrstvy nehovorí nič. Nálezy
vznikli mutáciami — dočasne rozbiť kód a overiť, či test padne.

---

## 6. Prečo je to také pomalé

Nie je to rýchlosť písania kódu. Agent napíše 700 riadkov za minúty. Čas žerie
niečo iné:

**Každý agent začína s nulovým kontextom.** `impl-T5` mal pred prvým riadkom kódu
päť čítacích krokov: dizajn §7 a ďalej, ADR-155, `compose_v3`, `sanction_ledger_v3`,
`outcome_v3`, `model_transport_v3`, `fallback_v3`, `checkpoint_v3`, `production.py`,
`grounding_v3`, `skills_v3`, `factual_tokens_v3`, `agent_citation_labels`,
`schemas.py`, plus všetky existujúce testy. **To je daň delegovania** — ja ten
kontext nesiem ďalej, agent ho musí zakaždým znovu nadobudnúť.

**Zadania musia byť obrovské.** Keďže agent nič nevie, brief má 1 500–2 500 slov
a musí obsahovať aj pasce, čísla z meraní a citácie z dizajnu. Napísať taký brief
trvá dlhšie než urobiť malú úlohu priamo.

**Všetko overujem znovu.** A správne — dnes to **dvakrát zmenilo záver**
(sondovací offset bol reálny nález, hoci ho agent zamietol; `ruff` regresia
naopak neexistovala). Ale znamená to, že tú istú vec vidím dvakrát.

**Kolo na úlohu nie je jedno.** Reálny cyklus bol: implementácia → review → oprava
→ moje overenie → niekedy druhá oprava. Pri štyroch úlohách to je **12+ behov
agentov**, nie štyri.

**Réžia, ktorá s prácou nesúvisí.** Idle slučka (26 zbytočných hlásení),
oneskorené notifikácie, `TaskStop` po každom agentovi.

**A jednorazovo: strata 6 hodín práce.** To nie je vlastnosť postupu, ale
zaplatilo sa to v tom istom dni.

Zhrnuté: **pomalé nie je generovanie, ale opakované nadobúdanie kontextu
a dvojité overovanie.**

---

## 7. Treba to vôbec? Nezvládne to Claude Code sám?

Poctivá odpoveď: **väčšinu áno, a časť tejto réžie bola zbytočná.**

**Čo by bolo rýchlejšie priamo.** T2 (`skills_v3.py`) a T3 (`factual_tokens_v3.py`)
sú jednotlivé nové moduly s jasným zadaním. Napísať ich priamo — s kontextom,
ktorý už mám — by bolo takmer isto rýchlejšie než napísať brief, počkať na
agenta, prečítať report a overiť ho. To isté platí pre všetky opravy po review:
nález je známy, oprava je malá, brief je drahší než zmena.

**Čo delegovanie naozaj prinieslo.** Jedinú vec, ale dôležitú: **nezávislú
review**. Sedem `must_fix` nálezov našli agenti, ktorí kód nepísali a mali
zadanie hľadať konkrétne triedy chýb mutáciami. Vlastnú prácu si človek ani
model kontroluje horšie — a dnešný vzor (mechanizmus napísaný, nezapojený,
testy zelené) je presne ten, ktorý autor prehliadne, lebo *vie, ako to malo
fungovať*.

Druhá, menšia: **paralelizmus** pri troch nezávislých review naraz.

**Čo bolo len réžia.** Workflow registrácia, phase guards, delegation guards —
to sú projektové pravidlá z `CLAUDE.md` (*„FORBIDDEN: Delegating to delivery
agents without an active workflow"*), nie moja voľba. Dávajú audit stopu, ale
k správnosti kódu dnes neprispeli.

### Praktický záver

| kedy delegovať | kedy robiť priamo |
|---|---|
| **review** — vždy, a s mutačným zadaním | jednotlivý nový modul |
| práca vo viacerých nezávislých vetvách naraz | oprava známeho nálezu |
| úloha, ktorá vyžaduje prečítať 15 súborov, ktoré ja čítať nepotrebujem | čokoľvek, kde brief bude dlhší než zmena |

Inak povedané: **delegovať kontrolu, nie písanie.** Dnes to bolo opačne a to je
hlavný dôvod, prečo to trvalo deväť hodín.

---

## 8. Odporúčania

**Pre zadania**
- Nedávaj verifikačnú bránu, ktorú nemožno splniť uprostred atomickej trojice.
  Baseline platí **na konci** práce, medzistav smie byť červený.
- Zapracuj známe nálezy **vopred**. Pri obnove T4 sú všetky nálezy z review
  priamo v zadaní vrátane zdôvodnení a nameraných čísel — nepíše sa naivná
  verzia a nečaká sa na review.
- Vyžaduj **mutačný dôkaz**, nie tvrdenie. „Test prešiel" nie je dôkaz, že test
  niečo stráži.
- Menuj presný rozsah baseline príkazu — inak agent zmeria niečo iné a rozdiel
  pripíše cudzej práci.

**Pre overovanie**
- Nespoliehaj sa na report. Dnes som **každý** kľúčový nález overil sám a dvakrát
  to zmenilo záver (sondovací offset bol reálny; `ruff` regresia nebola).
- `tests/governance` a `tests/integration` sa do „všetko zelené" nerátajú —
  prvé treba spustiť menovite, druhé sa bez DSN ticho **preskočia**.

**Pre orchestráciu**
- Zastav agenta hneď po doručení reportu, kým sa hook neopraví.
- Nepúšťaj viac agentov naraz nad tie isté súbory. Dnes jeden omylom zmazal
  dočasný súbor druhého (neškodné, ale zbytočné).
- Priebežné WIP commity po každej dokončenej úlohe.

**Čo sa neosvedčilo**
- Absolútny zákaz git operácií. Chránil pred menším problémom a spôsobil väčší.
