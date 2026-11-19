# Plan 00257: cross host lxc team members

**Status**: In Progress (owner GO on Q44, 2026-11-19 11:57Z: audit teamchat end to end and fix every weakness; Phase A, the audit, is running)
**Created**: 2026-11-19
**Owner**: joseph
**Priority**: High

## Overview

The owner's ruling on Q44 has two parts:

- An LXC container on any host joins any v2 (`teamchat`) team.
- teamchat as a whole is audited and fixed properly. In the owner's words: "It CANNOT BE SHIT.
  IT MUST BE SOLID. all weaknesses resolved (line count does not need to be reduced necessarily
  but debloating is definitely OK)."

[REPORT.md](REPORT.md) is the zoom-out that led to that GO: weaknesses W1-W9 and the prior art.
[ARCHITECTURE.md](ARCHITECTURE.md) is the earlier, narrower note. This plan is now the audit and
the fixes.

Approved design (owner, through the coordinator):

1. **Lift B1.** LXC seats get the bridge address in their bundle, and every other seat gets the
   team's `url`, which may then be a name.

2. **Join codes.** `teamchat-admin member invite` mints a single-use code, and
   `teamchat seat join <code>` redeems it on any machine. This replaces ssh, sudo and the
   credential pipe.

3. **LXC on another host** joins as an ordinary client: a static binary and a user unit.

4. **Suspending the bus host is a feature.** Members must ride out the homeserver's suspend
   and resume cleanly. This is a hard requirement, and it gets a test.

5. **Members that lose connectivity handle it smoothly** (owner, Q44, 12:03Z). For example, an
   off-host member when the bus host suspends or the VPN drops:

   - The status line shows that a seat's team connection is lost.
   - Reconnection is automatic: deterministic attempts in a loop, with backoff.

   Today the member's own link already retries 1 s doubling to 30 s, timed on the boot clock,
   and shows `(down)` after 15 s (`member/teamsync/teamsync.go:26-32, 328-367`,
   `member/statusline/segment.go:131-155`). The work is to prove it end to end and close the
   gaps the audit finds. The mechanism stays the member's own loop, not cron: it wakes
   instantly on activity, and a dead link process is already shown in red.

Related plans:

- Plan 00209 is v2's design.
- Plan 00246 Task 8.8 built cross-network ccy pool seats: url-keyed seats, TLS with the team
  CA, and `seat place-issued`.
- Plan 00220's overlay is dropped from the bus (Plan 00209 direction 7).

## Goals

- Every weakness resolved: W1-W9 (REPORT.md) and every audit finding of HIGH or above. Each
  MEDIUM is either fixed or recorded with the owner's agreement.
- One team holds LXC members on its own host and seats on any other machine. A seat on another
  machine is added with one join code.
- Members ride out a suspend and resume of the bus host with no lost, duplicated or wedged
  delivery, proven by a test.

## Non-Goals

- Any degrade. Every current capability survives, and dropping one needs the owner's yes.
- Any v1 change: v1 is feature-frozen. **Overridden for the session auto-start (Task B.12):**
  the owner's order, relayed by the coordinator, is that no LXC member session starts when
  its container boots, in either generation, so v1's `helpers/agent_bus_lxc/members.py` no
  longer enables or starts the unit, and v1 ccy-teams gains the on-demand start. Nothing
  else in v1 changes under this plan.
- Docker or VM guests.
- A teamchat proxy (REPORT.md: not needed).

## Rules (owner, through the coordinator)

- Test-first, in small pull requests merged with `--merge` (no squash), each with CI green and
  an independent review.
- Journal as the work goes.
- No estate hostnames or IPs in the repository or in upstream issues. Never read vault or
  secret files.
- Milestone notes go to the coordinator: audit ready, each pull request merged, done. Anything
  needing the owner goes through the coordinator.

## Tasks

### Phase 0: report and decision

- [x] ✅ **Task 0.1**: the report ([REPORT.md](REPORT.md), research in `research/`).
- [x] ✅ **Task 0.2**: the owner's decision: GO (Q44, 11:57Z).

### Phase A: audit

- [x] ✅ **Task A.1**: Fable agents audited eight areas: admin, host, LXC, engine, link,
  security, durability and cross-cutting. Reports: `reports/audit-*.md`. Totals: 0 critical (2
  conditional), 24 high.
- [x] ✅ **Task A.2**: the merged list, [reports/AUDIT.md](reports/AUDIT.md). Each finding is
  assigned to a fix pull request, and owner decisions D1-D5 are listed.

### Phase B: fixes (one pull request each, test-first; order and findings in AUDIT.md)

- [ ] 🔄 **Task B.1**: seat url per kind (lifts B1).
- [ ] ⬜ **Task B.2a**: clocks and deadlines across a suspend; time sync on the bus host.
- [ ] ⬜ **Task B.2b**: delivery across an outage: catch-up, the outbox policy, failed takes.
- [ ] ⬜ **Task B.2c**: the suspend and outage proof: a real-Tuwunel gate and an end-to-end test
  that freezes the homeserver and cuts the network. It also suspends a member's host (a VM in
  the lab) behind the TLS terminator and resumes it: every seat on it reconnects through the
  terminator while its old connections are still held there (dead, until the 2-minute idle
  limit), with no lost or duplicated message.
- [ ] ⬜ **Task B.2d**: a team's listen address moves while the bus host is suspended (HOST-4,
  `reports/audit-host.md`). Nothing in the host layer handles suspend and resume: if the bus
  host comes back with another address on the serving interface (a DHCP lease, a re-made VPN
  interface), the terminator stays bound to the old literal, the leaf names it, the bundles
  carry the old url, and only a new `team apply` re-derives them; the health tick probes the
  recorded addresses and does not re-derive. Design (from B.5's report):
  - a unit that runs at resume (`After=suspend.target`) and a boot equivalent, which runs the
    health tick at once;
  - the tick compares the live interface addresses with each team's `units.json` listen set
    and reports a moved address as its own problem ("the host's address moved: run team
    apply");
  - a declaration rule: a team's url on a host that suspends is a DNS name or a static address.
    Coordinator ruling (under the owner's "all weaknesses resolved"): HOST-4 is not deferred; it
    is this task.
- [ ] ⬜ **Task B.3**: join codes (`member invite`, `seat join`).
- [ ] ⬜ **Task B.4**: LXC on another host as an ordinary client (static binary plus user unit,
  installed by a play).
- [x] ✅ **Task B.5**: host units and the TLS terminator (`subagent-reports/b5-report.md`); the
  review's fixes in `subagent-reports/b5-fix-report.md`. HOST-4 is Task B.2d. Coordinator
  ruling (under the owner's "all weaknesses resolved"): the terminator's per-source connection
  cap is a per-team setting, `connections_per_source` (default 256). Its earlier ruling that a
  source at its cap has its dead connections reclaimed by a liveness check was withdrawn by the
  coordinator after the second review: with 1024 slots the cap binds only at 256 connections
  from one address, a ride-out at most briefly doubles a NAT's seats' connections, the
  keepalive probe cannot see a connection with unacknowledged data (a `/sync` answered after the
  suspend), and the reclaim raced twice. Over its cap a connection is refused at once; dead
  connections are freed by the 2-minute idle limit while the member's link backs off and
  reconnects (journal, `subagent-reports/b5-fix2-report.md`). The second review (`subagent-reports/b5-fix-review.md` item 5) found the default per-source cap
  equal to every slot, so SEC-4 was off by default; the owner-side decision was the
  coordinator's ruling (c): the terminator holds 1024 connections (`DefaultMaxConns`) and one
  source 256 by default (`DefaultMaxPerSource`), so by default no one address holds every slot
  (`subagent-reports/b5-fix2-report.md`). The homeserver takes 1024 upstream connections: Tuwunel
  raises its own soft descriptor limit to the hard one at start (`maximize_fd_limit`).
- [ ] 🔄 **Task B.12**: no LXC member session starts with its container (owner's order: "there
  is no need for claude to start as soon as the LXC container boots ... remove that
  completely"). Both bus generations: the member's session unit has no `[Install]` and
  `Restart=no`, apply never enables, links or starts it, and removes an earlier wants link
  (stopping that session). The only start is on demand: ccy-teams' Start its session in its LXC
  container (v1), `teamchat-admin lxc restart --member` (v2). Host proof:
  `lxc-no-autostart-acceptance.bash`. Report: `subagent-reports/lxc-no-autostart-report.md`.
- [ ] ⬜ **Task B.6**: LXC apply robustness.
- [ ] ⬜ **Task B.7**: durability (D1 for restore).
- [ ] ⬜ **Task B.8**: agent-facing text (D2 for the unbuilt verbs).
- [ ] ⬜ **Task B.9**: policy and admin (D3 for the approval list).
- [ ] ⬜ **Task B.10**: debloat and tests.
- [ ] ⬜ **Task B.11**: docs against code (W9).

### Phase C: review

- [ ] ⬜ **Task C.1**: the `qa-reviewer` agent over the finished work. Host acceptance queued
  in `meta-deploy.list`.

## Success Criteria

- [ ] A container on host Y holds a working seat on a team whose homeserver runs on host X,
  while that team also has LXC members on host X.
- [ ] A seat on another machine is added with one join code: no ssh from the bus host, no
  piped password.
- [ ] A test suspends the homeserver and resumes it, and every member catches up with no lost
  or duplicated message.
- [ ] No audit finding of HIGH or above remains open.

## Delivery & Milestones

- Report: REPORT.md. Architecture note: ARCHITECTURE.md.
