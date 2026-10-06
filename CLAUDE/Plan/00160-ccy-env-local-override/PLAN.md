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

The owner asked for a tracked, commented `ccy.env.local.dist`, created automatically. It
holds placeholders for overrides specific to one install, never secrets, first of all the
hooks daemon's host role override (`HOOKS_DAEMON_HOSTNAME`). A real `ccy.env.local` says
which dist version it was based on, and is reviewed when the dist moves on. **ccy owns the
dist** (owner, 2026-10-06): `.claude/ccy/` is ccy's, and not every hooks-daemon project is a
ccy project. An install's `ccy.env.local` is placed by that install's own IaC, never by ccy,
the daemon or an agent.

- [x] ✅ **Task 3.1**: ccy's generated `.claude/ccy/.gitignore` lets `ccy.env.local.dist`
  through, an older file is given the exception on launch, and the tracked-files guard
  accepts it; `ccy.env.local` stays ignored and is refused if tracked.
  `scripts/test-ccy-gitignore-safety.bash`, a `qa-all.bash` gate, runs the real check in a
  throwaway repository; it was red on the three dist cases first. CCY 3.81.0.
- [x] ✅ **Task 3.2**: ccy writes the dist on every launch (`lib/common.bash`
  `ccy_env_local_dist_sync`, after the `.gitignore` guard): first line
  `# ccy.env.local.dist version N` (`CCY_ENV_LOCAL_DIST_VERSION`), comments only, rewritten
  when its text differs, left alone (with a warning) when a newer ccy wrote it. A
  `ccy.env.local` whose `# based on ccy.env.local.dist version N` line is older or missing
  draws a launch warning, judged against the newer of ccy's and the dist's version; ccy
  never writes it. A failed write stops the launch. `scripts/test-ccy-env-local-dist.bash`,
  a `qa-all.bash` gate, red first. CCY 3.83.0. `qa-reviewer`: FIX-BEFORE-MERGE, all four
  fixed ([report](subagent-reports/261006-qa-reviewer-dist-opus-5-5.md)). Upstream issue #88 (daemon-owned) withdrawn
  and closed. The prototype install is the hooks daemon's own SDLC runner, whose
  `ccy.env.local` the infra agent places by IaC.
- [ ] ⬜ **Task 3.4**: Review (`qa-reviewer`), deploy through `deploy.bash`, and commit this
  repository's own generated dist after the first launch on 3.83.0.
- [x] ✅ **Task 3.3**: The owner said yes ("Absolutely"): ccy binds an existing
  `ccy.env.local` read-only over the workspace (`ccy_env_local_mount_args`), so a session
  cannot rewrite its own role override. No file, no mount. Tested in the same gate. CCY
  3.84.0. Read-only paths in general, listed in `ccy.env`/`ccy.env.local`, are a separate
  issue (the owner's suggestion).

## Success Criteria

- [ ] A session in a project with `.claude/ccy/ccy.env.local` prints that it sourced it,
  and its values are set in the session.
- [ ] `git status` in that project does not list the file.
- [ ] After a launch on CCY 3.83.0, `.claude/ccy/ccy.env.local.dist` exists, is tracked, and
  names its version; a `ccy.env.local` based on an older one draws the launch warning.

## Delivery & Milestones

- <!-- delivery commit hash -->
