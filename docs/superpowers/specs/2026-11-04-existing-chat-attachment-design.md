# Pripojenie existujúceho chatu k tímu Stratus

Dátum: 2026-11-04. Stav: používateľ schválil návrh aj preferované natívne
posielanie medzi Codex chatmi; Codex konektor je implementovaný a overený; pripojenie existujúcich chatov
Claude/OpenCode zostáva otvorené. Nadväzuje na
[spravované tímy](2026-11-04-automatic-project-team-design.md).

## Požiadavka a zvolený spôsob

Používateľ chce pripojiť aj existujúci chat a viesť z neho prácu tímu.
Výslovne vybral jednorazové pripojenie: v pôvodnom chate povie
„pripoj tento chat k tímu“, potom už spolupráca pokračuje automaticky.
Následne upresnil, že Codex má využiť podobný spôsob ako existujúce
posielanie správ medzi jeho chatmi. Táto cesta má prednosť pred budovaním
vlastného odlišného spôsobu doručovania.
Zostávajú požiadavky na nízku spotrebu, prebúdzenie nečinného príjemcu,
prideľovanie modelov podľa rolí a zachovanie spec, spec-complex, bug a e2e.

„Ľubovoľný chat“ znamená ľubovoľný používateľom zvolený chat, ktorého
aplikácia poskytuje overiteľné pripojenie. Samotný názov, cesta k histórii
alebo existencia MCP nestačí na automatické budenie. Chat bez potrebného
rozhrania dostane konkrétne vysvetlenie chýbajúceho pripojenia; nevznikne
náhradný chat vydávaný za pôvodný.

Cieľom sú existujúce chaty v Codexe vrátane desktop aplikácie, Claude Code
a OpenCode. Iné aplikácie môžu pridať konektor s rovnakým kontraktom.
Ak sa pripojenie niektorého cieľového hosta nepodarí overiť, vykáže sa
chýbajúca podpora a dôvod; tým sa nesplní celý tento cieľ. Prípadné vydanie
s užším rozsahom sa nesmie označiť za hotové pripojenie ľubovoľného chatu.

Zvažované spôsoby:

- **Jednorazové pripojenie z pôvodného chatu — voľba používateľa.**
  Aplikácia potvrdí identitu a projekt. Zachová sa pôvodný kontext; ďalšie
  správy a výsledky doručuje overené spojenie bez opakovaných ručných krokov.
- Výber zo zoznamu chatov iba v dashboarde: pohodlný tam, kde host už
  poskytuje oprávnený prístup, ale samotný zoznam nepotvrdzuje možnosť
  prebúdzať alebo bezpečne odoslať vstup. Môže byť doplnkovým vstupom po
  jednorazovom pripojení hosta, nie obchádzaním tohto overenia.
- Spustiť nový proces s ID starej relácie: zamietnuté pre pripojenie.
  Nezaručuje doručenie do otvoreného okna a môže vytvoriť dvoch zapisovateľov.
  Existujúci režim nových relácií spúšťaných Stratusom ostáva samostatný.

## Používateľský priebeh a koordinátor

1. Používateľ v existujúcom chate požiada o pripojenie. Spojenie dodá
   natívnu identitu, hosta, projekt a dostupné schopnosti. Stratus overí
   projekt a dostupnosť spojenia; nežiada ručné prepisovanie interných ID.
2. Ak projekt ešte nemá tím, pripojenie ho predvolene vytvorí s týmto chatom
   ako vedúcim a s existujúcou projektovou šablónou pracovníkov. Noví
   pracovníci sa spustia až pri práci. Nástroj umožní vytvorenie vypnúť.
3. Ak tím existuje, chat sa pripojí ako člen. Vedenie sa nepreberie potichu:
   používateľ môže zvoliť „Viesť tím z tohto chatu“, keď tomu nebráni
   rozpracované delegovanie alebo vlastníctvo workflow. Tím má jedného
   jednoznačného vedúceho, bez závislosti od poradia členov v zozname.
4. Používateľ ďalej zadáva úlohy v pôvodnej aplikácii. Vedúci plánuje,
   deleguje a prijíma výsledky; Stratus spravuje frontu, spojenia, limity
   a modelové profily. Preposielanie nepotrebuje ďalší koordinačný model.
5. Výsledok kolegu sa doručí do toho istého chatu. Konektor čaká na voľný
   chat; ak medzitým začne nový používateľský turn, natívne Codex rozhranie
   môže správu pripojiť k nemu bez prerušenia. Stratus rozlišuje pôvodnú
   používateľskú prácu od explicitne potvrdenej tímovej úlohy. Pri schvaľovaní
   alebo otázke pre používateľa sa automatická práca nevloží namiesto jeho
   odpovede. Dashboard aj pôvodný chat ukazujú ten istý tím a rovnaké úlohy.
6. „Odpojiť od tímu“ zastaví ďalšie tímové vstupy a zachová aplikáciu,
   konverzáciu, model a jej históriu. Opätovné pripojenie musí overiť hosta;
   nespustí naslepo staré nejasne doručené úlohy.

Návrhové rozhodnutie pre chat, ktorý nie je vedúcim: nové tímové zadanie
odovzdá súčasnému vedúcemu a výsledok sa vráti pôvodnému zadávateľovi do
tohto chatu. Používateľ nemusí prepínať okná ani preberať vedenie kvôli
jednému zadaniu. Správa uchová overeného pôvodcu a korelačné ID cez celý
tok zadávateľ → vedúci → pracovníci → vedúci → zadávateľ. Logické zadanie
zostáva čakajúce, kým vedúci nespracuje výsledky delegovanej práce. Stručné
odovzdanie čakania nespustí predčasnú odpoveď zadávateľovi; konečná syntéza
sa vráti raz aj pri ďalšom vnorenom delegovaní. Stavové oznámenia
nevytvárajú ďalšie modelové kolá; zúčastnení sa prebúdzajú iba pri potrebnej
práci alebo pripravenom výsledku. Bežná otázka nesúvisiaca s tímovou prácou
sa automaticky nerozpošle tímu.

Vlastný rozpracovaný workflow takého člena sa neprevedie na vedúceho.
Pripojenie ostane členstvom a rozhranie vysvetlí konflikt, ak používateľ
požiada viesť tento workflow cez tím. Nový tímový workflow začína vedúci;
člen nesmie cez preposlanie predstierať oprávnenie iného vlastníka.

Pripojenie nie je povolením na prácu mimo zadania používateľa. Správy kolegov
sú označené pracovné podklady. Oprávnenia na nástroje naďalej schvaľuje
používateľ natívnym spôsobom; člen tímu ich nemôže schváliť za neho.

## Modely, workflow a spotreba

Pripojený chat si zachová model a úroveň uvažovania. Stratus zisťuje ich
aktuálne hodnoty z hosta, neprepisuje ich tímovou šablónou. Neznáme hodnoty
označí ako neznáme. Ak úloha vyžaduje iný profil, vyberie vhodného kolegu
alebo spustí vlastného pracovníka; neprepne potichu používateľov chat.
Platí projektová politika vrátane GPT-6.1 Sol pre implementáciu, Astry pre
návrh a review a zložitejšieho profilu pri spec-complex. Explicitné používateľské
voľby a projektové overrides majú prednosť.

Vedúci ostáva vlastníkom workflow. Existujúci workflow možno naviazať iba
pri zhodnom overenom vlastníkovi. Pripojenie člena nemení vlastníka ani fázu;
prevzatie cudzieho rozpracovaného workflow nie je súčasťou tohto rozšírenia.
Delegácia uchová scope, task ID, fázu, generáciu a revíziu podkladov.
Výsledok kolegu nepreskočí kontrolu ani nepoužije starý review PASS na
novšiu implementáciu. Nezávislý kontrolór má stále oddelený kontext.

Koordinácia musí fungovať aj počas používateľom začatého turnu v pôvodnom
chate. Server overí jeho pôvod, identitu a aktívny rozsah; nevymyslí si
falošnú úlohu odoslanú správcom, aby obišiel dnešnú kontrolu vlastníctva.

Čakanie, registrácia, reconnect a sledovanie stavu nevolajú model. Model sa
prebudí len kvôli konkrétnej práci alebo výsledku, ktorý odblokuje jeho úlohu.
Zostávajú zlučovanie správ, deduplikácia, stručné odovzdanie a limity tímu.
Celá história pripojeného chatu sa neposiela kolegom ani preventívne
nenačítava do Stratusu. Čítajú sa potrebné metadáta a ohraničené výsledky
konkrétnych tímových vstupov, prípadne ich stav pri obnove.

Limity Stratusu pokrývajú automatické tímové vstupy. Ručné používanie
pôvodného chatu pokračuje aj pri pozastavenom tíme a nie je vydávané za
spotrebu kontrolovanú tímovým limitom. Chýbajúca telemetria nie je nula;
prebudenie môže zahŕňať náklady existujúceho natívneho kontextu.

## Kontrakt pripojenia

Člen rozlišuje `owned` (relácia spustená Stratusom) a `attached` (externý
chat). Pripojenie uchová hosta, natívne ID, overený projekt, generáciu
spojenia, dostupné schopnosti a stav. Jedna natívna konverzácia smie mať
len jednu aktívnu tímovú väzbu. Po reconnecte starý transport nesmie
odosielať vstupy ani dokončovať úlohy novej generácie.

Samostatný transport pre pripojenie poskytne:

- overenie identity, projektu a aktuálneho profilu pôvodným hostom;
- udalosti aj o používateľských turnoch, čakaní na schválenie a odpojení;
- bezpečné natívne zaradenie vstupu vrátane prípustného zlúčenia do
  súbežného používateľského turnu; explicitné potvrdenie korelačného ID;
- rozlíšenie prijatia, začatia, dokončenia a neurčitého výsledku odoslania;
- ohraničené zosúladenie konkrétneho vstupu po výpadku a odpojenie väzby.

Natívne ID z voľného modelového argumentu nie je autentifikácia. Párovanie
potvrdí host alebo session-bound hook/plugin cez jednorazové spojenie;
prístupový údaj patrí transportu, nie textu poslanému modelu. Je viazaný na
projekt, tím, člena, hosta a generáciu. Verejné odpovede a dashboard ho
nezobrazia. Všeobecné zadanie ľubovoľnej sieťovej adresy z modelového nástroja
nie je mechanizmom registrácie hosta.

Neúspešné overenie neskončí zeleným stavom „pripojený“. Rozhranie rozlíši
nájdený chat, spojenie čakajúce na overenie, pripravený, pracujúci,
čakajúci na používateľa a odpojený chat s vysvetlením ďalšieho kroku.
Pre automatickú spoluprácu sú potrebné overené budenie aj korelácia výsledku.
Samotná registrácia alebo úspešné HTTP odoslanie nepotvrdzuje dokončenie.

Deterministická fronta musí rešpektovať aj natívne používateľské vstupy.
Samotné „over idle, potom odošli“ nestačí, ak medzi tým môže začať nový
turn. Adaptér musí využívať natívne bezpečné radenie alebo preukázateľne
bezpečné odmietnutie/synchronizáciu. Pri neurčitom prijatí sa vstup najprv
zosúladí; bez dôkazu sa znovu neodošle.

Zastavenie tímu a ukončenie Stratusu externý proces nezabijú. Ručný natívny
turn sa neprerušuje. Prerušiť možno len jednoznačne identifikovaný tímový
turn, ak to host podporuje. Už odoslaná práca môže pri odpojení dobehnúť;
jej výsledok nespustí ďalšie kolo. Pôvodná registrácia chatu v Comms ostáva
živá, pokiaľ je živý host. Po reštarte Stratusu ostáva automatika pozastavená.

## Overené adaptéry a zostávajúce obmedzenia

Presné verzie, postup a hranice dôkazov sú v
[zázname overenia](../../testing/existing-chat-attachments.md).

- **Codex desktop:** živý test po výslovnom súhlase používateľa preukázal
  prebudenie rovnakého chatu po skončení pôvodného turnu a zachovanie jeho
  kontextu. Konektor používa dodanú štandardnú MCP fasádu `codex-app-tools`,
  skutočnú identitu aktuálneho chatu z metadát volania a nové identifikátory
  nástrojových volaní. Nepreberá identitu z voľného argumentu ani globálnej
  premennej zdieľaného MCP procesu. Súhlas s pripojením povoľuje tímové vstupy
  len do tohto chatu. Modelové overrides sa neposielajú.

  Samostatné `wait_threads` v teste vrátilo chybu hosta. Konektor preto mimo
  modelu pravidelne číta ohraničený stav cez `read_thread`; uchová iba
  metadáta aktuálneho turnu a najviac dvoch predchádzajúcich ukončení.
  História ani text predchádzajúcich odpovedí sa neposielajú ďalšiemu modelu.
  Chýbajúci dôkaz ukončenia predchádzajúceho turnu pozastaví spojenie.

  Natívny send vracia len ID chatu. Nemá idempotentný kľúč vstupu ani
  potvrdenie výsledku, preto sa odoslanie skúša raz. Agent následne volá
  `team_ack` a `team_result` pre konkrétnu úlohu; všeobecná idle udalosť
  ani text poslednej odpovede nie sú dôkazom jej splnenia. Pri čakaní na
  delegovanú prácu agent pred skončením turnu odovzdá stručný stav čakania,
  aby ho výsledok kolegu mohol neskôr prebudiť. Súbežný ručný vstup si
  zachová vlastný rozsah oprávnení. Natívne schvaľovanie ostáva pôvodné.

  Projektové nastavenie odovzdáva názvy premenných potrebných na pripojenie
  k pôvodnému konektoru aplikácie. Samotné Codex CLI bez tohto konektora
  neposkytuje desktop pripojenie. Netvorí sa druhý daemon, súkromné IPC ani
  ďalší proces zapisujúci cez `resume` do pôvodnej histórie.
- **Claude Code:** `asyncRewake` podporuje budenie, ale čakajúci hook má
  konečný timeout. Neoveril sa spôsob trvalého čakania ani obnovenia po jeho
  vypršaní bez ďalšieho modelového turnu. Persistent Channels vyžaduje
  povolenie pri štarte; doplnenie do ľubovoľného otvoreného chatu nie je
  overené. Existujúce chaty preto zatiaľ tento konektor nemajú.
- **OpenCode:** V1 a V2 pracujú s rovnakým natívnym ID. Izolovaný test V2
  fronty prijal a povýšil druhý vstup, ale po dokončení prvého vstupu sa
  neobjavil zodpovedajúci druhý výsledok. V2 čakanie navyše vrátilo 503.
  Natívne vykonanie fronty nie je preukázané; pripojenie existujúceho chatu
  zostáva nedostupné. Legacy `prompt_async` nie je overenou náhradou.

Nové relácie všetkých troch nástrojov spravované Stratusom zostávajú
samostatne podporované. Implementovaný Codex konektor sa nesmie označiť za
hotové pripojenie všetkých troch aplikácií.

## Zásah do existujúcej implementácie

- `teams`: oddeliť vlastníctvo relácie od členstva v tíme, pridať overenú
  väzbu externého turnu, jeho životný cyklus, frontu a perzistentné rezervácie.
- `teamruntime` a natívne integrácie: samostatné pripojenie k existujúcemu
  hostu; nikdy nerecyklovať vlastniaci `Open` adaptér na cudzí chat.
- `api` a `mcp`: jednorazové pripojenie/odpojenie a overená session identita;
  zachovať projektové hranice a pôvodné workflow kontroly.
- Synchronizácia Comms a výsledkov: odpojenie tímu nie je ukončenie cudzieho
  chatu; dokončenie vyžaduje správne ID vstupu, nie len všeobecnú idle udalosť.
- Dashboard: ukázať pôvod člena, stav spojenia, rolu vedúceho a konkrétny
  chýbajúci krok. História a model ostávajú v pôvodnej aplikácii.

## Kritériá dokončenia

1. Živé overenie každého podporovaného hosta preukáže tú istú natívnu
   session, zachovaný kontext a výsledok viditeľný v pôvodnom chate.
   Nezačne druhý proces zapisujúci do tej istej histórie.
2. Celý tok začne v pôvodnom chate: používateľ zadá úlohu, vedúci naviaže
   vlastný workflow, deleguje, skončí turn a výsledok kolegu ho prebudí
   v tej istej konverzácii. Nie je potrebné kopírovanie do dashboardu.
3. Súbežný ručný vstup, schválenie, duplicitná udalosť, výpadok po prijatí,
   reconnect a reštart nevyvolajú súbežné alebo opakované vykonanie úlohy.
   Nejasný výsledok sa pozastaví a zostane viditeľný.
4. Odpojenie, zastavenie tímu a obnova nezrušia používateľský turn ani
   pôvodnú aplikáciu. Neobnovia tajne starý automatický vstup.
5. Profily, projektové hranice, vlastník workflow a review dôkazy ostanú
   vynútené aj pri externom vedúcom. Nečinnosť spôsobí nula modelových
   volaní; tímové limity nezablokujú ručné používanie pôvodného chatu.

Testy používajú nové izolované chaty v testovacom projekte, nie existujúce
UVO konverzácie. Pred živým overením sa ohraničí počet modelových vstupov.
Zistený chýbajúci konektor je konkrétny výsledok preverenia, nie povolenie
obísť pôvodnú aplikáciu.

## Primárne podklady

- [Claude hooks](https://code.claude.com/docs/en/hooks) a
  [cross-session messaging](https://code.claude.com/docs/en/cross-session-messaging).
- [OpenCode pluginy](https://opencode.ai/docs/plugins/),
  [server](https://opencode.ai/docs/server/) a [SDK](https://opencode.ai/docs/sdk/).
- [Codex app-server](https://learn.chatgpt.com/docs/app-server) a
  [CLI reference](https://learn.chatgpt.com/docs/cli/reference).
- Lokálne `teams/types.go`, `teams/manager.go`, `teams/recovery.go`,
  `api/routes_teams.go`, `api/team_sync.go`, `api/team_workflow.go`,
  `mcp/teams.go`, natívne adaptéry a OpenCode plugin.
