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
- Any v1 change: v1 is feature-frozen.
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

- [ ] 🔄 **Task A.1**: Fable agents audit eight areas: admin, host, LXC, engine, link,
  security, durability and cross-cutting. Their reports go in `reports/audit-*.md`, ranked by
  severity.
- [ ] ⬜ **Task A.2**: the findings merged into one ranked list, [reports/AUDIT.md](reports/AUDIT.md),
  with each finding assigned to a fix pull request. Milestone note to the coordinator.

### Phase B: fixes (one pull request each, test-first)

- [ ] ⬜ **Task B.1**: lift B1, giving each seat kind its own base url.
- [ ] ⬜ **Task B.2**: members ride out the bus host's suspend and resume, and a lost
  connection: `(down)` on the status line, automatic reconnect, queued sends delivered once.
  Tested by suspending the homeserver and by cutting the network path.
- [ ] ⬜ **Task B.3**: join codes (`member invite`, `seat join`).
- [ ] ⬜ **Task B.4**: LXC on another host as an ordinary client (static binary plus user unit,
  installed by a play).
- [ ] ⬜ **Task B.5 onward**: the audit's other findings, grouped by area. To be listed after
  Task A.2.
- [ ] ⬜ **Task B.n**: docs against code (W9).

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
