# Run summary: {{RUN_ID}}

| Field | Value |
|---|---|
| Command | `{{COMMAND}}` |
| Rigor | {{RIGOR}} |
| Date | {{DATE}} |
| Plan | `{{PLAN}}` |
| Model (controlling session) | {{MODEL}} |
| Recommended effort | {{EFFORT}} (the effort setting itself is not observable by the plugin) |
| Review dispatches | {{REVIEW_DISPATCHES}} |

## Baseline

Before the run:

{{BASELINE_BEFORE}}

After the run:

{{BASELINE_AFTER}}

## Tasks

| Task | Status | Review verdict | Fix rounds | Commits |
|---|---|---|---|---|
{{TASKS}}

## Rulings

Every autonomous decision taken during the run, with its reason and what it costs if wrong.

{{RULINGS}}

## Review findings rejected by source

Findings that the controller re-verified against the pinned source and rejected (FR-012).

{{REJECTED_FINDINGS}}

## Deferred minor findings

{{DEFERRED_MINORS}}

## Upstream findings collected

Recorded in `findings/upstream.yaml`; file them with `/guide:upstream`.

{{UPSTREAM_FINDINGS}}

## Validation results

{{VALIDATION}}
