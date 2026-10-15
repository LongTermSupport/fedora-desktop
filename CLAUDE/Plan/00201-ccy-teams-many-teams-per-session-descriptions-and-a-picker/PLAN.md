# Plan 00201: ccy --teams — many teams per session, team descriptions, and a picker

**Status**: In Progress (spec done, M3 replies built; M1-M2 descriptions next, then M4-M7)
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
  ("DO NOT GATE ON ME - get this all done properly and pragmatically"). The owner may
  overrule any of them.

### Phase 2: Build (tests first; milestones M1-M7 of the spec)

Bug fixed first: `teams_for` (`declare.py teams-for`, behind `ccy-teams for-checkout`) now
honours a team's `exclude` through the bus's own `protocol.is_excluded`, so a team that excludes
a repository is no longer offered for a checkout of it. Report:
`subagent-reports/261015-teams-for-exclude-sonnet.md`.

- [ ] ⬜ **Task 2.1**: M1, descriptions declared and shown on the host.
- [ ] ⬜ **Task 2.2**: M2, descriptions reach agents (team record, room topic, `pingbus status`,
  SessionStart); container bump.
- [x] ✅ **Task 2.3**: M3, replying: `recv` prints a REPLY command; `--to` takes a seat's
  short name. Built test-first in `helpers/pingbus` (S-D1 to S-D6); Q8 taken as the spec
  recommends (REPLY after HUMAN and PEER only, not PING), pending the owner's confirmation.
  Report: `subagent-reports/261015-m3-reply-sonnet.md`.
- [ ] ⬜ **Task 2.4**: M4, the shared chooser in Go (`ccy-teams choose-teams`), scope widening,
  ccy wired.
- [ ] ⬜ **Task 2.5**: M5, several teams per session: multi-select, launch block, status line,
  `send`/`say` team inference.
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
