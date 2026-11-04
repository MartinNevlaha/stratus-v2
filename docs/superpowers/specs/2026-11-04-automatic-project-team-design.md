# Automatická spolupráca tímu spúšťaného cez Stratus

Dátum: 2026-11-04. Stav: schválené a implementované na vetve `codex/automatic-project-team`; záverečné overenie je v implementačnom pláne.

## Zámer a rozhodnutia používateľa

Používateľ nechce ručne premenovávať agentov, vyhľadávať príjemcov, zadávať
príkazy na čakanie ani prenášať správy medzi oknami. Výslovne vybral:

1. Automatické dohadovanie práce a odovzdávanie výsledkov v rovnakom projekte.
2. Prebúdzanie nečinných chatov, keď majú dostať súvisiacu prácu alebo odpoveď.
3. Spúšťanie spolupracujúceho tímu cez Stratus.
4. Zachovanie postupov `spec`, `spec-complex`, `bug` a `e2e` vrátane rolí,
   fáz, kontrol a prideľovania práce podľa zložitosti.
5. Výslovné využitie GPT Astra aj v Codexe, najmä pri návrhoch a reviews.
   Hlasový prepis „GPT Aster“ sa tu interpretuje ako GPT-6 Astra, ktorý je
   uvedený v lokálnom katalógu Codexu. Konkrétne predvolené úrovne nižšie sú
   schválená tímová šablóna; nejde o zmenu globálnych nastavení.
6. Nízka tokenová réžia celej spolupráce; automatizácia nemá znamenať
   neustále rozhovory agentov ani najdrahší model pri každom kroku.

## Zvažované prístupy

- Lepšie pokyny a prehľad existujúcich agentov: malá zmena, ale nezabezpečí
  prebudenie nečinných relácií. Používateľ požaduje širší rozsah.
- Pripojenie samostatne otvorených aplikácií: zachová dnešné okná, no budenie
  závisí od zapnutých kanálov a dostupných natívnych spojení. Pre ľubovoľný
  existujúci Codex desktop chat sa dostupnosť spojenia nepotvrdila.
- **Tím spustený cez Stratus — používateľom vybraný prístup:** Stratus vlastní
  spojenia a životný cyklus tímových relácií; podporované natívne rozhrania
  zabezpečia vstup, výsledky, stav a schvaľovanie. Nevytvára sa ďalší AI model
  iba na preposielanie správ.

## Používateľský priebeh

V projektovom dashboarde pribudne zobrazenie **Tím**. Používateľ vyberie
dostupné nástroje a stlačí **Spustiť tím**. Odporúčaný predvolený výber je
Claude + Codex, iba ak sú oba pripravené; OpenCode možno pridať. Ide o návrh
predvoleného nastavenia, nie o používateľom vybranú zostavu.

Používateľ zadá bežnú úlohu jednému z členov. Ostatní vidia stručný popis
jeho práce. Ak sa práca týka ich zadania, dohodnú si rozdelenie, vyžiadajú
potrebné informácie a odovzdajú výsledok. Príjemca čakajúci bez aktívneho
výpočtu sa prebudí v tej istej konverzácii. Meno príjemcu a interné ID rieši
systém. Pomenovanie rolí zostane voliteľné.

Pri úlohe bude viditeľná aj odborná rola, zvolený model a úroveň uvažovania.
Prehľad rozozná požadované nastavenie od nastavenia potvrdeného natívnym
nástrojom; ak runtime skutočný model neoznamuje, nebude ho vydávať za overený.

Rozhranie ukazuje pri každom členovi jeho nástroj, aktuálnu úlohu a skutočný
stav: pripravený, pracuje, čaká na kolegu, potrebuje používateľa alebo chyba.
Používateľ môže otvoriť konverzáciu, poslať vlastný pokyn a skontrolovať
spoločné úlohy. Výsledky a otázky sa zobrazujú v dashboarde. Ovládanie obsahuje
**Pozastaviť** a **Zastaviť tím**. Pozastavenie zakáže nové automatické vstupy
a zachová frontu; aktívny turn môže dobehnúť. Zastavenie preruší bežiacu prácu
podporovaným natívnym spôsobom a ukončí len procesy vlastnené týmom.

Zatvorenie prehliadača tím nezastaví. Po reštarte servera ostane tím
pozastavený, kým používateľ nepotvrdí pokračovanie; nevzniknú duplicitní
pracovníci. Dokončenie zadania nechá agentov nečinných bez periodických
modelových volaní. Vyčerpanie limitu alebo potreba schválenia je viditeľný
stav, nie dôvod na automatické opakovanie do nekonečna.

## Hranice projektu a práce

Projekt má stabilnú identitu odvodenú od kanonického koreňa konfigurácie.
Overené Git worktrees toho istého checkoutu patria do rovnakého projektu;
nesúvisiace klony sa nezlúčia podľa názvu priečinka ani remote URL. Registrácia
uchová aj skutočný pracovný adresár konkrétneho agenta.

Server odvodzuje rozsah zo session identity. Vyhľadávanie kolegov, broadcast,
priame správy a zdieľané úlohy sú pre automatickú spoluprácu obmedzené na
rovnaký projekt. Staré relácie bez overenej projektovej identity sa automaticky
nepriradia. Existujúca história zostane zachovaná. Dashboard môže mať prehľad
viacerých projektov, ale pri ovládaní tímu musí byť cieľový projekt jednoznačný.

Aktuálna práca sa vezme z workflow vlastneného danou session, ak existuje.
Pri práci bez workflow agent na začiatku a pri zmene úlohy aktualizuje stručný
popis cez nástroj. Názov posledného shell príkazu sa nevydáva za opis zadania.
Priebeh sa posiela ostatným pri podstatnej zmene, nie po každom nástroji.

Automatické dohadovanie platí len v rámci úloh zadaných používateľom.
Vlastníctvo zdieľanej úlohy sa priraďuje atomicky; konfliktné úpravy agenti
dohodnú pred prácou. Táto koordinácia nie je globálny zámok súborového systému.
Žiadny agent nemá na základe správy kolegu rozširovať zadanie, nasadzovať,
meniť oprávnenia alebo schvaľovať požiadavky namiesto používateľa.

## Zachovanie postupov spec, spec-complex, bug a e2e

Tím je spôsob vykonávania existujúceho workflow. Nevytvára samostatný workflow
pre každého kolegu a nemení existujúci graf fáz:

- `spec`: plan → implement → verify → learn → complete.
- `spec-complex`: plan → discovery → design → governance → plan → implement
  → verify → learn → complete; schválenie návrhu/plánu ostáva v existujúcom
  postupe, nepridáva sa duplicitná schvaľovacia vrstva tímu.
- `bug`: analyze → fix → review → complete, s návratom review → fix.
- `e2e`: setup → plan → generate → heal → complete, s podporovaným návratom
  heal → generate. Pri spec zostáva podporovaný návrat verify → implement.

Jeden vedúci agent ostáva vlastníkom workflow a vykonáva jeho prechody.
Delegovaná práca má explicitné workflow ID, task ID, rolu, očakávaný výstup
a revíziu podkladov. Ďalší člen môže patriť inému nástroju, ale dostane len
oprávnenie k pridelenej úlohe. Existujúce kontroly vlastníctva sa neodstránia:
pribudne kontrolovaná väzba delegovanej úlohy na konkrétnu cudziu session.
Prijatie výsledku nemení vlastníka workflow ani automaticky neposúva fázu.

Review posudzuje aktuálnu revíziu implementácie; po oprave sa znovu overia
dotknuté zistenia. Historický PASS ani ukončenie turnu nestačí na dokončenie.
Pri `spec-complex` sa kvôli úspore nevynechá discovery, architektúra,
governance ani záverečné overenie. Schválené výstupy týchto fáz sa opakovane
použijú ako podklady a neposudzujú od nuly pri každom odovzdaní práce.

## Výber rolí, modelov a úrovne uvažovania

Vedúci určí odbornosť, náročnosť a riziko úlohy. Správca tímu tieto požiadavky
preloží cez projektovú politiku na konkrétny runtime, profil, model a effort.
Samotný názov nástroja (Claude/Codex/OpenCode) nestačí ako vykonávací profil.
Pravidlo sa rozlíši pred spustením úlohy a uloží sa jeho dôvod aj použitá verzia.

Poradie nastavení: výslovná voľba používateľa pre úlohu → projektový profil
roly/modelov → zvolená tímová šablóna. Všeobecný model používateľa sa použije
len tam, kde politika neurčuje konkrétnu voľbu, typicky pre vedúceho tímu.
Model nesmie omylom zdediť všetka delegovaná práca iba preto, že ho používa
vedúci. Explicitnú voľbu nemožno potichu nahradiť lacnejším modelom.

Existujúce Claude profily a ich projektové úpravy sa zachovajú vrátane
nastavených modelov a úsilia. Rovnako sa zachovajú OpenCode overrides.
Ich modelové aliasy sa neprekladajú automaticky na názvy modelov Codexu.
Codex zatiaľ nemá v kontrolovanom prostredí pevné projektové roly s modelmi;
pre jeho vetvu tímu sa preto doplní samostatná politika. Jej návrh:

| Úloha v Codexe | Model | Effort |
|---|---|---|
| Návrh, architektúra a plán zmeny | `gpt-6-astra` | `high`; pre spec-complex alebo zložité rozhodnutie `xhigh` |
| Zložitá diagnostika alebo rozhodnutie s vysokým rizikom | `gpt-6-astra` | `high` |
| Nezávislá záverečná kontrola spec/spec-complex/bug | `gpt-6-astra` | `high`; pri vysokej zložitosti `xhigh` |
| Bežná implementácia a integrácia podľa schváleného plánu | `gpt-6.1-sol` | `high` |
| Mechanická, presne ohraničená nízkoriziková úprava | `gpt-6-luna` | `medium` |

Používateľ požaduje Astru najmä pre návrhy a kontroly; voľby Sol/Luna
a jednotlivé effort úrovne sú navrhnuté predvolené hodnoty na úsporné
vykonávanie. Tímová šablóna ich ukáže a umožní zmeniť pred spustením.
Táto tabuľka nemení globálne nastavenia bežných Codex chatov.

Pri štarte a pred dispatchom sa overí dostupnosť konkrétneho modelu, effort
a potrebných nástrojov v cieľovom runtime. Lokálny modelový cache je podklad,
nie záruka práva na model v každom účte. Ak voľba nie je dostupná alebo
prekračuje nastavený limit, úloha dostane stav potrebuje používateľa;
prípadnú náhradu musí povoľovať výslovná projektová politika alebo používateľ.

Natívny subagent sa môže použiť, ak adaptér vie explicitne odovzdať požadovaný
model a effort a spojiť výsledok s task ID. Inak správca použije vlastnenú
pracovnú session so správnym profilom. Opätovné využitie pracovníka rešpektuje
projekt, workflow a profil; zmena modelu nesmie potichu meniť nesúvisiacu prácu.
Kontrolór dostane samostatný kontext s podkladmi na kontrolu, nie len záver
implementátora. Predvolené opakované drobné opravy idú implementátorovi;
Astra znovu posudzuje dotknuté zmeny a záverečný výsledok.

## Správa natívnych relácií

Go server dostane správcu tímov a adaptéry s rovnakým kontraktom: kontrola
pripravenosti, spustenie/obnova, pridanie vstupu, sledovanie stavu a výstupu,
spracovanie žiadostí o schválenie, prerušenie a ukončenie. Databáza uchová
väzbu projekt → tím → člen → natívna session. Jeden aktívny správca vlastní
jednu session; Stratus nepreberá ľubovoľný už bežiaci proces.

- **Codex:** Stratusom vlastnený app-server a dokumentované thread/turn API.
  Zachová sa natívne thread ID a pravidlá schvaľovania. Model a effort každej
  pracovnej session/turnu sú explicitne vyriešené podľa politiky rolí vyššie.
  Nejde o pripojenie druhého procesu k otvorenému desktop chatu. Lokálny
  `codex queue --resume` existuje, ale predvolený daemon tu nebeží; samotný
  úspešný exit queue príkazu nie je dôkaz prebudenia.
- **OpenCode:** Stratusom vlastnený lokálny server a jeho verejné session API.
  Prebúdzanie používa vstup do konkrétnej session; stav a výsledok sa sledujú
  cez udalosti. HTTP 204 znamená prijatie, nie dokončenie práce.
- **Claude:** riadená dlhodobá session cez podporované streaming rozhranie
  Claude Agent SDK/CLI. Implementácia musí najprv potvrdiť viac následných
  turnov v tom istom procese/session a tok používateľských schválení.
  Vlastný interaktívny Channels režim nie je predvolený runner: je research
  preview a vyžaduje osobitnú aktiváciu. Presná forma malého SDK pomocníka
  oproti priamemu CLI adaptéru je implementačné rozhodnutie po overení
  podporovaného kontraktu; prípadná nová runtime závislosť musí byť priznaná.

Použijú sa existujúce prihlásenia a modely z vyriešenej politiky. Spustenie tímu urobí
kontrolu pripravenosti a ukáže konkrétny chýbajúci krok. Nepoužíva sa vkladanie
kláves do cudzích terminálov, úprava súborov histórie, obchádzanie trust
mechanizmov ani plošné povoľovanie nástrojov.

## Správy, budenie a obmedzenie slučiek

Rozšíri sa existujúca comms databáza a nástroje. Správa sa najprv trvalo uloží
s adresátom a projektom. Budenie je samostatná operácia viazaná na túto správu;
úspešné odoslanie notifikácie nesmie samo vymazať inbox. Potvrdenie prijatia
natívnym hostom a spracovanie správy sú odlišné stavy.

Na session beží najviac jeden turn. Správy pre pracujúceho agenta sa spoja
do ďalšieho vstupu na bezpečnej hranici. Krátke súvisiace správy sa zlúčia.
Trvalá fronta a identifikátory správ umožnia obnovu po páde; doručenie môže byť
opakované, preto potvrdenia a zmeny úloh musia byť idempotentné. Pri neurčitom
výsledku odoslania sa najprv zosúladí natívna história/stav, až potom opakuje
vstup. Presné schémy, rezervácie a retry intervaly ostávajú implementátorovi.

Vstup do modelu výslovne označí správu od kolegu ako externý pracovný podklad,
nie nový pokyn používateľa. Samotné potvrdenie „OK“, pripojenie kolegu alebo
nezmenený stav neprebúdza model. Návrh predvoleného limitu: po štyroch
automatických nadväzujúcich kolách bez preukázateľného posunu konkrétnej úlohy
sa daná výmena pozastaví a zobrazí používateľovi. Vyhodnotenie posunu musí
vychádzať z úloh/výsledkov, nie iba z počtu správ. Jeden projektový prepínač
vypne automatickú spoluprácu bez vymazania histórie.

## Úsporná prevádzka

- Registrácia, smerovanie, sledovanie stavu, budenie a výber z už určeného
  profilu sú deterministické operácie servera, bez samostatného LLM volania.
  Výber odbornosti/náročnosti je súčasťou existujúceho plánovania, nie nový
  modelový rozhovor pre každú správu.
- Nečinný agent nevolá model ani neudržiava nekonečnú slučku modelových
  `message_wait` volaní. Čakanie rieši server a natívny transport.
- Žiadne plošné posielanie každej aktualizácie všetkým členom. Prebúdza sa
  iba adresát, ktorý môže vykonať prácu alebo ktorému odpoveď odblokuje úlohu.
- Odovzdanie obsahuje cieľ, rozsah, relevantné rozhodnutia, dotknuté súbory,
  kritériá dokončenia a odkazy na artefakty. Celé histórie kolegov sa nekopírujú.
  Potrebné doplňujúce podklady sa načítajú cielene; skrátenie sa nesmie tváriť
  ako úplný obsah a nesmie vynechať podmienku úspechu či oprávnení.
- Projektový prehľad sa pri bežnej aktualizácii posiela ako zmena. Výstupy
  návrhu, plánu a analýzy sa ukladajú a znovu používajú. Zhrnutie pre odovzdanie
  vzniká v bežnom výsledku agenta, bez osobitného súhrnného modelového volania.
- Existujúci pracovník pokračuje v súvisiacej úlohe, ak má vhodný profil;
  nezačína sa nový kontext pri každom kroku. Nezávislá kontrola a oddelenie
  nesúvisiacich úloh majú prednosť pred opätovným použitím kontextu.
- Záverečná kontrola dostane relevantný diff, schválené požiadavky a dôkazy
  overenia. Po oprave sa posiela zmena oproti kontrolovanej revízii a otvorené
  zistenia; ďalšie súvislosti musí mať kontrolór dostupné na vyžiadanie.
- Tím má nastaviteľné limity paralelných behov, automatických kôl a výmen bez
  posunu. Pri vyčerpaní sa pozastaví s uloženým stavom. Nastavené tokenové alebo
  finančné limity sa vyhodnocujú z dostupnej natívnej telemetrie; tam, kde
  údaje chýbajú, UI ukáže neznámu spotrebu a nepredstiera tvrdý tokenový limit.
  Bez merania ostávajú vynútiteľné limity počtu spustení a súbežnosti.
- Dashboard ukáže spotrebu podľa workflow, roly a modelu, ak ju host poskytuje,
  a osobitne počet automatických prebudení. Nepredkladá vymyslené percento
  úspory. Pred nasadením sa porovná rovnaká reprezentatívna úloha s pôvodným
  postupom vrátane opakovaných opráv, aby sa nižšia réžia nezamieňala za
  vynechanú kontrolu alebo neúplný výsledok.

## Overenie a uvedenie do prevádzky

Pred zapnutím sa overí:

1. Rovnaký projekt sa rozpozná z podadresára a platného worktree; cudzie
   projekty s rovnakým názvom sa nikdy nezmiešajú.
2. Nový člen vidí prácu existujúcich členov a naopak; nemenný kontext sa
   neopakuje pri každom nástroji.
3. Dvaja členovia nemôžu súčasne prevziať jednu zdieľanú úlohu.
4. Správa pre idle session skutočne spustí ďalší turn s rovnakou natívnou
   identitou a zachovaným kontextom pre každý podporovaný runtime.
5. Busy session sa nespustí druhýkrát; paralelný používateľský vstup,
   schvaľovanie, prerušenie a retry zachovajú poradie a prioritu používateľa.
6. Reštart, výpadok transportu a duplicitná udalosť nestratia úlohu ani
   nespôsobia duplicitné dokončenie. Pozastavenie a zastavenie ukončia budenie.
7. Stav bez posunu zastaví ping-pong a nevyvoláva pravidelné modelové volania.
8. `spec`, `spec-complex`, `bug` a `e2e` zachovajú pôvodné prechody aj brány;
   kolega v inom runtime môže vykonať pridelenú úlohu, ale nemôže prevziať
   workflow, preskočiť fázu alebo dokončiť cudziu úlohu. Starý review výsledok
   nesmie schváliť novšiu implementáciu.
9. Dispatch explicitne používa model a effort z politiky, zachová projektové
   overrides a nezmení Astru na predvolený model. Nedostupný model zablokuje
   danú úlohu bez tichej náhrady. Požadovaný model sa nezamieňa za overený.
10. Prázdny inbox, nezmenená prítomnosť a potvrdenie bez akcie spôsobia nula
    modelových prebudení; opakované oznámenia sa zlúčia. Odovzdanie neposiela
    plné histórie a opakovaná kontrola používa správnu základnú revíziu.
11. Limity sa vynútia aj pri reštarte a súbežných správach; chýbajúce údaje
    o tokenoch nie sú nula. Meranie zahŕňa vedúceho, pracovníkov aj kontroly.

Najprv sa použijú kontraktové testy s falošnými transportmi, potom ohraničené
živé overenie v samostatnom testovacom projekte. Existujúce UVO relácie sa
nepoužijú ako testovací materiál. Aplikácia označí runtime ako podporovaný
až po overení jeho toku vrátane schválení a obnovy. Táto fáza je návrh;
inštalácia novej verzie servera a spustenie živého tímu zatiaľ neprebehli.

## Primárne podklady

- Existujúce `db/comms.go`, `api/routes_comms.go`, `hooks/comms.go`,
  `integrations/codex/`, `cmd/stratus/plugins-opencode/stratus.ts` a ich testy.
- [Codex app-server](https://learn.chatgpt.com/docs/app-server)
  a [queue CLI](https://learn.chatgpt.com/docs/cli/reference).
- [Výber modelov](https://learn.chatgpt.com/docs/model-selection)
  a [GPT-6 Astra](https://developers.openai.com/api/docs/models/gpt-6-astra).
  Presné rozdelenie rolí vyššie je návrh Stratusu podľa požiadavky používateľa,
  nie hotová politika prevzatá z dokumentácie modelov.
- [OpenCode Server](https://opencode.ai/docs/server/),
  [SDK](https://opencode.ai/docs/sdk/) a [pluginy](https://opencode.ai/docs/plugins/).
- [Claude streaming input](https://code.claude.com/docs/en/agent-sdk/streaming-vs-single-mode)
  a [Channels reference](https://code.claude.com/docs/en/channels-reference).
