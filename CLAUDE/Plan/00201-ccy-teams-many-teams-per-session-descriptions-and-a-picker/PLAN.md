# Plan 00201: ccy --teams — many teams per session, team descriptions, and a picker

**Status**: In Progress. M1-M7 built and deployed; the automated two-team acceptance last
passed on the host at F44 cab255ad4 (commit d2612e8d7), with Tasks 2.21 to 2.23's checker
fixes. The relay task 2.8 is superseded; Tasks 2.11, 2.12, 2.15, 2.17 and 2.18 are carried by
Plan 00209. Left for workers: Task 2.19 (the wait in the Python lock helper), Tasks 2.24 to
2.26 (from Task 2.22's referrals), a scripted test human for the Element checks, and the
final qa-reviewer. Left for the host agent: the interactive checks of
[ACCEPTANCE-owner.md](ACCEPTANCE-owner.md) driven through tmux (Tasks 2.7, 2.12 check 11,
2.14, 2.15 step 13) and Task 2.20's `ccy-teams lxc-add` of the member refused before. Left for
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
  status line shows `👥 A,B`; `pingbus send` infers the team from `--re` or `--to`, `say` from
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
  purges and checks the firewalld rules are gone. Report:
  `subagent-reports/261015-m7-acceptance-opus.md`. M3 changed with Task 2.9; the automated part
  passed again on the host at F44 cab255ad4 (commit d2612e8d7). The interactive steps of
  [ACCEPTANCE-owner.md](ACCEPTANCE-owner.md) (chooser, scope widening, launch block, status
  line, cc consent, shutdown, settings screen) are the host agent's: it drives them through
  tmux `send-keys` and reads them with `capture-pane`, on throwaway teams in an uncovered
  scratch checkout. Its checks that post "from Element" use a scripted test human (a worker
  builds it: a declared test human whose token the agent-bus admin tool mints, never the
  owner's account). Check 03 (homeserver start times unchanged) becomes a scripted leg of
  `_acceptance-m7.inc.bash` (a worker). Check 10 (remove the added scope) has nothing to remove
  when the checks ran on throwaway teams.

- [ ] ❌ **Task 2.8**: Superseded by Plan 00209 D4: relay mode is deferred to v2. (Was: S-D1 in relay mode: `_deliver` (`helpers/pingbus/cli.py`) writes a
  relay-delivered `HUMAN` or `PEER` to the spool with no `REPLY` line, so the session never
  gets the ready reply command. Expected to be carried by the structured-injection work of
  [Plan 00194](../00194-agent-bus-live-delivery-supervisor-plugin/PLAN.md) on branch
  `feat/bus-inject-message`; check the reply line is there when it merges, else add it here
  (tests first). Found in `subagent-reports/261015-bus-bugs-opus.md`.)

- [x] ✅ **Task 2.9**: the final qa-reviewer's findings
  (`subagent-reports/261015-final-qa-reviewer.md`), CCY 3.98.7 / container 2.62. The `👥 A,B`
  status line works in any checkout: the member kit's `settings.json` makes `pingbus status-line`
  the status line (command-line settings outrank the project's), which runs the session's own
  status line first; the hooks-daemon handler is gone, and the tests are pingbus's, in the
  helper-tests gate. A record SessionStart could not write is told to the session and shown in red
  (`👥 no record ...`), never as nothing. S-C6, S-C8 and Q6/Q7 match what was built; `cc --teams`
  makes `.claude/ccy/` only after the consent (S-E4). Report:
  `subagent-reports/261015-qa-review-fixes-opus.md`.

- [x] ✅ **Task 2.10**: UX fixes before the owner's first day with teams (branch
  `fix/teams-ux`), CCY 3.98.9 / container 2.64 / plugin 1.2.1: a read receipt for each
  delivered human message, `say --re` threading replies (the `reply:` command carries it), a
  human's non-ASCII text shown as written with the hostile set still escaped (SPEC-inject.md
  E2/E8/E9 amended), the stopped-team and unknown-team launch refusals, Open every team in
  Element, "Talking to agents from Element" in docs/agent-bus.md, and
  [ACCEPTANCE-owner.md](ACCEPTANCE-owner.md) matched to the code with a Morning start and the
  00194 H1-H7 checks. Report: `subagent-reports/261015-ux-fixes-opus.md`.

- [x] ✅ **Task 2.11**: the client-side findings of the end-to-end security audit, CCY 3.98.10 /
  container 2.65, plugin 1.2.2, branch `fix/teams-security-client`. H1: `agent-bus seat take` and
  `seat remove` end with `sudo -k` however they end, and a `ccy-teams` run that made sudo ready
  drops it before exiting (`sudo.Forget`); the `sudo -v` advice is gone. H2: cc's settings deny
  Bash the plain routes to the keyring and Element's profile, and the consent says ccy is the
  safe choice. M2, M3, permission laundering and L7 in the skill and the delivery frame; L1 in
  the consent and the launch block; L4: a copied password serves one paste (`wl-copy --paste-once`); M1, H2,
  L1 in the docs' Limits. Deployed at F44 c3391ba9 (Plan 00194 journal, 2026-10-19 20:53). Report:
  `subagent-reports/261015-security-client-opus.md`.
  Plan 00209 carries this as an expectation ("Requirements carried from other plans").

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

- [x] ✅ **Task 2.13**: the Morning start dry run's ccy-teams defects (branch
  `fix/teams-signin-first-run`; ccy-teams only, no CCY or container bump). After a first
  password, Open in Element no longer says "Copy it now" (it said so with Clipboard Indicator
  on); the new-password question defaults to Yes for someone with no Element session, No
  otherwise; "Create a team for OWNER/REPO now?" defaults to No (S-B15). Deployed at F44
  c3391ba9 (play-claude-yolo.yml).

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

- [ ] ❌ **Task 2.16**: Folded into Plan 00209 (agent-bus v2), carried there as a requirement:
  a rename touches the homeserver, accounts, rooms, tokens and every seat, which v2 rebuilds,
  so a v1 rename would be built twice. v1 keeps the installer's refusal of a changed name.
  (Was: rename a team, end to end. Not built: the settings screen shows the name
  as fixed, with the reason. A rename must make the new team (homeserver unit, data folder,
  server name, accounts, room), carry over or re-create its history, re-issue every member's
  token and bundle, move every seat and session record, re-point every running session, then
  take the old team down; today the installer refuses a changed name (`render.check_unchanged`).
  Needs its own research and spec first (SPEC-many-teams.md section F, "Rename").)

- [x] ✅ **Task 2.17**: three picker defects the owner reported (branch `fix/teams-full-handles`,
  ccy-teams and ccy-sessions only, no CCY or container bump). (1) Every member is shown by its
  whole handle, the coordinator's with `(coordinator)` beside it, through one renderer
  (`agentbus.Label`, `board.Row.Label`): the list, `--list`, the menus, the seat messages,
  `choose-seat` and `session remove`; a bare seat number, which two checkouts can share, is
  never shown where the handle is known. (2) A checkout inside an
  LXC container's files seen from the host (the folder ccy-teams was started in) was offered
  for a host session by Ctrl-N, the start-up offer and `for-checkout`. The new
  `internal/hostcheckout` answers whose checkout a folder is (git's top, against root's LXC path,
  `lxc.lxcpath` in the world-readable `/etc/lxc/lxc.conf`, else `/var/lib/lxc`; no root, and an
  unparseable file is a refusal); every place ccy-teams and ccy-sessions take the folder as a
  checkout asks it and refuses a container's, pointing to `ccy-teams lxc-add`. No host_vars
  held such a path. (3) The table's columns are as wide as their longest value in terminal
  cells and nothing is cut short; a terminal too narrow keeps TEAM, MEMBER, KIND and STATE on
  the row and shows the rest (a team's description as ABOUT) in the row's header (`--list`:
  on the line below). Fable's FIX-FIRST
  (`subagent-reports/261016-teams-full-handles-review-fable.md`): the sudoers rule dropped,
  the description no longer cut, the last bare seats labelled. Re-review's FIX-FIRST
  (`subagent-reports/261019-teams-full-handles-rereview-fable.md`): the session menus' last
  bare seats labelled; `fix/context-headings` merged in, its headings' crumbs and context lines
  naming seats by handle and its truncation dropped. Final review:
  `subagent-reports/261019-teams-full-handles-final-fable.md`. Deployed at F44 c3391ba9
  (play-claude-yolo.yml). Report: `subagent-reports/261016-teams-full-handles-opus.md`.
  Plan 00209 carries this as an expectation ("Requirements carried from other plans").

- [x] ✅ **Task 2.18**: ccy-teams waits for the play lock instead of dying on it (the owner:
  "hits the lock then just chokes and dies"; a description applied while a meta-deploy unit
  held the lock, exit 75). Branch `fix/teams-wait-play-lock`, ccy-teams only, no CCY or
  container bump. Spec: [SPEC-many-teams.md](SPEC-many-teams.md) section G (S-G1 to S-G11). New
  `internal/playlock` is the Go side of `helpers/play_lock/lock.py` (same file, refusals and
  holder line) with a bounded wait; `ops.Locked` takes it (a screen with Stop waiting on a
  terminal, stderr otherwise, 30 minutes at most) and holds it across the host_vars write and
  every play, handed to each play as run.bash's delegation. Every flow writes inside the hold,
  so a wait that does not end in the lock changes nothing; exit 75 is said as a refused lock.
  F44 merged after `fix/context-headings`' final: the wait screen is headed through `ui.Heading`
  at the width `tput cols` gives through the runner, and its lines (the holder's play and pid)
  wrap to the window (`ui.PickSpec.WrapHeader`), never cut. Numbered 2.17 until F44's
  full-handles task took that number. Deployed at F44 c3391ba9 (play-claude-yolo.yml). Report:
  `subagent-reports/261016-teams-wait-play-lock-opus.md`. Spec section G was written after the
  code (journal correction); Fable's review: `subagent-reports/` on `review/teams-wait-play-lock`.
  Plan 00209 carries this as an expectation ("Requirements carried from other plans").

- [ ] ⬜ **Task 2.19**: run.bash waits on the play lock (`lock.py acquire --wait`, bounded,
  naming the holder) instead of exiting 75, so meta-deploy, the panel's run-play and a play run
  by hand wait for a ccy-teams hold as ccy-teams now waits for theirs. It changes how every
  caller of run.bash behaves, and run.bash is on the bash-limits ratchet, so the wait belongs in
  the Python helper. Decided as a **coordinator default, changeable by the owner** (standing
  "do not gate on me"): build it, in `helpers/play_lock/lock.py` (`acquire --wait`, bounded,
  naming the holder), as this task proposes. Every caller goes from "exit 75 at once" to "wait,
  bounded, saying for whom, then exit 75", which is friendlier for each of them and reversible.
  Left: the build, tests first (a worker), then a host run through meta-deploy.

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

- [x] ✅ **Task 2.21**: M7's host run stopped at M3: the checker still expected the Python
  `exec pingbus status-line` guard after Plan 00213 made it run `pingbus-status-line`. A
  checker defect: `m7_check.py` copied seat.py's value. It now expects exactly what this
  checkout's `seat.session_settings` writes for a seated session, and that its guard ends by
  running the kit settings' command; `DESCRIPTION_MAX` is read from protocol.py, and Plan
  00194's `goodbye_check.py` reads `TEXT_KEY`, the goodbye template and `DISCONNECT_TEXT` from
  pingbus. Defence: `checker-copies-cited-constant` (`tests/helpers/plan_acceptance/`,
  CLAUDE/QA.md), red with 4 findings. The host rerun passed: the M7 acceptance passed on the
  host at F44 cab255ad4 (commit d2612e8d7).
  The independent search (`subagent-reports/261022-m3-owned-value-independent-search.md`)
  found more copies than a sound rule catches. Every copy of a named or computed value is now
  read from the product, no product code changed (its A1-A3, A6-A14, A16-A19; A15's subject is
  a plan script): `subagent-reports/261022-m3-referred-fixes.md`. That includes the drifted
  Tuwunel pin in Plan 00161's `triage_probe.py` (v1.9.3, now read from `tuwunel.pin`, v1.9.4).
  Round 2 (`subagent-reports/261022-m3-round2-fixes.md`) read B3 (board table), B6 (removal
  words), B13 (login words) and B7's ✓/✗ marks from the product; B9's `DISCONNECT_TEXT` was
  already read. M7's and 00203 M3's teams segment is judged by `m7_check expect-segment` /
  `expect-segment-rows`, read from statusline.py (`_team_piece`, `ROLE_LETTERS`) since Plan
  00217 changed it.
  LEFT, CHECKER FIXES: split off by the coordinator as Task 2.22 (below); they do not block
  this task's merge.
  Referred (now decided under Task 2.22, a coordinator default): the inline message or command text (A4, A5, B2, B4, B5, B7's
  "relaunches once"/"re-baseline", B8, B10-B12, B14, B15), and Plan 00211's
  `_acceptance-stage2.inc.bash` fake A3 seat grep for `a host member must not run as a user with an Element profile` (inline in config.py's `ConfigError` and roles.go). The product holds each
  as text inside a call, not a named value, so the checker can read it only once the product
  names it: deployed code changes with version bumps and host deploys, a product decision.
  Report: `subagent-reports/261022-m3-dbf-report.md`.

- [x] ✅ **Task 2.22**: Follow-up to Task 2.21, split off by the coordinator so that Task 2.21's
  branch could merge: the copies of product values that Task 2.21's search found in other
  plans' checkers and left unfixed. Each one reads its value through the `m7_check`
  commands Task 2.21 added (`session-paths`, `hot-marks`, `status-line-names`), as Plan
  00203's M3 already does in `m3_read_names`. The sites, all in Plan 00203:

  - `_acceptance-m4.inc.bash`: the `✓` step mark at 238, 239, 242, 251, 256, 284, 322, 330,
    346, 358 and 366; `/tmp/pingbus-home` and `PINGBUS_STATUS_LINE_INNER` at 306;
    `ccy/membership/.../membership.json` at 109 and 315.
  - `_acceptance-m6.inc.bash`: the `✓`/`✗` marks at 281, 282, 341, 363 and 387;
    `membership.json` at 94.
  - `m2-check.bash`: `MOUNT=/run/ccy/membership` at 24, `HOME_IN=/tmp/pingbus-home` at 25,
    `membership.json` at 86, 95, 116, 121 and 123.
  - `_triage-hs.inc.bash`: `/tmp/pingbus-home/watch.log` at 157, 161, 162 and 171.
  - Also Plan 00161's `triage_probe.pinned_tuwunel_version`, which repeats
    `declare.declared_software`'s inline pin parse (round 2 F3). Both are strict, but they
    can drift, so declare.py should expose the reader.
  - Also the inline-text copies Task 2.21 refers to the owner: the product has to name each
    text as a constant before a checker can read it.

  The conversion merged into F44 at cab255ad4 and its host reruns passed there (commit
  d2612e8d7); what it did is the second paragraph under Task 2.23 below. The work its
  referrals call for is Tasks 2.24 to 2.26, so this task is closed. Its referrals are
  decided as **coordinator defaults, changeable by the owner** (standing "do not gate on me"):

  - Naming an inline message text as a product constant (a Go format constant, a Python
    constant), so a checker can read it, is allowed, with the CCY or container bump it costs.
    Task 2.12 of Plan 00211 already did exactly this (`QUESTION_DUPLICATE`) and merged. This
    covers the inline texts of Task 2.21 and below, the hot-package format texts, the pingbus
    and agent-bus texts, and the inline values the product does not name: Task 2.24.
  - Search rows 17, 21, 29, 30, b17 and b34 (unsure, or not soundly a copy): treated as not a
    copy, and left as they are. The conservative reading: no product change is made for a site
    nobody could show is a copy, and a later finding can still convert one.
  - B4's wire tags: not a copy (the protocol specification, which a checker may state).
  - The plan-folder `test_*.py` that no gate runs: Task 2.25.
  - The bash "only the last command substitution of one assignment reaches errexit" shape:
    Task 2.26.

- [ ] ⬜ **Task 2.24**: Name each referred inline text or value as a constant in the product,
  tests first, and convert the checker that copies it to read the constant through `m7_check product-value`, under `checker-copies-cited-constant`. One branch per product area (ccy-teams
  Go, pingbus and agent-bus Python), with the version bumps each needs and a meta-deploy entry.
  The rows are Task 2.21's referred list and the first three bullets of Task 2.23's referred
  list (report `subagent-reports/261022-sweep-dbf-report.md`). A worker.

- [ ] ⬜ **Task 2.25**: The helper-tests gate also runs each plan folder's own `test_*.py`
  (`CLAUDE/Plan/*/test_*.py`), so a plan checker's tests going red is seen (Plan 00161's
  `test_acceptance_check` went red at `a5486643d` unseen). A worker.

- [ ] ⬜ **Task 2.26**: A rule on `src-go/qa/bashlimits`' syntax tree for an assignment holding
  more than one command substitution, where only the last one's failure reaches errexit (the
  shape that hid a failed read in Plan 00211's stage 2), by Defence Before Fix. A worker.

- [x] ✅ **Task 2.23**: the host
  run failed "session multi's container running: not within 600 s". The base image had just
  been rebuilt, so the launch first rebuilt the project image (a 24-step build). A checker
  defect: the container wait counted the build. The launch's two start waits are now
  `m7_poll_launch`: while an image build runs (a held lock in
  `$XDG_RUNTIME_DIR/ccy/image-builds/`, judged as `ccy --build-lock` judges it, by
  `plan_image_build` in `CLAUDE/Plan/_image-build.inc.bash`) the wait starts again, with no
  cap on the build; a failed build ends the launch, which still fails at once. Tests, red
  first: `LaunchWaitTest` in `test_m7_check.py` and the library's in
  `scripts/test-planlib.bash`, both driving the launcher's own `ccy_image_build_locked`. The
  sweep found the same wait in Plans 00161 (U20), 00194 (goodbye) and 00203 (M2), now on the
  same library. Report: `subagent-reports/261022-launch-waits-for-image-build.md`. The host
  rerun passed: the M7 acceptance passed with the image-build wait, and again at F44 cab255ad4
  (commit d2612e8d7).

  Task 2.22's conversion, merged at cab255ad4, whose host reruns passed there (commit
  d2612e8d7); the referrals below are decided under Task 2.22. Defence:
  `checker-copies-cited-constant` gained a second clause (an `OWNED` product value held by an
  active plan checker), red `b0dd7c9e1`. Converted, each reading the product through
  `m7_check` (`session-names`, `membership-dirs`, and `product-value FILE NAME [FILL...]` for
  any named text of a `helpers/` module or a `src-go/` string const): every site listed above;
  the value copies of Plans 00128, 00161, 00194, 00201, 00203, 00209, 00211 and 00213 (83
  findings at the red for today's `OWNED`, 0 at the tip); the Tuwunel pin, which now has one
  reader (`declare.pinned_tuwunel_version`, CCY 3.100.13, container 2.79.8); and the texts the
  product names (hooks.py `TEMPLATES`, envelope.py `KIND_LINES`, remove.go
  `removedHomeserverDown`, newly named). Each converted checker refuses a wrong product in a
  scratch copy (23 cases). Report: `subagent-reports/261022-sweep-dbf-report.md`.
  Referred, now decided under Task 2.22 (one line each; detail in the report):

  - The ccy-teams hot-package texts (search b1-b7, b9-b19, b22, b23) and 00203 M4/M6
    fragments: built by concatenation inside each call, so naming them is a format constant
    per message in Go code, a product change.
  - The pingbus and agent-bus inline texts (A4, A5, b25, b26, b31, b33, B10): naming them
    changes the image (a container bump) and the agent-bus software digest.
  - Inline values the product does not name either (`.config/pingbus`, the agent-bus install
    paths, unit names, the `ccy-session=` label, `LAST_TOKEN=`, and the 00080, 00109, 00137,
    00148 rows): the product has to name each before a checker can read it.
  - Search rows 17, 21, 29, 30, b17 and b34: unsure, or not soundly a copy.
  - B4's wire tags: the protocol specification (`docs/agent-bus-protocol.md` sections 13 and
    15), which a checker may state; not a copy.
  - No QA gate runs a plan folder's own `test_*.py` (the helper-test gate reads only
    `tests/helpers/`), so Plan 00161's `test_acceptance_check` went red at `a5486643d` (the
    HUMAN room field) unseen; its fixtures are fixed, the gate gap is Task 2.25.
  - In bash, only the last command substitution of one assignment reaches errexit (00211
    stage 2 hid a failed read this way; fixed). No rule detects the shape yet; it needs the
    syntax tree (`src-go/qa/bashlimits` already parses every bash file).

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
