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
- [ ] 🔄 **Task 2.2**: `deploy.bash` runs `play-claude-yolo.yml` (now CCY 3.81.0, Task
  3.1 included). Deployed: meta-deploy `20261006-124250`, failed=0, the image rebuilt.
  OWNER: the check `deploy.bash` prints at its end (a project with a `ccy.env.local`).

### Phase 3: `ccy.env.local.dist`, the tracked template (owner's request)

The owner asked for a tracked, commented `ccy.env.local.dist`, created automatically and
refreshed with each new hooks-daemon version. It holds placeholders for overrides specific
to one install, never secrets, first of all the daemon's host role override
(`HOOKS_DAEMON_HOSTNAME`). A real `ccy.env.local` says which dist version it was based on,
so the agent can review it when the dist moves on. Its content is the daemon's settings and
the daemon already copies files into `.claude/ccy/` on install and upgrade, so the daemon
owns the template. ccy only has to let it be tracked.

- [x] ✅ **Task 3.1**: ccy's generated `.claude/ccy/.gitignore` lets `ccy.env.local.dist`
  through, an older file is given the exception on launch, and the tracked-files guard
  accepts it; `ccy.env.local` stays ignored and is refused if tracked.
  `scripts/test-ccy-gitignore-safety.bash`, a `qa-all.bash` gate, runs the real check in a
  throwaway repository; it was red on the three dist cases first. CCY 3.81.0.
- [ ] ⬜ **Task 3.2**: UPSTREAM: the daemon ships the template, rewrites it on install and
  upgrade with a version marker, and gives a SessionStart advisory when a `ccy.env.local`'s
  "based on" line is older. Filed as claude-code-hooks-daemon issue #88. When it lands,
  upgrade the daemon here and commit the template it writes. The owner's direction
  (2026-10-06, commented on #88): the hooks daemon's own SDLC runner install is the
  prototype, taking its role through `HOOKS_DAEMON_HOSTNAME` in its `ccy.env.local`.
  The file is placed by that install's own IaC, never by the daemon or an agent.
- [ ] 🧑 **Task 3.3**: OWNER decision (the owner: agents "probably shouldn't even be allowed
  to edit it"): ccy mounts an existing `ccy.env.local` read-only over the workspace, so a
  session cannot rewrite its own role override. Recommended. Also suggested upstream on #88:
  a daemon guard that denies agent writes to it.

## Success Criteria

- [ ] A session in a project with `.claude/ccy/ccy.env.local` prints that it sourced it,
  and its values are set in the session.
- [ ] `git status` in that project does not list the file.
- [ ] After a daemon upgrade carrying issue #88, `.claude/ccy/ccy.env.local.dist` exists, is
  tracked, and names the daemon version that wrote it.

## Delivery & Milestones

- <!-- delivery commit hash -->
