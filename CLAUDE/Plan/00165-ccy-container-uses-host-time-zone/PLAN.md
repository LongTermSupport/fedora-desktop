# Plan 00165: ccy container uses host time zone

**Status**: In Progress
**Created**: 2026-10-09
**Owner**: joseph
**Priority**: Medium

## Overview

The owner reported that a ccy session's status line showed an hour earlier than the
desktop clock. Inside a ccy container `date` prints UTC: the image's `/etc/localtime`
links to `Etc/UTC` and nothing sets `TZ`. The host is in a UK zone, on British Summer
Time. The clock itself is the same (a container shares the host's kernel clock); only
the time zone differs, so the fix is the zone, not the time.

ccy now resolves the host's IANA zone name on the host and passes it to the container as
`TZ`. It asks `timedatectl show -p Timezone --value` first and falls back to the target of
the host's `/etc/localtime` link. The name must have IANA grammar and exist under the
host's `/usr/share/zoneinfo`; if neither source gives such a name, the launch stops with
the reason rather than starting on UTC.

`TZ` was chosen over podman's `--tz=local`: ccy also supports docker, which has no `--tz`,
and `TZ` is overridable by an `export TZ=...` in a project's `ccy.env` / `ccy.env.local`,
which the entrypoint sources after the launcher's environment. The image already carries
`tzdata`, but only because the floating `node:lts-slim` base happens to include it; it is
now installed by name (container 2.49), because without zoneinfo glibc reads an unknown
`TZ` as UTC silently.

## Goals

- Every container ccy starts shows the host's local time.
- An unresolvable host zone stops the launch with a reason; it never falls back to UTC.
- An explicit `TZ` in a project's `ccy.env` or `ccy.env.local` still wins.

## Non-Goals

- Honouring a host shell's own `TZ` export. The desktop clock follows the system zone
  (timedatectl), which is what the owner compares against.
- Changing the clock itself: the container already shares the host's kernel clock.

## Tasks

### Phase 1: Implement

- [x] ✅ **Task 1.1**: `scripts/test-ccy-host-time-zone.bash` first (RED): host zone passed
  through, link fallback, refusals, launcher argv, `ccy.env` / `ccy.env.local` override.
- [x] ✅ **Task 1.2**: `ccy_host_time_zone` in `lib/common-pure.bash`; the launcher
  resolves it and passes `-e "TZ=$CCY_HOST_TZ"` on its single `run`, which every launch
  path shares (interactive, headless, `--teams` seats, `ccy --` passthrough).
- [x] ✅ **Task 1.3**: `tzdata` named in the base Dockerfile; container 2.49; CCY 3.89.0;
  changelog and `docs/ccy.md` row; the test wired into `scripts/qa-all.bash`;
  `deploy.bash` and the `meta-deploy.bash` entry.

### Phase 2: Deploy and verify

- [ ] ⬜ **Task 2.1**: The owner runs `./CLAUDE/Plan/meta-deploy.bash` (this plan's
  `deploy.bash` runs `play-claude-yolo.yml`), then starts a fresh ccy session; the image
  rebuilds once.
- [ ] ⬜ **Task 2.2**: Inside the new session `date` and the status line show the same
  time as the desktop clock.
- [ ] ⬜ **Task 2.3**: qa-reviewer agent over the branch diff.

## Success Criteria

- [ ] `date` inside a fresh ccy session matches the host's desktop clock, zone included.
- [x] `scripts/test-ccy-host-time-zone.bash` passes and is a hard gate in `qa-all.bash`.

## Delivery & Milestones

- Implementation commit on the plan's worktree branch (CCY 3.89.0, container 2.49).
