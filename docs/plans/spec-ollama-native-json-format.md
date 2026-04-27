# Plan: Ollama native format:"json" mode for evolution LLM calls

## Cieľ
Pridať per-request `ResponseFormat` pole do `CompletionRequest`, implementovať ho v OpenAI/Ollama klientovi ako `response_format:{"type":"json_object"}` v HTTP body, a zapnúť ho v dvoch konkrétnych call-siteoch (`hypothesis.generateWithLLM`, `experiment.executePromptTuningWithLang` evaluator) aby gemma4:e4b cez ollamu vracala parseovateľný JSON bez markdown fences.

## Nie-ciele (explicit non-goals)
- Nemeníme `anthropic.go`, `zai.go` — providers bez JSON mode supportu dostanú silent no-op (pole sa zahodí).
- Nemeníme `config.Config` — feature je per-request, nie per-klient.
- Nemeníme wiki, product_intelligence, code_analyst callery — zostávajú bez JSON mode, `ParseJSONResponse` ich kryje ako predtým.
- Baseline + proposed prompt calls v `experiment.go:133, :144` **nemeníme** — tie majú vracať prózu, nie JSON.
- Nezavádzame `JSONSchema`, `Strict`, enum typy ani nový config flag.
- Nemažeme `ParseJSONResponse` — zostáva ako defense-in-depth fallback.
- Žiadny nový ADR (ADR-0001 pokrýva zmenu).

## Súčasný stav + root cause
- `gemma4:e4b` cez ollamu posiela JSON obalený markdown fences (```` ```json ... ``` ````), občas s prózou pred/po.
- `llm.ParseJSONResponse` robí greedy scan na prvé `{`/`[` a posledné `}`/`]` — pri niektorých gemma výstupoch sa trafí do vnútorného výrezu a `json.Unmarshal` spadne.
- Dnes `openAIRequest` struct (`openai.go:31-41`) neposiela `response_format` — ollama ho podporuje natívne v OpenAI-compat vrstve, ale Stratus ho nevyužíva.
- Relevantné call-sites (hypothesis generation, prompt_tuning evaluator) očakávajú JSON — ideálni kandidáti na zapnutie JSON mode.

Súbory v scope:
- `internal/insight/llm/client.go` — rozšíriť `CompletionRequest`
- `internal/insight/llm/openai.go` — pridať field + mapping v `Complete`
- `internal/insight/evolution_loop/hypothesis.go` — set `ResponseFormat:"json"` v `generateWithLLM`
- `internal/insight/evolution_loop/experiment.go` — set `ResponseFormat:"json"` iba v evaluator call (line 167-172)
- `internal/insight/llm/openai_regressions_test.go` — nové httptest testy
- `internal/insight/evolution_loop/experiment_test.go` + `hypothesis_test.go` — nové testy na response_format propagáciu

## Navrhovaná zmena (surgical)
1. `client.go:67-72`: pridať `ResponseFormat string` do `CompletionRequest` (hodnoty `""` alebo `"json"`, komentár v godoc).
2. `openai.go:31-41`: pridať interný typ `openAIResponseFormat struct { Type string }` a pole `ResponseFormat *openAIResponseFormat json:"response_format,omitempty"` do `openAIRequest`.
3. `openai.go:120-125`: v `Complete` po zostrojení `body` — ak `req.ResponseFormat == "json"` → set `body.ResponseFormat = &openAIResponseFormat{Type: "json_object"}`. Akákoľvek iná neprázdna hodnota → `slog.Warn` s kontextom a správanie ako `""` (silent no-op).
4. `hypothesis.go:178-183`: pridať `ResponseFormat: "json"` do `llm.CompletionRequest` literal.
5. `experiment.go:167-175`: pridať `ResponseFormat: "json"` **iba** do evaluator `Complete` call. Baseline (line 133) a proposed (line 144) zostávajú bezo zmeny.

## Dizajn — kľúčové rozhodnutia
- **String type pre `ResponseFormat` (namiesto enum/konšt.):** Karpathy Simplicity First. Dva stringy (`""` / `"json"`) stačia.
- **`*openAIResponseFormat` pointer s `omitempty`:** pri prázdnom `ResponseFormat` sa pole v HTTP body vôbec neobjaví (regresná safety pre iných providerov).
- **Anthropic/Zai silent no-op:** polepšovať ich presahuje scope; `ParseJSONResponse` drží fallback.
- **Neplatná hodnota `ResponseFormat` → warning + no-op:** `config-validation.md` hovorí log + best-effort. Nezavádzame error, aby sa API nezaväzovalo.
- **Evaluator áno, baseline/proposed nie:** evaluator MUSÍ vrátiť JSON; baseline/proposed sú plaintext — JSON mode by obsah poškodil.

## Budget (Principle 2 — Simplicity First)
- LOC budget: ~25 riadkov produkčného kódu + ~90 riadkov testov
- Nové súbory: **0**
- Nové polia/typy: `CompletionRequest.ResponseFormat string` (exportované), `openAIResponseFormat` (neexportovaný internal), `openAIRequest.ResponseFormat *openAIResponseFormat` (neexportovaný)
- Nové dependencies: **0**

## Úlohy (TDD order — testy PRVÉ)

0. **Napísať 3 failing testy v `openai_regressions_test.go`:**
   - `TestOpenAIClient_Complete_ResponseFormatJSON_IncludesInBody` — httptest handler unmarshaluje body, assertuje `response_format == {"type":"json_object"}`.
   - `TestOpenAIClient_Complete_ResponseFormatEmpty_OmitsFromBody` — regresia: bez ResponseFormat pole chýba.
   - `TestOpenAIClient_Complete_ResponseFormatInvalid_OmitsFromBody` — invalid hodnota → pole chýba, call nevráti error.

1. **Napísať failing testy pre call-sites:**
   - `TestHypothesisGenerator_generateWithLLM_UsesJSONResponseFormat` v `hypothesis_test.go` (mock `completeFn` zachytí `ResponseFormat`).
   - `TestExperimentRunner_EvaluatorCallUsesJSONResponseFormat` v `experiment_test.go` (poradový index volaní: baseline/proposed bez JSON mode, evaluator s JSON mode).

2. **Implementácia LLM klienta:** pridať `ResponseFormat string` field do `CompletionRequest`, `openAIResponseFormat` struct + field do `openAIRequest`, switch v `Complete` (nil/json_object/warn).

3. **Wire up call-sites:** `hypothesis.go:178-183` a `experiment.go:167-175` evaluator — pridať `ResponseFormat: "json"` do literalu.

4. **Spustiť `go test ./internal/insight/llm/... ./internal/insight/evolution_loop/...`** — nové testy PASS + žiadne regresie. Build: `go build ./...`.

5. **Manuálna verifikácia:** reštartovať stratus, triggernúť `POST /api/evolution/trigger` s `timeout_ms=300000`, overiť že nový run má `hypotheses_count > 0` a aspoň jedna `prompt_tuning` hypotéza má `experiment_metric != 0.75` (dôkaz, že evaluator reálne odpovedal JSON-om a nespadol na simuláciu).

## Úspešné kritériá (Goal-Driven Execution)
- [ ] Všetkých 5 nových unit testov PASS
- [ ] `go test ./...` v `insight/llm` a `evolution_loop` bez regresií
- [ ] `go build ./...` čistý
- [ ] Manuálny evolution run: `hypotheses_count > 0`; aspoň 1 prompt_tuning hypothesis s `experiment_metric ∈ (0, 1), ≠ 0.75`
- [ ] V logoch stratus servera počas behu NIE sú varovania `evolution hypothesis generator: LLM generation failed` ani `experiment runner: LLM prompt_tuning failed`

## Riziká
- **Ollama OpenAI-compat vrstva nemusí prepínať cez `response_format`** — niektoré verzie ollamy vyžadujú natívne `format:"json"` v `/api/generate`. Predbežný smoke test (skorší v session): `response_format: json_object` v `/v1/chat/completions` s gemma4 **vrátilo prázdny obsah** — možno chyba, možno ignorované. Mitigácia: v úlohe 5 overiť, ak by to znovu zlyhalo, defense-in-depth `ParseJSONResponse` stále kryje a follow-up PR môže prepnúť na native `/api/chat`.
- **gemma4:e4b môže v JSON mode stratiť časť schémy** — risk nezávislý od tohto plánu; `raw` struct tolerantne akceptuje chýbajúce polia.
- **`slog.Warn` import**: overiť že `openai.go` už používa `log/slog`; ak nie, 1 import navyše.
