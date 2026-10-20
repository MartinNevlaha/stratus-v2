# Continue a workflow

Read the explicit workflow ID with the discovered get_workflow tool. Check that its runtime is codex. Bind it with bind_workflow_session using the current native session ID from SessionStart. Read the workflow's design, plan, phase, pending tasks, and relevant memory, then continue its existing Superpowers process without repeating completed tasks or approvals. Do not assume `/resume`, `claude --resume`, or `/goal` exists. If the workflow belongs to another runtime, explain the conflict; do not take it over. A completed workflow stays complete.
