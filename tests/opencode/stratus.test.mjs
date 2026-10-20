import test from 'node:test';
import assert from 'node:assert/strict';
import { Stratus } from '../../cmd/stratus/plugins-opencode/stratus.ts';
const legacy={id:'spec-legacy',session_id:'legacy-s',phase:'verify'};
const codex={id:'spec-codex',session_id:'codex:s1',runtime:'codex',phase:'verify'};
async function plugin(t,workflows){t.mock.method(globalThis,'fetch',async (url,opts)=>{const path=new URL(url).pathname;if(path==='/api/dashboard/state')return Response.json({workflows});if(path==='/api/workflows/spec-codex')return Response.json(codex);if(path==='/api/retrieve/dirty'){assert.equal(opts.method,'POST');assert.deepEqual(JSON.parse(opts.body),{paths:['src/a.go']});return Response.json({});}throw new Error(`Unexpected URL: ${url}`);});return Stratus();}
test('legacy verify remains blocked with a Codex workflow present',async t=>{const p=await plugin(t,[legacy,codex]);await assert.rejects(p['tool.execute.before']({tool:'edit',sessionID:'legacy-s'},{args:{}}),/verify/);});
test('Codex-only state does not block unrelated legacy edits',async t=>{const p=await plugin(t,[codex]);await p['tool.execute.before']({tool:'edit',sessionID:'legacy-s'},{args:{}});});
test('explicit Codex task cannot bypass filtering',async t=>{const p=await plugin(t,[legacy,codex]);await assert.rejects(p['tool.execute.before']({tool:'task',sessionID:'legacy-s'},{args:{subagent_type:'delivery-backend-engineer',prompt:'Workflow ID: spec-codex'}}),/workflow/i);});
test('legacy dirty-index notification unchanged',async t=>{const p=await plugin(t,[legacy]);await p['tool.execute.after']({tool:'edit',args:{path:'src/a.go'}});});
