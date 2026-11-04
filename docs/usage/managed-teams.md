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

## Spotreba

Stratus nevolá ďalší model na smerovanie správ ani na čakanie. Prázdna fronta,
prítomnosť kolegu a jednoduché potvrdenia nespúšťajú model. Súvisiace pripravené
správy sa zlúčia do jedného prebudenia; odovzdávajú sa stručné výsledky a odkazy
na uložené podklady. Plné zadanie ostáva dostupné cez `team_status(task_id)`.

Platí limit súbehu, počet spustení a automatických kôl bez pokroku. Ak nástroj
vracia spotrebu, zobrazuje sa aj počet tokenov a možno nastaviť jeho limit.
Pri neznámej spotrebe platí počet spustení; údaj **neznáma** neznamená nulu.
Kontrola limitu pred novým spustením nemôže zastaviť tokeny už rozbehnutého
natívneho výpočtu presne na hranici. Cenu služby ani presné percento úspory
Stratus neodhaduje z neúplných dát. Natívne nástroje stále načítavajú vlastné
projektové pokyny a históriu.

## Rozsah a overenie

Tím vlastní nové natívne relácie. Nepreberá ľubovoľné existujúce okná aplikácií.
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
