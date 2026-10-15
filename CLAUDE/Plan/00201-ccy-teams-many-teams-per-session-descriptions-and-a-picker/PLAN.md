# Plan 00201: ccy --teams — many teams per session, team descriptions, and a picker

**Status**: In Progress (M1-M5 built; M6 cc on the shared flow next, then M7)
**Created**: 2026-10-15
**Owner**: joseph
**Priority**: High

## Overview

`ccy --teams` puts a session on one agent team. The owner, after using it:

- **A session must be able to join several teams**, and it must be plain, in the launcher and
  in the session, which teams it is on.
- **Each team needs a description**, shown wherever teams are listed or chosen, so it is clear
  what a team is for.
- **No hard stop when no team covers the checkout.** Today a checkout no team covers ends
  with "No team on this machine works on OWNER/REPO … name a team: ccy --teams <team>". It
  should offer the teams to choose from instead of making the owner type a name.
- More generally, ccy and ccy-teams should work together more smoothly.

A team trusts specific GitHub owners and repositories (Plan 00161). So joining a team that
does not cover this checkout means widening that team's scope to this repository, applied
by the play, before the seat can be trusted. The picker has to say so and do it, not just
start a seat the bus will refuse.

The agent team bus is Plan 00161's. This plan changes how sessions join teams; it does not
change the bus protocol unless the research shows it must.

## Goals

- One ccy session holds seats on several teams, chosen at launch, and shows them.
- Every team has a description: declared with the team, and shown by `ccy-teams`, the
  `ccy --teams` picker and `pingbus status`.
- `ccy --teams` in a checkout no team covers offers every team, with its description, and
  creating one. Choosing a team that does not cover the checkout offers to add this
  repository to its scope, applies it, and then starts the session.
- Fail fast throughout: no silent fallback to "no team".

## Non-Goals

- Changing who a team trusts without the owner's yes.
- Teams across machines (Plan 00197).

## Tasks

### Phase 1: Research and spec

- [x] ✅ **Task 1.1**: Map the current design and record, for each piece, whether it assumes
  one team per session: [RESEARCH-current-design.md](RESEARCH-current-design.md). Several
  teams already work end to end through `ccy --teams 1@alpha,2@beta` (unit-tested, never run
  live); the one-team assumptions are the bare-`--teams` chooser and the picker's
  join offer. `cc --teams` has drifted from ccy (one team, no chooser).

- [ ] 🔄 **Task 1.2**: [SPEC-many-teams.md](SPEC-many-teams.md): numbered expectations for
  descriptions, the shared chooser (no-cover, several-cover, scope widening), several teams
  per session, replying to a peer, and `cc` on the same flow. Decisions Q1-Q10: the spec's
  recommendations are taken, by the coordinator, on the owner's instruction of 2026-10-15
  not to wait for him and to decide properly and pragmatically. The owner may
  overrule any of them.

### Phase 2: Build (tests first; milestones M1-M7 of the spec)

Bug fixed first: `teams_for` (`declare.py teams-for`, behind `ccy-teams for-checkout`) now
honours a team's `exclude` through the bus's own `protocol.is_excluded`, so a team that excludes
a repository is no longer offered for a checkout of it. Report:
`subagent-reports/261015-teams-for-exclude-sonnet.md`.

- [x] ✅ **Task 2.1**: M1, descriptions declared and shown on the host (S-A1 to S-A7, S-A13 to
  S-A16). Built test-first: team file `description`, `declare.py` create/check/describe/
  `undescribed` and the 7-field `TEAM` line, the board's MEMBER cell, the create wizard's question,
  the picker's one-time offer, Quick Launch's `Team:` line, and an installer case proving a
  description-only change restarts nothing. Q1 (required at create, optional in the file) taken as
  the spec recommends. Report: `subagent-reports/261015-m1-m2-sonnet.md`.
- [x] ✅ **Task 2.2**: M2, descriptions reach agents (S-A8 to S-A12), revised after review: the
  description travels as the admin-written room topic, read by the syncer into `about.json`
  (only from the admin, only if it passes the grammar, never the fixed sentence), NOT as a
  team-record key, because an unknown record key makes every older member exit 10. Then
  `pingbus status` prints `ABOUT` and SessionStart says "team T is for: ...". Container 2.57 /
  CCY 3.97.4. No deploy-order hazard remains. Q2 and Q3 as the spec recommends. Report:
  `subagent-reports/261015-m1-m2-sonnet.md`.
- [x] ✅ **Task 2.3**: M3, replying: `recv` prints a REPLY command; `--to` takes a seat's
  short name. Built test-first in `helpers/pingbus` (S-D1 to S-D6); Q8 taken as the spec
  recommends (REPLY after HUMAN and PEER only, not PING), pending the owner's confirmation.
  Report: `subagent-reports/261015-m3-reply-sonnet.md`.
- [x] ✅ **Task 2.4**: M4, the shared chooser in Go (`ccy-teams choose-teams`), scope widening,
  ccy wired (CCY 3.97.6). Single team (S-B1 to S-B15 without multi-select): `declare.py choices`
  and `add-repo`, the Go chooser, the wizard's chooser mode, `for-checkout --choose` and
  `ccy_seats_resolve` deleted. Report: `subagent-reports/261015-m4-chooser-sonnet.md`.
- [x] ✅ **Task 2.5**: M5, several teams per session (S-B3 `--multi`, S-C1 to S-C9), CCY 3.97.8 /
  container 2.58. `choose-teams` list is a multi-select with kept marks across rounds; `describe-seats --block` prints the launch block after `seat take`; SessionStart's first line names the teams; a
  project status-line handler shows `bus: A,B`; `pingbus send`/`say` infer the team from `--re` or
  `--to` or fail with the choices. The status-line segment reads a per-session record the pingbus
  SessionStart hook writes (the daemon is shared, so not the environment). Its tests
  (`.claude/hooks/handlers/status_line/tests`) are in no QA gate: they need the daemon's package and
  its venv's pydantic, which only exist on a checkout with an installed daemon, and routing them
  would be a new qa-changed check in Go; the host run must restart and health-check the daemon and
  look at a two-team session's status line. M6 must also print the launch block for `cc --teams`.
  Reports: `subagent-reports/261015-m5-many-teams-sonnet.md`, review
  `subagent-reports/261015-m5-review-fable.md`.
- [ ] ⬜ **Task 2.6**: M6, `cc --teams` on the shared flow, its consent inside it.
- [ ] ⬜ **Task 2.7**: M7, host run through `meta-deploy.bash`; the owner launches a session on
  two teams from an uncovered checkout; the two-team acceptance leg. qa-reviewer over the
  plan.

## Success Criteria

- [ ] From a checkout no team covers, `ccy --teams` offers the teams with their descriptions,
  and one choice later the session is on that team.
- [ ] One session is on two teams, and both the launcher and `pingbus status` say so.
- [ ] Every team listing shows the team's description.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00201-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- <!-- milestone or delivery commit hash -->
