# Plan 00209 M0: plan bookkeeping (report to the build session)

Branch `feat/bus-v2-m0-plan`. It merged F44 first, because the branch was 147 commits behind and
had neither 00211 nor 00213. The only conflict was `CLAUDE/Plan/README.md`, resolved by keeping
both entries.

## What changed

- **00209 PLAN.md**: status (the plan of record, In Progress); a plan-of-record paragraph; the
  Owner decisions table (D1-D20 defaulted on 2026-10-21; D16 = 25 s from 00213; D9 one-way);
  Milestones M0-M6; Requirements carried from other plans (00211, 00213, 00194, 00201, and the
  plan acceptance suites); Folded in from other plans; Tasks 1.4 and 1.5 done; new Tasks
  1.6-1.8 (M1); stage 6 split into Task 7.1 (6a) and Task 7.2 (6b).
- **00209 DESIGN.md**: section 14 points to the PLAN table. R26 and 8.2 now say 25 s, not 1 s.
- **00209 RESEARCH-python-scope.md** (new): the verdict for every module, with file:line evidence.
- **00213 PLAN.md**: the Non-Goals sentence names 00209; the hooks goal points to 00209 Task
  3.1; Tasks 3.1 and 3.2 are marked superseded (❌) with links.
- **README.md**: 00213 is now In Progress, and its line says the hooks port moved; 00209 is In
  Progress.
- Journal entries in 00209 and 00213 (through mkplan).

## Task 1: the Python scope

No bus module is Ansible-loaded. The only plugin directory is `callback_plugins/`, which holds
`play_ledger.py` and imports nothing from the bus. There are no `library/`, `module_utils` or
other plugin directories, and no project roles. Every module is one of these:

- a CLI that a play runs (`python3 -m`): `config_restart`, `bridge`, `timer_state`,
  `agent_bus_lxc.members` and `element`;
- run by the installer, the root wrapper, the health unit or ccy-teams (the zipapps and
  `declare`);
- a library that only those import.

All of it is in scope. `bundle.py` (the zipapp builder) is retired, not ported. The per-module
stages are in RESEARCH-python-scope.md.

## Task 2: the open tasks of 00194 and 00201

The table is in PLAN.md "Folded in from other plans". In short:

- **Fold into 00209**: 00194 2.0e goes to Task 3.1 (Go SessionEnd, still waiting on hooks-daemon
  #127). 00194 2.0g goes to Task 7.2.
- **Superseded**: 00201 2.8 (relay `REPLY` line), because D4 drops relay. Recommend cancelling
  it in 00201.
- **Stay in their plans**: 00194 1.2, 2.0c, 2.0d and 2.0f (the code is merged; only host checks
  remain), and 2.1, 2.1b and 2.2 (waiting on #119; to be specified against v2 later). 00201
  2.11, 2.12 and 2.15 stay too, and are carried as expectations. 00201 2.16 (rename) should be
  built on v2 after stage 5.
- **Unaffected**: 00201 2.7, 2.13, 2.14, 2.17, 2.18 and 2.19.

## Task 3: constraints the design missed

01. **D16 contradiction.** DESIGN recommended keeping 1 s, but 00213 Task 1.3 had already shipped
    25 s. Fixed in DESIGN.
02. **00211 is absent from DESIGN.** Its verbs, envelope lines, `ROOM` lines, side rooms, health
    drift check and `report_per_hour` are now carried, stage by stage.
03. **00194 2.0i.** Notices skip the duplicate window and keep the token bucket. DESIGN R42 only
    covers the window.
04. **00194 2.0h and 2.0f.** `disconnect.log`, and `ignored.log` with `ignored=`, are not in DESIGN.
05. **PostToolUse starts pingbus only in relay mode** (`hooks.json:29`), so 00213's port order was
    wrong. A Go hook should refuse `PINGBUS_DELIVERY=relay` loudly.
06. **SessionEnd: two limits, not a mismatch** (corrected after review). `hooks.json:50` sets the
    plugin hook's own 6 s timeout; DESIGN 8.2's 8 s is the session-wide
    `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS` budget (`settings.json:9`). Both stay, and the Go
    goodbye must finish inside 6 s.
07. **The goodbye needs the Matrix client at stage 2.** The Go SessionEnd hook posts the goodbye
    itself, so `bus/matrix` and `fakehs` are needed at stage 2, not stage 4.
08. **M1 conflicts with DESIGN 3.2** ("no stage ships code nothing calls"). Resolved: corpora
    merge alone, and each Go package merges with its first caller.
09. **The existing Go status line** is in `internal/pingbus/statusline`, while DESIGN 3.2 puts it
    in `bus/member`. This is the build session's call.
10. **Plan acceptance suites run v1.** Those of 00161, 00201, 00203 and 00211 run v1's Python and
    zipapps, so stage 8 must re-point or retire them.
11. **Install-specific names in BUILD-BRIEF.md.** Its line 3 names the coordinator's seat handle
    and team name. Replaced with placeholders after review; the commit that introduced them
    stays in unpushed branch history, which is the coordinator's call.

## For the owner or the coordinator

- **D9 (one-way)** must be raised before stage 8.
- **D4 drops relay.** 00194's design (owner decisions A-G) made relay the one path. D4 is
  reversible, but it overrides an owner-stated design, so tell the owner.
- **The stage 6 split** means an extra ccy image bump (D17).
- **D10 still waits on the owner's root test** of the 00192 installer. This is a host step, not a
  decision.
