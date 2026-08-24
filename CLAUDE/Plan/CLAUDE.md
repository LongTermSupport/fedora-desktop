# Plan Lifecycle

## Directory Structure

```
CLAUDE/Plan/
  README.md              # Index of all plans (this file's parent)
  CLAUDE.md              # This file - lifecycle instructions
  NNNNN-description/     # Active plans (5-digit zero-padded)
    PLAN.md              # Plan document with tasks and status
    deploy.bash          # Optional: run this plan's Ansible deploy (HOST-only)
    triage.bash          # Optional: verify health (planning stage and/or post-deploy)
    acceptance.bash      # Optional: plan-specific test/acceptance script
  Completed/
    NNNNN-description/   # Completed plans (moved here when done)
```

## Plan-Local Scripts & Artifacts — IN STONE

**Every transient, plan-specific script or artifact lives INSIDE its plan folder,
never at the project root.** This keeps the repo root clean and lets a plan's
tooling travel with it into `Completed/`.

- `deploy.bash` — runs the plan's Ansible command(s) (HOST-only; never run Ansible
  in the CCY container).
- `triage.bash` — confirms things are OK, at planning stage and/or after deploy
  (read-only, re-runnable).
- testing / `acceptance.bash` (and any other plan-specific test/scratch script,
  fixtures, captured logs) — all in the plan folder.

Resolve the repo root with `git rev-parse --show-toplevel`, not a fixed `../` hop.

**NO project-root scripts/artifacts that are not persistently relevant and useful.**
Permanent deliverables — a QA gate wired into `qa-all.bash`, the `tests/`
regression suite, reusable `helpers/`, user tools under `files/` — stay in their
normal homes. Plan scaffolding does not. See `CLAUDE/PlanWorkflow.md`
("Plan-Local Scripts & Artifacts — IN STONE") for the full rule and the
transient-vs-persistent test.

## Plan Lifecycle

### 1. Create

- Run `CLAUDE/Plan/mkplan.bash "descriptive-kebab-name"` — it takes a lock, allocates
  the next number atomically from the git counter, and scaffolds the folder, `PLAN.md`
  and `JOURNAL/`
- Fill in `PLAN.md` with goals, tasks and status
- Add an entry to `README.md` under **Active Plans** (the script does not do this)

**Hand-creating the folder is BLOCKED**, not merely discouraged: `mkdir CLAUDE/Plan/NNNNN-name` is denied by the `plan_number_helper` handler whenever the
scaffolder is deployed. The two paths were never equivalent — `mkdir` claims a number
the moment the folder appears but nothing records the claim until `PLAN.md` is written,
so a second agent reading the counter in that window is handed the **same** number and
the collision surfaces only at the commit gate, once both folders exist.

That is not hypothetical here: **two sessions each created a plan 00082 on 2026-08-24**,
one via `mkplan.bash` and one merged through a PR. The later one to notice renumbered.
Plan 00079 is a second instance, renumbered from 00078 for the same reason — the counter
is `--local` git config and is never pushed, so two clones allocate independently.

### 2. Execute

- Work through tasks
- Update task status in `PLAN.md` as you go
- Reference plan in commits: `Plan NNNNN: Description`

### 3. Complete

When all tasks are done:

1. Update plan status to `Complete` with date
2. Move folder to `CLAUDE/Plan/Completed/NNNNN-description/`
3. Update `README.md`: remove from Active, add to Completed, update stats
4. Commit the move

```bash
git mv CLAUDE/Plan/NNNNN-desc CLAUDE/Plan/Completed/NNNNN-desc
```
