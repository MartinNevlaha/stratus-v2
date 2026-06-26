# Code-analyst produkuje opakované false-positive findings — popis chýb

**Repo:** `stratus-v2`
**Balík:** `internal/insight/code_analyst/` (hlavne `analyzer.go`, `prompts.go`)

**Symptóm:** LLM code-analyzer opakovane vracia findings, ktoré sú buď úplne
nesprávne (popisujú bug, čo neexistuje), alebo majú zlé čísla riadkov.
Pozorované naprieč viacerými súbormi cieľového projektu.

---

## Bug 1 — Modelu sa neposielajú čísla riadkov (→ všetky line-refy sú zlé)

- **Kde:** `analyzer.go:56` (`content := string(rawContent)`) — surový obsah
  súboru ide do promptu bez prefixu čísel riadkov, no `prompts.go:17` od modelu
  žiada „reference exact line numbers". Model si riadky počíta sám →
  systematicky zlé `line_start`/`line_end`.
- **Dôkaz:** súbor plne pod 32 KB capom (32 549 B) dostal finding s tvrdením
  „riadky 297-305", reálny kód bol na 600-640. Plne viditeľný súbor, a aj tak
  zlé čísla → príčina je chýbajúce číslovanie.
- **Fix:** pred zostavením `userPrompt` (`analyzer.go:63`) prefixovať každý
  riadok `content` jeho 1-based číslom (napr. `"%d\t%s"`). Voliteľne v prompte
  uviesť, že riadky sú očíslované a `line_start/line_end` sa majú brať z týchto
  prefixov.

## Bug 2 — Tvrdé orezanie na 32 KB bez upozornenia (→ „client hangs", „unbounded loop" a pod.)

- **Kde:** `analyzer.go:13` (`maxFileContentBytes = 32 * 1024`) +
  `analyzer.go:57-59` — súbory > 32 KB sa odseknú na byte 32768; model
  nedostane žiadnu informáciu, že vidí len časť súboru, a usudzuje o
  control-flow, ktorý mu bol orezaný.
- **Dôkaz:**
  - `chat/router.py` (39 527 B): v 32 KB je viditeľných len prvých **721**
    riadkov. Model dostal finding „swallowed exception leaves client hanging",
    lebo **nevidel** terminátor streamu `data: [DONE]` (r. 819) ani
    `except asyncio.CancelledError` (r. 820) — sú za hranicou orezania.
  - `pipeline.py` (66 147 B, > 2× cap): viditeľných len prvých **761** riadkov;
    analyzovaný blok je 623-869, takže model videl len jeho prvú polovicu.
- **Fix (jedna z možností, vzostupne podľa kvality):**
  1. minimálne: keď je súbor orezaný, vložiť do promptu explicitné upozornenie
     „file truncated: showing N of M lines; do not infer control flow beyond the
     shown region";
  2. lepšie: zvýšiť cap / chunkovať súbor a analyzovať po častiach s prekryvom;
  3. najlepšie: analyzovať po logických jednotkách (funkcie/triedy) s plným
     telom každej jednotky.

## Bug 3 — Žiadny overovací (refutačný) prechod

- **Kde:** `analyzer.go:88-93` — výstup modelu sa po naparsovaní iba prefiltruje
  (`filterFindings`, confidence + kategória) a vráti. Niet druhého kroku, ktorý
  by každý finding skúsil **vyvrátiť** oproti skutočnému kódu.
- **Dopad:** confident-but-wrong findings prejdú nezmenené. (Manuálne vyvrátenie
  jedným cieleným prechodom zhodilo 3 zo 4 testovaných findings.)
- **Fix:** pridať voliteľný druhý LLM prechod „adversarial verify" — vstup:
  finding + relevantný (neorezaný) úsek kódu; úloha: pokús sa tvrdenie vyvrátiť,
  pri pochybnosti default `rejected`. Prepúšťať len findings, ktoré verifikáciu
  prežijú.

## Bug 4 — Self-reported confidence sa berie ako pravda, prah je nízky

- **Kde:** `analyzer.go:105` (`f.Confidence < a.confidenceThreshold`), default
  threshold 0.7.
- **Dopad:** falošné findings si samy priradili confidence 0.90-0.95, takže
  prahový filter ich nevie odchytiť. Self-assessed confidence nie je spoľahlivý
  signál.
- **Fix:** nespoliehať sa na confidence ako primárny filter (riešené Bugom 3);
  prípadne kalibrovať/ignorovať self-confidence.

## Bug 5 — Prompt nehovorí rešpektovať zámerné vzory

- **Kde:** `prompts.go:3-22` (`codeAnalysisSystemPrompt`).
- **Dopad:** model flaguje aj kód s explicitným zámerom — napr.
  `# noqa: BLE001 — fail-open` (zámerný broad-except v streamovacom endpointe)
  alebo idiomatické framework vzory — ako „critical" bug.
- **Fix:** doplniť do system promptu pravidlo, nech bere do úvahy zámernosť:
  rešpektovať `noqa`/lint-suppression komentáre, dokumentovaný fail-open dizajn,
  idiomatické vzory frameworku; nehlásiť ich ako bug, ak je zámer zrejmý z
  komentára/kontextu.

---

## Priorita

- **Bug 1 a Bug 2** majú najväčší dopad a sú najlacnejšie (malé zmeny v
  `analyzer.go`) — odstránia väčšinu šumu.
- **Bug 3** je najsilnejšia poistka proti zvyšku.
- **Bug 4 / 5** sú doplnkové.
