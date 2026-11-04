# Automatická spolupráca tímu spúšťaného cez Stratus

Dátum: 2026-11-04. Stav: návrh na schválenie, zatiaľ bez implementácie.

## Zámer a rozhodnutia používateľa

Používateľ nechce ručne premenovávať agentov, vyhľadávať príjemcov, zadávať
príkazy na čakanie ani prenášať správy medzi oknami. Výslovne vybral:

1. Automatické dohadovanie práce a odovzdávanie výsledkov v rovnakom projekte.
2. Prebúdzanie nečinných chatov, keď majú dostať súvisiacu prácu alebo odpoveď.
3. Spúšťanie spolupracujúceho tímu cez Stratus.

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

## Správa natívnych relácií

Go server dostane správcu tímov a adaptéry s rovnakým kontraktom: kontrola
pripravenosti, spustenie/obnova, pridanie vstupu, sledovanie stavu a výstupu,
spracovanie žiadostí o schválenie, prerušenie a ukončenie. Databáza uchová
väzbu projekt → tím → člen → natívna session. Jeden aktívny správca vlastní
jednu session; Stratus nepreberá ľubovoľný už bežiaci proces.

- **Codex:** Stratusom vlastnený app-server a dokumentované thread/turn API.
  Zachová sa natívne thread ID, aktuálny model a pravidlá schvaľovania.
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

Použijú sa existujúce prihlásenia a zvolené modely. Spustenie tímu urobí
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
- [OpenCode Server](https://opencode.ai/docs/server/),
  [SDK](https://opencode.ai/docs/sdk/) a [pluginy](https://opencode.ai/docs/plugins/).
- [Claude streaming input](https://code.claude.com/docs/en/agent-sdk/streaming-vs-single-mode)
  a [Channels reference](https://code.claude.com/docs/en/channels-reference).
