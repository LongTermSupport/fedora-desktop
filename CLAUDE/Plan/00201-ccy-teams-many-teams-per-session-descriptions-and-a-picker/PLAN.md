# Plan 00201: ccy --teams — many teams per session, team descriptions, and a picker

**Status**: In Progress (M1-M7 built and deployed; the automated two-team acceptance passed on the host; waiting on the owner's steps in ACCEPTANCE-owner.md and the final qa-reviewer)
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

- [x] ✅ **Task 1.2**: [SPEC-many-teams.md](SPEC-many-teams.md): numbered expectations for
  descriptions, the shared chooser (no-cover, several-cover, scope widening), several teams
  per session, replying to a peer, and `cc` on the same flow. Decisions Q1-Q10: the spec's
  recommendations are taken, by the coordinator, on the owner's instruction of 2026-10-15
  not to wait for him and to decide properly and pragmatically. The owner may
  overrule any of them. The spec is written and Q1-Q10 are decided.

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
  recommends (REPLY after HUMAN and PEER only, not PING): taken, the owner may overrule.
  Report: `subagent-reports/261015-m3-reply-sonnet.md`.
- [x] ✅ **Task 2.4**: M4, the shared chooser in Go (`ccy-teams choose-teams`), scope widening,
  ccy wired (CCY 3.97.6). Single team (S-B1 to S-B15 without multi-select): `declare.py choices`
  and `add-repo`, the Go chooser, the wizard's chooser mode, `for-checkout --choose` and
  `ccy_seats_resolve` deleted. Report: `subagent-reports/261015-m4-chooser-sonnet.md`.
- [x] ✅ **Task 2.5**: M5, several teams per session (S-B3 `--multi`, S-C1 to S-C9), CCY 3.97.8 /
  container 2.58. `choose-teams` list is a multi-select with kept marks across rounds; `describe-seats --block` prints the launch block after `seat take`; SessionStart's first line names the teams; a
  status line shows `bus: A,B`; `pingbus send` infers the team from `--re` or `--to`, `say` from
  `--to`, or they fail with the choices. The segment reads a per-session record the pingbus
  SessionStart hook writes. It was first a hooks-daemon handler in this repository only; after the
  final review it is the kit's `pingbus status-line` (Task 2.8). M6 must also print the launch
  block for `cc --teams`.
  Reports: `subagent-reports/261015-m5-many-teams-sonnet.md`, review
  `subagent-reports/261015-m5-review-fable.md`.
- [x] ✅ **Task 2.6**: M6, `cc --teams` on the shared flow, its consent inside it (S-E1 to S-E6),
  CCY 3.97.10 / container 2.58 as first built, 3.97.11 / 2.59 after the M6 review fixes, and 3.98.2 / 2.60 once the integration merge joined the goodbye and relay line. `cc` takes ccy's list grammar and, on a terminal, calls
  `ccy-teams choose-teams --type=host`; the Element consent is Go (`chooser/consent.go`), asked after
  the seats are chosen and before any is made, reading and writing `~/.config/pingbus/element-accepted`
  with the old rules. `agent-bus seat check`, `seat take --type=host`, then the launch block (M5 F6)
  and `describe-seats --sentence` for the prompt. `cc-teams.bash` lost `cc_teams_seat` and
  `cc_teams_accept` (170 to 113 code lines). The kit plugin's SessionStart hook covers the status-line
  record on the host (pinned in `scripts/test-cc-teams.bash`). Report:
  `subagent-reports/261015-m6-cc-teams-sonnet.md`.
- [ ] 🔄 **Task 2.7**: M7, host run through `meta-deploy.bash`; the owner launches a session on
  two teams from an uncovered checkout; the two-team acceptance leg. qa-reviewer over the
  plan. Automated part passed on the host at F44 6affbfec (D1-D3, M1-M4, R1-R2, P1, C1):
  [acceptance.bash](acceptance.bash)
  (legs in `_acceptance-m7.inc.bash`, judgements in `m7_check.py`, tested by
  `test_m7_check.py`) makes two throwaway teams declared only in a run-directory host_vars
  (`CCY_TEAMS_HOST_VARS`), checks descriptions (`ccy-teams --list`, room topic, `ABOUT`), one
  headless session on both (launch block, per-session record, `pingbus status-line` in the
  session's container, `say` inference and refusal, SessionEnd), a plain session, then
  purges and checks the firewalld rules are gone. The interactive steps (chooser, scope widening, cc consent) are the owner's:
  [ACCEPTANCE-owner.md](ACCEPTANCE-owner.md). Report:
  `subagent-reports/261015-m7-acceptance-opus.md`. M3 changed with Task 2.9, so the automated
  part must pass again after the host run.
- [ ] ⬜ **Task 2.8**: S-D1 in relay mode: `_deliver` (`helpers/pingbus/cli.py`) writes a
  relay-delivered `HUMAN` or `PEER` to the spool with no `REPLY` line, so the session never
  gets the ready reply command. Expected to be carried by the structured-injection work of
  [Plan 00194](../00194-agent-bus-live-delivery-supervisor-plugin/PLAN.md) on branch
  `feat/bus-inject-message`; check the reply line is there when it merges, else add it here
  (tests first). Found in `subagent-reports/261015-bus-bugs-opus.md`.
- [x] ✅ **Task 2.9**: the final qa-reviewer's findings
  (`subagent-reports/261015-final-qa-reviewer.md`), CCY 3.98.7 / container 2.62. The `bus: A,B`
  status line works in any checkout: the member kit's `settings.json` makes `pingbus status-line`
  the status line (command-line settings outrank the project's), which runs the session's own
  status line first; the hooks-daemon handler is gone, and the tests are pingbus's, in the
  helper-tests gate. A record SessionStart could not write is told to the session and shown in red
  (`bus: no record ...`), never as nothing. S-C6, S-C8 and Q6/Q7 match what was built; `cc --teams`
  makes `.claude/ccy/` only after the consent (S-E4). Report:
  `subagent-reports/261015-qa-review-fixes-opus.md`.
- [ ] 🔄 **Task 2.10**: the client-side findings of the end-to-end security audit, CCY 3.98.10 /
  container 2.65, plugin 1.2.1, branch `fix/teams-security-client`. H1: `agent-bus seat take` and
  `seat remove` end with `sudo -k` however they end, and a `ccy-teams` run that made sudo ready
  drops it before exiting (`sudo.Forget`); the `sudo -v` advice is gone. H2: cc's settings deny
  Bash the plain routes to the keyring and Element's profile, and the consent says ccy is the
  safe choice. M2, M3, permission laundering and L7 in the skill and the delivery frame; L1 in
  the consent and the launch block; L4: a copied password is cleared after 45 seconds; M1, H2,
  L1 in the docs' Limits. Host run pending (meta-deploy). Report:
  `subagent-reports/261015-security-client-opus.md`.

## Success Criteria

- [ ] From a checkout no team covers, `ccy --teams` offers the teams with their descriptions,
  and one choice later the session is on that team.
- [ ] One session is on two teams, and both the launcher and `pingbus status` say so.
- [ ] Every team listing shows the team's description.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00201-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Research and spec: 15e34376; `teams_for` exclude fix: 1f30360e
- M3 (replying): 23f3021d
- M1+M2 (descriptions): c38787c5
- M4 (shared chooser, scope widening): 718b8067
- M5 (several teams per session): 202b254f; integrated with F44 in daae0612
- M6 (`cc --teams` on the shared flow): 397ca234; integrated in c95036bb
- M7 (two-team acceptance): 3c137fa4; the REPLY pairing check: 6affbfec
- Final review fixes: 3a4014b0; ccy play queued for CCY 3.98.5: 43d6b861
