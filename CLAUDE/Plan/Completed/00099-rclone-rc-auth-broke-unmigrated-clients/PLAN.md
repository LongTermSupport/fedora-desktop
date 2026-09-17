# Plan 00099: rclone RC auth broke every un-migrated RC client

**Status**: Complete (2026-09-17)
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

- [x] ✅ **Task 5.4**: Confirm `acceptance.bash` passes after deploy — ACCEPTED 10/10.
  The gate grew checks `[0]` and `[6b]` between the pre-deploy run above and this
  one, which is why the two totals do not reconcile against each other; the
  COVERAGE line the gate now prints exists so a reader never has to work that
  out from a bare pass count again

- [x] ✅ **Task 5.5**: Run `./scripts/qa-all.bash` — passed, 441 files

- [x] ✅ **Task 5.6**: Run the `qa-reviewer` agent over the full diff — FIX-BEFORE-MERGE
  (0 blocking, 3 should-fix, 8 minor, 3 nits); report in
  `subagent-reports/260916-qa-reviewer-opus-5.md`. Every finding actioned except
  `m6`: converting the plan scripts onto `_planlib.inc.bash` requires `plan_require_host`
  on `acceptance.bash`, which would make the COVERAGE harness unrunnable. The owner's call
  — see the 14:28 handoff entry in `JOURNAL/00099-Journal-26-09-16.md`.

- [x] ✅ **Task 5.8**: Closing `qa-reviewer` round — FIX-BEFORE-MERGE (0 blocking,
  5 should-fix); report in `subagent-reports/260916-qa-reviewer-close-opus-5.md`.
  All five actioned, and three were this plan's own defect class recurring one
  level up:

  - **S1** — check [7] read `qa-deployed-drift.bash`'s documented SKIP as
    `PASS repo and host are in sync`, because all three skip paths exit 0. That is
    the `m7` fix (`✓`→`⚠`) being laundered straight back out by the consumer, and
    it fires **on the host** inside a linked worktree. Now matched on the output
    and failed
  - **S2** — check [3] passed having examined **zero** files when the glob matched
    nothing. Counted and stated now; zero fails
  - **S3** — `ftp-camera` was the last client still hardcoding `localhost:5572`,
    which stopped being the address when `play-rclone.yml` moved to
    `rc_port_base + mount_index`. `m4`'s premise that every client discovered the
    address was simply wrong, and the `m4` fix made check [6] probe a *different*
    endpoint from the client — so the gate stopped touching the thing it vouched
    for. Discovery now lives in the shared library as
    `rclone_rc_addr_for_mount`, used by `ftp-camera` and by `triage.bash`, which
    carried the same hardcoded address
  - **S4** — the plan index still claimed "ACCEPTED 10/10 on the host",
    contradicting this file
  - **S5** — `m6` re-judged, and the owner trade-off holds for `acceptance.bash`
    only. `deploy.bash` ran `ansible-playbook` **without `cd`-ing to the repo
    root**, and every path in `ansible.cfg` is relative — inventory, `roles_path`,
    the vault setting, and `callback_plugins` (Plan 00109's ledger). The host run
    worked because the operator happened to be standing at the root. Both
    `deploy.bash` and `triage.bash` are now on `_planlib.inc.bash`;
    `acceptance.bash` deliberately is not, and says why on the line

- [x] ✅ **Task 5.7**: Deployed on the HOST — `play-rclone.yml` and
  `play-ftp-camera.yml`, via `deploy.bash`, then `acceptance.bash`. Checks 0–6b all
  PASS against a live mount: no deployed client calls `rclone rc` directly, and the
  authenticated `core/stats` and `vfs/refresh` calls both succeed. `COVERAGE: 9 of 9`,
  so the run is a complete one and the PASSes mean what they say. Superseded as
  evidence by Task 5.9 — the deployed files have changed since

- [x] ✅ **Task 5.9 — RE-DEPLOY, because Tasks 5.8 and 5.10's fixes changed deployed
  files. DONE** by the 2026-09-17 batch run: `deploy.bash` then `acceptance.bash`,
  ACCEPTED on `COVERAGE: 9 of 9`, 0 failed, checks [6] and [7] both passing — so the
  drift this task existed to close is closed. See the JOURNAL day-file. `ftp-camera` and `rclone-rc-auth.bash` both changed after Task 5.7's host
  run, so the host is running a build this repo no longer contains. That is the drift
  this plan's own gate exists to catch, and leaving 5.7 ticked without saying so
  would be the Plan 00094 failure repeated by this plan. Run `deploy.bash` then
  `acceptance.bash` again — or `untracked/meta-deploy.bash`, which runs this
  alongside every other waiting plan

- [x] ✅ **Task 5.10**: Closing `qa-reviewer` rounds 2–6 — BLOCK, then FIX-BEFORE-MERGE four
  times. Each fix is mutation-tested with a control, in `falsification/` (see its README).
  One thread runs through all five: a check that reads clean whether or not it looked.
  Rounds 2–4, the gate approximated `ftp-camera --copy`'s input and each approximation was
  defeated by a different normalisation; ended by deleting the stand-in. Rounds 5 and 6, a
  call to an undefined `note` and a `grep` assignment that killed the run before the guard
  written for that case — both on branches nothing had ever executed. Round by round:
  [JOURNAL/](JOURNAL/); reviews in `subagent-reports/`.

  **"Every finding actioned" was wrong when this said it.** Carried: `m6` from Task 5.6
  (owner's call). Round-5 nits 8–10 went unactioned and unmentioned until round 6 named
  them; all three are done now.

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
  plan — it `cp -r`s the whole 780-file tree, so re-shipping is the user's call.
  Ticked by Task 5.9's host re-run: check [6] passed against the address the client
  itself resolves
- [x] `rclone-cache-status` and `rclone-tail` both report live figures
- [x] No helper **this plan owns** differs from its deployed copy. The criterion as
  first written said "no repo-owned helper", which is a whole-host claim this plan
  cannot make true: the two that differ belong to `play-lxcfreeze.yml` and
  `play-podfreeze.yml`, which is the gate doing its job on work outside this plan.
  Ticked by Task 5.9's host re-run: check [7] passed with zero drift
- [x] No comment or doc claims the stats endpoints are unauthenticated
- [x] `acceptance.bash` fails pre-deploy, and post-deploy every check in this plan's
  scope passes — 0–6b, `COVERAGE: 9 of 9`. Ticked by Task 5.9's host re-run, whose
  overall verdict was ACCEPTED, not merely "REJECTED on out-of-scope files"
- [x] QA passes (`./scripts/qa-all.bash`)
- [x] `qa-reviewer` returns PASS — round 8, which verified fixes 2 and 3 against the
  code (running `falsify-round5-note.bash` to drive the differing-address branch and
  watch the COVERAGE CORRECTION lines print) and named one stale sentence as the only
  thing between it and PASS. That sentence is fixed. Report in `subagent-reports/`

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
- **Two deployed scripts this plan does not own WERE drifted on the host, and no
  longer are.** An earlier acceptance run's check [7] rejected on `2 of 74`:
  `lxcfreeze` and `files/home/.local/lib/freeze/freeze-common.bash`, owned by
  `play-lxcfreeze.yml` and `play-podfreeze.yml`. Neither is one of this plan's five
  files. The 2026-09-17 host run's check [7] passed with zero drift, so whatever ran
  those plays in the meantime cleared it. Kept as a record because this bullet was
  the stated reason a success criterion could not be ticked, and deleting it would
  leave that history unexplained. The gate remains whole-host by design, so it will
  reject again for any repo/host disagreement — including one this plan did not cause.

## Risks & Mitigations

| Risk                                                                | Impact | Probability | Mitigation                                                                                                                                                                              |
| ------------------------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Deploying restarts the mount and interrupts an in-flight write-back | H      | L           | `deploy.bash` refuses while an `ftp-camera` process is running, as 00094's did                                                                                                          |
| The drift gate false-positives in CCY, blocking every commit        | M      | M           | Gate skips when no deployed copy exists; container has none                                                                                                                             |
| A fifth RC client exists that triage did not find                   | M      | L           | `acceptance.bash` check 3 greps every deployed `rclone-*` / `ftp-camera` for a bypassing `rclone rc` call. NOT a whole-repo gate — a new client added outside that glob would be missed |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only. Blow-by-blow lives in JOURNAL/. -->

- Plan opened; root cause confirmed by triage (F1–F6)

- **Delivery: `942fb724`** — the library, all four migrated clients, both plays, the drift
  gate and its `qa-all.bash` wiring, in one commit.
  **Its message says "Plan 00072", and that is not a mislabel of another plan**: this plan
  was created as 00072, collided with the real Plan 00072
  (`ccy-assert-rootless-engine`), and was renumbered afterwards — the folder it created
  carried a `00072-Journal-…` file. `git log --grep=00099` therefore finds nothing, which
  is exactly how Task 5.6 nearly failed to locate its own diff. Recorded here so the next
  reader does not repeat the search.

- `scripts/qa-deployed-drift.bash` has since been edited by Plans 00081, 00110 and 00122 —
  this plan owns its introduction, not its current state

- **The delivery is not one commit, and treating it as one is what made the host go
  stale.** Task 5.7's host run was at 15:41; every commit after it that touched a deployed
  file leaves the host behind again, which is why Task 5.9 exists and why success criteria
  1, 3 and 5 are unticked. **The list is not written out here.** It was, twice, and was
  wrong both times — it said "three" when there were five, and the commit that corrected
  it did not add itself. Derive it instead:

  ```bash
  git log --format='%h %ad %s' --date=format:'%m-%d %H:%M' 942fb724..HEAD -- files/home/.local/bin/
  ```

  Anything in that output dated after 09-16 15:41 postdates the host run
