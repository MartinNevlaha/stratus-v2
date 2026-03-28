#!/usr/bin/env python3
"""Run the description optimization loop for a skill."""

import json
import os
import subprocess
import sys
import tempfile
import random
from pathlib import Path


def load_eval_set(path):
    with open(path) as f:
        return json.load(f)


def evaluate_description(skill_path, description, eval_set, model, runs_per_query=3):
    """Evaluate a description by running test queries and checking if the skill triggers."""
    results = {"triggered": 0, "total": 0}

    for item in eval_set:
        should_trigger = item.get("should_trigger", True)
        query = item["query"]

        triggered_count = 0
        for _ in range(runs_per_query):
            prompt = f"You have access to a skill with this description: {description}\n\nUser query: {query}\n\nShould you use this skill? Answer only YES or NO."
            try:
                result = subprocess.run(
                    ["claude", "-p", "--model", model, prompt],
                    capture_output=True,
                    text=True,
                    timeout=30,
                )
                answer = result.stdout.strip().upper()
                if "YES" in answer:
                    triggered_count += 1
            except (subprocess.TimeoutExpired, FileNotFoundError):
                pass

        triggered = triggered_count > runs_per_query // 2
        results["total"] += 1
        if triggered == should_trigger:
            results["triggered"] += 1

    return results["triggered"] / results["total"] if results["total"] > 0 else 0


def propose_improvement(current_description, failed_queries, model):
    """Use Claude to propose an improved description."""
    prompt = f"""Current skill description:
{current_description}

This description failed to trigger correctly on these queries:
{json.dumps(failed_queries, indent=2)}

Please propose an improved description that:
1. Triggers correctly on the failed queries
2. Is still concise (under 200 words)
3. Includes both what the skill does AND when to use it

Return ONLY the new description text, nothing else."""

    try:
        result = subprocess.run(
            ["claude", "-p", "--model", model, prompt],
            capture_output=True,
            text=True,
            timeout=60,
        )
        return result.stdout.strip()
    except (subprocess.TimeoutExpired, FileNotFoundError):
        return current_description


def main():
    import argparse

    parser = argparse.ArgumentParser(description="Run description optimization loop")
    parser.add_argument("--eval-set", required=True, help="Path to eval set JSON")
    parser.add_argument("--skill-path", required=True, help="Path to skill directory")
    parser.add_argument(
        "--model", default="claude-sonnet-4-20250514", help="Model to use"
    )
    parser.add_argument(
        "--max-iterations", type=int, default=5, help="Max optimization iterations"
    )
    parser.add_argument("--verbose", action="store_true", help="Print progress")
    args = parser.parse_args()

    eval_set = load_eval_set(args.eval_set)

    random.shuffle(eval_set)
    split = int(len(eval_set) * 0.6)
    train_set = eval_set[:split]
    test_set = eval_set[split:]

    skill_md_path = os.path.join(args.skill_path, "SKILL.md")
    with open(skill_md_path) as f:
        content = f.read()

    current_description = ""
    lines = content.split("\n")
    in_fm = False
    for line in lines:
        if line.strip() == "---":
            if in_fm:
                break
            in_fm = True
            continue
        if in_fm and line.strip().startswith("description:"):
            current_description = line.strip()[len("description:") :].strip().strip('"')

    if args.verbose:
        print(f"Current description: {current_description[:100]}...")
        print(f"Train set: {len(train_set)}, Test set: {len(test_set)}")

    results = []
    best_description = current_description
    best_test_score = 0

    for i in range(args.max_iterations):
        if args.verbose:
            print(f"\n--- Iteration {i + 1}/{args.max_iterations} ---")

        train_score = evaluate_description(
            args.skill_path, current_description, train_set, args.model
        )
        test_score = evaluate_description(
            args.skill_path, current_description, test_set, args.model
        )

        if args.verbose:
            print(f"Train score: {train_score:.2f}, Test score: {test_score:.2f}")

        results.append(
            {
                "iteration": i + 1,
                "description": current_description,
                "train_score": train_score,
                "test_score": test_score,
            }
        )

        if test_score > best_test_score:
            best_test_score = test_score
            best_description = current_description

        if test_score >= 1.0:
            if args.verbose:
                print("Perfect test score achieved!")
            break

        failed_queries = []
        for item in train_set:
            should_trigger = item.get("should_trigger", True)
            triggered = True
            if triggered != should_trigger:
                failed_queries.append(item)

        if failed_queries:
            new_description = propose_improvement(
                current_description, failed_queries, args.model
            )
            if new_description and new_description != current_description:
                current_description = new_description
            else:
                if args.verbose:
                    print("No improvement proposed, stopping.")
                break

    output = {
        "best_description": best_description,
        "best_test_score": best_test_score,
        "iterations": results,
    }

    output_path = os.path.join(
        os.path.dirname(args.eval_set), "optimization_result.json"
    )
    with open(output_path, "w") as f:
        json.dump(output, f, indent=2)

    print(f"\nBest test score: {best_test_score:.2f}")
    print(f"Best description: {best_description[:200]}...")
    print(f"Results saved to {output_path}")


if __name__ == "__main__":
    main()
