# Plan 00201: ccy --teams — many teams per session, team descriptions, and a picker

**Status**: In Progress (research and spec)
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

- [ ] 🔄 **Task 1.1**: Map the current design. Cover:

  - how `ccy --teams` picks a team (`src-go/internal/teams/find`, the ccy launcher);
  - seats and the seat registry;
  - the member client's per-team state (`pingbus --team`, `status` already lists teams);
  - the inbox watcher and wake path;
  - the plugin and the hooks;
  - the trust check.

  For each, record whether it assumes one team per session.

- [ ] ⬜ **Task 1.2**: `SPEC-many-teams.md`: numbered expectations for multi-team seats, team
  descriptions (where declared, validated, shown), and the picker (no-cover, several-cover,
  scope widening), with fail-fast cases. Owner reviews the decisions it raises.

### Phase 2: Build (tests first)

- [ ] ⬜ **Task 2.1**: Team descriptions, end to end.
- [ ] ⬜ **Task 2.2**: The picker for an uncovered checkout, with scope widening.
- [ ] ⬜ **Task 2.3**: Several teams per session.
- [ ] ⬜ **Task 2.4**: Host run through `meta-deploy.bash`; the owner launches a session on two
  teams from an uncovered checkout. qa-reviewer over the plan.

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
