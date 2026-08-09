# Plan QA

Lint and sweep the plan tree. Plan QA enforces plan hygiene at three stages
(edit-time lint, commit gate, session sweep); this page covers running those
same checks **on demand** from the CLI.

All commands go through the deployed wrapper, which resolves the daemon's venv
for you:

```bash
.claude/hooks-daemon/bin/hooks-daemon plan-qa <mode>
```

> In self-install mode (the daemon's own repo) the wrapper is at
> `./bin/hooks-daemon` instead. Everything else is identical.

## Modes

### Sweep the whole tree

```bash
.claude/hooks-daemon/bin/hooks-daemon plan-qa --sweep
```

Checks index/folder bijection, number collisions, statistics recount, archive
structure, status-vs-location coherence and staleness across every plan.

**Exit 1 while findings remain**, so this is CI-able.

### Lint one plan document

```bash
.claude/hooks-daemon/bin/hooks-daemon plan-qa --lint CLAUDE/Plan/00042-thing/PLAN.md
```

Runs the edit-stage rules against a single file: status line present and valid,
header/body coherence, task grammar, and the `plan-doc-size` tiers.

### Check the staged tree before committing

```bash
.claude/hooks-daemon/bin/hooks-daemon plan-qa --check-staged
```

Runs the cross-file commit-gate invariants against what is **staged** — a new
plan folder stages its README row in the same commit, a terminal-status flip
ships the `git mv` plus README/statistics update atomically, and so on.

Run this before `git commit` to see what the gate will say.

### Machine-readable output

Add `--json` to any mode:

```bash
.claude/hooks-daemon/bin/hooks-daemon plan-qa --sweep --json
```

## When it says nothing

```
Plan QA: 0 findings — plan tree is clean.
```

If it reports `plan workflow is disabled in config — nothing to check`, plan QA
is off for this project. Enable it under `plan_workflow` in
`.claude/hooks-daemon.yaml`.

## Policy

All thresholds and allowlists live under `plan_workflow.qa` in
`.claude/hooks-daemon.yaml` — archive directory names, the staleness window, the
legacy-plan allowlist (grandfathered plans only ever advise) and the
number-collision allowlist. One policy block is shared by the CLI and all three
enforcement stages.

See `docs/guides/HANDLER_REFERENCE.md` for the full option list.

## Troubleshooting

If the command appears to do nothing or the wrapper is missing, see
[references/troubleshooting.md](references/troubleshooting.md). A missing
`bin/hooks-daemon` means the install or upgrade did not complete — re-run
`/hooks-daemon upgrade`.
