# Plan 00177: ccy-teams, human friendly

**Status**: In Progress (host run 26-10-26: 6.4, 10.4 and 10.5 passed; 7.5 and 9.5 still open; Phase 11 passed on the host; Phase 12 passed on the host, 12.5's fix included, on 27-10-26; 12.6, the flaky test, fixed in the test on 27-10-26)
**Created**: 2026-10-13
**Owner**: joseph
**Priority**: High

## Overview

`ccy-teams` (Plan 00161) is the owner's manager for agent teams. Its first real use on the
host went badly. It asked for a sudo password with no context. After three failed password
attempts it carried on and said "No agent teams on this machine", which is a false statement
built on a failure it had hidden. With no teams it only said that `create` exists, instead of
offering to run it. Its output has no colour or structure. In `create`, the prompt "Humans of
the team, comma-separated (their Matrix account names)" left the owner asking what it meant.
It does not say these are the people who read and write in the team room through Element,
that the installer makes their accounts, or what a sensible answer looks like. The repos and
subnets prompts have the same problem.

The owner asked (2026-10-13): make it much more human friendly, with colour and formatting;
offer to create a team when there is none; think about the UX; do a full critical pass.

## Goals

- Every failure is reported as what it is. A refused sudo never turns into "no teams".
- No teams: offer to create one there and then, and walk through it.
- Colour and layout that make teams, members, seats, sessions and restore marks easy to scan,
  in line with the other ccy pickers and with the project's interactive-script rules.
- Ask for sudo only when it is needed, saying why first, or remove the need where that is
  safe (for example a narrowly scoped, read-only sudoers rule installed by IaC). Record the
  decision and its security reasoning.
- Every verb and key gives plain-language guidance on what happens next. For example, after
  `create`: what is declared, and that a meta-deploy run installs it.

## Non-Goals

- Changing the bus itself (pingbus, agent-bus, homeserver), except a read-only listing
  path if the sudo decision needs one.

## Tasks

Completed phases are summarised here; their full text, every task id included, is in
[DONE-phases.md](DONE-phases.md).

### Phase 1: Critical UX review (done)

Task 1.1: the full critical pass, written into `UX-REVIEW.md`.
[Full text](DONE-phases.md#phase-1-critical-ux-review).

### Phase 2: Build (done)

Tasks 2.1 and 2.2: every UX-REVIEW finding fixed (no-teams offer, create flow, scoped read-only
sudoers rule, colour, help; D1 and D2 deferred), and the qa-reviewer's findings.
[Full text](DONE-phases.md#phase-2-build).

### Phase 3: Host (done)

Task 3.1: host run passed 26-10-13; the owner's first real use became Phase 4.
[Full text](DONE-phases.md#phase-3-host).

### Phase 4: The owner's first-use findings (done)

Tasks 4.1 to 4.11: the stepped create wizard, Element and sign-in from create, seat defaults and
the `ccy --teams <team>` chooser, team scope, `cc --teams` as a host member, and the qa-reviewer
fixes. [Full text](DONE-phases.md#phase-4-the-owners-first-use-findings).

### Phase 5: Host (done)

Task 5.1: deployed and the owner's round trip through Element worked; the clunky UX became
Phase 6. [Full text](DONE-phases.md#phase-5-host).

### Phase 6: No hidden keys (done)

Tasks 6.1 to 6.4: a row menu for every action, Element offered before a new password, Element
started apart from the board; host PASS 26-10-26.
[Full text](DONE-phases.md#phase-6-no-hidden-keys-from-the-owners-host-test).

### Phase 7: `ccy --teams` finds the team (the owner's host test)

- [x] ✅ **Task 7.1**: `ccy --teams` with no value offers the teams whose scope covers this
  checkout's GitHub `owner/repo` (an organisation team by its owner, a repository team by
  the repository), saying so when none does. When the machine has exactly one team and it
  covers the checkout, it is taken without a list; with two or more teams the list is always
  shown, covering teams first (Plan 00201 S-B2, which replaced "taken when exactly one
  matches"; Task 11.1).
  The matching and the choice live in the Go `ccy-teams`; the launcher, on the bash-limits
  ratchet, only calls it and must not grow. Built as `ccy-teams for-checkout` (CCY 3.96.0).
  The owner's test found that an uncovered checkout made them type a team name; Plan 00201
  M4 replaced that with `ccy-teams choose-teams` (its `--choose` mode gone), which the launcher
  calls, and brought cc onto the same flow. Deployed. The owner's test of it is Plan 00201's
  [ACCEPTANCE-owner.md](../00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/ACCEPTANCE-owner.md)
  step 5 (Task 7.5).
- [x] ✅ **Task 7.2**: The quick launch ("use the previous configuration") remembers the team:
  `.last-launch.conf` keeps the `--teams` list (`LAST_TEAMS`, config format 2), and reusing
  it launches into the same team; an explicit `--teams` overrides it. Only a person's yes to
  the offer takes the team: a headless, no-terminal or restore launch never does (CCY 3.97.3,
  Plan 00161 M2.9 defect). Deployed; the unattended half passed on the host in Plan 00161's
  acceptance (M2.9, M2.10 at F44 6affbfec). The person's yes is the owner's test: Plan
  00201's ACCEPTANCE-owner.md steps 3 and 6 (Task 7.5).
- [x] ✅ **Task 7.3**: The owner's host run found `play-agent-bus-element.yml` refusing them
  after they accepted `cc --teams`. The reason: "~/.config/pingbus exists: this user holds
  pingbus bundles". Task 4.10's acceptance record lives in that folder, so the Element
  check refused every owner who had accepted. A cc seat's bundles live in the checkout.
  `helpers/agent_bus/element.py` now passes a folder that holds only the acceptance record,
  valid by pingbus's own `config.element_accepted`. Anything else there still refuses.
  Built test first. The owner's run of the Element play on the host passed (26-10-15).
- [x] ✅ **Task 7.4**: A person is never left with an account they cannot sign in to. After an
  apply (from `ccy-teams create` or `ccy-teams apply <team>`), passed or failed, every person
  of the team who has no Element session (the picker's check) is offered the sign-in on a
  terminal, or told the exact way (`ccy-teams`, Enter on the team, Open `<team>` in Element)
  without one. When the sessions cannot be read (the homeserver is not up after a failed
  apply) it says so with the same way. The apply's failure still stands and still exits 1.
  Diagnosis: `subagent-reports/261015-room-access-diagnosis-opus.md`; the report
  `261015-create-signin-gap-sonnet.md`. Built test first and deployed; the owner's test is
  Task 7.5.
- [x] ✅ **Task 7.5**: The host agent tests Phase 7 on the host, in tmux. Nothing to deploy (all of it is in
  the deployed `ccy-teams` and CCY). Tasks 7.1 and 7.2: Plan 00201's ACCEPTANCE-owner.md steps
  3, 5 and 6. Task 7.4: with a team where a person has never signed in to Element, run
  `ccy-teams apply <team>` in a terminal; after the apply it offers that person the sign-in.
  Host run 26-10-26 (throwaway team): 7.2's quick launch, the seat question, status line and
  `pingbus status`, and 7.4 PASS (the apply path does not offer Element after the password:
  an observation). Still open: step 5 FAILS (Task 11.1), and the launch block is missing (11.2).
  Host run 27-10-27 (throwaway teams, after 11.1 and 11.2): steps 3, 5 and 6 PASS (the Quick
  Launch Team line, the seat and role questions with Keep as is, the launch block and the
  status line naming both seats).

### Phase 9: Add an LXC container to a team from ccy-teams, no hand edit of localhost.yml

To put an LXC container's agent in a team the owner had to hand-edit
`environment/localhost/host_vars/localhost.yml` (an `agent_bus_lxc_members` entry: team,
container, user, host, checkout, role) and then run `ccy-teams apply <team>`. Teams are
already declared without a hand edit (`ccy-teams create` through `declare.py`); members now
are too, by the same route.

- [x] ✅ **Task 9.1**: `helpers/agent_bus/declare.py` gains `lxc-add` (with `--dry-run`),
  `lxc-check`, `lxc-members`, `lxc-remove` (sets `state: absent`, which the play applies by
  parking the member) and `lxc-suggest-seat` (replaced in Task 9.7 by `lxc-derive`). The entry is judged by
  `helpers/agent_bus_lxc/members.py`'s own `parse_members`, imported not copied, as the last
  of the entries already there: a duplicate (team, container, user), a second present
  coordinator and a bad role or user are refused (exit 64) by the play's rule. Declare adds
  that the team must be declared present, that the host is written as the play reads it, and
  that the checkout is a path the line reader keeps whole. Red 13 failures, green 77 tests.
- [x] ✅ **Task 9.2**: The Go client (`internal/declare`: `LxcAdd`, `LxcAddDryRun`, `LxcCheck`,
  `LxcMembers`, `LxcSuggestSeat`, `LxcCheckSeat`), and `lxcfreeze.Lister` (the one owner of the
  `lxc-*` tools: the host's running containers, and a container's ordinary users from
  `getent passwd`, both as root through the sudo seam, failing with the play to run when LXC
  is not installed).
- [x] ✅ **Task 9.3**: `ccy-teams lxc-add <team>` and a menu line on every row, *Add an LXC
  container's agent to `<team>`* (no hidden key; in the menu, the footer, the help and the
  usage text): package `internal/teams/lxcadd`. It asks, on a terminal, for the container
  (running ones), the seat (only when the container's name gives none the play can take, or
  another container of the team holds it), the user (suggested from the container), the
  checkout, the handle's host word (`worker` suggested, never the container's name) and the role (coordinator only while the team
  has none, declared or live), each checked by `declare.py` and asked again up to three
  times; a summary and one yes; then `declare.py lxc-add` and the apply of that team; then
  what is left inside the container (`ccy --teams <team>` as that user in the checkout, and
  `gh auth login` if needed; a failed apply says the same with the command to run). Task 9.7
  removed the seat and host word questions: the handle is derived.
- [x] ✅ **Task 9.4**: `docs/agent-bus-lxc-members.md`, `docs/agent-bus.md`,
  `localhost.yml.dist` and the usage text say to use the action; hand-editing stays
  documented as the alternative.
- [ ] ⬜ **Task 9.5**: The host agent tests it on the host, with sudo, so with the owner present (meta-deploy of `play-claude-yolo.yml`
  builds the new `ccy-teams`): `ccy-teams lxc-add <team>` for a real container.
  PARTIAL 26-10-26: container, user and checkout questions and Esc pass on a throwaway; the
  rest needs a prepared container (no play makes its user, checkout or gh login). Finding: 11.3.
  27-10-27 on a throwaway Ubuntu LXC (user and a checkout made as test set-up): container list,
  user, checkout suggestion, role, review box, the save and the apply all PASS; the join stops
  at members.py's "gh is required and must be logged in" (78) and lxc-add says "declared … but
  not joined". Left for the owner: gh installed and logged in, in a real container; then the
  join, the agent start and its pingbus check. Finding: Task 9.9.
- [x] ✅ **Task 9.9**: A member lxc-add declared that never joined (no bundle, as when gh is
  missing) could not be taken off: `state: absent` failed in `members.py` `_absent_one` with
  "the entry leaves out its host or seat", blocking every `play-agent-bus.yml` run (host agent,
  27-10-27). Fixed in the absent path (option 1; lxc-add still writes no host or seat, Task
  9.7's derivation stays the play's): with no bundle, the member record's handle is parked;
  with no record, the `<repo>+<host>` is derived from the checkout (or the entry's `repo` for a
  destroyed container) and the team searched: no active unheld member of it means nothing to
  park, one is refused by name with the `seat` to set. A present member that cannot join (gh)
  failing every apply of its team is by design. Red 10, green 167.
  [Report](subagent-reports/261027-task-9-9.md). Host check: lxc-add a throwaway container
  without gh, `declare lxc-remove`, then `ccy-teams remove <team>` passes with nothing parked.
  Host, 27-10-27 at F44 fe894347c: PASS. On a throwaway team and an Ubuntu container with no
  gh, lxc-add stopped at "gh is required" (exit 1, "declared … but not joined", by design);
  `declare lxc-remove` exit 0 ("declared absent"); `ccy-teams remove` ran "Take the LXC members
  declared absent off their teams" unchanged (nothing parked) and removed the team, exit 0.
- [x] ✅ **Task 9.6**: The review of `lxc-add` answered, F1 to F9.
  [Full text](DONE-phases.md#phase-9-the-done-tasks-96-to-98).
- [x] ✅ **Task 9.7**: The handle is derived from the repository (`helpers/agent_bus_lxc/derive.py`),
  never asked; the flow asks only container, user, checkout and role.
  [Full text](DONE-phases.md#phase-9-the-done-tasks-96-to-98).
- [x] ✅ **Task 9.8**: The review of the derivation answered, F1 to F8.
  [Full text](DONE-phases.md#phase-9-the-done-tasks-96-to-98).

### Phase 10: The host agent's board findings (driving `ccy-teams` in tmux)

- [x] ✅ **Task 10.1**: No menu offers what the board calls unavailable. Outside a project the
  header says "Ctrl-N unavailable" and every menu's start line now says
  "Start a ccy session in <team>: unavailable, <why>" (the house wording of `ccy-sessions`'
  new-session row; choosing it shows the refusal). The independent search found three more of
  the class, fixed the same way: Start on a team that is not up, Attach on a seat with no running
  session or open elsewhere, Attach in Running sessions on a session open elsewhere. Defence
  Before Fix: no rule (a run-time fact), recorded in
  [GoProjectRules.md](../../GoProjectRules.md#defect-classes-that-cannot-be-a-rule);
  [search](subagent-reports/261023-search-unavailable-offered.md).
- [x] ✅ **Task 10.2**: Keys typed between two screens are not echoed (`^[[B` on the next
  screen after Esc from a menu). `internal/ttyecho` holds the terminal's echo off for the run of
  `ccy-teams`, `ccy-sessions` and the other tools the search found showing screens with work
  between (`podfreeze`, `lxcfreeze`, `open`, `ftp-camera`); a child given the terminal for its
  own input, and a numbered reply read in the tool itself, run with the echo the terminal had. No rule (run-time state across
  processes), recorded beside 10.1; [search](subagent-reports/261023-search-echo-between-screens.md).
- [ ] ⬜ **Task 10.3**: The host agent drives `ccy-teams` in tmux again: from `~`, the team and
  person menus say Start is unavailable; Esc from a menu then Down pressed quickly shows no
  `^[[B`.
- [x] ✅ **Task 10.4**: An Esc is never lost to the key after it (the host agent's finding: in a
  row menu, Down within about 50 ms of Esc, or both in one `tmux send-keys`, left the menu open
  and moved its cursor; Ctrl-R then turned a seat's restore off with no question). Cause, proven
  with real fzf: fzf reads an Esc and the next key inside its 100 ms wait as that key with Alt
  (`alt-down`), which no screen bound. Defence Before Fix: failing tests first, a project rule,
  the fix, and the sweep of every screen a person leaves with Esc (fzf, Bubble Tea, gum, tmux).
  - [x] ✅ **Task 10.4.1**: Failing tests: every fzf screen leaves on each of the 69 Esc-joined
    keys; pathinput cancels on a joined Esc; the teams list comes back on the row after Esc then
    Down or Up.
  - [x] ✅ **Task 10.4.2**: The rule `project_esc_joined`
    ([GoProjectRules.md](../../GoProjectRules.md#project_esc_joined)), red on its own: 5 findings
    through `./scripts/qa-changed.bash --base F44`, 3 Go (`ui.Picker.Pick`, `ui.Picker.Choose`,
    pathinput) and 2 shell (`ccy_tmux_pick` in tmux-session.bash, Ctrl-R in history-search.bash).
  - [x] ✅ **Task 10.4.3**: Green: every instance fixed (fzf binds the joined keys to abort,
    Esc then Down or Up in a row menu moves the list cursor, pathinput cancels on a joined Esc),
    tmux `escape-time` 10, CCY 3.100.24 (released as 3.100.25 with the F44 merge), a `changed-plays` run queued in `meta-deploy.list`.
  - [x] ✅ **Task 10.4.4**: The Defence Before Fix conformance review conformed: red at the
    rule commit with exactly the 5 findings, green at the fix, no false positives. Its one note:
    `files/etc/tmux.conf` is not covered by `qa-changed`.
  - [x] ✅ **Task 10.4.6**: fzf's word editing kept (qa-reviewer finding, coordinator
    decision): `alt-b`, `alt-f`, `alt-d` and `alt-bs` are no longer bound to abort, so Ctrl-R
    and the `ui` filters keep a query being edited by word; the rule requires the other 65 keys
    (`fzfbind.EscJoined`, less `fzfbind.WordEditing`) and flags a word key bound to abort. Only
    Esc then b, f, d or Backspace within fzf's wait is still lost; the arrows stay covered.
    The freeze golden argv re-pinned under bug 28. CCY 3.100.26.
  - [x] ✅ **Task 10.4.5**: Host re-check after the deploy and a tmux server restart: Esc and
    Down in one `tmux send-keys` close a ccy-teams row menu and move the list cursor; Esc then a
    key leaves `ccy_tmux_pick` and Ctrl-R; `tmux show -s escape-time` says 10.
    PASS 26-10-26 (host agent, private tmux): row menu 7 of 8 (the first lost the Down, not
    reproduced), `ccy_tmux_pick` 4 of 4, Ctrl-R 3 of 3, escape-time 10 with no restart needed;
    again on podfreeze and lxcfreeze after their rebuild.
- [x] ✅ **Task 10.5**: Host check of the gum decision below, after the `escape-time` 10
  deploy and a tmux server restart: in a ccy-teams gum confirm (the take-down "Back up first?"
  will do), Esc then Down sent as two `tmux send-keys` calls 20 ms apart answers No and leaves;
  the same in a gum choose and a gum input cancels. Only both keys in one `send-keys` call may
  still lose the Esc. PASS 26-10-26 (host agent): split 20 ms apart, a real ccy-teams confirm
  and gum confirm, choose and input 9 of 9 leave; joined in one call 6 of 6 lose the Esc, as
  decided below.

#### Owner decisions (Task 10.4, an Esc joined to the key after it)

- **gum prompts** (decided by delegated review on the owner's instruction: **keep gum**,
  option c). About 38 call sites, all through `internal/ui/ui.go`, lose the Esc only of a
  one-write `\e\e[B`; with tmux `escape-time` 10 only a `tmux send-keys` with both keys, or a
  paste, reaches it, and nothing destructive is behind a lost Esc. Standing condition: a
  destructive gum confirm defaults to No or sits behind a typed name. Revisit if Task 10.5 shows
  gum losing an Esc from keys typed apart. Analysis: the 10-26 journal decision entry.
- **wsi-model-manager (Textual)** (owner: "fix"): Esc then a letter becomes Alt+letter (the Esc
  is lost), and Esc then Enter reaches the app as a plain Enter. On the remove confirmation,
  which focuses Yes, that removes the model. To be fixed (queued); a possible mitigation is
  `ESCDELAY=10` before textual is imported.
- **Restore toggle** (open; asked of the owner): proposed, not changed. Turning a seat's restore OFF (the menu row and
  Ctrl-R from the list) asks first (Confirm, default No, naming the seat and that its session
  will not come back after a reboot); turning it ON needs no question.

### Phase 11: Findings of the host run of 26-10-26 (done)

Tasks 11.1 to 11.4: chooser texts corrected (rule `chooser-skip-condition-text`), the launch
block printed after `session add` and `move`, `root` refused as an LXC agent's user, and
`remove` takes the team's Element launcher away. Host PASS 26-10-26 at bc581f0ae; its findings
are Phase 12. [Full text](DONE-phases.md#phase-11-findings-of-the-host-run-of-26-10-26).

### Phase 12: Findings of the Phase 11 host run

- [x] ✅ **Task 12.1**: The launch block printed by `session add` and `move` says "(new seat:
  it joins as a worker)" for seats that already existed (the session's held seat after add;
  the move's own step said "seat 1: exists"). `newSeatWords` (`internal/teams/choose/choose.go`)
  is chosen when the seat is missing from `Bus.SeatList(ctx, "")`, which reads the cwd's
  checkout, and `hot.go` prints the block from the ccy-teams terminal, whose cwd need not be the
  session's checkout: likely a lookup against the wrong checkout, not wording. Confirmed by test
  (`TestTheBlockReadsTheSessionsCheckoutNotTheWorkingDirectory`, red then green). `choose.Block`
  now takes the session's checkout; a seat list agent-bus refused or could not all read says
  "role unknown", never "new seat". The launcher's own subcommands keep the cwd: they run in the
  checkout by contract.

- [x] ✅ **Task 12.2**: `session add` into a team created a minute earlier printed "left aside
  1 joined room of an earlier room version" (`helpers/agent_bus/admin.py`, from `joined_rooms`).
  It counts every joined room not of room version 12, and Tuwunel's own server admin room is one,
  so a new team counts 1: the count is right and the words mislead. Say what it is (or leave the
  server's admin room out of the count). Now only rooms whose create event the team admin sent
  are named ("left aside N room(s) the team admin made in an earlier room version (not a bus
  room)"); the server's admin room, sent by the server's own user, is left out without a guessed
  name or alias.

- [x] ✅ **Task 12.3**: A key typed just after Esc reaches the next prompt: in `ccy-teams remove`, Esc answered "Back up first?" No and a Down sent 20 ms later moved step 2's cursor off
  its default "Leave it running". Harmless there (Enter and the typed name are still needed),
  but a key acts on a screen the person has not seen. Keeping held keys is deliberate
  (`internal/ttyecho/ttyecho.go`: they "stay in the input for the next screen to read"), so the
  fix names which prompts drain: at least a prompt that follows an Esc answer, and every prompt
  of a destructive flow. Decided rule (`ui.Drain`, stated in the ttyecho package doc): a screen
  discards the waiting input (tcflush) when it follows a leave (Esc, Ctrl-C, a No; gum confirm
  gives Esc and No one exit, so both count) or belongs to a destructive flow: `ccy-teams remove`,
  `ccy-teams restore`, and ccy-sessions' End question. Every other screen keeps type-ahead. One
  Drain is shared by a command's gum and fzf. Tests: ui fakes, ops and sessions flows, and a
  real pty in `ttyecho_test.go`.

- [x] ✅ **Task 12.4**: Host check, after meta-deploy (`play-claude-yolo.yml`, which builds
  ccy-teams, ccy-sessions and CCY 3.100.30 with its image, container 2.79.12; then
  `play-agent-bus.yml`, which rebuilds the host's `agent-bus.pyz` with the new `admin.py`). On
  throwaway teams only, as Task 11.2:

  1. Launch a scratch session in team A, then from a terminal whose cwd is another checkout run
     `ccy-teams session add <session> <team B> --yes`: the block names both seats with their
     role (`worker`), never "new seat". Then `session move` the other way: the same.
  2. `session add` into a team made a minute earlier prints no "left aside" line.
  3. `ccy-teams remove <team>`: Esc on "Back up first?" then Down within 50 ms: step 2's cursor
     is still on "Leave it running". Elsewhere (the board after a plain answer), a key typed while
     the next screen loads still reaches it.

  Host, 26-10-26 at e3777b225, on throwaway teams: 1 and 2 PASS; 3's second half PASS (the
  board keeps type-ahead). 3's first half FAILS 8 of 8: a Down sent 1 to 40 ms after Esc still
  moved step 2 off "Leave it running", while a plain No left it there. Task 12.5.

- [x] ✅ **Task 12.5**: Task 12.3's drain does not work on the host: in `ccy-teams remove`, Esc on
  "Back <team> up first?" then a Down sent as a separate write 1 to 40 ms later still reaches
  step 2. Reproduce in a pty test with the real gum (Esc, a pause, then Down as its own write),
  find why 12.3's pty test passed while the host failed and fix the test so it models the host,
  then fix the drain; type-ahead elsewhere is kept.
  Built (code done; host check open). Cause, measured on the real gum 0.17 in a pty: the hold and
  the flush do run (two flushes, one per question), but the confirm exits about 3 ms after the
  Esc, the flush follows at once, and the next gum starts reading its input at once and draws
  some 40 ms later: a Down 5 to 40 ms after the Esc arrives after the flush and before the draw,
  and gum takes it. 12.3's pty test pressed its keys before the flush and ran no real program, so
  it proved only that tcflush discards what is already queued. Fix: a draining screen waits
  `settleTime` (150 ms, `ttyecho.Hold.Settle`) before it discards, then runs. Tests:
  `ui/drain_pty_test.go` on the real gum (red 4 of 5 gaps before the fix; the plain-answer half
  keeps type-ahead), and `ttyecho` (settle before the flush, the real 150 ms in a synctest
  bubble, a key arriving during the settle discarded on a real pty). Left: a key pressed after
  the 150 ms and before gum draws (its own start, about 40 ms) still reaches it.
  Host, after meta-deploy (`play-claude-yolo.yml`), on a throwaway team: `ccy-teams remove <team>`, Esc on "Back <team> up first?", then Down as a separate send-keys 1, 5, 10, 20 and 40
  ms later: step 2's cursor stays on "Leave it running" every time. The board after a plain
  answer still takes a key typed while it loads. Host, 27-10-26 at f9073ce05: PASS, 16 of 16
  stayed on "Leave it running" (was 0 of 8); a Down after step 2 draws still moves it (the
  check is not vacuous); a 300 ms gap carries, as expected past the 150 ms wait; the board keeps
  type-ahead, 5 of 5.

- [x] ✅ **Task 12.6**: Task 12.5's real-gum test is flaky:
  `internal/ui` `TestOnTheRealGumADownSentJustAfterEscDoesNotReachTheNextQuestion/1ms` failed in
  one `qa-changed` run on 27-10-26 (a branch that does not touch `internal/ui`) and passed when
  run alone 3 times. A test whose verdict depends on machine load is a gate that cries wolf.
  Measure how often it fails under load (e.g. `-count` with parallel load), find whether the
  1 ms case races the settle or the test's own timing, and make the verdict deterministic
  without weakening what it proves.
  Done: the test's own timing. Under load, 9 of 80 runs of the 1 ms case failed, every one with
  "the screen never showed What happens to it?". When the Down is written before gum has read
  the Esc, the two are read as one sequence, which gum takes as Alt+Down: the confirm does not
  end (probe: Esc and Down in one write, the confirm never ends, 3 of 3). Fix, test only: the
  gap now counts from the confirm's end (a runner that reports the confirm's Run returning),
  so the Down always lands between the confirm and the next draw. A second defect, also in the
  test and seen by the coordinator's qa-all under `-race`: on a failure, gumPty's cleanup
  closed the pty while the gum goroutine still ran (a data race on the terminal's fd). The
  cleanup now waits for that goroutine. Probe red, then green: race reported, then none.
  After, with `-race` and under CPU load: 0 of 50 (1 ms) and 0 of 150 (all gaps) failed. Beside
  a parallel `go test -race ./...`, 0 of 100 failed. Before, with `-race`: 2 of 50 failed, and
  the race was reported. Still red without the settle (`Settle` nil), with `-race`: 10 of 10
  runs, idle and loaded. No host run: only a test changed. Report: [subagent-reports/261027-task-12-6.md](subagent-reports/261027-task-12-6.md).

- Fact-check of this phase:
  [subagent-reports/261026-fact-check-phase12.md](subagent-reports/261026-fact-check-phase12.md).

## Success Criteria

- [ ] On the host, `ccy-teams` with no teams offers to create one, and a refused or cancelled
  sudo is reported as such, never as "no teams".
- [ ] The owner finds it clear and pleasant to use (their judgement). Coordinator default:
  accepted unless the owner objects; changeable by the owner at any time. It is met once the
  host agent's Tasks 6.4 and 7.5 pass and the owner has raised no objection.

## Delivery & Milestones

- Plan created; work handed to a sub-agent in a worktree.
