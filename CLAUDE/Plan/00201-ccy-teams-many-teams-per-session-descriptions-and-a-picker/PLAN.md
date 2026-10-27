# Plan 00201: ccy --teams — many teams per session, team descriptions, and a picker

**Status**: In Progress. M1-M7 built and deployed; the automated two-team acceptance last
passed on the host at F44 cab255ad4 (commit d2612e8d7), with Tasks 2.21 to 2.23's checker
fixes. The relay task 2.8 is superseded; Tasks 2.11, 2.12, 2.15, 2.17 and 2.18 are carried by
Plan 00209. Left for workers: Task 2.27 (proposed: the copies Task 2.24 could not read), a scripted test human for the Element checks, and the
final qa-reviewer. Left for the host agent: the interactive checks of
[ACCEPTANCE-owner.md](ACCEPTANCE-owner.md) driven through tmux (Tasks 2.7, 2.12 check 11,
2.14, 2.15 step 13) and Task 2.20's `ccy-teams lxc-add` of the member refused before; the
Morning start's interactive seat chooser (steps 5 and 6, the seat and role questions) runs on two
throwaway teams that [chooser-teams-up.bash](chooser-teams-up.bash) leaves up and
[chooser-teams-down.bash](chooser-teams-down.bash) purges, checking their firewalld rules gone. Left for
the owner: nothing a check needs; only signing in to Element with the owner's own account, if
the owner wants that session.
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

- [x] ✅ **Task 1.1**: the current design mapped: [RESEARCH-current-design.md](RESEARCH-current-design.md). [Full text](DONE-tasks.md).

- [x] ✅ **Task 1.2**: [SPEC-many-teams.md](SPEC-many-teams.md) written; Q1-Q10 decided. [Full text](DONE-tasks.md).

### Phase 2: Build (tests first; milestones M1-M7 of the spec)

Bug fixed first: `teams_for` (`declare.py teams-for`, behind `ccy-teams for-checkout`) now
honours a team's `exclude` through the bus's own `protocol.is_excluded`, so a team that excludes
a repository is no longer offered for a checkout of it. Report:
`subagent-reports/261015-teams-for-exclude-sonnet.md`.

- [x] ✅ **Task 2.1**: M1, team descriptions declared and shown on the host. [Full text](DONE-tasks.md).

- [x] ✅ **Task 2.2**: M2, descriptions reach agents (the room topic, `ABOUT`). [Full text](DONE-tasks.md).

- [x] ✅ **Task 2.3**: M3, replying: a REPLY command, `--to` by short name. [Full text](DONE-tasks.md).

- [x] ✅ **Task 2.4**: M4, the shared chooser in Go and scope widening (CCY 3.97.6). [Full text](DONE-tasks.md).

- [x] ✅ **Task 2.5**: M5, several teams per session (CCY 3.97.8). [Full text](DONE-tasks.md).

- [x] ✅ **Task 2.6**: M6, `cc --teams` on the shared flow, its consent inside it. [Full text](DONE-tasks.md).

- [ ] 🔄 **Task 2.7**: M7, host run through `meta-deploy.bash`; the owner launches a session on
  two teams from an uncovered checkout; the two-team acceptance leg. qa-reviewer over the
  plan. Automated part passed on the host at F44 6affbfec (D1-D3, M1-M4, R1-R2, P1, C1):
  [acceptance.bash](acceptance.bash)
  (legs in `_acceptance-m7.inc.bash`, judgements in `m7_check.py`, tested by
  `test_m7_check.py`) makes two throwaway teams declared only in a run-directory host_vars
  (`CCY_TEAMS_HOST_VARS`), checks descriptions (`ccy-teams --list`, room topic, `ABOUT`), one
  headless session on both (launch block, per-session record, `pingbus status-line` in the
  session's container, `say` inference and refusal, SessionEnd), a plain session, then
  purges and checks the firewalld rules are gone. Report:
  `subagent-reports/261015-m7-acceptance-opus.md`. M3 changed with Task 2.9; the automated part
  passed again on the host at F44 cab255ad4 (commit d2612e8d7). The interactive steps of
  [ACCEPTANCE-owner.md](ACCEPTANCE-owner.md) (chooser, scope widening, launch block, status
  line, cc consent, shutdown, settings screen) are the host agent's: it drives them through
  tmux `send-keys` and reads them with `capture-pane`, on throwaway teams in an uncovered
  scratch checkout. Its checks that post "from Element" use a scripted test human (a worker
  builds it: a declared test human whose password the agent-bus admin tool sets with
  `human password`, the sender logging in with it; never the owner's account). Check 03 (homeserver start times unchanged) becomes a scripted leg of
  `_acceptance-m7.inc.bash` (a worker). Check 10 (remove the added scope) has nothing to remove
  when the checks ran on throwaway teams.

- [ ] ❌ **Task 2.8**: superseded by Plan 00209 D4 (relay mode deferred to v2). [Full text](DONE-tasks.md).

- [x] ✅ **Task 2.9**: the final qa-reviewer's findings (CCY 3.98.7). [Full text](DONE-tasks.md).

- [x] ✅ **Task 2.10**: UX fixes before the owner's first day with teams (CCY 3.98.9). [Full text](DONE-tasks.md).

- [x] ✅ **Task 2.11**: the client-side security audit findings (CCY 3.98.10); carried by Plan 00209. [Full text](DONE-tasks.md).

- [ ] 🔄 **Task 2.12**: the final release security review's findings
  (`subagent-reports/261015-final-release-security-fable.md`), branch `fix/teams-final-review`,
  CCY 3.98.11 / container 2.66 / plugin 1.2.3. S1: no clipboard offer while Clipboard Indicator
  is on (it honours no password hint), said in one line; Limits and the owner's check 11. N1:
  the read receipt waits for the transcript check (SPEC-inject E16). N2: the sign-in lock in
  Limits. N3: JSON-decode before comparing, in the skill and frame. N4: SPEC-inject E2 names
  the nonce. N5: the pre-swap backup also runs on a rendered config change
  (`helpers/agent_bus/config_restart.py`). Deployed at F44 c3391ba9; left: check 11, the host agent's on a test
  human (a password reset logs out only that human's Element sessions): Ctrl-P with Clipboard
  Indicator on and off (`gnome-extensions disable`/`enable`), the top-bar menu by screenshot,
  and `wl-paste` showing a second paste is empty. Report:
  `subagent-reports/261015-final-review-fixes.md`.
  Plan 00209 carries this as an expectation ("Requirements carried from other plans").

- [x] ✅ **Task 2.13**: the Morning start dry run's ccy-teams defects. [Full text](DONE-tasks.md).

- [ ] 🔄 **Task 2.14**: shut down every session of a team from ccy-teams (the owner: "pass /exit
  to all members of a team"), branch `feat/teams-shutdown`, ccy-teams only. Spec:
  [SPEC-shutdown.md](SPEC-shutdown.md) (SD1-SD16). `ccy-teams shutdown <team> [--yes]` and a
  team row's menu line show the team's live sessions and what cannot be reached from here,
  ask (default No), type `/exit` into each session's pane through tmux (Escape first when
  busy), stop one that has not ended in time, check each seat is released, and report each
  member. Local only: nothing on the bus. Deployed at F44 c3391ba9 (play-claude-yolo.yml);
  left: check 12 of ACCEPTANCE-owner.md, the host agent's (a throwaway team with two ccy
  sessions in tmux, `ccy-teams shutdown <team>` from a pane, the released-seat lines read back).

- [ ] 🔄 **Task 2.15**: a team's settings from ccy-teams (the owner: "a single update config
  thing … that then opens a pane that then has all of the different things editable"), branch
  `feat/team-settings`, CCY 3.99.12.5 / container 2.77.4 (helpers/agent_bus is in the image). A removed person
  is recorded in `removed_humans` and the team file refuses the name for good (S-F14, Fable's
  FIX-FIRST, `subagent-reports/261016-team-settings-review-fable.md`). Spec: [SPEC-many-teams.md](SPEC-many-teams.md) section
  F (S-F1 to S-F14, with the field table). A team row's **Edit settings** opens one screen of
  every field with its value: description, people, GitHub owner, repositories, excluded
  repositories, path limit and allowed networks are edited in place and judged by the new
  `declare.py set --check`; Save and apply shows each change and what applying does, writes
  them in one `declare.py set` and applies through `ops.ApplyTeams`. The coordinator changes at
  once through `seats.ChangeRole`; name, port, listen, server name and forge API are fixed,
  each saying why. `describe` and `drop-limit` are now `set` with one key. Deployed at F44
  c3391ba9 (play-claude-yolo.yml); left: ACCEPTANCE-owner.md check 13, the host agent's (the
  screen driven through tmux keys, the homeserver start time unchanged, the room topic read
  with the test human's token).
  Report: `subagent-reports/261016-team-settings-opus.md`.
  Plan 00209 carries this as an expectation ("Requirements carried from other plans").

- [ ] ❌ **Task 2.16**: folded into Plan 00209: renaming a team. [Full text](DONE-tasks.md).

- [x] ✅ **Task 2.17**: three picker defects: whole handles, LXC checkouts, column widths; carried by Plan 00209. [Full text](DONE-tasks.md).

- [x] ✅ **Task 2.18**: ccy-teams waits for the play lock; carried by Plan 00209. [Full text](DONE-tasks.md).

- [x] ✅ **Task 2.19**: run.bash waits on the play lock (run.bash 1.34.0). [Full text](DONE-tasks.md).

- [ ] 🔄 **Task 2.20**: `ccy-teams lxc-add` asked three times over "entries 5 and 6 both
  declare <user>@<container>" for a member an earlier run had declared (branch
  `fix/lxc-add-already-declared`, CCY 3.99.15.5 / container 2.78.18, the image's declare.py).
  DBF class: a declare.py refusal read by its type and not its kind, so a refusal of the
  host_vars file is asked again as a wrong answer; defence `project_refusal_kind`
  ([GoProjectRules.md](../../GoProjectRules.md)), red at 0ca99358c with 4 findings (lxcadd
  `checked`; create `checked`, `isRefusal`, `askHumans`), green over both modules after the
  fix. declare.py now gives a value alone 64 and an entry against the file 78; lxc-add says a
  member declared as given is declared already and applies after one yes, names each key that
  differs and offers to update, and stops at once on a refusal of the file, naming it. The
  create wizard stops the same way, an interrupt cancels, and a declare.py that crashes is said
  as a failure, not a refusal of the file. After the DBF conformance review and Opus round 1 the
  rule clears a function only on `ErrUsage` or the exit code handed on whole (red on three new
  fixture shapes; its one new real-tree finding, lxc-add's dry-run refusal, stops with the code
  in `&stop{...}`, so the rule now counts a code handed on inside a returned literal). After
  review round 2 a code handed on clears only the recovery whose block returns it
  unconditionally (red at 7e40c23a9 on a code handed on for an interrupt only); the shapes the
  rule still misses are listed in GoProjectRules.md and referred to the owner as a rule
  narrower than the hazard. Decided as a **coordinator default, changeable by the owner**: a
  rule narrower than its hazard, with what it misses listed in GoProjectRules.md, is this
  project's established pattern (every rule there has its "Not caught"), so the branch merges
  with the misses recorded; it merged into F44 at dda4735c9 (DBF CONFORMS, Opus MERGE). In declare.py a team not declared present is 78 (`ccy-teams apply`
  and `lxc-add` of an absent team keep their own 64, `ops.go:160`, `adder.go:341`: a command-line
  argument, never asked again), `set` gives 78 only when the refusing rule reads no key the
  change sets (a table of the keys `exclude`, `humans` and `org` read), `add-repo` of a
  repository the team trusts already is UNCHANGED, `lxc-add --update` refuses to lose a comment,
  and `create`'s "already declared" names `ccy-teams apply`. lxc-add's Stop on a member declared otherwise names
  `ccy-teams apply <team>`. Found and fixed with it: the
  wizard read any failing `gh api` as "no such name" (a probe read as a verdict, the class in
  GoProjectRules.md "cannot be a rule"): only gh's `(HTTP 404)` is not found, any other failure
  ends the run with gh's words. Also D1 of the M7 acceptance (`m7_check.py`) reads the list's
  columns from its heading (64d5bc3b6). Deployed: `play-claude-yolo.yml` passed on the host at
  F44 b0eb800fa (commit eaa947411) and again at cab255ad4. Left, for the host agent: run
  `ccy-teams lxc-add <team>` from a pane for the member that was refused before, and record
  that it says the member is declared already and applies after one yes. Reports:
  `subagent-reports/261022-lxc-add-*.md`.

- [x] ✅ **Task 2.21**: M7's checker copied seat.py's guard; `checker-copies-cited-constant` built. [Full text](DONE-tasks.md).

- [x] ✅ **Task 2.22**: the other plans' copies converted; its referrals decided as coordinator defaults. [Full text](DONE-tasks.md).

- [x] ✅ **Task 2.24**: the inline texts named in the product and read by the checkers, in three branches. [Full text](DONE-tasks.md).

- [ ] ⬜ **Task 2.27** (proposed by Task 2.24's worker): the copies Task 2.24 left. (a) The
  homeserver unit names as named templates, the checkers of Plans 00194, 00201 and 00203 reading
  them, in `OWNED`. (b) A reader for the bash and other-language products of the left rows (B5,
  B11, B12, B14, B15, rows 19, 22, 25-27, 31, 32), or a judgement per row that it stays a copy.
  (c) 00194's acceptance reads `the team syncs again` (cli.py inline), found on the way.

- [x] ✅ **Task 2.25**: the `plan-tests` gate runs each plan folder's own tests. [Full text](DONE-tasks.md).

- [x] ✅ **Task 2.26**: the `assignment-masks-substitution-failure` rule. [Full text](DONE-tasks.md).

- [x] ✅ **Task 2.23**: the container wait no longer counts an image build; Task 2.22's conversion recorded. [Full text](DONE-tasks.md).

- [ ] ⬜ **Task 2.28**: `CCY_TEAMS_HOST_VARS` is honoured half-way: ccy-teams writes a team's
  change into that file, but `runPlay` (`src-go/internal/teams/ops/ops.go`) runs
  `play-agent-bus.yml` with only `-e agent_bus_only_teams`, so the play reads the default
  host_vars and fails its assert, and the launch then says the team is "not applied" (host
  agent, 27-10-27, on chooser-teams-up's teams: steps 03, 10 and 13 cannot pass). A setting that
  changes where ccy-teams writes must change what it applies too, or be refused. Make every play
  run use the same host_vars ccy-teams wrote (or refuse an override the play cannot read), test
  first; sweep for any other reader of the default path that ignores the override.

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
