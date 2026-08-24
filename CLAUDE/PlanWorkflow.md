# Planning Workflow

This document defines the planning workflow for the **fedora-desktop** repository — a
public Ansible Infrastructure-as-Code project that provisions a Fedora desktop. All
contributors and AI agents follow it so work stays trackable and the plan state never
drifts from the code state.

> **This repo is Ansible + Bash + GNOME-extension JavaScript — not a Python project.**
> The QA gate is `./scripts/qa-all.bash`. There is no `scripts/qa/`, no pytest, no
> coverage gate, and no TDD-handler workflow here. (Those belong to the hooks-daemon
> dependency, not to this repo.)

---

## Core Principles

1. **Plan before execute** — non-trivial work gets a documented plan before code changes.
2. **Break down complexity** — decompose multi-phase work into concrete, testable tasks.
3. **Track everything** — every task has a status; update it in real time.
4. **Document decisions** — capture the rationale for architectural choices.
5. **Plans are living documents** — update them as you learn.
6. **Never let plan state lag code state** — see the [Plan Commit Rule](../CLAUDE.md#plan-commit-rule).
7. **Fail fast** — the project's #1 rule applies to plan work too: surface problems, don't paper over them.

---

## Plan Structure

### Directory Layout

```
CLAUDE/
└── Plan/
    ├── 00035-gh-multi-account-hardening/
    │   ├── PLAN.md                 # Main plan document
    │   ├── {supporting-docs}.md    # Optional analysis / research docs
    │   ├── deploy.bash             # Optional: run the plan's Ansible deploy (HOST)
    │   ├── triage.bash             # Optional: confirm health pre-deploy / post-deploy
    │   ├── acceptance.bash         # Optional: plan-specific test/acceptance script
    │   └── assets/                 # Optional diagrams, logs, etc.
    ├── 00049-full-repo-audit/
    │   ├── PLAN.md
    │   ├── triage.bash
    │   └── research/
    ├── Completed/                  # Finished plans are moved here
    ├── Archive/                    # Superseded / historical plans
    └── README.md                   # Index of all plans
```

### Plan-Local Scripts & Artifacts — IN STONE

> **Every transient, plan-specific script or artifact lives INSIDE its plan
> folder — never at the project root.** A plan's own `deploy.bash`, `triage.bash`,
> test/`acceptance.bash`, fixtures, logs, and scratch files belong in
> `CLAUDE/Plan/NNNNN-name/`, so they travel with the plan into `Completed/` and
> never clutter the repo root.

**Canonical plan-local scripts** (all optional, all fail-fast, all HOST-run where
they deploy or touch the live system — never run Ansible inside the CCY container):

- **`deploy.bash`** — runs the plan's Ansible command(s) (e.g.
  `ansible-playbook playbooks/imports/.../play-foo.yml`). A thin, idempotent wrapper.
- **`triage.bash`** — gathers grounded facts. **Every diagnostic probe belongs
  in this script — never hand the user a one-off command to run in chat.**
  Full reference, patterns and checklist: [PlanTriage.md](PlanTriage.md).
  Run it **at planning stage** (to
  capture the current/broken state) **and/or after `deploy.bash`** (to verify the
  change landed). Read-only / non-destructive; safe to re-run.
- **Testing / `acceptance.bash`** (and any other test script) — plan-specific
  verification that is not a permanent repo gate.

Resolve the repo root with `git rev-parse --show-toplevel` (NOT a fixed `../`
hop) — these scripts sit several directories deep, and the path must survive the
move into `Completed/`.

### All three write their own log — not just `triage.bash`

**Every plan-local script tees its full output into its own plan's `logs/`
directory.** This was written down for `triage.bash` and quietly skipped for the
other two, which left a failed deploy existing only in the operator's terminal
scrollback — so the only way it could reach an agent was by being copy-pasted
back by hand, the precise workflow [PlanTriage.md](PlanTriage.md) exists to
eliminate. A deploy that fails, and an acceptance gate that returns a verdict,
are exactly the output worth keeping.

```bash
# After argument parsing (so `--help` never creates directories), before the work:
PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
mkdir -p "$PLAN_DIR/logs"
LOG="$PLAN_DIR/logs/deploy.log"        # or triage.log / acceptance.log
exec > >(tee "$LOG") 2>&1
echo "Logging this run to: $LOG" >&2
```

- Resolve `PLAN_DIR` from **`BASH_SOURCE[0]`**, never from the repo root, so the
  path keeps working after the plan is archived.
- Use a **fixed filename** so the latest run is always at a predictable path.
- `CLAUDE/Plan/**/logs/` is **gitignored** (`.gitignore`). That combination is
  the whole point: the log sits inside the repo, so it is readable from a CCY
  container at the same repo-relative path with no copy-paste — while never
  reaching this public repo, which matters because deploy and triage output
  names hosts, units, private addresses and home directories.
- Put the block **after** option parsing and any `--list`/`--help` branch, and
  verify that `--help` still works without creating a `logs/` directory.
- The case to check is the **failing** one: a script that writes to both streams
  and then exits non-zero must keep its exit code, and its final `VERDICT:` line
  must still reach the file.

**The dividing line — transient vs. persistent:**

- **Transient (→ plan folder):** anything whose usefulness ends with the plan —
  one-off deploy/triage wrappers, plan acceptance tests, migration scripts,
  captured output, scratch fixtures.
- **Persistent (→ `scripts/`, `tests/`, `helpers/`, etc.):** anything that stays
  useful after the plan ships — a permanent QA gate wired into `qa-all.bash`, the
  regression unit suite under `tests/`, a reusable helper under `helpers/`, a
  user-facing tool under `files/`. These are deliverables, not plan scaffolding.

> **Rule: NO project-root-level scripts or artifacts that are not persistently
> relevant and useful.** If it only matters while this plan is in flight, it goes
> in the plan folder. When in doubt, ask: *"after this plan is Complete and moved
> to `Completed/`, would anyone still run this from the repo root?"* — if no, it is
> plan-local.

(Plan-folder `*.bash` is still discovered and linted by `qa-bash.bash` /
`qa-all.bash` — being plan-local does not exempt a script from QA.)

### Plan Numbering — the git counter is authoritative

Plan numbers are **5 digits, zero-padded** (`00049-`, `00050-`). The next number is held
in git config, **not** derived from a folder scan (folder scans miss `Completed/` and
disagree across branches — the `plan_number_helper` handler will block such scans).

**To scaffold a new plan folder**, use the bundled script — it reads the counter
atomically, allocates the next number, and creates the `NNNNN-name/PLAN.md` skeleton:

```bash
CLAUDE/Plan/mkplan.bash "descriptive-kebab-name"
```

The `PLAN.md` skeleton is rendered from the tracked, project-owned template
`CLAUDE/Plan/_TEMPLATE_.md` (placeholders `{{PLAN_NUMBER}}`, `{{PLAN_TITLE}}`,
`{{CREATED_DATE}}`, `{{OWNER}}`) — customise that file freely; it is seeded from the
daemon default when missing and never overwritten on upgrade. The script's built-in
skeleton is only a fallback for when no `_TEMPLATE_.md` exists.

**To read the next number only** (without creating a folder):

```bash
git config --local hooksdaemon.latestPlanNumber   # authoritative latest, e.g. 50
```

Add 1 and zero-pad → `00051-description/`. The hooks daemon advances this counter
automatically whenever a plan is created.

---

## Plan Document Structure

Every `PLAN.md` follows this shape. **Do not include effort estimates, durations, target
dates, or a timeline** — the `plan_time_estimates` handler blocks that content in plan
documents. Plans describe *what* and *why*, not *when*.

**Journal day-files are exempt** from that rule. A journal records what *did* happen, so
an elapsed duration there is a historical fact, not a forward estimate — write it freely.
The exemption is by location as well as name: anything under a `JOURNAL/` directory is
journal territory, whatever its filename.

### PLAN.md has enforced size limits

A `PLAN.md` is re-read in full at the start of every session that touches the plan, so its
size is a **recurring context cost**. The daemon enforces tiers on the plan document only:
advisory past ~18 KB or 350 lines, a louder warning past ~25 KB or 500 lines, and edits are
**blocked** past ~35 KB or 900 lines. Only an edit that *grows* an oversized file is
blocked — shrinking is silent, and ticking a checkbox merely advises, so an over-long plan
can always be refactored down.

`JOURNAL/` day-files and this directory's `README.md` index are exempt at any size.

**Deleting content is not the remedy.** There are exactly two:

1. **Relocate** the narrative into the plan's `JOURNAL/` day-file — append-only and
   unbounded by design. This is almost always the right move.
2. **Split the plan** if the task tree itself is the bulk. An over-scoped plan is not fixed
   by better journalling.

A commit that shrinks a `PLAN.md` by 2,000+ bytes without staging a journal entry is
flagged, because that shape usually means content was destroyed rather than moved.

```markdown
# Plan XXXXX: [Plan Title]

**Status**: Not Started | In Progress | Complete | Blocked | Cancelled
**Created**: YYYY-MM-DD
**Owner**: [Name/Agent]
**Priority**: High | Medium | Low

## Overview

[2–3 paragraphs: what this plan achieves and why.]

## Goals

- Clear, measurable goal 1
- Clear, measurable goal 2

## Non-Goals

- Explicitly what this plan will NOT do (scope control)

## Context & Background

[Relevant background, prior decisions, links to supporting docs.]

## Tasks

### Phase 1: [Phase Name]

- [ ] ⬜ **Task 1.1**: Description
  - [ ] ⬜ Subtask 1.1.1
- [ ] ⬜ **Task 1.2**: Description

### Phase 2: [Phase Name]

- [ ] ⬜ **Task 2.1**: Description

## Dependencies

- Depends on: Plan 00048 (Complete)
- Blocks: Plan 00050 (Not Started)

## Technical Decisions

### Decision 1: [Title]
**Context**: why a decision is needed
**Options considered**: A (pros/cons), B (pros/cons)
**Decision**: chose A because […]
**Date**: YYYY-MM-DD

## Success Criteria

- [ ] Criterion 1
- [ ] QA passes (`./scripts/qa-all.bash`)

## Risks & Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| …    | H/M/L  | H/M/L       | …          |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/NNNNN-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Milestone or delivery commit hash
```

**The blow-by-blow activity stream lives in `JOURNAL/`, not in `PLAN.md`.** Each
plan folder carries a `JOURNAL/` of append-only, per-day
`NNNNN-Journal-YY-MM-DD.md` files (findings, dead-ends, in-flight decisions,
hand-off state). `PLAN.md` keeps only the thin, curated `## Delivery & Milestones`
stub above. `mkplan.bash` scaffolds `JOURNAL/` with a seeded day-1 file. See
[CLAUDE/PlanJournalling.md](PlanJournalling.md) for the entry grammar and the
append-only discipline. Legacy plans that still carry `## Notes & Updates` are
never rewritten — the new structure applies to new material.

---

## Task Status System

| Status       | Icon | Meaning                                |
| ------------ | ---- | -------------------------------------- |
| Not Started  | ⬜   | Not yet begun                          |
| In Progress  | 🔄   | Currently being worked on              |
| Completed    | ✅   | Finished **and verified** (QA passing) |
| Blocked      | 🚫   | Cannot proceed (note why)              |
| Cancelled    | ❌   | No longer needed                       |
| On Hold      | ⏸️   | Paused                                 |
| Needs Review | 👁️   | Done, awaiting review                  |

Rules:

1. **Limit work in progress** — 1–2 tasks marked 🔄 at a time.
2. **Update immediately** — change status the moment state changes.
3. **Document blocks** — when marking 🚫, add a note explaining why.
4. **Only mark ✅ after verification** — relevant QA must pass first.

---

## QA Before Completion

**Before marking any task ✅ or committing**, run the project QA gate. The required
command depends on what you changed (see `CLAUDE/QA.md` for the authoritative reference):

| Changed files              | QA command                                         |
| -------------------------- | -------------------------------------------------- |
| Bash / Python / Ansible    | `./scripts/qa-all.bash`                            |
| GNOME-extension JavaScript | `cd extensions && node_modules/.bin/eslint <file>` |

`qa-all.bash` runs six stages (bash syntax + shellcheck, Python compile + ruff, semgrep
patterns, Ansible fail-fast grep, `ansible-playbook --syntax-check`, JS `node --check` +
ESLint). A missing **required** tool is a hard failure, in line with the fail-fast rule —
do not engineer around it; fix the dependency in IaC.

Include the QA step explicitly as a task subitem:

```markdown
- [ ] ⬜ **Implement feature X**
  - [ ] ⬜ Edit the playbook / script / extension
  - [ ] ⬜ Run QA: `./scripts/qa-all.bash`
  - [ ] ⬜ Fix any findings
  - [ ] ⬜ (On HOST, not in CCY container) deploy + test the playbook result
```

> **CCY container reminder**: if the project path is `/workspace/`, you are in a CCY
> container — **edit and commit only, never run Ansible playbooks**. Deployment and
> live testing happen on the HOST. See `CLAUDE/ContainerRules.md`.

---

## TodoWrite vs Plans

**Use TodoWrite** for very small, low-risk, single-session work with no architectural
decisions.

**Create a Plan** for medium+ work, anything with risk, multi-phase work, architectural
or design decisions, or work that may be resumed later or needs to be understood by
others.

If a TodoWrite list grows past ~5 items or spans multiple sessions, promote it to a
proper plan, migrate the tasks, and reference the plan in your work.

---

## Workflow Steps

1. **Identify work** — does it fit an existing plan? If not, and it is non-trivial,
   create a new plan.
2. **Create the plan** — run `CLAUDE/Plan/mkplan.bash "descriptive-kebab-name"` to
   scaffold `NNNNN-name/PLAN.md`; add an entry to `CLAUDE/Plan/README.md`.
3. **Break down tasks** — phases → concrete, testable tasks. A good task names a single
   verifiable outcome ("Make `play-docker.yml` rootful and import it from
   `playbook-main.yml`"), not "work on Docker".
4. **Execute** — mark the plan 🔄, work tasks in order, update status in real time, run
   QA before each commit, and reference the plan in commit messages.
5. **Review** — **run the `qa-reviewer` agent over the plan's full diff. This is a
   required final step, not an optional extra.** `qa-all.bash` is mechanical and passes
   green on work that is structurally wrong — misplaced in the IaC graph, a new playbook
   that should have been an edit, a missing version bump, plan/docs drift, a self-test
   that does not exercise what it vouches for. Resolve every BLOCK and
   FIX-BEFORE-MERGE finding before step 6. See `CLAUDE/QA.md`.
6. **Complete** — verify all success criteria, confirm QA passes, mark tasks ✅, set the
   plan to Complete with a completion date, and move the folder to `Completed/` when
   appropriate.

---

## Git Integration

### When to commit plans

Per the [Plan Commit Rule](../CLAUDE.md#plan-commit-rule), never let plan state lag the
code it tracks.

- **Plan + code in one commit** (preferred when both change together):
  ```bash
  git add CLAUDE/Plan/00049-full-repo-audit/PLAN.md playbooks/imports/play-docker.yml
  git commit -m "Plan 00049: make Docker rootful and core"
  ```
- **Plan-only commit** (encouraged for new plans, research, decision notes, status
  updates):
  ```bash
  git add CLAUDE/Plan/00049-full-repo-audit/
  git commit -m "Plan 00049: add documentation-drift research"
  ```

**Prohibited**: committing code that completes plan tasks while leaving the plan file
unchanged on disk.

### Commit message reference

```
Plan 00049: realign documentation with the deployed playbooks

- Fix Docker rootless→rootful drift across the user docs
- Rewrite PlanWorkflow.md for this repo

Refs: CLAUDE/Plan/00049-full-repo-audit
```

### Quick pre-commit check

```bash
git status                              # look for untracked CLAUDE/Plan/ dirs + unstaged plan edits
./scripts/qa-all.bash                   # for Bash / Python / Ansible changes
git add CLAUDE/Plan/NNNNN-description/  # stage the plan alongside related code
```

---

## Plan Index

Keep `CLAUDE/Plan/README.md` current as the index of active, completed, blocked, and
cancelled plans.

---

## AI Agent Guidelines

1. **Check for an existing plan** before starting work.
2. **Create a plan** for any work that is non-trivial or multi-session.
3. **Use `CLAUDE/Plan/mkplan.bash "name"` to create plans** — it reads the git counter
   atomically and scaffolds the folder. Never derive the number from a folder scan.
4. **Update task status in real time** as you work.
5. **Run `./scripts/qa-all.bash` before commits** that touch Bash/Python/Ansible
   (ESLint for extension JS).
6. **Run the `qa-reviewer` agent before marking a plan Complete** — required, and the
   only gate that catches structural/convention defects. When something gets past it,
   add that case to `.claude/agents/qa-reviewer.md` rather than only fixing the instance.
7. **Reference the plan** in every related commit for traceability.
8. **Respect the CCY container boundary** — edit and commit only; deploy on the HOST.
9. **Document blockers immediately**; do not silently stop or work around a failure.
