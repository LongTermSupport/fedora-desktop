# Plan 00201: the done and superseded tasks, in full

Moved verbatim from [PLAN.md](PLAN.md), which keeps one line for each and the open tasks.

## Task 1.1

- [x] ✅ **Task 1.1**: Map the current design and record, for each piece, whether it assumes
  one team per session: [RESEARCH-current-design.md](RESEARCH-current-design.md). Several
  teams already work end to end through `ccy --teams 1@alpha,2@beta` (unit-tested, never run
  live); the one-team assumptions are the bare-`--teams` chooser and the picker's
  join offer. `cc --teams` has drifted from ccy (one team, no chooser).

## Task 1.2

- [x] ✅ **Task 1.2**: [SPEC-many-teams.md](SPEC-many-teams.md): numbered expectations for
  descriptions, the shared chooser (no-cover, several-cover, scope widening), several teams
  per session, replying to a peer, and `cc` on the same flow. Decisions Q1-Q10: the spec's
  recommendations are taken, by the coordinator, on the owner's instruction of 2026-10-15
  not to wait for him and to decide properly and pragmatically. The owner may
  overrule any of them. The spec is written and Q1-Q10 are decided.

## Task 2.1

- [x] ✅ **Task 2.1**: M1, descriptions declared and shown on the host (S-A1 to S-A7, S-A13 to
  S-A16). Built test-first: team file `description`, `declare.py` create/check/describe/
  `undescribed` and the 7-field `TEAM` line, the board's MEMBER cell, the create wizard's question,
  the picker's one-time offer, Quick Launch's `Team:` line, and an installer case proving a
  description-only change restarts nothing. Q1 (required at create, optional in the file) taken as
  the spec recommends. Report: `subagent-reports/261015-m1-m2-sonnet.md`.

## Task 2.2

- [x] ✅ **Task 2.2**: M2, descriptions reach agents (S-A8 to S-A12), revised after review: the
  description travels as the admin-written room topic, read by the syncer into `about.json`
  (only from the admin, only if it passes the grammar, never the fixed sentence), NOT as a
  team-record key, because an unknown record key makes every older member exit 10. Then
  `pingbus status` prints `ABOUT` and SessionStart says "team T is for: ...". Container 2.57 /
  CCY 3.97.4. No deploy-order hazard remains. Q2 and Q3 as the spec recommends. Report:
  `subagent-reports/261015-m1-m2-sonnet.md`.

## Task 2.3

- [x] ✅ **Task 2.3**: M3, replying: `recv` prints a REPLY command; `--to` takes a seat's
  short name. Built test-first in `helpers/pingbus` (S-D1 to S-D6); Q8 taken as the spec
  recommends (REPLY after HUMAN and PEER only, not PING): taken, the owner may overrule.
  Report: `subagent-reports/261015-m3-reply-sonnet.md`.

## Task 2.4

- [x] ✅ **Task 2.4**: M4, the shared chooser in Go (`ccy-teams choose-teams`), scope widening,
  ccy wired (CCY 3.97.6). Single team (S-B1 to S-B15 without multi-select): `declare.py choices`
  and `add-repo`, the Go chooser, the wizard's chooser mode, `for-checkout --choose` and
  `ccy_seats_resolve` deleted. Report: `subagent-reports/261015-m4-chooser-sonnet.md`.

## Task 2.5

- [x] ✅ **Task 2.5**: M5, several teams per session (S-B3 `--multi`, S-C1 to S-C9), CCY 3.97.8 /
  container 2.58. `choose-teams` list is a multi-select with kept marks across rounds; `describe-seats --block` prints the launch block after `seat take`; SessionStart's first line names the teams; a
  status line shows `👥 A,B`; `pingbus send` infers the team from `--re` or `--to`, `say` from
  `--to`, or they fail with the choices. The segment reads a per-session record the pingbus
  SessionStart hook writes. It was first a hooks-daemon handler in this repository only; after the
  final review it is the kit's `pingbus status-line` (Task 2.8). M6 must also print the launch
  block for `cc --teams`.
  Reports: `subagent-reports/261015-m5-many-teams-sonnet.md`, review
  `subagent-reports/261015-m5-review-fable.md`.

## Task 2.6

- [x] ✅ **Task 2.6**: M6, `cc --teams` on the shared flow, its consent inside it (S-E1 to S-E6),
  CCY 3.97.10 / container 2.58 as first built, 3.97.11 / 2.59 after the M6 review fixes, and 3.98.2 / 2.60 once the integration merge joined the goodbye and relay line. `cc` takes ccy's list grammar and, on a terminal, calls
  `ccy-teams choose-teams --type=host`; the Element consent is Go (`chooser/consent.go`), asked after
  the seats are chosen and before any is made, reading and writing `~/.config/pingbus/element-accepted`
  with the old rules. `agent-bus seat check`, `seat take --type=host`, then the launch block (M5 F6)
  and `describe-seats --sentence` for the prompt. `cc-teams.bash` lost `cc_teams_seat` and
  `cc_teams_accept` (170 to 113 code lines). The kit plugin's SessionStart hook covers the status-line
  record on the host (pinned in `scripts/test-cc-teams.bash`). Report:
  `subagent-reports/261015-m6-cc-teams-sonnet.md`.

## Task 2.8

- [ ] ❌ **Task 2.8**: Superseded by Plan 00209 D4: relay mode is deferred to v2. (Was: S-D1 in relay mode: `_deliver` (`helpers/pingbus/cli.py`) writes a
  relay-delivered `HUMAN` or `PEER` to the spool with no `REPLY` line, so the session never
  gets the ready reply command. Expected to be carried by the structured-injection work of
  [Plan 00194](../00194-agent-bus-live-delivery-supervisor-plugin/PLAN.md) on branch
  `feat/bus-inject-message`; check the reply line is there when it merges, else add it here
  (tests first). Found in `subagent-reports/261015-bus-bugs-opus.md`.)

## Task 2.9

- [x] ✅ **Task 2.9**: the final qa-reviewer's findings
  (`subagent-reports/261015-final-qa-reviewer.md`), CCY 3.98.7 / container 2.62. The `👥 A,B`
  status line works in any checkout: the member kit's `settings.json` makes `pingbus status-line`
  the status line (command-line settings outrank the project's), which runs the session's own
  status line first; the hooks-daemon handler is gone, and the tests are pingbus's, in the
  helper-tests gate. A record SessionStart could not write is told to the session and shown in red
  (`👥 no record ...`), never as nothing. S-C6, S-C8 and Q6/Q7 match what was built; `cc --teams`
  makes `.claude/ccy/` only after the consent (S-E4). Report:
  `subagent-reports/261015-qa-review-fixes-opus.md`.

## Task 2.10

- [x] ✅ **Task 2.10**: UX fixes before the owner's first day with teams (branch
  `fix/teams-ux`), CCY 3.98.9 / container 2.64 / plugin 1.2.1: a read receipt for each
  delivered human message, `say --re` threading replies (the `reply:` command carries it), a
  human's non-ASCII text shown as written with the hostile set still escaped (SPEC-inject.md
  E2/E8/E9 amended), the stopped-team and unknown-team launch refusals, Open every team in
  Element, "Talking to agents from Element" in docs/agent-bus.md, and
  [ACCEPTANCE-owner.md](ACCEPTANCE-owner.md) matched to the code with a Morning start and the
  00194 H1-H7 checks. Report: `subagent-reports/261015-ux-fixes-opus.md`.

## Task 2.11

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

## Task 2.13

- [x] ✅ **Task 2.13**: the Morning start dry run's ccy-teams defects (branch
  `fix/teams-signin-first-run`; ccy-teams only, no CCY or container bump). After a first
  password, Open in Element no longer says "Copy it now" (it said so with Clipboard Indicator
  on); the new-password question defaults to Yes for someone with no Element session, No
  otherwise; "Create a team for OWNER/REPO now?" defaults to No (S-B15). Deployed at F44
  c3391ba9 (play-claude-yolo.yml).

## Task 2.16

- [ ] ❌ **Task 2.16**: Folded into Plan 00209 (agent-bus v2), carried there as a requirement:
  a rename touches the homeserver, accounts, rooms, tokens and every seat, which v2 rebuilds,
  so a v1 rename would be built twice. v1 keeps the installer's refusal of a changed name.
  (Was: rename a team, end to end. Not built: the settings screen shows the name
  as fixed, with the reason. A rename must make the new team (homeserver unit, data folder,
  server name, accounts, room), carry over or re-create its history, re-issue every member's
  token and bundle, move every seat and session record, re-point every running session, then
  take the old team down; today the installer refuses a changed name (`render.check_unchanged`).
  Needs its own research and spec first (SPEC-many-teams.md section F, "Rename").)

## Task 2.17

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

## Task 2.18

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

## Task 2.19

- [x] ✅ **Task 2.19**: run.bash waits on the play lock (`lock.py acquire --wait`, bounded,
  naming the holder) instead of exiting 75, so meta-deploy, the panel's run-play and a play run
  by hand wait for a ccy-teams hold as ccy-teams now waits for theirs. It changes how every
  caller of run.bash behaves, and run.bash is on the bash-limits ratchet, so the wait belongs in
  the Python helper. Decided as a **coordinator default, changeable by the owner** (standing
  "do not gate on me"): build it, in `helpers/play_lock/lock.py` (`acquire --wait`, bounded,
  naming the holder), as this task proposes. Every caller goes from "exit 75 at once" to "wait,
  bounded, saying for whom, then exit 75", which is friendlier for each of them and reversible.
  Built: `lock.py wait --fd N` takes the lock on run.bash's own descriptor (the hold is the
  open file description's, so it outlives the helper), waits up to 30 minutes (ccy-teams'
  bound; `FEDORA_DESKTOP_PLAY_LOCK_WAIT` seconds overrides it), names the holder from its
  note, and exits 75 when the bound passes; run.bash 1.34.0 calls it in place of `flock -n`.
  Unit tests and `scripts/test-run-bash-single-play.bash` cover it. Left: nothing needs a
  host run of its own; the next run.bash on the host uses it.

## Task 2.21

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

## Task 2.22

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
  - The plan-folder `test_*.py` that no gate runs: Task 2.25, which the `plan-tests` gate had
    already closed.
  - The bash "only the last command substitution of one assignment reaches errexit" shape:
    Task 2.26.

## Task 2.24

- [x] ✅ **Task 2.24**: Name each referred inline text or value as a constant in the product,
  tests first, and convert the checker that copies it to read the constant through `m7_check product-value`, under `checker-copies-cited-constant`. One branch per product area (ccy-teams
  Go, pingbus and agent-bus Python), with the version bumps each needs and a meta-deploy entry.
  The rows are Task 2.21's referred list and the first three bullets of Task 2.23's referred
  list (report `subagent-reports/261022-sweep-dbf-report.md`). A worker.
  Done for every row `product-value` can read, in three branches; no byte printed changes (each
  named text pinned by a test), so no host check of its own: the next acceptance runs read the
  constants. Report: [subagent-reports/261027-task-2-24.md](subagent-reports/261027-task-2-24.md).

  - `fix/00201-constants-teams-go` (a3c5e70ba): ccy-teams' hot step lines and messages
    (`hot/texts.go`), `roles.go setTo`, `choose.go`'s launch block; m7_check gains
    `product-pattern`; 00203 M3, M4, M6 converted. Red 10, green 0.
  - `fix/00201-constants-pingbus` (df354133a): cli.py, inbox.py, seat.py, config.py,
    statusline.py texts (A4, A5, B10/b31, b26, b33, b11, `.config/pingbus`); m7_check, 00161
    U20/U23, 00203 M4 and the homeserver triage, 00211 stage 2, 00194 delivery_check converted.
    Red 15, green 0. M4's down check had never matched the status line; fixed. CCY 3.100.31,
    container 2.79.13.
  - `fix/00201-constants-agent-bus`: health.py and checkout.py texts and the backup tar prefix
    (B8/b25, `agent-bus-state-`); 00203 R6-R8 converted. Red 6, green 0. CCY 3.100.32,
    container 2.79.14. meta-deploy: `changed-plays`.
  - Left, because `product-value` reads only `helpers/` Python and `src-go/` Go and these live
    elsewhere: B5 (`LAST_TEAMS=`, `LAST_TOKEN=`, the `ccy-session=` label, `projects/-workspace`:
    claude-yolo bash; rows 31, 32), cc's mktemp (row 19, `files/var/local/claude-code/cc`), B11
    (speech-to-text extension JS), B12 (wsimedia Go outside `src-go/internal`), B14 (run.bash),
    B15 (entrypoint.sh, rclonecache/convert Go, self_update Python), rows 22 and 25-27 (Plans
    00080, 00137, 00109, 00148), and the Tuwunel path (agent-bus-install only). Also left: the
    homeserver unit names (row 24): Python holds them (`checkout.hs_unit`, `health.backup_timer`
    and the rest) but about 25 checker sites in five plans copy them: Task 2.27.

## Task 2.25

- [x] ✅ **Task 2.25**: A gate runs each plan folder's own `test_*.py`, so a plan checker's
  tests going red is seen (Plan 00161's `test_acceptance_check` went red at `a5486643d`
  unseen). Already built: the `plan-tests` gate (`src-go/qa/plantests`, commits 7c6974126 and
  60f028790) runs every active plan folder's own Python tests, in `qa-changed.bash` too.

## Task 2.26

- [x] ✅ **Task 2.26**: A rule on `src-go/qa/bashlimits`' syntax tree for an assignment holding
  more than one command substitution, where only the last one's failure reaches errexit (the
  shape that hid a failed read in Plan 00211's stage 2), by Defence Before Fix. A worker.
  Built as `assignment-masks-substitution-failure` ([QA.md](../../QA.md#assignment-masks-substitution-failure)),
  in the bash-limits gate with no allowlist, branch `fix/bash-multi-substitution-rule`. The
  class, from bash 5.2 runs under errexit: an assignment-only statement with two or more
  substitutions not nested in one another (scalar, several assignments, array literal, `+=`,
  default, arithmetic); a declaration builtin assigning any substitution (SC2155 misses the
  array and `${1:-$(...)}` forms); a substitution in a command's prefix assignment. It judges
  every bash file the gate finds, exempt from the limits or not, but `src-go/vendor/`, the
  daemon-owned files and `.semgrep/` (fixtures, never run). Red `63daece62`: 27 findings in
  17 files; all fixed and each rewrite proven (value unchanged, failure now stops: under `-e`
  by errexit, and with an explicit `|| exit` in the two `set -uo pipefail` scripts), with ccy
  3.100.19 for `lib/network-management.bash` and `changed-plays` queued in meta-deploy. The
  independent search also found 4 in `play-github-cli-multi.yml`'s shell functions, which the
  rule cannot see (bash inside a playbook); fixed by hand, the `git-which-account` reads with
  `|| return 1` (an interactive function, no errexit). Not this class, recorded in QA.md: a
  substitution as a command's argument, and one nested in another (`inherit_errexit`).

## Task 2.23

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
    HUMAN room field) unseen; its fixtures are fixed, the gate gap is closed by the `plan-tests` gate (Task 2.25).
  - In bash, only the last command substitution of one assignment reaches errexit (00211
    stage 2 hid a failed read this way; fixed). Defended by Task 2.26's rule.
