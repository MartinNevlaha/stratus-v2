# Tímy spúšťané cez Stratus

V dashboarde otvor **Tím**. Stratus overí dostupnosť Codexu, Claude Code a OpenCode
bez zadania práce modelu. Skontroluj modely pri rolách, nastav limity a zvoľ
**Spustiť tím**. Potom napíš zadanie vedúcemu tímu. Pracovníci sa spúšťajú až keď
majú prácu; výsledok zobudí čakajúceho príjemcu v jeho pôvodnej konverzácii.

Predvolená šablóna používa Claude na vedenie, ak je dostupný spolu s Codexom.
Codex používa GPT-6.1 Sol/high na implementáciu, GPT-6 Astra/high na návrh,
diagnostiku a nezávislú kontrolu, a GPT-6 Luna/medium na jednoduché úpravy.
Pri zložitých úlohách a spec-complex sa Astra prepne na xhigh cez samostatný
profil. Každú rolu možno pred spustením zmeniť. Žiadne globálne nastavenie modelu
sa nemení a nedostupný model sa potichu nenahrádza.

Tím preberá projektové modely z `.claude/agents` a `opencode.json`.
Voliteľné `.stratus.json` → `managed_team.profiles` môže nastaviť profil podľa
nástroja a roly (`lead`, `implementation`, `design`, `review`, `diagnosis`,
`mechanical`). Hodnota je objekt `model` a `effort`; runtime je názov nadradeného
kľúča (`codex`, `claude-code`, `opencode`). Pre OpenCode zvoľ explicitný model
vo formáte `provider/model` a jeho podporovaný variant.

Fázy spec, spec-complex, bug a e2e ostávajú pôvodné. Vedúci vlastní workflow,
pracovníci dostávajú ohraničené úlohy. Výsledok nemení fázu ani automaticky
neschvaľuje oprávnenia. Kontrola sa viaže na konkrétnu revíziu práce; pri zmene
podkladov sa staré schválenie nepoužije na novú verziu.

**Pozastaviť** zastaví odosielanie ďalších zadaní, rozpracovaný výpočet môže
dobehnúť. **Zastaviť** preruší a ukončí len relácie spustené týmto tímom.
Zatvorenie dashboardu tím nezastaví. Po reštarte servera je tím pozastavený;
**Pokračovať** obnoví jednoznačne čakajúcu prácu. Nejasný výsledok odoslania
sa automaticky neopakuje: najprv skontroluj výsledok, pri členovi zvoľ **Obnoviť po kontrole** a potom
zadaj nový pokyn. Ak je celý tím pozastavený, použi aj **Pokračovať**.
Natívne požiadavky na oprávnenia sa zobrazia pri členovi na tvoje rozhodnutie.

## Pripojenie existujúceho Codex chatu

V pôvodnom Codex desktop chate povedz **„Pripoj tento chat k tímu Stratus.“**
Ak projekt ešte nemá tím, vytvorí sa s týmto chatom ako vedúcim. Ak už tím
existuje, chat sa pridá ako člen. Pri viacerých tímoch si vyberieš konkrétny.
Vedúceho možno zmeniť v dashboarde cez **Viesť tím z tohto chatu**, keď tomu
nebráni rozpracovaná práca alebo vlastníctvo workflow.

Ďalej zadávaš prácu v tom istom chate. Člen, ktorý nie je vedúcim, odovzdá
tímové zadanie vedúcemu a konečný výsledok dostane späť. Keď vedúci čaká na
pracovníkov, priebežný stav čakania zadávateľa neprebúdza. Nadväznosť sa zachová
aj pri viacerých pracovníkoch a ďalšom delegovaní. Pripojený chat si zachová
históriu, model aj úroveň uvažovania. Ak host model neoznámi, dashboard ho
označí ako neznámy; vhodný profil pre delegovanú prácu zabezpečia pracovníci.

Pripojenie vyžaduje túto verziu Stratusu, projektové MCP a pôvodný konektor
aplikácie Codex. Po aktualizácii binárky obnov projektové nastavenie pomocou
`stratus refresh --target codex` a nechaj Codex znovu načítať MCP nastavenie.
Ak Codex ešte nie je v projekte nastavený, použi `stratus init --target codex`.
Projekt musí byť dôveryhodný a jeho Stratus MCP povolené. Pripojenie overí
skutočnú identitu chatu a cestu projektu; chýbajúce údaje oznámi ako chybu.
Ručne zadané ID chatu ich nenahrádza. Samotný Codex CLI bez desktop konektora
tento spôsob pripojenia neposkytuje.

Správa kolegu môže pri súbehu prísť do práve začatej používateľskej odpovede.
Agent potvrdí konkrétnu úlohu a odovzdá jej výsledok; samotné skončenie
odpovede sa za splnenie úlohy nepovažuje. Oprávnenia naďalej schvaľuješ
v pôvodnej aplikácii. Čakanie na tvoje rozhodnutie automatika nenahrádza.

**Odpojiť od tímu** alebo „odpoj tento chat od tímu“ zastaví ďalšie tímové
vstupy a ponechá pôvodný chat otvorený. Pri výpadku alebo reštarte povedz
v pôvodnom chate znovu „pripoj tento chat k tímu“. Nejasne doručené zadanie
sa automaticky neopakuje; najprv skontroluj jeho výsledok. Ak je tím
pozastavený, následne použi **Pokračovať**. Bežná ručná práca v chate môže
pokračovať aj pri pozastavenom alebo odpojenom tíme.

Pripojenie už existujúcich chatov Claude Code a OpenCode zatiaľ nie je
dostupné: pri Claude zostáva obmedzenie životnosti čakajúceho spojenia,
pri OpenCode test fronty nepotvrdil vykonanie a výsledok druhého vstupu.
Nové relácie oboch nástrojov spúšťané Stratusom fungujú samostatne.

## Spotreba

Stratus nevolá ďalší model na smerovanie správ ani na čakanie. Prázdna fronta,
prítomnosť kolegu a jednoduché potvrdenia nespúšťajú model. Súvisiace pripravené
správy sa zlúčia do jedného prebudenia; odovzdávajú sa stručné výsledky a odkazy
na uložené podklady. Plné zadanie ostáva dostupné cez `team_status(task_id)`.

Platí limit súbehu, počet tímových vstupov a automatických kôl bez pokroku. Ak nástroj
vracia spotrebu, zobrazuje sa aj počet tokenov a možno nastaviť jeho limit.
Pri neznámej spotrebe platí počet tímových vstupov; údaj **neznáma** neznamená nulu.
Ručné vstupy do pripojeného chatu sa do tímového limitu nezapočítavajú.
Kontrola limitu pred novým spustením nemôže zastaviť tokeny už rozbehnutého
natívneho výpočtu presne na hranici. Cenu služby ani presné percento úspory
Stratus neodhaduje z neúplných dát. Natívne nástroje stále načítavajú vlastné
projektové pokyny a históriu.

## Rozsah a overenie

Tím vlastní nové natívne relácie. Existujúci Codex desktop chat sa pripája
osobitne a jeho aplikáciu ani používateľský výpočet tím nevlastní.
Projektová identita vychádza z kanonickej cesty; overené Git worktrees patria
k rovnakému projektu, nezávislé klony ani rovnaké názvy priečinkov nie.
Aktuálna binárka Stratus sa členom odovzdá ako dočasné nastavenie MCP, aby
používali zodpovedajúce nástroje aj pri staršej projektovej konfigurácii.

Dňa 2026-11-04 prešli izolované živé dvojťahové testy všetkých troch nástrojov:
Codex 0.166.0-alpha.5.1, Claude Code 2.1.318, OpenCode 1.18.29. Druhý vstup
zachoval natívne ID aj informáciu z prvého vstupu. Oprávnenia, zlyhania,
obnova fronty, opakované požiadavky a súbeh sú overené protokolovými a
integračnými testami. Živé testy používali len šesť krátkych vstupov v nových
prázdnych projektoch; existujúce relácie neboli použité. Tieto testy nedokazujú
percentuálnu úsporu na reálnych vývojových úlohách.

Pripojenie Codex desktop chatu má samostatný živý test: po skončení pôvodnej
odpovede jedna povolená automatická správa prebudila ten istý chat, ktorý
správne vybavil značku z predchádzajúceho kontextu. Tento test overuje natívne
doručenie; celé prepojenie API, MCP, potvrdení a výsledkov overujú integračné
testy. Podrobnosti a hranice podpory sú v
[zázname overenia pripojení](../testing/existing-chat-attachments.md).

## Kontrola návrhu iným členom tímu

Pri zakladaní tímu možno voliteľne zapnúť kontrolu návrhu (`design_review` v
požiadavke na vytvorenie tímu: `enabled`, `reviewer_member_id`, `max_rounds`).
Recenzentom môže byť ktorýkoľvek člen okrem vedúceho (ten vlastní workflow) a
okrem pripojeného natívneho chatu; `max_rounds` je 1 až 10 (predvolene 3).
Bez tejto voľby sa nič nemení. Neplatné hodnoty sa odmietnu aj pri `enabled: false`.

Kontrola stráži len prechody, ktoré robí vedúci cez `transition_phase`:

| Workflow | Stráženy prechod | Kontrolované dokumenty |
|----------|------------------|------------------------|
| spec-complex | `design` → `governance` | `docs/plans/<workflow-id>-design.md` a každý súbor v `docs/adr/`, ktorý je od základného commitu workflowu nový alebo zmenený |
| spec-complex | `plan` → `implement` a `plan` → `accept` | rovnaké dokumenty návrhu: stačí PASS (alebo schválenie používateľom) pre ich aktuálny obsah, takže jeden PASS pokryje `governance` aj tento prechod; chýbajúci návrh prechod zablokuje |
| spec (jednoduchý) | `plan` → `implement` a `plan` → `accept` | `docs/plans/<workflow-id>-plan.md` |

Prechody späť (`design` → `plan`), zrušenie workflowu a prechody používateľa z
dashboardu (bez tokenu tímu) kontrola nikdy neblokuje; práve dashboard je cesta von,
keď kontrola uviazne. Telo požiadavky na zmenu fázy s opakovaným kľúčom (aj s
inou veľkosťou písmen, napr. `phase` a `Phase`) sa odmietne s 400.

Priebeh: prvý pokus o stráženy prechod vráti 409 `design_review_required` a
Stratus sám zadá recenzentovi úlohu (číta dokumenty podľa absolútnych ciest;
obsah sa do úlohy nevkladá). Recenzia sa viaže na presný obsah dokumentov (hash):
akákoľvek zmena kontrolovaných súborov znamená novú recenziu, nesúvisiace zmeny
inde v strome ju neovplyvnia. Výsledok musí obsahovať celý riadok
`Verdict: PASS` alebo `Verdict: FAIL`; odpoveď bez neho sa nikdy nepočíta ako PASS.
Každé kolo ide tomu istému zvolenému recenzentovi.

- **PASS** pre aktuálny obsah: vedúci môže znova zavolať `transition_phase`.
  Server fázu sám nikdy nemení.
- **FAIL**: pripomienky recenzenta automaticky dostane autor dokumentov (člen,
  ktorý naposledy dokončil úlohu roly návrh/architekt) ako úlohu na prepracovanie.
  Po jej dokončení sa so zmenenými dokumentmi automaticky začne ďalšie kolo. Pri
  jednoduchom spec je plán po prepracovaní zmenený, preto ho vedúci znova predloží
  používateľovi na schválenie. Recenzent nesmie byť autor (409
  `design_review_conflict`). Ak autora nemožno určiť, FAIL dostane vedúci a
  dokument prepracuje sám.
- **Kolá** sa počítajú od posledného PASS alebo schválenia používateľom pre
  predchádzajúci obsah: úprava už schválených dokumentov začína novou recenziou od
  kola 1. Po `max_rounds` kolách bez PASS vráti prechod 409
  `design_review_exhausted` a rozhodnúť musí používateľ.
- Chýbajúci alebo nebezpečný dokument (symbolický odkaz mimo `docs/`, súbor nad
  1 MiB, viac ako 20 ADR alebo viac ako 4 MiB spolu, názov súboru s inými znakmi než
  písmená, číslice a `. _ - /`) vráti 409 `design_review_documents_missing` /
  `design_review_documents_invalid`; telo uvádza, že používateľ môže fázu prepnúť z
  dashboardu.
- **Uviaznutá kontrola**: odpoveď 409 aj stav obsahujú `team_state`,
  `reviewer_state`, `stuck` a `reason`. Kontrola je uviaznutá, keď beží recenzia
  alebo prepracovanie, no tím je pozastavený či zastavený, alebo zodpovedný člen
  čaká na rozhodnutie používateľa (`needs_user`), je zastavený či chýba. Vedúci
  vtedy požiada používateľa.

Stav pre workflow vráti `GET /api/workflows/{id}/design-review`. Vyčerpané kolá sa
zobrazia aj vo fronte rozhodnutí. Používateľ môže v dashboarde kontrolu
prepísať (`POST /api/teams/{id}/design-review/override` s `workflow_id` a
`reason`, najviac 300 znakov): záznam platí len pre presný obsah dokumentov a po
ich ďalšej úprave sa kontrola vyžaduje znova. Prepísanie je možné po FAIL, pri
vyčerpaných kolách a pri uviaznutej kontrole; token člena tímu ho nikdy nesmie
vykonať. Každé prepísanie sa zapíše do pamäte projektu ako rozhodnutie
(`design_review_waived`) s dôvodom.

## Projektové kontroly

Odovzdanie (swarm handoff aj odovzdanie izolovaného tímu) hodnotí `checks_ok`
podľa kontrol projektu. Pre projekt s `go.mod` Stratus automaticky spustí
`go build` a `go vet` (záznamy `build` a `vet`). Pre iný projekt si kontroly
pridáte do `.stratus.json` v koreni projektu:

```json
{
  "checks": [
    { "name": "test", "cmd": ["npm", "test"] },
    { "name": "lint", "cmd": ["npm", "run", "lint"], "timeout_sec": 300 }
  ]
}
```

Pravidlá (pri porušení sa kontroly ignorujú, odovzdanie ukáže upozornenie
`invalid checks configuration: …` a použije sa automatická detekcia):

- najviac 8 kontrol; `name` zodpovedá `^[a-z][a-z0-9_-]{0,31}$`, je jedinečný a nie je
  `placeholder_scan` (vyhradený názov);
- `cmd` je argv bez shellu: 1 až 16 prvkov, každý 1 až 256 znakov, bez NUL a nového
  riadku (shell potrebujete len vtedy, keď ho zapíšete sami: `["sh", "-c", "…"]`);
- `timeout_sec` je 0 alebo vynechané (predvolene 120 s) až 900 s; súčet všetkých
  časových limitov (nenastavený sa počíta ako 120 s) je najviac 3600 s;
- `checks_artifacts` je vyhradený názov (rovnako ako `placeholder_scan`).

Príkaz beží v adresári odovzdávaného stromu (integračný worktree, inak koreň
projektu), dedí prostredie servera a po prekročení limitu sa ukončí celá skupina
procesov kontroly, nielen priamy potomok (záznam `<name> TIMED OUT after <n>s`).
Celý beh má vlastný strop: súčet limitov + 30 s; kontroly, ktoré by ho prekročili,
sa zapíšu ako `TIMED OUT (run deadline)`. Príkaz, ktorý skončí, ale nechá bežať proces
na pozadí držiaci jeho výstup, sa zapíše ako zlyhanie (a proces sa ukončí). Do záznamu sa ukladá posledných 16 KiB výstupu bez
riadiacich znakov a so zakrytými tajomstvami. Kontroly sa spúšťajú v uvedenom
poradí; `placeholder_scan` beží vždy.

`checks_ok` platí, len keď každá očakávaná kontrola (`expected_gates` v odpovedi)
prešla na aktuálnej čistej revízii a prešla aspoň jedna kontrola okrem
`placeholder_scan`. Ak nič nie je nastavené a nie je `go.mod`, odovzdanie ukáže
upozornenie a riadok `checks: none configured`, čo nikdy nie je „OK“.

Konfigurácia pochádza z projektového `.stratus.json`, ktorý server načítal pri
štarte, nie zo súboru `.stratus.json` vo worktrees: agenti tam môžu súbory meniť,
preto sa tam nastavené kontroly ignorujú. Po zmene súboru reštartujte server
(konfigurácia sa nenačíta znova za behu). Príkaz s relatívnou cestou
(`./scripts/test.sh`) sa hľadá v odovzdávanom strome, teda spúšťa kód z neho.

**Bezpečnosť.** Každá kontrola spúšťa kód, ktorý napísal agent (testy, skripty,
`package.json`, `Makefile` z odovzdávaného stromu), s plným prostredím servera a
oprávneniami jeho používateľa. Tajomstvá v prostredí servera (kľúče, tokeny) sú
tomuto kódu viditeľné. Nastavujte len kontroly, ktoré chcete spustiť nad kódom
agentov, a nespúšťajte server s oprávneniami či tajomstvami, ktoré tak nechcete
zdieľať.

**Kontrola nesmie zanechať zmeny v pracovnom strome.** Reporty, `dist/` a podobné
výstupy musia byť v `.gitignore`. Ak kontrola po sebe nechá nové alebo zmenené
súbory (HEAD sa nezmení), riadky kontrol ostanú viazané na pôvodnú čistú revíziu,
zapíše sa info riadok `checks_artifacts` (nie je v `expected_gates`) a odovzdanie
ukáže upozornenie `check(s) left files in the working tree: <až 5 ciest>; add them to
.gitignore or make the check clean up`. Strom je potom „špinavý“, a `checks_ok` zostane
`false`, kým súbory nepridáte do `.gitignore` alebo ich neodstránite; Stratus súbory
nikdy nemaže. Ak kontrola posunie HEAD (napr. urobí commit), riadky dostanú revíziu
`+moved` a platiť nebudú.

**Zmeny `.stratus.json` v projekte.** Súbor `.stratus.json`, ktorý sa zlúči do
koreňa projektu, začne platiť až po reštarte servera a treba ho posudzovať ako kód:
`cmd` v ňom sa spustí s vyššie uvedenými oprávneniami.

**Úprava súboru počas behu servera.** Súbor `.stratus.json` upravujte, keď server
nebeží, alebo server hneď po úprave reštartujte: uloženie nastavení z dashboardu
prepíše súbor z konfigurácie v pamäti a môže zahodiť ručné úpravy urobené počas behu.

Spustenie kontrol pre misiu beží najviac raz naraz: ďalšie nastavenie stavu
`verifying`, kým beh trvá, nový beh nespustí (stav sa zmení, beh sa len zaloguje).
