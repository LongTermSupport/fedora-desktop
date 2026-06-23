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
    │   └── assets/                 # Optional diagrams, logs, etc.
    ├── 00049-full-repo-audit/
    │   ├── PLAN.md
    │   ├── triage.md
    │   └── research/
    ├── Completed/                  # Finished plans are moved here
    ├── Archive/                    # Superseded / historical plans
    └── README.md                   # Index of all plans
```

### Plan Numbering — the git counter is authoritative

Plan numbers are **5 digits, zero-padded** (`00049-`, `00050-`). The next number is held
in git config, **not** derived from a folder scan (folder scans miss `Completed/` and
disagree across branches — the `plan_number_helper` handler will block such scans).

**To scaffold a new plan folder**, use the bundled script — it reads the counter
atomically, allocates the next number, and creates the `NNNNN-name/PLAN.md` skeleton:

```bash
CLAUDE/Plan/mkplan.bash "descriptive-kebab-name"
```

**To read the next number only** (without creating a folder):

```bash
git config --local hooksdaemon.latestPlanNumber   # authoritative latest, e.g. 50
```

Add 1 and zero-pad → `00051-description/`. The hooks daemon advances this counter
automatically whenever a plan is created.

---

## Plan Document Structure

Every `PLAN.md` follows this shape. **Do not include effort estimates, durations, target
dates, or a timeline** — the `plan_time_estimates` handler blocks that content in
`CLAUDE/Plan/*.md`. Plans describe *what* and *why*, not *when*.

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

## Notes & Updates

### YYYY-MM-DD
- Progress note / change.
```

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
| `ccy-ctrl-z-patch.js`      | `./scripts/qa-ctrl-z-patch.bash`                   |

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
5. **Complete** — verify all success criteria, confirm QA passes, mark tasks ✅, set the
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
   (ESLint for extension JS, `qa-ctrl-z-patch.bash` for the CCY patch).
6. **Reference the plan** in every related commit for traceability.
7. **Respect the CCY container boundary** — edit and commit only; deploy on the HOST.
8. **Document blockers immediately**; do not silently stop or work around a failure.
