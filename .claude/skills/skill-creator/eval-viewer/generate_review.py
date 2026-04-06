#!/usr/bin/env python3
"""Generate an HTML review page for skill eval results."""

import json
import os
import sys
import webbrowser
from http.server import HTTPServer, HTTPServerRequest
 from pathlib import Path


def generate_html(workspace_dir, skill_name, benchmark_path=None, previous_workspace=None, static_output=None):
    html = """<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<title>Skill Eval: """ + skill_name + """</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif; margin: 0; padding: 20px; background: #0a1a1a; color: #e0e0e0; }
h1 { color: #58a6ff; margin-bottom: 20px; }
h2 { color: #c9d1d9; }
.tabs { display: flex; gap: 10px; margin-bottom: 20px; }
.tab-btn { padding: 8px 16px; border: 1px solid #30363d; background: #161b22; color: #8b949e; cursor: pointer; border-radius: 6px; }
.tab-btn.active { background: #238636; color: white; border-color: #238636; }
.eval-card { background: #161b22; border: 1px solid #30363d; border-radius: 8px; padding: 16px; margin-bottom: 12px; }
.eval-card h3 { color: #58a6ff; margin-bottom: 8px; }
.eval-card pre { white-space: pre-wrap; background: #0d1117; padding: 12px; border-radius: 4px; overflow-x: auto; }
.feedback { width: 100%; min-height: 80px; padding: 8px; background: #0d1117; border: 1px solid #30363d; border-radius: 4px; color: #c9d1d9; margin-top: 8px; font-family: monospace; font-size: 13px; }
.nav { display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px; }
.prev-btn, .next-btn { padding: 8px 16px; background: #21262d; border: 1px solid #30363d; color: #c9d1d9; cursor: pointer; border-radius: 6px; }
.prev-btn:disabled, .next-btn:disabled { opacity: 0.5; cursor: default; }
.submit-btn { padding: 8px 16px; background: #238636; color: white; border: none; border-radius: 6px; cursor: pointer; }
.benchmark-table { width: 100%; border-collapse: collapse; }
.benchmark-table th, .benchmark-table td { padding: 8px 12px; border: 1px solid #30363d; text-align: left; }
.benchmark-table th { background: #21262d; color: #c9d1d9; }
.pass { color: #3fb950; }
.fail { color: #f85149; }
</style>
</head>
<body>
<h1>Skill Eval Results: """ + skill_name + """</h1>
<div class="tabs">
<button class="tab-btn active" onclick="showTab('outputs')">Outputs</button>
<button class="tab-btn" onclick="showTab('benchmark')">Benchmark</button>
</div>
<div id="outputs-tab">
<div id="eval-container"></div>
<div class="nav">
<button class="prev-btn" id="prev-btn" onclick="prevEval()">&#9604; Previous</button>
<span id="eval-counter">1 / 1</span>
<button class="next-btn" id="next-btn" onclick="nextEval()">Next &#9655;</button>
</div>
<br>
<button class="submit-btn" onclick="submitReviews()">Submit All Reviews</button>
</div>
<div id="benchmark-tab" style="display:none">
<div id="benchmark-container"></div>
</div>
<script>
const evals = [];
let currentIdx = 0;

function showTab(tab) {
    document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
    event.target.classList.add('active');
    document.getElementById('outputs-tab').style.display = tab === 'outputs' ? 'block' : 'none';
    document.getElementById('benchmark-tab').style.display = tab === 'benchmark' ? 'block' : 'none';
}

function showEval(idx) {
    currentIdx = idx;
    const container = document.getElementById('eval-container');
    if (evals.length === 0) {
        container.innerHTML = '<p>No eval results found.</p>';
        return;
    }
    const eval = evals[idx];
    const grades = eval.grades ? '<h4>Grades</h4><pre>' + JSON.stringify(eval.grades, null, 2) + '</pre>' : '';
    const prevOutput = eval.prevOutput ? '<details><summary>Previous Output</summary><pre>' + eval.prevOutput + '</pre></details>' : '';
    const prevFeedback = eval.prevFeedback ? '<p><em>Previous feedback:</em> ' + eval.prevFeedback + '</p>' : '';
    container.innerHTML = '<div class="eval-card"><h3>' + eval.name + '</h3><p><strong>Prompt:</strong></p><pre>' + eval.prompt + '</pre>' + grades + prevOutput + '<p><strong>Output:</strong></p><pre>' + (eval.output || 'No output') + '</pre>' + prevFeedback + '<textarea class="feedback" placeholder="Your feedback..." id="feedback-' + idx + '">' + (eval.feedback || '') + '</textarea></div>';
    document.getElementById('eval-counter').textContent = (idx + 1) + ' / ' + evals.length;
    document.getElementById('prev-btn').disabled = idx === 0;
    document.getElementById('next-btn').disabled = idx === evals.length - 1;
}

function prevEval() { if (currentIdx > 0) showEval(currentIdx - 1); }
function nextEval() { if (currentIdx < evals.length - 1) showEval(currentIdx + 1); }

function submitReviews() {
    const reviews = evals.map((e, i) => ({
        run_id: e.run_id,
        feedback: document.getElementById('feedback-' + i)?.value || '',
        timestamp: new Date().toISOString()
    }));
    const data = JSON.stringify({ reviews, status: "complete" }, null, 2);
    const blob = new Blob([data], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'feedback.json';
    a.click();
    URL.revokeObjectURL(url);
}

// Load eval data from workspace directory
// This would be populated by the actual script invocation
showEval(0);
</script>
</body>
</html>
"""
    if static_output:
        with open(static_output, "w") as f:
            f.write(html)
        print(f"Static viewer written to {static_output}")
        return

    class EvalHandler(HTTPServerRequest):
        def do_GET(self):
            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.wfile.write(html.encode())

    port = 8765
    server = HTTPServer(("localhost", port), EvalHandler)
    print(f"Viewer running at http://localhost:{port}")
    webbrowser.open(f"http://localhost:{port}")


if __name__ == "__main__":
    import argparse
    parser = argparse.ArgumentParser()
    parser.add_argument("workspace_dir")
    parser.add_argument("--skill-name", required=True)
    parser.add_argument("--benchmark", default=None)
    parser.add_argument("--previous-workspace", default=None)
    parser.add_argument("--static", default=None, dest="static_output")
    parser.add_argument("--port", type=int, default=8765)
    args = parser.parse_args()

    generate_html(
        args.workspace_dir,
        args.skill_name,
        benchmark_path=args.benchmark,
        previous_workspace=args.previous_workspace,
        static_output=args.static_output
    )
