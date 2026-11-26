# Plan 00268: Lightning memory leak

**Status**: In Progress (research done; Phase 1 evidence waits on the laptop's host seat)
**Created**: 2026-11-26
**Owner**: dev
**Priority**: High

## Overview

Lightning (`org.lightning_matrix.Lightning`, Flatpak), the Matrix client the team's humans use to
watch agent teams (Plan 00254), grew to 84.7G in the busy team's instance and was killed by
systemd-oomd on 2026-11-25 at 18:08:56Z, while a quieter team's instance of the same build stayed
at 0.35G for 21 h (reported by the laptop's host seat, unchecked here). The owner's top priority
(2026-11-26): find the leak and fix it, upstream where it lives; the owner suspects teamchat's
traffic triggers a latent defect in the client.

The reported rate (about 200 MiB a minute on average) is far beyond what upstream measured per
message, so the leading suspects are a client loop that a teamchat pattern starts, or per-event
work that grows with history. [RESEARCH.md](RESEARCH.md) holds the evidence, what teamchat sends
that a human room does not, and the ranked hypotheses H1-H7 with a test for each.

A memory cap on the instance's scope (MemoryHigh 6G / MemoryMax 8G) is a separate stopgap in
another batch; this plan decides at the end whether it stays.

## Goals

- Name the mechanism that grows the busy team's Lightning, with a measurement that shows it.
- Reproduce it away from the owner's laptop, one traffic pattern at a time.
- Get it fixed upstream (an issue with evidence and a reproduction; a pull request where we can),
  and change teamchat only if teamchat's traffic is shown to be the trigger.
- A busy team's Lightning holds steady memory over a working day.

## Non-Goals

- The memory cap itself (another batch; referenced here only for the keep-or-remove decision).
- A standing fork of Lightning (Plan 00254 rejected it; a short-lived patched build only if
  upstream cannot take a fix and the owner asks).
- Changing teamchat's protocol on a guess.

## Tasks

### Phase 1: Evidence from the laptop

- [x] ✅ **Task 1.1**: Research: Lightning's stack, upstream issues, teamchat's traffic, ranked
  hypotheses, reproduction design ([RESEARCH.md](RESEARCH.md)).
- [ ] ⬜ **Task 1.2**: The laptop's host seat runs the read-only commands in
  [laptop-commands.md](laptop-commands.md) (build and update history, which process and which
  memory class, busy thread, growth per instance over an hour, logs, data sizes). Record the
  anonymised results in RESEARCH.md section 1 and re-rank the hypotheses.
- [ ] ⬜ **Task 1.3**: A read-only traffic probe (Go): a team's event counts by room, type,
  relation (thread, reply, edit), receipt and presence change, per hour, over a window, with no
  content. Run on the busy team for 25 Nov and on a quieter team for comparison.
- [ ] ⬜ **Task 1.3a**: The stall window first: the bus host's admin seat runs
  [bus-host-commands.md](bus-host-commands.md) for 25 Nov 17:00:00Z-17:15:00Z in the busy team's
  main, questions and reports rooms (event shapes only, no bodies). Name the event shape at or
  just before 17:09:12Z that could start a GUI-thread runaway (RESEARCH.md 8.1).
- [x] ✅ **Task 1.4**: Owner decision D1, then IaC: a per-team key `lightning_diagnostics` in an
  `agent_bus_element_teams` entry. When true, the launcher's Exec line adds
  `LIGHTNING_SCROLL_MEMORY_TRACE=1 LIGHTNING_SYNC_TRACE=100` and
  `--log-file <team>/cache/lightning-diagnostic.log` (RESEARCH.md 7.3, 7.4, 7.5; `=100` logs
  each event slower than 100 ms, since the Flathub build compiles the per-event line out). Test first in
  `src-go/internal/lightning` (`entries.go` key, `lightning.go` `execLine`) and in
  `helpers/agent_bus/element.py` (`PROFILE_KEYS`), the key in `localhost.yml.dist` and
  `docs/agent-bus.md`. Deployed by the play's `agent-bus-lightning` tag through
  `meta-deploy.bash`; on for the busy team, only on Lightning 0.10.7 or later (its `--log-file`
  cap; the laptop's version is unchecked: laptop-commands.md step 1). Built; the owner sets the
  key in host_vars and runs the queued play.
- [ ] ⬜ **Task 1.5**: Owner question Q20: what the incident left behind. The laptop's host seat
  runs [laptop-commands.md](laptop-commands.md) section 10 (systemd's CPU and peak accounting of
  the killed scope, Lightning's stderr in the journal or the ccy-teams log, any rolling log).
  Answer: [Q20-answer.md](Q20-answer.md).
- [x] ✅ **Task 1.6**: Dogfooding fault: `systemd.LaunchClient` appends a client's stderr to
  `<ccy-teams state dir>/<client>-<team>.log` with no cap or rotation. Log it as a GitHub issue
  before Task 1.4 turns tracing on. Logged: issue #429 (the fix is that issue's, not this plan's).
- [x] ✅ **Task 1.7**: Catch the trigger next time (RESEARCH.md 7.5): slow-event sync tracing in
  Task 1.4's key, and a user-level watchdog (IaC) that, when the instance's log is silent for
  60 s while its RSS climbs past a threshold, records per-thread CPU and an all-threads
  backtrace once. Lightning's own stall tracer logs only when a stall ends, so it cannot. Built:
  `agent-bus-lightning watch` (`internal/lightning/watch`, W1-W12; L17), the user service
  `agent-bus-lightning-watch`, run by the play only while a team has the key true.

### Phase 2: Reproduce

- [ ] ⬜ **Task 2.1**: A load generator (Go, `src-go/`): N agent accounts and a human on a test team
  made by the real rooms engine on Tuwunel (`tuwunelhs`), sending through `proto.Composer`; one
  switch and rate per pattern (pings with thread answers, question threads, notices, reports,
  `m.read` receipts, presence flaps, big bodies), and a replay of Task 1.3's counts. Unit tests on
  `fakehs`.
- [ ] ⬜ **Task 2.2**: Lightning from Flathub in the VM lab (or a headless nested compositor with a
  D-Bus session and a Secret Service), signed in to the test team; a sampler writing RSS, PSS,
  `memory.stat`, CPU time and cumulative event counts every 30 s.
- [ ] ⬜ **Task 2.3**: First, replay Task 1.3a's window (its event shapes in order and spacing)
  with a thread panel open, then the rest of the busy day. First candidates (RESEARCH.md 8.3):
  the questions-room thread post at 17:06:16Z, its read-state marking, and the 7.6 KB reply just
  before it; then the deferred-work arm: replay the window, then (a) an idle trim at once
  against (b) trims suppressed. Confirmed only when the event loop stops
  (the 300 s timers go silent) and the JS heap climbs, as on the laptop.

### Phase 3: Isolate

- [ ] ⬜ **Task 3.1**: One pattern at a time (RESEARCH.md 5.2 arms), each with the window focused,
  unfocused and a thread panel open; growth against events (H2) and against time with traffic
  stopped (H1).
- [ ] ⬜ **Task 3.2**: Profile the triggering arm: heaptrack on a source build (or the Flatpak's
  `--devel` SDK), Qt's QML GC statistics, `LIGHTNING_RUST_LOG`; name the allocation site.
- [ ] ⬜ **Task 3.3**: Audit the QML the trigger points at (0.10.7's thread panel, edit following,
  `MessageDragSelection.qml`) for an unbounded loop or a re-entrant model or signal cycle (H8).

### Phase 4: Fix upstream

- [ ] ⬜ **Task 4.1**: Search upstream again, then file the issue on `lightning-matrix/lightning`
  with the measurements, the memory class, the triggering pattern and the generator to run it;
  link it here and in Plan 00254 Task 2.7.
- [ ] ⬜ **Task 4.2**: A pull request with a failing test first, where the cause is in code we can
  reach (Plan 00254's build-test route, extended with Rust if needed).
- [ ] ⬜ **Task 4.3**: The fix reaches a release on Flathub (the play updates Lightning each run).

### Phase 5: teamchat's side (only if teamchat's traffic is shown to be the trigger)

- [ ] ⬜ **Task 5.1**: Change the triggering pattern in teamchat (for example fewer threads in main,
  fewer receipts, no presence flaps), as its own design note and Defence Before Fix, with the
  reproduction as the test; check Plan 00267's `teamchat.receipt` events against it before they go
  live.

### Phase 6: Verify live and decide the cap

- [ ] ⬜ **Task 6.1**: The reproduction holds steady memory on the fixed build (and the teamchat
  change, if any).
- [ ] ⬜ **Task 6.2**: The busy team's instance on the laptop over a working day: growth sampled with
  laptop-commands.md step 6. PASS, FAIL or NOT TESTED.
- [ ] ⬜ **Task 6.3**: Owner decision D2: keep or remove the stopgap cap.
- [ ] ⬜ **Task 6.4**: Run the `qa-reviewer` agent over the plan's diff; resolve its findings.

## Owner decisions

| ID  | Question                                                                                                                      | Recommendation                                                                         |
| --- | ----------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| D1  | Launch the busy team's Lightning with its traces and a log file?                                                              | Yes, Task 1.4's memory tracing                                                         |
| D2  | Keep the memory cap once the fix is verified?                                                                                 | Decide after Task 6.2                                                                  |
| D3  | Where `kernel.yama.ptrace_scope` is 1 (play-self-update.yml), lower it while tracing, so the watchdog's backtrace can attach? | No: it guards the become password; the per-thread lines still record (RESEARCH.md 7.5) |

## Dependencies

- Plan 00254 (Lightning as the default team client): the launcher, the per-team instances, the
  upstream relationship and its build-test route. This plan does not change 00254's tasks.
- Plan 00267 (lossless delivery): its `teamchat.receipt` events add reports-room traffic; Task 5.1
  checks them.
- Plan 00181 (ccy session limits) covers containers, not desktop clients: no overlap.

## Success Criteria

- [ ] The mechanism is named and shown by a measurement, on the laptop or in the reproduction.
- [ ] An upstream issue (and a pull request where we could) carries the evidence; a release
  carries the fix.
- [ ] A busy team's Lightning stays within a steady band over a working day on the laptop.
- [ ] Any teamchat change is tested by the reproduction.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00268-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Research and plan (this plan's first commit).
