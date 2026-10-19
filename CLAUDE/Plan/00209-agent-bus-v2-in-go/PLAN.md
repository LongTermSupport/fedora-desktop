# Plan 00209: agent bus v2 in go

**Status**: Not Started (design proposed; awaiting Fable's review and the owner's decisions D1-D13)
**Created**: 2026-10-19
**Owner**: joseph
**Priority**: High

## Overview

The agent team bus is Python (the admin tool, `pingbus`, the LXC helper, `declare.py`), bash
(the installer, the root wrapper) and Go (`ccy-teams`), glued by marker lines, exit codes and
`sudo`. Opening `ccy-teams` takes 9-23 s because every bus read is a serial Python process
(about 0.45 s each, 22 of them at six checkouts, three teams and six people), and every running
session starts a Python process for its status line every second and for each hook.

The owner asked for a version 2: the bus rebuilt in Go from its requirements, properly
optimised, not translated. [DESIGN.md](DESIGN.md) is that design: one Go codebase (host daemon,
CLI, installer, member client), a read model `ccy-teams` uses in-process with one request to the
daemon, no sudo for reads, protocol 1 and every state file unchanged so v1 and v2 run side by
side, and a staged cut-over with rollback. Its requirements come from four research digests of v1
(`RESEARCH-*.md`).

## Goals

- `ccy-teams` opens in about 1 s at the owner's scale (D13), with no bus process started by an open.
- No Python and no bash on any bus path: one Go codebase in `src-go`, deployed by the plays.
- No sudoers rule for the bus; every change still needs the owner's password.
- Every v1 requirement and hard-won behaviour kept (DESIGN.md section 2), each as a numbered
  expectation in its stage's spec.
- v1 and v2 coexist on the same state until v1 is retired; each stage can be rolled back by a
  redeploy.

## Non-Goals

- A wire protocol 2 (D5).
- Plan 00197's seat broker (D11): it builds on `agent-busd` later.
- The supervisor-plugin delivery path of Plan 00194 Task 2.1 (D4), until its upstream API ships.
- Reworking the git, tmux and podman probes of the board.

## Tasks

### Phase 1: Design

- [x] ✅ **Task 1.1**: Research v1 (four digests, committed as `RESEARCH-*.md`).
- [x] ✅ **Task 1.2**: [DESIGN.md](DESIGN.md): requirements, shape, read model, state and
  versions, privilege, concurrency, member client, homeserver, migration, tests, what is not
  carried, observability, decisions, risks, host checks.
- [ ] ⬜ **Task 1.3**: Fable reviews DESIGN.md against v1 and the research; findings fixed.
- [ ] ⬜ **Task 1.4**: The owner decides D1-D13; DESIGN.md updated to the decisions.

### Phase 2: Foundations (stage 1)

- [ ] ⬜ **Task 2.1**: `SPEC-proto-state.md`; `bus/proto` and `bus/state`, test first.
- [ ] ⬜ **Task 2.2**: The state and protocol fixture corpora; the Python helper tests read them too.
- [ ] ⬜ **Task 2.3**: `bus/matrix` and `fakehs` with Tuwunel's recorded quirks.

### Phase 3: Installer (stage 2)

- [ ] ⬜ **Task 3.1**: Plan 00192 Task 3.2's Go installer merged after the owner's root test (D10).
- [ ] ⬜ **Task 3.2**: The install lock, the health check in Go.

### Phase 4: In-process reads (stage 3)

- [ ] ⬜ **Task 4.1**: `SPEC-view.md`; `bus/declare` read side, stamps with the source-digest
  software field, seats in-process, one systemd call; ccy-teams switched; H1 before, H10 after.

### Phase 5: The daemon's view (stage 4)

- [ ] ⬜ **Task 5.1**: `SPEC-agent-busd.md`; `agent-busd` with the view socket, its unit and
  sandbox, `/etc/agent-bus/owner`; ccy-teams reads it; H3-H5, H10.
- [ ] ⬜ **Task 5.2**: The sudoers rule removed, after the owner accepts Task 5.1.

### Phase 6: Admin changes (stage 5)

- [ ] ⬜ **Task 6.1**: Admin changes in the daemon, the Go `agent-bus` CLI, `bus/declare` write
  side, seat take and remove in Go; the bash wrapper deleted.

### Phase 7: pingbus v2 (stage 6)

- [ ] ⬜ **Task 7.1**: `SPEC-pingbus.md`; hooks and status line in Go.
- [ ] ⬜ **Task 7.2**: Keeper and worker; the remaining commands; H7-H9.

### Phase 8: LXC (stage 7)

- [ ] ⬜ **Task 8.1**: LXC member apply in Go, guest ops through `pingbus guest`; H8.

### Phase 9: Retire v1 (stage 8)

- [ ] ⬜ **Task 9.1**: Delete the Python and bash bus code and the zipapps; legacy-form migration (D9).
- [ ] ⬜ **Task 9.2**: `qa-reviewer` over the plan's whole diff.

## Success Criteria

- [ ] An open of `ccy-teams` meets D13's bar on the host (H10).
- [ ] No sudoers rule mentions the bus; no Python is started by the bus on the host or in a member.
- [ ] Every stage's expectations pass, and the corpora pass in both implementations until stage 8.
- [ ] The rollback drill (H9) passed before stage 6 was accepted.

## Delivery & Milestones

- Design proposed on `feat/agent-bus-v2-design`.
