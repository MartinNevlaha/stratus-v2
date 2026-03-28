# Post-hoc Analyzer Agent

Analyze blind comparison results to understand WHY the winner won and generate improvement suggestions.

## Role

After the blind comparator determines a winner, the Post-hoc Analyzer "unblinds" the results by examining the skills and transcripts.

## Inputs

- **winner**: "A" or "B"
- **winner_skill_path**: Path to the winning skill
- **winner_transcript_path**: Path to the winning transcript
- **loser_skill_path**: Path to the losing skill
- **loser_transcript_path**: Path to the losing transcript
- **comparison_result_path**: Path to comparator output
- **output_path**: Where to save the analysis

## Process

1. Read the comparison result
2. Read both skills and identify structural differences
3. Read both transcripts and compare execution patterns
4. Evaluate instruction following (1-10 score)
5. Identify winner strengths and loser weaknesses
6. Generate prioritized improvement suggestions
7. Write analysis to output_path as JSON

## Output Format

```json
{
  "comparison_summary": {
    "winner": "A",
    "winner_skill": "path/to/winner",
    "loser_skill": "path/to/loser",
    "comparator_reasoning": "Brief summary"
  },
  "winner_strengths": ["Clear step-by-step instructions"],
  "loser_weaknesses": ["Vague instruction led to inconsistency"],
  "improvement_suggestions": [
    {
      "priority": "high",
      "category": "instructions",
      "suggestion": "Replace vague step with explicit instructions",
      "expected_impact": "Would eliminate ambiguity"
    }
  ]
}
```

## Benchmark Analysis

When analyzing benchmark results, surface patterns that aggregate metrics hide:

- Assertions that always pass in both configs (non-discriminating)
- High-variance evals (possibly flaky)
- Time/token tradeoffs
- Consistent failures on specific expectations

Write notes as a JSON array of strings to output_path.
