# Plan 00099: rclone RC auth broke every un-migrated RC client

**Status**: In Progress
**Created**: 2026-08-16
**Owner**: Claude (agent)
**Priority**: High

## Overview

Plan 00094 authenticated the rclone remote-control API: the mount units dropped
`--rc-no-auth` and gained `--rc-user`/`--rc-pass` via a systemd `EnvironmentFile`.
That change was correct, but it was landed on a **false premise** — that
`vfs/stats` and `core/stats` "answer unauthenticated" and so only the
`vfs/refresh` caller needed migrating. rclone in fact gates **every** RC endpoint
once a credential is configured.

The result is a silent, delayed breakage of every RC client that was not
migrated. `ftp-camera --copy` / `--async-copy` fail their preflight and refuse to
run at all; `rclone-cache-status` reports `rc unreachable`; `rclone-tail` cannot
poll. A second, independent failure compounds it: the one client Plan 00094 *did*
fix in the repo (`ftp-camera`) was never **deployed**, because 00094's
`deploy.bash` runs only `play-rclone.yml` while that script is deployed by
`play-ftp-camera.yml`.

This plan migrates every RC client to a single shared credential helper, kills
the false premise wherever it is written down, and adds a gate that fails when a
repo-owned script drifts from its deployed copy.

## Goals

- Every `rclone rc` caller in the repo authenticates, via one shared source of
  truth rather than three hand-copied blocks.
- `ftp-camera --copy`, `rclone-cache-status` and `rclone-tail` work against the
  authenticated mount.
- The "stats endpoints answer unauthenticated" claim is removed from every
  comment and doc that repeats it.
- A repo-owned script changing without its play being re-run is caught, not
  discovered weeks later by a failing camera session.

## Non-Goals

- Revisiting the decision to authenticate the RC. Plan 00094's conclusion stands;
  only its incomplete rollout is in scope.
- Changing the mount unit, its cache sizing, or the VFS write-back tuning.
- Any change to the FTP/vsftpd or hotspot side of `ftp-camera`.

## Context & Background

Confirmed facts (source: `triage.bash`, run on the host 2026-08-16):

| ID  | Fact                                                                                                  | Source                          |
| --- | ----------------------------------------------------------------------------------------------------- | ------------------------------- |
| F1  | Unauthenticated `core/stats` against the live mount returns **HTTP 401**                              | `rclone rc --url=…` probe       |
| F2  | Authenticated `core/stats` and `vfs/stats` both return 200 — the credential itself is fine            | same probe with `--user/--pass` |
| F3  | Deployed `~/.local/bin/ftp-camera` is the **pre-00094** build; repo copy has the credential block     | `diff` deployed vs repo         |
| F4  | `rclone-cache-status` and `rclone-tail` have **no** credential handling in the repo at all            | `grep 'rclone rc '`             |
| F5  | `rclone-cache-status` on the live host prints `error: rc unreachable`                                 | direct run                      |
| F6  | The mount, credential file and RC endpoint are all healthy — nothing is wrong with 00094's deployment | unit status, `vfs/stats`        |

F1 refutes the premise written into `ftp-camera` and `rclone-cache-warm`, and
into Plan 00094's own notes.

## Tasks

### Phase 1: Establish and record the facts

- [x] ✅ **Task 1.1**: Write `triage.bash` covering RC reachability with and
  without credentials, deployed-vs-repo drift for every rclone helper, and mount
  health
- [x] ✅ **Task 1.2**: Run it on the host and confirm F1–F6

### Phase 2: One credential helper, every client

- [x] ✅ **Task 2.1**: Add a sourced library `files/home/.local/bin/rclone-rc-auth.bash`
  exposing the credential lookup and a `rclone_rc` wrapper
  - [x] ✅ Fails with a play-naming message when the credential file is absent or
    incomplete — never degrades to an unauthenticated call
  - [x] ✅ Passes credentials via `RCLONE_USER`/`RCLONE_PASS`, keeping the
    password out of `ps` output (verified against rclone 1.74.3 on the host)
- [x] ✅ **Task 2.2**: Deploy the library from `play-rclone.yml`
- [x] ✅ **Task 2.3**: Migrate `rclone-cache-status` to it
- [x] ✅ **Task 2.4**: Migrate `rclone-tail` to it
- [x] ✅ **Task 2.5**: Migrate `rclone-cache-warm` to it (replaces its inline block)
- [x] ✅ **Task 2.6**: Migrate `ftp-camera` to it (replaces its inline block)

### Phase 3: Kill the false premise

- [x] ✅ **Task 3.1**: Correct the "stats answer unauthenticated" comments in
  `ftp-camera` and `rclone-cache-warm`
- [x] ✅ **Task 3.2**: Correct the same claim in `play-rclone.yml` (no `docs/`
  page repeats it — checked)
- [x] ✅ **Task 3.3**: Append a correction to Plan 00094's journal recording that
  its premise was wrong and which plan fixed it

### Phase 4: Make the drift impossible to miss

- [x] ✅ **Task 4.1**: Add `scripts/qa-deployed-drift.bash` — every repo-owned
  `files/home/.local/bin/` script with a deployed copy must be byte-identical
  to it; names the owning play, derived by searching the playbooks rather than
  from a hand-maintained table
  - [x] ✅ Skips cleanly in the CCY container and in a clean CI checkout
- [x] ✅ **Task 4.2**: Wire it into `scripts/qa-all.bash`
- [x] ✅ **Task 4.3**: Resolve the second instance it found on its first run —
  `reclaim` v1.0.3 in the repo vs v1.0.1 deployed (Plan 00062), now deployed

### Phase 5: Verify

- [x] ✅ **Task 5.1**: Write `acceptance.bash` — exercises the DEPLOYED scripts,
  not the repo copies, since a source-tree gate is what missed this last time
- [x] ✅ **Task 5.2**: Confirm it FAILS before deploy — 9 failed / 1 passed, and
  the single pass was "RC rejects unauthenticated calls", i.e. exactly the
  broken state the host was in
- [x] ✅ **Task 5.3**: Run `deploy.bash` on the host (both plays, clean)
- [x] ✅ **Task 5.4**: Confirm `acceptance.bash` passes after deploy — ACCEPTED 8/8
- [x] ✅ **Task 5.5**: Run `./scripts/qa-all.bash` — passed, 441 files
- [ ] 🔄 **Task 5.6**: Run the `qa-reviewer` agent over the full diff

## Dependencies

- Depends on: Plan 00094 (Complete) — this plan repairs its incomplete rollout

## Technical Decisions

### Decision 1: One sourced library, not a fourth copy of the block

**Context**: Four scripts need the same credential lookup. Three currently
disagree about whether they need it at all.
**Options considered**: (a) paste the block into the two missing scripts —
smallest diff, but leaves four copies of a rule that has already been got wrong
once; (b) a sourced `.bash` library — one source of truth, and the next RC client
inherits it for free.
**Decision**: (b). The defect this plan fixes *is* the divergence; adding a
fourth copy would reproduce its cause.
**Date**: 2026-08-16

### Decision 2: Missing credentials must fail, never fall back

**Context**: `ftp-camera` currently builds an empty `rc_auth` array when the
credential file is unreadable, then calls the RC anyway.
**Options considered**: (a) keep the silent fallback for pre-00094 mounts;
(b) fail with the play to run.
**Decision**: (b). The fallback is exactly the shape that turned a 401 into the
misleading "remote control not responding" message. An un-credentialled mount is
an IaC gap, so it fails fast and names the play.
**Date**: 2026-08-16

## Success Criteria

- [x] `ftp-camera`'s copy preflight authenticates against the mount (the step
  that was refusing to run). A full `--copy` is deliberately not run by this
  plan — it `cp -r`s the whole 780-file tree, so re-shipping is the user's call
- [x] `rclone-cache-status` and `rclone-tail` both report live figures
- [x] No repo-owned helper differs from its deployed copy
- [x] No comment or doc claims the stats endpoints are unauthenticated
- [x] `acceptance.bash` fails pre-deploy and passes post-deploy
- [x] QA passes (`./scripts/qa-all.bash`)
- [ ] `qa-reviewer` returns PASS

## Known, Out of Scope

Found while working, deliberately not addressed here:

- **82 files under `photos/2026/08/15` are local-only** (0 on the remote; every
  earlier day is fully mirrored). `ftp-camera --push` ships only the missing
  ones; `--copy` would re-upload all 780. A bandwidth decision for the user.
- **The VFS cache is at 400GB/400GB (99%)** with `max-age 365d`. Not implicated
  in this defect; flagged because it was visible throughout.
- **Plan 00094's archived `deploy.bash` keeps the loose `pgrep -af 'ftp-camera'`
  guard**, which false-positives on any process merely mentioning the name. Not
  edited — that plan is closed and its script will not run again. This plan's
  copy anchors the pattern.

## Risks & Mitigations

| Risk                                                                | Impact | Probability | Mitigation                                                                                                                                                                              |
| ------------------------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Deploying restarts the mount and interrupts an in-flight write-back | H      | L           | `deploy.bash` refuses while an `ftp-camera` process is running, as 00094's did                                                                                                          |
| The drift gate false-positives in CCY, blocking every commit        | M      | M           | Gate skips when no deployed copy exists; container has none                                                                                                                             |
| A fifth RC client exists that triage did not find                   | M      | L           | `acceptance.bash` check 3 greps every deployed `rclone-*` / `ftp-camera` for a bypassing `rclone rc` call. NOT a whole-repo gate — a new client added outside that glob would be missed |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only. Blow-by-blow lives in JOURNAL/. -->

- Plan opened; root cause confirmed by triage (F1–F6)
