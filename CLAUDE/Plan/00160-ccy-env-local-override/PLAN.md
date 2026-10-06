# Plan 00160: ccy env local override

**Status**: In Progress
**Created**: 2026-10-06
**Owner**: joseph
**Priority**: Medium

## Overview

A project's ccy settings live in the tracked `.claude/ccy/ccy.env`, which the entrypoint
sources inside the container before `claude` runs. A setting that belongs to one checkout
only (one machine, one person's preference, a value that must not be committed) has no
home: editing `ccy.env` changes it for everyone who clones the project.

The owner asked for `ccy.env.local`: an untracked file beside `ccy.env`, sourced right
after it, so a checkout can add to or override the project's settings without a commit.
`.claude/ccy/.gitignore` ignores everything not on its allow-list, so the file is already
kept out of git.

The owner said a GitHub issue asks for this; none was found on 2026-10-06 in any
repository the agent can search, so this plan is written from the owner's spoken request.

## Goals

- The entrypoint sources `.claude/ccy/ccy.env.local` when present, after `ccy.env`, in
  the container only, never on the host.
- Its values win over `ccy.env`'s, and it can still use `${VAR:-default}` to let a host
  setting win.
- `docs/ccy.md` documents the file, its order, and that it is never committed.

## Non-Goals

- A local override for `mounts` or `allowed-hostnames`. Those are read on the host and
  are not asked for.
- Sourcing anything on the host.

## Tasks

### Phase 1: Build

- [x] ✅ **Task 1.1**: `scripts/test-ccy-project-env.bash`, a `qa-all.bash` gate, cuts the
  step out of the real entrypoint (between its `PROJECT-ENV` markers) and runs it against a
  throwaway project: neither file, each alone, both, the order, and that `.local` wins and
  can read `ccy.env`'s values. Red first (no block in the entrypoint).
- [x] ✅ **Task 1.2**: The entrypoint sources `ccy.env.local` after `ccy.env`, at top level so
  a `declare` stays global. CCY 3.80.0, container 2.43; changelog entry.
- [x] ✅ **Task 1.3**: `docs/ccy.md` (the start-up step, the layout, the `ccy.env` section),
  `CLAUDE/ContainerRules.md` (wrapper precedence) and the `--supervise` help text.

### Phase 2: Review and deploy

- [x] ✅ **Task 2.1**: `qa-all.bash` green; `qa-reviewer`: no blocker. Fixed: two error
  messages named the wrong file after the loop, the missing `QA.md` row, stale comments, and
  a `declare` test case
  ([report](subagent-reports/261006-qa-reviewer-opus-5-5.md)).
- [ ] 🔄 **Task 2.2**: `deploy.bash` runs `play-claude-yolo.yml`, and is in
  `meta-deploy.bash`. The next ccy session rebuilds the image once. OWNER: run meta-deploy,
  then the check `deploy.bash` prints at its end.

## Success Criteria

- [ ] A session in a project with `.claude/ccy/ccy.env.local` prints that it sourced it,
  and its values are set in the session.
- [ ] `git status` in that project does not list the file.

## Delivery & Milestones

- <!-- delivery commit hash -->
