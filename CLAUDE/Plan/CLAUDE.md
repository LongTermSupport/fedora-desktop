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

**Build these on [`_planlib.inc.bash`](_planlib.inc.bash)** — source it and use its primitives
rather than hand-rolling the repo-root walk, the run log, the prompts, the change gate or the
ansible invocation. Rules, bootstrap and skeletons:
[`../PlanScriptStandards.md`](../PlanScriptStandards.md).

> **CORRECTION**: this file previously said *"Resolve the repo root with
> `git rev-parse --show-toplevel`, not a fixed `../` hop."* The second half was right; the
> first half is **wrong** and caused a real failure. `git rev-parse` answers about the
> **cwd**, not the script, so a plan script run by path from another repo resolves to that
> repo — which is exactly what happened to Plan 00068's `triage.bash`. Use the
> script-relative, `.git`-bounded marker walk in
> [`../PlanScriptStandards.md`](../PlanScriptStandards.md) R1 instead; `plan_init` then
> exports `PLAN_REPO_ROOT` and `PLAN_SCRIPT_DIR` for you.

**NO project-root scripts/artifacts that are not persistently relevant and useful.**
Permanent deliverables — a QA gate wired into `qa-all.bash`, the `tests/`
regression suite, reusable `helpers/`, user tools under `files/` — stay in their
normal homes. Plan scaffolding does not. See `CLAUDE/PlanWorkflow.md`
("Plan-Local Scripts & Artifacts — IN STONE") for the full rule and the
transient-vs-persistent test.

## Plan Lifecycle

### 1. Create

- Create folder: `CLAUDE/Plan/NNNNN-description/`
- Write `PLAN.md` with tasks, goals, and status
- Add entry to `README.md` under **Active Plans**

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
