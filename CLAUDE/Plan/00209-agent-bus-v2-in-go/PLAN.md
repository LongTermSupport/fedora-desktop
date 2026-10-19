# Plan 00209: agent bus v2 in go

**Status**: Not Started (design revised after Fable's review; awaiting the owner's decisions D1-D20)
**Created**: 2026-10-19
**Owner**: joseph
**Priority**: High

## Overview

The agent team bus is Python (the admin tool, `pingbus`, the LXC helper, `declare.py`,
`element.py`), bash (the installer, the root wrapper) and Go (`ccy-teams`), glued by marker lines,
exit codes and `sudo`. Opening `ccy-teams` takes 9-23 s (measured on the host: `--list` 9.3-23.0 s
at a heavy load) because every bus read is a serial Python process (about 0.45 s each, 22 of them
at six checkouts, three teams and six people), and every running session starts a Python process
for its status line every second and for each hook.

The owner asked for a version 2: the bus rebuilt in Go from its requirements, properly
optimised, not translated. [DESIGN.md](DESIGN.md) is that design: a new process model and
privilege model on v1's data model. One Go codebase (`agent-bus` CLI, installer, member client),
reads `ccy-teams` makes in-process plus one short-lived reader for every team run as `agent-bus`
with no root, protocol 1 and every state file and lock unchanged so v1 and v2 run side by side,
and a staged cut-over with rollback that ships the owner-visible wins first. Its requirements
(R1-R50) come from four research digests of v1 (`RESEARCH-*.md`) and Fable's review.

## Goals

- `ccy-teams` opens in about 1 s at the owner's scale (D13), with one bus process (the reader)
  started by an open.
- No Python and no bash on any bus path: one Go codebase in `src-go`, deployed by the plays.
- No root on any read; every change still needs the owner's password.
- Every v1 requirement and hard-won behaviour kept (DESIGN.md section 2), each as a numbered
  expectation in its stage's spec.
- v1 and v2 coexist on the same state and locks until v1 is retired; each stage can be rolled
  back by a redeploy.

## Non-Goals

- A wire protocol 2 (D5), or changes to the data model before stage 8.
- A resident host daemon, and Plan 00197's seat broker (D1, D11): the broker's own plan decides
  whether it needs one.
- The supervisor-plugin delivery path of Plan 00194 Task 2.1 (D4), until its upstream API ships.
- Reworking the git, tmux and podman probes of the board.

## Tasks

Each stage's acceptance includes the `qa-reviewer` agent over that stage's diff, the owner's host
checks it names (DESIGN.md section 16), and H10.

### Phase 1: Design

- [x] ✅ **Task 1.1**: Research v1 (four digests, committed as `RESEARCH-*.md`).
- [x] ✅ **Task 1.2**: [DESIGN.md](DESIGN.md): requirements, shape, read model, state and
  versions, privilege, concurrency, member client, homeserver, migration, tests, what is not
  carried, observability, decisions, risks, host checks.
- [x] ✅ **Task 1.3**: Fable reviewed DESIGN.md (REWORK); the one rework round addressed findings
  1-12 (`subagent-reports/261019-v2-design-review-fable.md`, dispositions in
  `subagent-reports/261019-v2-design-opus.md`).
- [ ] ⬜ **Task 1.4**: The owner decides D1-D20; DESIGN.md updated to the decisions.

### Phase 2: In-process reads (stage 1)

- [ ] ⬜ **Task 2.1**: H1 at an ordinary load, with the interactive open, before any change.
- [ ] ⬜ **Task 2.2**: `SPEC-view.md` (stage 1 part); `bus/declare` read side, seats in-process,
  one systemd call, one `declare overview` kept for APPLIED; the decoders and corpus cases they
  need; ccy-teams switched; H10.

### Phase 3: Hooks and status line in Go (stage 2)

- [ ] ⬜ **Task 3.1**: `SPEC-pingbus-hooks.md`; Go `hook` and `status-line` reading v1 files, the
  dispatch to `pingbus.pyz` for the rest (B10), `goodbye.json` dropped (B7).
- [ ] ⬜ **Task 3.2**: Versioned host and LXC kits resolved at launch (R49, B9); H13.
- [ ] ⬜ **Task 3.3**: The ccy image: the container-version gate covers pingbus's Go sources, the
  container version bump, and the host's `pingbus` added to the deployed tools (Plan 00206).

### Phase 4: Installer (stage 3)

- [ ] ⬜ **Task 4.1**: Plan 00192 Task 3.2's Go installer merged after the owner's root test (D10).
- [ ] ⬜ **Task 4.2**: The install lock, the health check in Go with its sandbox (R7), the source
  digest in the stamp (B4) and the last `declare overview` removed; H6.

### Phase 5: The reader (stage 4)

- [ ] ⬜ **Task 5.1**: `SPEC-view.md` (reader part); `agent-bus view` and the Runas sudoers line;
  ccy-teams and the seat flows read it; H3-H5, H10.
- [ ] ⬜ **Task 5.2**: The old `(root)` sudoers lines removed, after the owner accepts Task 5.1.

### Phase 6: Admin changes and Element (stage 5)

- [ ] ⬜ **Task 6.1**: Admin changes in the Go `agent-bus` CLI (a child as `agent-bus`), the bash
  wrapper deleted, `bus/declare` write side, seat take and remove in Go; the registry coexistence
  test; H12.
- [ ] ⬜ **Task 6.2**: `agent-bus element` replaces `element.py` in `play-agent-bus-element.yml`
  (R50, D19).

### Phase 7: Keeper and worker (stage 6)

- [ ] ⬜ **Task 7.1**: `SPEC-pingbus.md`; keeper and worker with every member lock kept (R47), the
  status row, the remaining commands; the coexistence tests; H7-H9, H11.
- [ ] ⬜ **Task 7.2**: The ccy container version bump for the stage.

### Phase 8: LXC (stage 7)

- [ ] ⬜ **Task 8.1**: LXC member apply in Go (R33, R48), guest ops through `pingbus guest`, the
  kit sync without Python; H8.

### Phase 9: Retire v1 (stage 8)

- [ ] ⬜ **Task 9.1**: The legacy-form migration (D9) run and reported clean; then the Go decoders
  drop the legacy forms.
- [ ] ⬜ **Task 9.2**: Delete the Python and bash bus code and the zipapps, with the ccy container
  version bump.
- [ ] ⬜ **Task 9.3**: `qa-reviewer` over the plan's whole diff.

## Success Criteria

- [ ] An open of `ccy-teams` meets D13's bar on the host (H10).
- [ ] No sudoers rule runs a bus process as root; no Python is started by the bus on the host or
  in a member.
- [ ] Every stage's expectations pass, and the corpora and coexistence tests pass against v1 until
  stage 8.
- [ ] The rollback drill (H9) passed before stage 6 was accepted.

## Delivery & Milestones

- Design proposed on `feat/agent-bus-v2-design`; revised after Fable's review on
  `feat/agent-bus-v2-design-2`.
