# Plan 00177: ccy-teams, human friendly

**Status**: In Progress (host run 26-10-26: 6.4, 10.4 and 10.5 passed; 7.5 and 9.5 still open; Phase 11 holds its findings)
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

### Phase 1: Critical UX review

- [x] ✅ **Task 1.1**: A full critical pass over `ccy-teams` (every verb, key, prompt, message
  and failure path; first run, no teams, sudo refused or cancelled, no TTY, a team with no
  members). Write the findings into `UX-REVIEW.md` in this folder, each with its fix.

### Phase 2: Build

- [x] ✅ **Task 2.1**: Fix every finding from Task 1.1, with tests in
  `scripts/test-ccy-teams-command.bash`. Update `docs/agent-bus.md` "Getting started" where
  the flow changes. The no-teams offer and the new create flow came first, by the owner's
  order. Then sudo (a scoped read-only sudoers rule in play-agent-bus.yml), the people and
  their passwords, add-repo, colour (CCY 3.94.2: the shared picker passes `--ansi`) and
  help. UX-REVIEW.md's deferred D1 and D2 stay open.
- [x] ✅ **Task 2.2**: The `qa-reviewer` agent on the branch; fix its findings.

### Phase 3: Host

- [x] ✅ **Task 3.1**: Host run through meta-deploy (`play-claude-yolo.yml` for gum, CCY
  3.94.2 and ccy-teams; `play-agent-bus.yml` for the sudoers rule
  `/etc/sudoers.d/agent-bus-read-only`), then the owner's first real `ccy-teams` use. Host
  run passed 26-10-13 (both plays), and again with Plan 00161's D64/D65. The owner created
  a real team with `ccy-teams create`; their findings are Phase 4.

### Phase 4: The owner's first-use findings

- [x] ✅ **Task 4.1**: The create wizard was a wall of text. It now shows numbered steps
  (`1/4  GitHub owner`), a one-line hint each, a ✓ per answer and a short summary without
  `forge_api` or `path_prefixes`. gum's grey key-help line is hidden (gum 0.17 cannot
  recolour it).

- [x] ✅ **Task 4.2**: Create sets up Element itself (the `agent_bus_element_teams` entry
  and its play) and offers to open it. No message tells the owner to edit `localhost.yml`.
  Create declares the Element profile through `declare.py` and runs
  `play-agent-bus-element.yml` after `play-agent-bus.yml`.

- [x] ✅ **Task 4.3**: Offer sign-in straight after create (reuse `set_password`).

- [x] ✅ **Task 4.4**: Run from a repository root, `ccy-teams` offers to start a ccy session
  there as a team member, with `ccy --teams <team>` and no seat.

- [x] ✅ **Task 4.5**: Seats default to the next free number, and members are always shown as
  `<repo>.<N>`, counting from 1. Through 4.8's chooser; `agent-bus seat list` on a terminal
  shows the full handle and the type in words.

- [x] ✅ **Task 4.6**: ccy's offer to re-attach to a detached session keeps `--teams`: it
  attaches only to a session holding the requested seats, and otherwise says why it
  starts a new one (CCY 3.95.0).

- [x] ✅ **Task 4.7**: `path_prefixes` no longer limits paths in an org-wide team. It is
  optional in the team file and the team record (absent: any path); create writes none. An
  existing entry that names it keeps its limit until the line is removed.

- [x] ✅ **Task 4.8**: `ccy --teams <team>`, with no seat, is the normal form: on a terminal ccy
  shows the checkout's seats in that team (full handle, free or held), defaults to the lowest
  free one or a new number, and says the handle before taking it. Unattended launches carry
  the canonical `<seat>@<team>`. A mixed-case team name gets a lower-case suggestion.
  Built: `agent-bus seat choose` and a gum chooser in the launcher (CCY 3.95.0); headless,
  restore and restart are refused a bare team (`seat check --resolved`).

- [x] ✅ **Task 4.9**: `ccy-teams create` asks the team's scope: this repository (the
  default inside a checkout, named after it), the whole GitHub owner, or several
  repositories of any owners (one comma-separated input). Each repository is checked for
  syntax, existence and the gh login's push access, in a re-prompt loop. An absent
  `branches` means any branch, the same as an org-wide team (a protocol change).
  The 4.7 follow-up offers once to remove an old `path_prefixes` limit.
  [report](subagent-reports/261013-p177-create-sonnet.md)

- [x] ✅ **Task 4.10**: `cc --teams <team>`: a desktop Claude Code session, running as the
  owner, joins as a `host` member with its own Matrix account. The owner accepts that it
  sits beside their Element session (a misbehaving agent could use it). pingbus's refusal
  beside an Element profile becomes an explicit opt-in for that member. The guard is
  cc's alone, never project settings (those are shared with ccy sessions), and it is
  instruction, not containment: the session is told only the correct way to post (pingbus,
  as its own member). Nothing in its context mentions the owner's Element session or
  posting as anyone else, so the idea is never put in front of it. `cc --teams`
  passes `claude --settings <file>` with `Read(...)`/`Edit(...)` deny rules on Element's
  session folders. Claude Code's sandbox is left out for now: it would also restrict
  network access; a real boundary is investigated in issue #101
  ([research](RESEARCH-cc-boundary.md): a `systemd-run --user` service with
  `InaccessiblePaths=`, not built, the owner's call). Asked for by the owner
  26-10-13. Built and tested (`scripts/test-cc-teams.bash`); the acceptance of Element is
  per user, asked before the seat is made. [report](subagent-reports/task-4.10-cc-teams.md).
  Not yet run on the host.

- [x] ✅ **Task 4.11**: The `qa-reviewer` over Phase 4, and its fixes.
  [report](subagent-reports/261013-qa-reviewer-phase4.md). All fixed: the guard test, the
  meta-deploy list, the acceptance asked before the seat, the write-access check for
  `create --yes`, the bus address asked by create (never "edit localhost.yml"), and the
  advisories (container 2.52, CCY 3.95.2).
  [fixes](subagent-reports/261013-p177-fixes-sonnet.md). Full QA passes.

### Phase 5: Host

- [x] ✅ **Task 5.1**: meta-deploy (`play-agent-bus.yml`, `play-claude-yolo.yml`), then the
  owner's own test: `ccy-teams create` from a repository (scope, Element, sign-in, the
  offer to start a session), `ccy --teams <team>` choosing a seat, and `cc --teams <team>`.
  Deployed; the Go `ccy-teams` works, but the owner's test found the UX clunky: Phase 6.
  The owner signed in to Element, launched a ccy session into a team, and a message they
  posted in the room woke the idle session, which answered in the room: the round trip
  works. That the exchange is hidden inside tool calls in the session is Plan 00194. Done:
  the deploy and the test ran (journal 26-10-13 21:03, the seven fixes); the re-tests of
  `ccy --teams` and `cc --teams` are Task 7.5, of the board Task 6.4.

### Phase 6: No hidden keys (from the owner's host test)

The owner had to discover Ctrl-E to open Element; Enter on their name kept offering a new
password, which they set repeatedly, each time signing out their other sessions; and Ctrl-E
quit `ccy-teams` to start Element.

- [x] ✅ **Task 6.1**: Every action is reachable without knowing a key: Enter on any row opens
  a menu of what can be done to it, in words, each with its shortcut beside it; the footer
  names the selected row's actions, not a fixed list.
- [x] ✅ **Task 6.2**: A person who is signed in is offered Element first; a new password is a
  plainly labelled choice that says it signs out their N sessions, and asks before doing it.
  After a password is set, Element is offered at once.
- [x] ✅ **Task 6.3**: Opening Element starts it apart from `ccy-teams` and returns to the
  board, which then reads the person's sessions again. Owner on the host (26-10-26): Element
  works.
- [x] ✅ **Task 6.4**: The host agent tests it again on the host, driving `ccy-teams` in tmux.
  PASS 26-10-26 (throwaway team, board): the sign-in is offered first, and Element is offered as
  soon as the password is shown.

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
- [ ] ⬜ **Task 7.5**: The host agent tests Phase 7 on the host, in tmux. Nothing to deploy (all of it is in
  the deployed `ccy-teams` and CCY). Tasks 7.1 and 7.2: Plan 00201's ACCEPTANCE-owner.md steps
  3, 5 and 6. Task 7.4: with a team where a person has never signed in to Element, run
  `ccy-teams apply <team>` in a terminal; after the apply it offers that person the sign-in.
  Host run 26-10-26 (throwaway team): 7.2's quick launch, the seat question, status line and
  `pingbus status`, and 7.4 PASS (the apply path does not offer Element after the password:
  an observation). Still open: step 5 FAILS (Task 11.1), and the launch block is missing (11.2).

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
- [x] ✅ **Task 9.6**: The review of `lxc-add`
  ([report](subagent-reports/261014-fable-review-lxc-add.md)) is answered; each finding, in
  the order of the report:
  - F1 fixed: with no host given, `check_lxc` takes the host the same container and user
    already has, so a user in one team can be added to a second (Python and Go tests, the
    report's probe included).
  - F2, F3 fixed: `declare.py lxc-suggest-seat` says whether the container's name gives a
    seat the team does not already hold (any host, because the host is not yet known); when it
    does not, the flow asks for the seat with a free one suggested, checked at that question
    and asked again up to three times. A dry-run refusal at the summary now ends with
    "Refused; nothing was changed."
  - F4 fixed: the host word suggested is `worker`; `lxc-suggest-host` is gone.
  - F5 done: this branch's phase is 9 (Phase 8 belongs to `feat/ccy-teams-hot-add`).
  - F6 done: `meta-deploy.list` has the `play-claude-yolo.yml` entry and deliberately omits the 00161
    acceptance: that runs from F44's current list BEFORE this branch merges, because its
    message counts predate the goodbye (00196 has already run, so nothing else builds this
    `ccy-teams`).
  - F7 resolved: the host question is checked with team, container and user.
  - F8 fixed: names read from a container are shown only if the play's user pattern accepts
    them, and the read runs `env -i PATH=... HOME=/root`, as `members.py` does.
  - F9 done: the unused `LxcRemove` is gone (the Python `lxc-remove` stays for the play's
    `state: absent` route), and the spacing is fixed.
- [x] ✅ **Task 9.7**: The handle is derived from the repository, never asked (owner:
  "seat names / host word should be derived from the repo just like with ccy"). One rule in
  `helpers/agent_bus_lxc/derive.py`, built on `checkout.repo_from_origin` and
  `checkout.host_from_env_local` (the pure halves of what `agent-bus seat take` applies):
  `<repo>` from the checkout's origin, `<host>` the `HOOKS_DAEMON_HOSTNAME` its
  `ccy.env.local` assigns else `local`, `<seat>` the bundle's, else the container-name seat
  of a member already in the team, else the lowest number no active member of the same
  `<repo>+<host>.lxc` holds (`checkout.seat_choices`' rule; a parked number returns).
  `members.py` and `play-agent-bus.yml` derive for an entry that omits host and seat, so a
  hand-written entry needs team, container, user and checkout; `host` and `seat` stay as
  overrides, an entry that gives both is used as it is, and an existing entry keeps its
  handle. The checkout is read from the host's side through the container's LXC config
  (rootfs and `lxc.mount.entry` binds) by a root script that runs no git, follows no link and
  caps every size (owner's safety rules; a symlinked `.git/config`, an fsmonitor setting and
  an oversized file are tested). The flow asks only container, user, checkout (checkouts with
  a GitHub remote suggested) and role, and shows the derived handle before the yes. The host
  word and seat questions, `lxc-suggest-seat` and its Go client methods are gone;
  `declare.py` gains `lxc-derive`, `lxc-checkouts` and `lxc-script`. The users listing keeps
  `getent passwd` through `lxc-attach` (it needs the running container anyway, and the
  names it returns are already validated by the play's pattern).
- [x] ✅ **Task 9.8**: The review of the derivation
  ([report](subagent-reports/261014-fable-review-lxc-derive.md)) is answered; each finding, in
  the order of the report (every test written and seen to fail first, journalled):
  - F1 fixed: `resolve_members` returns a derived entry with `repo` set to the checkout's
    (`member.repo or identity.repo`), `_join` mints the handle and passes `--repo` from that
    value, and `_suggest` (which now has a repo to compare) refuses a container whose
    `pingbus suggest-handle` names another repo. A declared `repo:` that disagrees with the
    checkout's is refused too. `[Remote "origin"]` is read (section names are case-insensitive,
    the subsection is not). `insteadOf`: a config that has a `url.<base>.insteadOf` and an origin
    url is REFUSED, not applied. Why: applying it as git does needs every `url.*.insteadOf` value,
    longest prefix first, over includes and the global config the host cannot see; refusing is a
    dozen lines, cannot read the name wrongly, and a rewrite only in the agent's `~/.gitconfig`
    is caught by the agreement check above. The derive.py comment and the docs are corrected.
  - F2 fixed: an active numbered member of the identity that no entry holds (explicit seat, or
    the bundle of a declared entry anywhere in the file) is refused with the two ways out (state
    absent then present again, or another explicit `seat`), as the explicit-seat path refuses.
    A hand-joined member looks the same, so it is refused the same way; the `lxc-derive`
    preview cannot see bundles and skips such a number (`unheld_ok`). The review's suggestion of
    `seat: <n>` to reclaim is not offered: the explicit path itself refuses that, and the
    remedy it names is state absent then present.
  - F3 fixed: after the apply `ccy-teams lxc-add` reads the team back and reports the one new
    active handle of that repo and host; when it cannot name exactly one (none, several, or the
    read fails) it says the handle is only the expected one and where to look. The summary says
    "Handle (expected)".
  - F4 fixed: an `optional` bind with a missing source is skipped (LXC mounts nothing); any
    other missing source, or rootfs, is refused by name, with no traceback.
  - F5 fixed: a url wholly inside one pair of quotes with no escape is read as git reads it;
    any other quote, escape or continuation is refused.
  - F6 fixed: the root reader's refusal text has every non-printable character replaced.
  - F7 accepted as the review says; F8 fixed (`scripts/test-ccy-teams-command.bash` is mode 755).

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

### Phase 11: Findings of the host run of 26-10-26

- [x] ✅ **Task 11.1**: Task 7.1's chooser showed the full list when exactly one of several teams
  covers the checkout. The code is right and the texts were wrong: Plan 00201 replaced 7.1's
  chooser, and its decision Q5 (S-B2, taken on the owner's instruction) skips the list only
  when the machine has exactly one team; with two or more the owner always sees the list, as
  `TestACoveringTeamAlongsideOthersIsStillListed` pins. Defence Before Fix: the rule
  `chooser-skip-condition-text` ([QA.md](../../QA.md#chooser-skip-condition-text),
  `tests/helpers/agent_bus/test_chooser_texts.py`), red on 3 alone, widened to Go comments by
  the independent search (a 4th, a test comment); all 4 fixed: Task 7.1 above, 00201
  ACCEPTANCE-owner step 5, `docs/agent-bus.md`, `chooser_test.go`. Host: step 5 re-read
  against the new text (with two teams on the machine, the list is shown).
- [x] ✅ **Task 11.2**: Task 7.5's launch block ("This session is on 1 team:") is not printed when
  the seat is taken without `--teams`: `ccy_seats_take` (`lib/agent-bus-seats.bash:78`) prints
  it only for a non-empty canonical list. Traced every route that takes a seat. At launch
  (`--teams`, plain `ccy` whose Quick Launch takes the saved seats, a restart or restore
  replaying `--teams`) every route sets `CCY_SEATS` through `ccy_launch_teams` alone, and the
  one `ccy_seats_take "$CCY_SEATS"` after them all prints the block; pinned in
  `scripts/test-ccy-teams.bash` (the Quick Launch leaves the saved seats in `CCY_SEATS`; one
  setter, one take after it). A plain launch takes no seat, so it has no block. The route that
  printed none was Plan 00203's: `ccy-teams session add` or `move` on a running session that is
  already on a team (no relaunch), ccy or cc. Both now print the block on the ccy-teams terminal
  after the change (`choose.Block`, the code `describe-seats --block` runs); a first join still
  relaunches and the relaunch prints it in the session's pane. Built test first
  (`internal/teams/hot`). Host: `play-claude-yolo.yml` (new `ccy-teams`), then in tmux a plain
  `ccy` with Quick Launch's saved team shows the block above Claude's screen; if it does not, the
  pane is cleared after the launcher prints it (Claude Code's start), which this code cannot see.
- [ ] ⬜ **Task 11.3**: `ccy-teams lxc-add` accepts `root` as the agent's user at the question;
  check Task 9.1's intent (a bad user is refused) and refuse it there if so.
- [ ] ⬜ **Task 11.4**: `ccy-teams remove` leaves the team's `agent_bus_element_teams` entry and
  its Element launcher in the app menu: `play-agent-bus-element.yml` removes no undeclared
  profile. Fix in IaC.

## Success Criteria

- [ ] On the host, `ccy-teams` with no teams offers to create one, and a refused or cancelled
  sudo is reported as such, never as "no teams".
- [ ] The owner finds it clear and pleasant to use (their judgement). Coordinator default:
  accepted unless the owner objects; changeable by the owner at any time. It is met once the
  host agent's Tasks 6.4 and 7.5 pass and the owner has raised no objection.

## Delivery & Milestones

- Plan created; work handed to a sub-agent in a worktree.
