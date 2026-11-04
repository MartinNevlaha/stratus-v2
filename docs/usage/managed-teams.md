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
