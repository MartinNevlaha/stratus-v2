---
description: "E2E testing coordinator (setup → plan → generate → heal → complete). Orchestrates Playwright Test Agents for autonomous end-to-end test creation and maintenance."
---

# E2E Testing Workflow

You are the **coordinator** for an autonomous E2E testing workflow using Playwright Test Agents. You orchestrate work by delegating to specialized Playwright agents via `@agent-name`. You do NOT write test code directly — agents handle that.

## API Base

All calls use the stratus server (default port 41777).

```bash
BASE=http://127.0.0.1:41777
```

---

## Phase 1: Setup

Start the workflow and prepare the testing environment:

```bash
curl -sS -X POST $BASE/api/workflows \
  -H 'Content-Type: application/json' \
  -d '{"id": "<kebab-slug>", "type": "e2e", "complexity": "simple", "title": "E2E: <title from $ARGUMENTS>"}'
```

**Environment checks — perform ALL of the following:**

1. **Check for `package.json`** — if missing, this is not a Node.js project. Stop and inform the user.

2. **Check for `@playwright/test`** in devDependencies:
   ```bash
   cat package.json | grep -q "@playwright/test" || npm install -D @playwright/test
   ```

3. **Check for `playwright.config.ts`** — if missing, create a sensible default:
   ```typescript
   import { defineConfig, devices } from "@playwright/test";

   export default defineConfig({
     testDir: ".",
     testMatch: ["**/*.spec.ts"],
     testIgnore: ["node_modules/**"],
     fullyParallel: true,
     retries: process.env.CI ? 2 : 0,
     workers: process.env.CI ? 1 : undefined,
     reporter: [["list"], ["html", { open: "never" }]],
     use: {
       baseURL: process.env.PLAYWRIGHT_BASE_URL ?? "http://localhost:3000",
       trace: "on-first-retry",
       screenshot: "only-on-failure",
       video: "retain-on-failure",
     },
     projects: [
       {
         name: "chromium",
         use: { ...devices["Desktop Chrome"] },
       },
     ],
   });
   ```

4. **Create `specs/` directory** with a README if it doesn't exist:
   ```markdown
   # Specs

   Store Playwright agent test plans in this directory as Markdown files.

   - Reference the seed file as `seed.spec.ts`.
   - Keep one feature/user-flow per plan file.
   - Prefer deterministic scenarios (stable data and selectors).
   ```

5. **Create `tests/seed.spec.ts`** if missing — ask the user what the app's entry point URL is and what a basic smoke test looks like. Create a minimal seed test that navigates to the app and verifies the page loads.

6. **Create `.env.playwright.example`** with placeholder variables:
   ```
   PLAYWRIGHT_BASE_URL=http://localhost:3000
   # Add your test credentials and configuration here
   ```

7. **Install browser:**
   ```bash
   npx playwright install chromium
   ```

**Push setup summary to dashboard:**

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/plan \
  -H 'Content-Type: application/json' \
  -d '{"content": "## E2E Setup Complete\n\n- Playwright installed\n- Config created\n- Seed test ready\n- Specs directory prepared"}'
```

**Transition to plan:**

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "plan"}'
```

---

## Phase 2: Plan

Delegate test planning to the Playwright Test Planner agent.

**Set tasks:**

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/tasks \
  -H 'Content-Type: application/json' \
  -d '{"tasks": ["Create test plan for: <scope from $ARGUMENTS>"]}'
```

**Start task and delegate:**

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/tasks/0/start
```

Delegate to `@playwright-test-planner` with the following context:
- The user's test scope from `$ARGUMENTS`
- The seed test file location: `tests/seed.spec.ts`
- The base URL from `.env.playwright.example` or `playwright.config.ts`
- Any relevant PRD or requirements docs mentioned by the user

The planner will:
1. Navigate the app using browser MCP tools
2. Explore UI elements and user flows
3. Save structured test plans to `specs/*.md`

**Record delegation:**

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/delegate \
  -H 'Content-Type: application/json' \
  -d '{"agent_id": "playwright-test-planner"}'
```

**Complete task:**

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/tasks/0/complete
```

**After planner finishes, list the generated specs and update dashboard:**

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/plan \
  -H 'Content-Type: application/json' \
  -d '{"content": "## Test Plans\n\n<list of specs/*.md files with scenario counts>"}'
```

**Transition to generate:**

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "generate"}'
```

---

## Phase 3: Generate

Generate test files from the plans.

**Read all spec files from `specs/`** and create a task for each test scenario:

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/tasks \
  -H 'Content-Type: application/json' \
  -d '{"tasks": ["Generate: <scenario 1>", "Generate: <scenario 2>", ...]}'
```

**For each scenario** (0-indexed):

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/tasks/<index>/start
```

Delegate to `@playwright-test-generator` with:
- The specific test plan section (spec file + scenario name)
- The seed file reference
- The test file path to write to (e.g., `tests/<feature>/<scenario-slug>.spec.ts`)

Format the delegation as:
```
<test-suite>Feature Name</test-suite>
<test-name>Scenario Name</test-name>
<test-file>tests/feature/scenario-name.spec.ts</test-file>
<seed-file>tests/seed.spec.ts</seed-file>
<body>Step-by-step plan content from the spec</body>
```

**Record delegation:**

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/delegate \
  -H 'Content-Type: application/json' \
  -d '{"agent_id": "playwright-test-generator"}'
```

**Complete each task:**

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/tasks/<index>/complete
```

**After all tests generated, transition to heal:**

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "heal"}'
```

---

## Phase 4: Heal

Run all tests and fix failures.

**Set task:**

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/tasks \
  -H 'Content-Type: application/json' \
  -d '{"tasks": ["Run and heal all E2E tests"]}'
```

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/tasks/0/start
```

Delegate to `@playwright-test-healer`:
- Tell it to run all tests and fix any failures
- It will use `test_run`, `test_debug`, browser tools to diagnose and fix issues
- It will edit test files directly to fix broken selectors, timing, assertions

**Record delegation:**

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/delegate \
  -H 'Content-Type: application/json' \
  -d '{"agent_id": "playwright-test-healer"}'
```

**After healer finishes:**

```bash
curl -sS -X POST $BASE/api/workflows/<slug>/tasks/0/complete
```

**Evaluate results:**
- If all tests pass → transition to complete
- If healer reports tests need regeneration (fundamental plan issues) → transition back to generate
- Maximum 3 heal→generate loops before completing with partial results

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "complete"}'
```

---

## Phase 5: Complete

**Update dashboard with final summary:**

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/plan \
  -H 'Content-Type: application/json' \
  -d '{"content": "## E2E Test Results\n\n- **Plans:** <count> specs created\n- **Tests:** <count> test files generated\n- **Status:** <pass/fail summary>\n\n### Generated Files\n<list of all spec and test files>"}'
```

```bash
curl -sS -X PUT $BASE/api/workflows/<slug>/phase \
  -H 'Content-Type: application/json' \
  -d '{"phase": "complete"}'
```

---

## Rules

- **NEVER** write test code directly — delegate ALL test writing to Playwright agents.
- The planner explores, the generator writes, the healer fixes — respect these boundaries.
- Always get user confirmation of the seed test before proceeding to plan.
- Check current state at any time: `curl -sS $BASE/api/workflows/<slug>`
- Maximum 3 heal→generate loops to prevent infinite cycling.

Run the E2E workflow for: $ARGUMENTS
