#!/usr/bin/env python3
"""Aggregate benchmark results from grading files into benchmark.json."""

import json
import sys
import os
import statistics
from pathlib import Path


def load_grading(grading_path):
    with open(grading_path) as f:
        return json.load(f)


def load_timing(timing_path):
    if os.path.exists(timing_path):
        with open(timing_path) as f:
            return json.load(f)
    return {}


def aggregate_iteration(workspace_dir, skill_name):
    runs = []
    configs = {"with_skill": [], "without_skill": [], "old_skill": []}

    for eval_dir in sorted(Path(workspace_dir).glob("eval-*")):
        if not eval_dir.is_dir():
            continue

        eval_metadata_path = eval_dir / "eval_metadata.json"
        if eval_metadata_path.exists():
            with open(eval_metadata_path) as f:
                metadata = json.load(f)
        else:
            metadata = {"eval_id": eval_dir.name, "eval_name": eval_dir.name}

        for config in ["with_skill", "without_skill", "old_skill"]:
            config_dir = eval_dir / config
            if not config_dir.is_dir():
                continue

            grading_path = config_dir / "grading.json"
            if not grading_path.exists():
                continue

            grading = load_grading(grading_path)
            timing = load_timing(config_dir / "timing.json")

            summary = grading.get("summary", {})
            run_data = {
                "eval_id": metadata.get("eval_id", 0),
                "eval_name": metadata.get("eval_name", eval_dir.name),
                "configuration": config,
                "run_number": 1,
                "result": {
                    "pass_rate": summary.get("pass_rate", 0),
                    "passed": summary.get("passed", 0),
                    "failed": summary.get("failed", 0),
                    "total": summary.get("total", 0),
                    "time_seconds": timing.get("total_duration_seconds", 0),
                    "tokens": timing.get("total_tokens", 0),
                },
                "expectations": grading.get("expectations", []),
            }
            runs.append(run_data)
            configs[config].append(run_data)

    run_summary = {}
    for config_name, config_runs in configs.items():
        if not config_runs:
            continue
        pass_rates = [r["result"]["pass_rate"] for r in config_runs]
        times = [r["result"]["time_seconds"] for r in config_runs]
        tokens = [r["result"]["tokens"] for r in config_runs]

        def stats(values):
            if not values:
                return {"mean": 0, "stddev": 0, "min": 0, "max": 0}
            return {
                "mean": round(statistics.mean(values), 2),
                "stddev": round(statistics.stdev(values), 2) if len(values) > 1 else 0,
                "min": round(min(values), 2),
                "max": round(max(values), 2),
            }

        run_summary[config_name] = {
            "pass_rate": stats(pass_rates),
            "time_seconds": stats(times),
            "tokens": stats(tokens),
        }

    delta = {}
    if "with_skill" in run_summary and "without_skill" in run_summary:
        for metric in ["pass_rate", "time_seconds", "tokens"]:
            ws = run_summary["with_skill"][metric]["mean"]
            wos = run_summary["without_skill"][metric]["mean"]
            delta[metric] = f"+{round(ws - wos, 2)}"

    benchmark = {
        "metadata": {
            "skill_name": skill_name,
            "timestamp": __import__("datetime").datetime.now().isoformat(),
        },
        "runs": runs,
        "run_summary": run_summary,
        "delta": delta,
        "notes": [],
    }

    return benchmark


def main():
    if len(sys.argv) < 2:
        print(
            "Usage: python -m scripts.aggregate_benchmark <workspace-iteration-dir> [--skill-name <name>]"
        )
        sys.exit(1)

    workspace_dir = sys.argv[1]
    skill_name = "unknown-skill"

    if "--skill-name" in sys.argv:
        idx = sys.argv.index("--skill-name")
        if idx + 1 < len(sys.argv):
            skill_name = sys.argv[idx + 1]

    benchmark = aggregate_iteration(workspace_dir, skill_name)

    benchmark_path = os.path.join(workspace_dir, "benchmark.json")
    with open(benchmark_path, "w") as f:
        json.dump(benchmark, f, indent=2)

    md_path = os.path.join(workspace_dir, "benchmark.md")
    with open(md_path, "w") as f:
        f.write(f"# Benchmark: {skill_name}\n\n")
        for config, summary in benchmark.get("run_summary", {}).items():
            f.write(f"## {config}\n\n")
            f.write(f"| Metric | Mean | StdDev | Min | Max |\n")
            f.write(f"|--------|------|--------|-----|-----|\n")
            for metric, vals in summary.items():
                f.write(
                    f"| {metric} | {vals['mean']} | {vals['stddev']} | {vals['min']} | {vals['max']} |\n"
                )
            f.write("\n")

        if benchmark.get("delta"):
            f.write("## Delta (with_skill - without_skill)\n\n")
            for metric, val in benchmark["delta"].items():
                f.write(f"- **{metric}**: {val}\n")

    print(f"Benchmark written to {benchmark_path}")
    print(f"Markdown summary written to {md_path}")


if __name__ == "__main__":
    main()
