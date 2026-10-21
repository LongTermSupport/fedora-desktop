# Plan 00209: agent bus v2 in go

**Status**: In Progress (the plan of record for the whole Go rewrite, host side included; D1-D20
defaulted to the design's recommendations, see Owner decisions; a dedicated build session does
the work to [BUILD-BRIEF.md](BUILD-BRIEF.md), milestone M0 done, M1 and stage 1 next)
**Created**: 2026-10-19
**Owner**: joseph
**Priority**: High

## Overview

The agent team bus is Python (the admin tool, `pingbus`, the LXC helper, `declare.py`,
`element.py`), bash (the installer, the root wrapper) and Go (`ccy-teams`), glued by marker lines,
exit codes and `sudo`. Opening `ccy-teams` takes 9-23 s (measured on the host: `--list` 9.3-23.0 s
at a heavy load) because every bus read is a serial Python process (about 0.45 s each, 22 of them
at six checkouts, three teams and six people), and every running session starts a Python process
for its status line on every refresh and for each hook.

The owner asked for a version 2: the bus rebuilt in Go from its requirements, properly
optimised, not translated. [DESIGN.md](DESIGN.md) is that design: a new process model and
privilege model on v1's data model. One Go codebase (`agent-bus` CLI, installer, member client),
reads `ccy-teams` makes in-process plus one short-lived reader for every team run as `agent-bus`
with no root, protocol 1 and every state file and lock unchanged so v1 and v2 run side by side,
and a staged cut-over with rollback that ships the owner-visible wins first. Its requirements
(R1-R50) come from four research digests of v1 (`RESEARCH-*.md`) and Fable's review.

**Plan of record.** This plan owns the whole rewrite, stages 1-8, host side included. Plan 00213
keeps the work it has in flight, which is stage 2's status-line part: its Task 1.1 (the measure
script), Task 1.2 (why a session renders about twice a second), Task 2.2 (the status line's parity oracle) and Task 2.3 (the cut-over). Its Tasks 3.1
(the hooks port) and 3.2 (what stays in Python) are this plan's Task 3.1 and
[RESEARCH-python-scope.md](RESEARCH-python-scope.md). No bus module is Ansible-loaded, so all of
it is in scope (that file has the evidence per module).

## Goals

- `ccy-teams` opens in about 1 s at the owner's scale (D13), with one bus process (the reader)
  started by an open.
- No Python and no bash on any bus path: one Go codebase in `src-go`, deployed by the plays.
- No root on any read; every change still needs the owner's password.
- Every v1 requirement and hard-won behaviour kept (DESIGN.md section 2, and the requirements
  carried from other plans below), each as a numbered expectation in its stage's spec.
- v1 and v2 coexist on the same state and locks until v1 is retired; each stage can be rolled
  back by a redeploy.

## Non-Goals

- A wire protocol 2 (D5), or changes to the data model before stage 8.
- A resident host daemon, and Plan 00197's seat broker (D1, D11): the broker's own plan decides
  whether it needs one.
- The supervisor-plugin delivery path of Plan 00194 Task 2.1 (D4), until its upstream API ships.
- Reworking the git, tmux and podman probes of the board.

## Owner decisions

The owner's standing instruction is "DO NOT GATE ON ME", so each decision of DESIGN.md section 14
takes the design's recommended option, **defaulted 2026-10-21, and the owner may overturn it**.
The coordinator hears of any decision that turns out to be irreversible, or that contradicts the
owner's words, before the stage that acts on it.

| #   | Option taken                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| --- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | (c'): one Go reader as `agent-bus` by a NOPASSWD Runas rule, no root, no daemon (stage 4)                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| D2  | moot under D1 (c')                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| D3  | `sudo agent-bus` as root with the password, the change done by a child as `agent-bus`                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| D4  | relay mode dropped from v2; specified against v2 when the upstream plugin API ships; `relay/lock` stays the delivery lock. **Overrides the owner's own design** (Plan 00194, decisions A-G: relay as the one delivery path, the supervisor plugin owning `pingbus watch`), and with it parks 00194's delivery goal (Tasks 2.1, 2.1b, 2.2) behind stage 6b. Stage 2 already acts on it (the Go hook refuses relay mode, so a stage-2 kit has no relay path): raised with the coordinator at the M0 hand-over, and answered before Task 3.1 ships the relay refusal |
| D5  | wire protocol 1 kept through and after the migration                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| D6  | host_vars, edited by a Go YAML node editor (`go.yaml.in/yaml/v3`, an allowlist row)                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| D7  | LXC apply ported to Go at stage 7, play-driven as today                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| D8  | static amd64 only                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| D9  | legacy forms dropped at stage 8 after a migration that reports clean. **One-way**: raised with the coordinator before stage 8 starts                                                                                                                                                                                                                                                                                                                                                                                                                              |
| D10 | Plan 00192 Task 3.2's Go installer adopted, merged after the owner's root test                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| D11 | the seat broker in its own plan                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| D12 | the admin commands no program calls are kept                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| D13 | 1 s open at six checkouts, three teams, six people; redraw under 200 ms; `--list` and the interactive open                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| D14 | a reader failure is shown as v1 shows a refused `sudo -n agent-bus list`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| D15 | a keeper and a worker in every member. **A new resident process on every member machine**, which DESIGN.md asks the owner about: raised with the coordinator at the M0 hand-over, and answered before 6b starts                                                                                                                                                                                                                                                                                                                                                   |
| D16 | **not the design's 1 s**: `refreshInterval` is 25 s, set by Plan 00213 Task 1.3 (the coordinator's option A, the owner may change it), because the supervisor's context sidecar goes stale at 30 s                                                                                                                                                                                                                                                                                                                                                                |
| D17 | one ccy image bump per stage that changes the image (2, 6a, 6b, 8)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| D18 | versioned kits, resolved at launch                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| D19 | `element.py` ported to `agent-bus element` at stage 5                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| D20 | DESIGN.md 10.2's stage order, with the milestones below                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |

## Milestones

| Milestone | What                                                                                                                                                              | Stage (DESIGN.md 10.2) | Tasks                  |
| --------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------- | ---------------------- |
| M0        | Bookkeeping: plan of record, decisions defaulted, Python scope, other plans' open tasks                                                                           | none                   | 1.4, 1.5               |
| M1        | Go foundations: `bus/proto`, `bus/state`, `bus/matrix` with `fakehs`, the shared corpora `tests/fixtures/agent-bus-protocol` and `tests/fixtures/agent-bus-state` | none                   | 1.6-1.8                |
| (early)   | In-process `ccy-teams` reads, built beside M1                                                                                                                     | 1                      | 2.1, 2.2               |
| M2        | Hooks and status line in Go, versioned kits                                                                                                                       | 2                      | 3.1-3.3, 00213 2.2-2.3 |
| (between) | The installer, the reader, the admin CLI and `agent-bus element`, in that order                                                                                   | 3, 4, 5                | 4.1-6.2                |
| M3        | The short pingbus verbs in Go (6a)                                                                                                                                | 6, first part          | 7.1                    |
| M4        | Keeper, worker and delivery (6b)                                                                                                                                  | 6, second part         | 7.2, 7.3               |
| M5        | LXC                                                                                                                                                               | 7                      | 8.1                    |
| M6        | Retire v1                                                                                                                                                         | 8                      | 9.1-9.3                |

Two points on how the milestones fit the design:

- **M1 builds ahead of a caller**, and DESIGN.md 3.2 says no stage ships code nothing calls. The
  corpora have callers at once: the Python tests run them. A Go package merges to F44 with its
  first caller: stage 1 for the declarations and the state decoders it reads; stage 2 for the
  member files the hooks read, and for `bus/matrix` and `fakehs`, because the Go SessionEnd hook
  posts the goodbye itself (R28, R45) and charges the send gate in `outbox.json` (R42); stage 4
  for the admin endpoints the reader uses. It may be developed earlier on its own branch.
- **Stage 6 is split** into 6a (every remaining short verb in Go, the Python watcher still the
  receiver) and 6b (keeper, worker, delivery, and the status line reading `status-row.json`). This
  follows B10's rule of one implementation per command per kit version. 6a's coexistence tests
  (R47) run against the Python watcher. 6b's run against v1 sessions.

## Requirements carried from other plans

Each is an expectation of the stage named, as well as DESIGN.md section 2.

- **Plan 00211, the reports and questions rooms** (live in Python; its files are not edited from
  here): pingbus 0.2.0's `report`, `ask-human`, `thread` (`--ask`, `--close`), `questions` and
  `history --room` verbs; the `room:`, `question:` and `revises:` envelope lines; the `ROOM` lines
  of `agent-bus list`; the side rooms (`_ensure_side_rooms`, `_sync_questions_members` in
  `bootstrap`); the side-room drift check in health; `report_per_hour`; the role lines in the
  hooks. Its [SPEC.md](../00211-agent-bus-reports-and-questions-rooms/SPEC.md) is the authority.
  The stages: hooks at 2, health at 3, `ROOM` lines in the reader at 4, admin at 5, verbs at 6a,
  sync and room routing at 6b.
- **Plan 00213, the status line**: the bus segment's own TTL cache (SPEC-status-line S32-S36),
  separate from the hooks daemon's refresh; the 25 s refresh (D16); stderr may differ in wording
  and keeps its shape, and empty stays empty (S24, S37). At 6b the line reads `status-row.json`.
  The 6b spec says whether the cache still earns its place, and the owner's separate-TTL rule
  holds either way.
- **Plan 00194**: pingbus's own notices (the disconnected notice, the session-end goodbye and
  `seat farewell`) skip the 60 s duplicate window and keep the token bucket (its Task 2.0i; the
  goodbye at stage 2, the seat commands at 6a). `seat disconnected` and the launcher write each outcome to the owner-only
  `.claude/ccy/pingbus/disconnect.log` (2.0h, stage 6a). An unaddressed human message gets one
  `ignored.log` line per member and `pingbus status` counts `ignored=` (2.0f, stage 6b).
- **Plan 00201**, when each reaches F44: the client-side security findings (2.11, 2.12) and the
  team-settings rules S-F1 to S-F14 (2.15) are expectations of stages 5 and 6b, and also: 2.12's
  N5 (the pre-swap backup on a rendered config change, in `config_restart.py`) of stage 3, where
  Task 4.2 makes it an installer function; 2.15's S-F14 (`removed_humans`, the name refused for
  good, in `teamfile.py` and `declare.py`) of stage 1, whose decoders accept it, and stage 3,
  whose installer validation keeps the refusal; 2.17's `internal/hostcheckout` refusal and single
  handle renderer of stage 1, whose in-process seats rewire `teams/board/collect.go` and the
  picker; 2.18's play-lock hold across the host_vars write and the apply (S-G1 to S-G11) of
  stage 5's `bus/declare` write side.
- **The plan acceptance suites** of Plans 00161, 00201, 00203 and 00211 run v1's Python and
  zipapps. Stage 8 re-points or retires each before it deletes the Python.

## Folded in from other plans

Recommended to the coordinator, who decides; those plans' files are not edited from here. On
accepting a fold, the coordinator marks the folded task ❌ in its own plan, pointing to the 00209
task that now owns it, so that no task has an owner in two plans.

| Plan task                          | State there                               | Recommendation                                                                                                                                                                                                                                                                |
| ---------------------------------- | ----------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 00213 3.1                          | the hooks port                            | **moved here**: Task 3.1                                                                                                                                                                                                                                                      |
| 00213 3.2                          | what stays in Python                      | **moved here**: answered by [RESEARCH-python-scope.md](RESEARCH-python-scope.md)                                                                                                                                                                                              |
| 00194 1.2                          | the owner's tick on the design            | stays in 00194; its relay part is overtaken by D4                                                                                                                                                                                                                             |
| 00194 2.0c                         | merged; host checks H1-H7 pending         | stays in 00194 (v1's acceptance); SPEC-inject E1-E26 are 6b expectations                                                                                                                                                                                                      |
| 00194 2.0d                         | merged; the owner's restart check pending | stays in 00194; E25 and E26 (R24) are 6b expectations                                                                                                                                                                                                                         |
| 00194 2.0e                         | waits on hooks-daemon #127                | **fold into Task 3.1**: the SessionEnd hook is Go from stage 2, so build it there once #127 ships                                                                                                                                                                             |
| 00194 2.0f                         | merged; the owner's check pending         | stays in 00194; `ignored.log` is a 6b expectation                                                                                                                                                                                                                             |
| 00194 2.0g                         | not started: should `empty` be logged     | **fold into Task 7.2**: a receive rule, decided in its spec                                                                                                                                                                                                                   |
| 00194 2.1, 2.1b, 2.2               | wait on hooks-daemon #119                 | stay in 00194; when #119 ships they are specified against v2 (D4), not on the Python relay. This parks 00194's delivery goal behind 6b, and a stage-2 kit already refuses relay mode, so D4 goes to the coordinator at the M0 hand-over and is answered before Task 3.1 ships |
| 00201 2.8                          | relay-mode spool lacks a `REPLY` line     | **superseded** by D4: relay mode is not carried; cancel in 00201                                                                                                                                                                                                              |
| 00201 2.11, 2.12, 2.15, 2.17, 2.18 | in-flight Python and Go fixes             | stay in 00201; carried as expectations (above)                                                                                                                                                                                                                                |
| 00201 2.16                         | team rename, not designed                 | stays its own work; build it on v2 after stage 5 (Go installer and admin), not in Python                                                                                                                                                                                      |
| 00201 2.7, 2.13, 2.14, 2.19        | acceptance, ccy-teams Go, run.bash        | unaffected                                                                                                                                                                                                                                                                    |

## Tasks

Each stage's acceptance includes the `qa-reviewer` agent over that stage's diff, the owner's host
checks it names (DESIGN.md section 16), and H10.

### Phase 1: Design, bookkeeping and foundations (M0, M1)

- [x] ✅ **Task 1.1**: Research v1 (four digests, committed as `RESEARCH-*.md`).
- [x] ✅ **Task 1.2**: [DESIGN.md](DESIGN.md): requirements, shape, read model, state and
  versions, privilege, concurrency, member client, homeserver, migration, tests, what is not
  carried, observability, decisions, risks, host checks.
- [x] ✅ **Task 1.3**: Fable reviewed DESIGN.md (REWORK); the one rework round addressed findings
  1-12 (`subagent-reports/261019-v2-design-review-fable.md`, dispositions in
  `subagent-reports/261019-v2-design-opus.md`).
- [x] ✅ **Task 1.4**: D1-D20 decided by default (Owner decisions above), D16 by Plan 00213, D17 and
  D20 amended for the stage 6 split; D4 and D15 are raised with the coordinator at the M0
  hand-over, D9 before stage 8, each answered by the point its row gives. The owner may overturn any. DESIGN.md section 14 points here.
- [x] ✅ **Task 1.5**: M0: this plan made the plan of record and 00213 split to match; the
  milestones; the Python scope per module
  ([RESEARCH-python-scope.md](RESEARCH-python-scope.md)); the other plans' open tasks and
  requirements (above).
- [ ] ⬜ **Task 1.6**: The shared corpora (DESIGN.md 5.4), `tests/fixtures/agent-bus-protocol`
  and `tests/fixtures/agent-bus-state`, run by the Python tests first, with Plan 00211's room
  forms and the Unicode-15 envelope cases.
- [ ] ⬜ **Task 1.7**: `bus/proto` and `bus/state`, test first against the corpora; each part
  merges with its first caller.
- [ ] ⬜ **Task 1.8**: `bus/matrix` and `bus/matrix/fakehs` with Tuwunel's recorded quirks as
  switches; the client and send path merge with the Go goodbye (stage 2), the admin endpoints with
  the reader (stage 4).

### Phase 2: In-process reads (stage 1)

- [ ] ⬜ **Task 2.1**: H1 at an ordinary load, with the interactive open, before any change.
- [ ] ⬜ **Task 2.2**: `SPEC-view.md` (stage 1 part); `bus/declare` read side, seats in-process,
  one systemd call, one `declare overview` kept for APPLIED; the decoders and corpus cases they
  need; ccy-teams switched; H10.

### Phase 3: Hooks and status line in Go (stage 2, M2)

The status line is Plan 00213's: its Task 2.1 is done (`src-go/internal/pingbus/statusline`),
Task 2.2 is the parity oracle and Task 2.3 the cut-over; its Task 1.1 measures before and after,
and its Task 1.2 (in progress) finds why a session renders about twice a second.

- [ ] ⬜ **Task 3.1**: Not shipped until D4 has been answered (its refusal of relay mode acts on
  D4). `SPEC-pingbus-hooks.md`; Go `hook` reading v1 files, the dispatch to
  `pingbus.pyz` for the rest (B10), `goodbye.json` dropped (B7), a parity oracle per hook on
  00213's golden pattern. Port order by measured cost (00213 Task 1.1): in socket mode the hooks
  that start pingbus are SessionStart, UserPromptSubmit, Stop and SessionEnd, since PostToolUse
  runs it only in relay mode. Plan 00211's role lines; Plan 00194 Task 2.0e (a scheduled restart
  posts no goodbye) once hooks-daemon #127 ships. Two coexistence tests from the start, because
  stage 2 shares `state/` with v1 (R47): the Go goodbye charging the send gate in `outbox.json`
  under the outbox lock (R42) against a v1 `send` in a second real process, and the
  `said-goodbye.json` it writes read by v1's `seat disconnected` (H11). The goodbye finishes
  inside the plugin hook's 6 s timeout, under the session's 8 s SessionEnd budget; the spec states
  and keeps both. Formerly 00213 Task 3.1.
- [ ] ⬜ **Task 3.2**: Versioned host and LXC kits resolved at launch (R49, B9); H13.
- [ ] ⬜ **Task 3.3**: The ccy image: the container-version gate covers pingbus's Go sources, the
  container version bump, and the host's `pingbus` added to the deployed tools (Plan 00206).

### Phase 4: Installer (stage 3)

- [ ] ⬜ **Task 4.1**: Plan 00192 Task 3.2's Go installer merged after the owner's root test (D10).
- [ ] ⬜ **Task 4.2**: The install lock, the health check in Go with its sandbox (R7) and Plan
  00211's side-room drift check, the source digest in the stamp (B4) and the last
  `declare overview` removed; `render`, `bridge.py`, `config_restart` and `timer_state` become
  installer functions and their play tasks go; H6.

### Phase 5: The reader (stage 4)

- [ ] ⬜ **Task 5.1**: `SPEC-view.md` (reader part); `agent-bus view` and the Runas sudoers line,
  Plan 00211's `ROOM` lines included; ccy-teams and the seat flows read it; H3-H5, H10.
- [ ] ⬜ **Task 5.2**: The old `(root)` sudoers lines removed, after the owner accepts Task 5.1.

### Phase 6: Admin changes and Element (stage 5)

- [ ] ⬜ **Task 6.1**: Admin changes in the Go `agent-bus` CLI (a child as `agent-bus`), the bash
  wrapper deleted, `bus/declare` write side, seat take and remove in Go, Plan 00211's side rooms
  in `bootstrap`; the registry coexistence test; H12.
- [ ] ⬜ **Task 6.2**: `agent-bus element` replaces `element.py` in `play-agent-bus-element.yml`
  (R50, D19).

### Phase 7: Keeper and worker (stage 6, M3 and M4)

- [ ] ⬜ **Task 7.1**: 6a: `SPEC-pingbus.md`; every remaining short verb in Go (`recv`, `wait`,
  `history`, `send`, `say`, `status`, `seat disconnected`, `seat farewell`, `seat retire`, `reach`, Plan
  00211's verbs), with `seat exec` and `seat keep` left for 6b, the Python watcher
  still the receiver; the lock coexistence tests against it (R47); a ccy image bump.
- [ ] ⬜ **Task 7.2**: 6b, once D15 has been answered: keeper and worker with every member lock kept (R47), delivery
  (SPEC-inject E1-E26), the status row and the status line switched to it, Plan 00194 Task 2.0g's
  rule; the coexistence tests; H7-H9, H11; a ccy image bump.

### Phase 8: LXC (stage 7, M5)

- [ ] ⬜ **Task 8.1**: LXC member apply in Go (R33, R48), guest ops through `pingbus guest`, the
  kit sync without Python; H8.

### Phase 9: Retire v1 (stage 8, M6)

- [ ] ⬜ **Task 9.1**: D9 raised with the coordinator; the legacy-form migration run and
  reported clean; then the Go decoders drop the legacy forms.
- [ ] ⬜ **Task 9.2**: The plan acceptance suites that run v1 re-pointed or retired; delete the
  Python and bash bus code, the zipapps and `bundle.py`'s play tasks, with the ccy container
  version bump.
- [ ] ⬜ **Task 9.3**: `qa-reviewer` over the plan's whole diff.

## Success Criteria

- [ ] An open of `ccy-teams` meets D13's bar on the host (H10).
- [ ] No sudoers rule runs a bus process as root; no Python is started by the bus on the host or
  in a member.
- [ ] CPU per status-line render and per hook event measured before and after (00213 Task 1.1).
- [ ] Every stage's expectations pass, and the corpora and coexistence tests pass against v1 until
  stage 8.
- [ ] The rollback drill (H9) passed before stage 6b was accepted.

## Delivery & Milestones

- Design proposed on `feat/agent-bus-v2-design`; revised after Fable's review on
  `feat/agent-bus-v2-design-2`.
- M0 on `feat/bus-v2-m0-plan`.
