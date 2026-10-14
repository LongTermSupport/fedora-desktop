# Plan 00177: ccy-teams, human friendly

**Status**: In Progress (Phase 4 built and reviewed; waiting on the owner's host test, Task 5.1)
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

- [ ] **Task 5.1**: meta-deploy (`play-agent-bus.yml`, `play-claude-yolo.yml`), then the
  owner's own test: `ccy-teams create` from a repository (scope, Element, sign-in, the
  offer to start a session), `ccy --teams <team>` choosing a seat, and `cc --teams <team>`.
  Deployed; the Go `ccy-teams` works, but the owner's test found the UX clunky: Phase 6.
  The owner signed in to Element, launched a ccy session into a team, and a message they
  posted in the room woke the idle session, which answered in the room: the round trip
  works. That the exchange is hidden inside tool calls in the session is Plan 00194.

### Phase 6: No hidden keys (the owner's host test)

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
  board, which then reads the person's sessions again.
- [ ] ⬜ **Task 6.4**: The owner tests it again on the host.

### Phase 7: `ccy --teams` finds the team (the owner's host test)

- [ ] 🔄 **Task 7.1**: `ccy --teams` with no value offers the teams whose scope covers this
  checkout's GitHub `owner/repo` (an organisation team by its owner, a repository team by
  the repository), choosing alone when exactly one matches and saying so when none does.
  The matching and the choice live in the Go `ccy-teams`; the launcher, on the bash-limits
  ratchet, only calls it and must not grow. Built (`ccy-teams for-checkout [--choose]`, CCY
  3.96.0), awaiting the owner's host test.
- [ ] 🔄 **Task 7.2**: The quick launch ("use the previous configuration") remembers the team:
  `.last-launch.conf` keeps the `--teams` list (`LAST_TEAMS`, config format 2), and reusing
  it launches into the same team; an explicit `--teams` overrides it. Built, awaiting the
  owner's host test.

### Phase 8: Add a running ccy session to a team, without restarting it

The owner: "browse running ccy sessions and add them to a team directly" from ccy-teams. A
restart-based add is out (the owner can restart a session himself): a joining session keeps
running. **Builds after Plan 00194 Phase 2**, whose `ccy-agent-bus` supervisor plugin types
into the session, so a session needs no Claude Code plugin to receive. Design: Plan 00194
DESIGN.md section 3, "Joining a running session".

What was checked (read from the code, not run):

- `pingbus` is in every image (`/opt/claude-yolo/optional/agent-bus/pingbus`, with the Claude
  plugin and settings), but the entrypoint links it onto PATH and loads the plugin only when
  the launch passed `PINGBUS_SEATS`. A running non-team session has the files, no PATH entry.

- The environment is fixed at container start. `PINGBUS_HOME=/tmp/pingbus-home` and
  `PINGBUS_TEAMS` are made by `pingbus seat exec`, which also takes each seat's lock and holds
  it across the `exec`. A running claude, and every command it runs, has neither, and a
  running container's environment cannot be changed.

- Claude Code loads plugins and settings at start, so a running session never gains the
  pingbus plugin (its skill, the sent echo). The supervisor can type, so receiving works
  without it.

- A supervisor restart (the age restart) replays the launcher's in-memory arguments, not the
  registry record; a reboot restore replays the record. A team recorded for a joined session
  survives a reboot but not an age restart unless Task 8.6 is done.

- The ccy supervisor plugin is loaded only for `--max-age/--run-for/--until` today, and
  Plan 00194 plans it for `--teams` launches.

- [ ] ⬜ **Task 8.1**: The `ccy-agent-bus` plugin is loaded in every supervised ccy session
  (Plan 00194 Task 3.1) and is idle while the session holds no seat. A session started
  before it shipped, run `--no-supervise`, or under a supervisor without plugin API 1.1
  cannot join: ccy-teams says so and does not offer "Add to team" for it.

- [ ] ⬜ **Task 8.2**: How a session is told apart in a checkout whose containers share it.
  The host writes a join marker in the seat's bundle directory naming the container (its
  hostname inside is the container id, which the host reads from the engine); the plugin
  acts on a marker naming its own container. Verify on the host that the plugin sees that
  hostname and the host can map record to container; if not, key it by Claude session id.

- [ ] ⬜ **Task 8.3**: A running container's commands get the seat's environment without a
  restart: `pingbus join`, run by the plugin, builds a session home (as `seat exec` does),
  takes the seat lock and keeps it in a small detached holder for the container's life (a
  launch into the seat is then refused as held), starts the watcher and writes a per-session
  env file; the join envelope tells the agent to run `pingbus` by its full path with that
  home. Test first in `helpers/pingbus` (fake clock, lock, home).

- [ ] ⬜ **Task 8.4**: A Go ccy-teams "Running sessions" view lists live ccy sessions from the
  registry and tmux (the ccy-sessions listing, not a second one). Each row has the Phase 6
  Enter menu with "Add to team..." and, for a session already in a team, "Remove from team".
  Also `ccy-teams add-session <session> <team>[/<seat>]` and `remove-session <session> [<team>]` for scripts. A new package; the picker gains only a row kind and a menu. Test
  first through the teamtest fakes.

- [ ] ⬜ **Task 8.5**: Add: choose the team, then the seat (lowest free, or new); say what
  will happen; create the seat (sudo, asked first, as `ccy --teams` does) and its bundle;
  write the join marker; record `--teams` in the session's record (the `record-seats` path)
  so a later restore keeps it; then say what the session will see (the "you have joined team
  T as seat S" envelope, within seconds when idle) and that nothing restarted. Remove is the
  reverse: marker withdrawn, record updated, the plugin stops the watcher, releases the lock
  and tells the session. Fail fast: session not found, not a ccy session, no plugin
  heartbeat, sudo refused; the record is rolled back when the marker cannot be written.

- [ ] ⬜ **Task 8.6**: The age restart keeps a joined team: the relaunch takes `--teams` from
  the record, by a `ccy-teams` filter on the existing relaunch line in the launcher, with no
  growth of the files on the bash-limits ratchet.

- [ ] ⬜ **Task 8.7**: Docs and tests: `docs/agent-bus.md` (seats section) and the usage text
  say a joined session gets the Claude plugin extras (skill, sent echo) at its next start,
  not at once. Tests in the ccy-teams and session-registry bash suites; CCY version bump if
  `files/var/local/claude-yolo/` changes.

- [ ] ⬜ **Task 8.8**: The owner tests it on the host with a running session.

## Success Criteria

- [ ] On the host, `ccy-teams` with no teams offers to create one, and a refused or cancelled
  sudo is reported as such, never as "no teams".
- [ ] The owner finds it clear and pleasant to use (their judgement).

## Delivery & Milestones

- Plan created; work handed to a sub-agent in a worktree.
