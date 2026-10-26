# Plan 00203: teams hot control and robustness

**Status**: In Progress
**Created**: 2026-10-16
**Owner**: joseph
**Priority**: High

## Overview

The owner's request, in their own words: "i would like to be able to "hot" control ccy sessions -
add them, remove them, update them into teams, set roles etc all whilst the ccy session is
running. the whole system needs to be totally robust, systemd services with restart for the
required server side, etc etc. take this to the fully robust and solid level that I need".

Today a session's teams, seats and role are fixed when it is launched. Changing them means a
relaunch. Parts of the system also stop working after a transient fault and do not come back. The
known case: a session's watcher exits for good when its homeserver restarts. This plan makes
membership a live, controllable property of a running session, and makes every server-side and
session-side part of the team system recover by itself.

## Goals

- From `ccy-teams`, as the owner on the host, while the session runs:
  - add a running ccy session to a team;
  - remove it from a team;
  - move it between teams;
  - change its role (coordinator or worker).
- The session picks the change up without a relaunch, and says so in its own conversation.
- Every long-running server-side part is a systemd service that restarts on failure and starts at
  boot. Every session-side part recovers from a transient fault (a homeserver restart, a network
  blip, a host reboot) without a human.
- Health is visible: `ccy-teams` and `pingbus status` say plainly what is down and what to do.
- The whole team's lifecycle from `ccy-teams`: wound down and shut down inside a set window,
  then resumed as it was at that shutdown, coordinator first (Tasks 3.16 to 3.19).

## Non-Goals

- Control from inside a session, or from the bus. Hot control is the owner's, run locally on the
  host, never a Matrix message a peer could send.
- Multi-host teams.

## Tasks

### Phase 1: audit and design

- [ ] 🔄 **Task 1.1**: Robustness audit of the whole team system, server and session side
  (`subagent-reports/261016-robustness-audit-opus.md`).
- [x] ✅ **Task 1.2**: Hot control design and spec ([SPEC-hot-control.md](SPEC-hot-control.md),
  revision 2, H1-H35, milestones M0-M7). Revised after the Fable review
  ([subagent-reports/261016-hot-control-spec-review-fable.md](subagent-reports/261016-hot-control-spec-review-fable.md)).
  Notes: [subagent-reports/261016-hot-control-design-opus.md](subagent-reports/261016-hot-control-design-opus.md).
- [ ] ⬜ **Task 1.3**: Milestones for the build, from 1.1 and 1.2, each one shippable.

### Phase 2: server-side robustness (audit gaps 3, 4, 9, 10)

Built on `fix/teams-server-robustness`; design and evidence in
`subagent-reports/261016-server-robustness-opus.md`, host proof in [ACCEPTANCE.md](ACCEPTANCE.md)
(`acceptance.bash`, R1-R9; B1-B2 after a reboot).

- [ ] 🔄 **Task 2.1** (gap 4): homeserver readiness, watchdog and restart on every exit; a
  minutely health check that restarts one whose client API stops answering.
- [ ] 🔄 **Task 2.2** (gap 3): `agent-bus seat check`/`take` wait, bounded, for a starting
  homeserver.
- [ ] 🔄 **Task 2.3** (gap 10): stale or failed backups and restarts on the board and in
  `agent-bus health status`; the sign-in limits said where a password is issued.
- [ ] 🔄 **Task 2.4** (gap 9): `service: true` LXC members run as a unit in their container.
  R9 judges the running containers only; each stopped one is named offline, never a failure
  (the owner's rule, Plan 00211 Task 2.8).
- [ ] ⬜ **Task 2.5**: host acceptance passed (meta-deploy), and B1-B2 after a reboot.
- [ ] 🔄 **Task 2.6** (owner, urgent): play-agent-bus.yml never fails because an LXC member's
  container is not running: a joined one is skipped, any other deferred to the first run with it
  up, and every running member is joined as before. The member record
  `/var/lib/agent-bus-applied/lxc/<team>.json` says what each run found; `ccy-teams apply`, the
  board and `lxc-add` say what was held back. [SPEC-lxc-stopped-members.md](SPEC-lxc-stopped-members.md)
  L1-L11, built on `fix/lxc-stopped-member` and finished on `fix/lxc-stopped-member-2` (the board,
  the picker and lxc-add); Fable's FIX-FIRST (docs, the record of a team with no entry left, the
  stamps' group refusal) fixed on `fix/lxc-stopped-member-3`; deploy queued in meta-deploy.list. ccy-teams and the play only: no CCY
  or image change.

### Phase 3: hot control build (the spec's milestones, tests first)

- [x] ✅ **Task 3.0** (M1; its commits call it Task 2.2): pingbus. The 3.1 and 3.2 parsers with
  `checkout` (`membership.py`, the one reader, M2's folded in), `active_teams` from
  `PINGBUS_MEMBERSHIP` (H4, H8), `pingbus seat keep` as the only lock holder for every seat with
  `LOCK_UN` and its H10 hook line (D4, H9, H10; audit F5), an empty membership in `seat exec`
  (one `seat exec`, keeping M2's launch-time inbound rule), `seat retire`, `reach`. With M2
  merged and F44's health check fix: CCY 3.99.3, container 2.70. Host acceptance: [ACCEPTANCE.md](ACCEPTANCE.md) M1,
  `_acceptance-m1.inc.bash`. Notes:
  [subagent-reports/261016-m1-pingbus-opus.md](subagent-reports/261016-m1-pingbus-opus.md).

- [x] ✅ **Task 3.1** (M2; its commits call it Task 2.3): the launcher and the entrypoint: the membership directory and its
  read-only relabelled mount, the `ccy-session` label, `PINGBUS_MEMBERSHIP`, `ccy-teams internal membership-init`, the kit in every named session under the inert contract, `--no-bus`,
  `PINGBUS_SEATS` and `ccy-seats` retired for ccy; the H34 spike (Claude Code does not re-read
  `--settings`, so inbound is decided at launch: a session on no team gets none, and its first
  join relaunches it once). Fixed after the Fable review
  ([subagent-reports/261016-m2-review-fable.md](subagent-reports/261016-m2-review-fable.md)).
  CCY 3.99.1, container 2.68, plugin 1.3.0. Host acceptance: [ACCEPTANCE.md](ACCEPTANCE.md) M2,
  `acceptance.bash` (a dispatcher; `_acceptance-m2.inc.bash` starts its own throwaway
  sessions), then the Plan 00201 and 00161 acceptances, ported to the membership, all queued in
  meta-deploy. Notes: [subagent-reports/261016-m2-launcher-opus.md](subagent-reports/261016-m2-launcher-opus.md).
  After the first host run (CCY 3.99.4.2): a ccy run from inside another session's pane is
  unnamed and writes no session's record, `record-seats` included (it had taken the outer
  session's identity: the U20 abort); the relabel is proved by labels on a Permissive host too;
  the 00201 and 00161 launches are unnamed by construction, and U20 M2.11 proves a named seated
  session. After the second (F44 7fd72106): M2's and U20 M2.11's tmux sessions run ccy under the
  pane's own shell, never `exec`'d (with every stream redirected, tmux hung up a pane nothing
  held, before ccy started), and record its exit status; U20 M2.3 stops the supervised watcher
  (SPEC-inject E26) and proves the `watcher_failed` path to `pingbus wait`.

- [x] ✅ **Task 3.2** (M3): the first end-to-end hot add and remove. `ccy-teams session add` and
  `remove` (per-step ✓/✗, idempotent, rolled back; re-baseline and goodbye on remove; the first
  join of a zero-seat session relaunches once through the supervisor's restart replay), one
  shared session discovery (`internal/teams/discover`, the shutdown lookup folded in), the
  watcher and waiter following the membership with the keeper starting the watcher (H11), the
  `MEMBERSHIP` kind with its hook fallback (H12, H13), the status line (H14), mid-turn removal
  (H15, H16). CCY 3.99.8, container 2.73, lifecycle plugin 1.1.0. Host acceptance:
  [ACCEPTANCE.md](ACCEPTANCE.md) M3, `_acceptance-m3.inc.bash`. Notes:
  [subagent-reports/261016-m3-opus.md](subagent-reports/261016-m3-opus.md).

- [ ] 🔄 **Task 3.3** (M4): the full non-interactive surface, built on `feat/hot-control-m4`;
  Fable's review said FIX-FIRST
  ([subagent-reports/261016-m4-review-fable.md](subagent-reports/261016-m4-review-fable.md)).
  B1, B2, N1, N3, N5 and the acceptance points are fixed; N2 and N4 are open for the owner
  (Decisions 5 and 6). Host acceptance is still to come. `ccy-teams session role`, `move`, `list` and
  `repair`, the coordinator options of remove and move (`--coordinator=demote|hand-to=<seat>`),
  `add --role`, `remove --park` (H23-H25, H10); one change at a time per session and one seat
  allocation at a time per checkout, `agent-bus seat take` and `seat choose` honouring both
  (H20, H21, with Fable's N4: a stopped session's restorable record names its seats); the
  roster limit (H27), the session ending mid-change (H28), `ccy-teams remove <team>` taking its
  sessions off first (H29); the in-session `👥 <team>(down)` status (H26). With F44's third
  acceptance fixes merged and Fable's fixes: CCY 3.99.10.1, container 2.75.1. Host acceptance:
  [ACCEPTANCE.md](ACCEPTANCE.md) M4, `_acceptance-m4.inc.bash`. As built, and what is not:
  SPEC-hot-control.md H21. Notes:
  [subagent-reports/261016-m4-opus.md](subagent-reports/261016-m4-opus.md).

- [ ] 🔄 **Task 3.4** (M5): hot control from the picker, built on `feat/hot-control-m5`. In
  `ccy-teams`, the teams list's last row, Running sessions (or Tab, printed beside it), lists
  the sessions `session list` shows. Enter on one lists what can be done to it, in words: Add
  to a team, Remove from `<team>` (the coordinator's role goes to no one or to another
  member), Change role in `<team>`, Move to another team, Attach and, for an unguarded session,
  Repair. No action is reached by a key alone (InteractiveScripts.md rule 14; the owner
  rejected the first build's Ctrl-A/X/O). Every change runs the M4
  `ccy-teams session` command with the words a person would type, so the questions, steps,
  locks and rollback are the same. A refused answer is said and asked again, 3 times at most.
  ccy-teams only: no launcher or container change, so no version bump. Fable review and the
  owner's steps ([ACCEPTANCE.md](ACCEPTANCE.md) M5) are to come. As built:
  SPEC-hot-control.md, M5 as built. Notes:
  [subagent-reports/261016-m5-opus.md](subagent-reports/261016-m5-opus.md).

- [ ] 🔄 **Task 3.7** (owner feedback on M5; its commits call it Task 3.5): the coordinator's
  two paths, built on `fix/coordinator-two-paths`, merged after Fable's MERGE; host acceptance
  to come. A team with no coordinator is offered one plainly; a team that
  has one is a takeover that names the current coordinator and whether it runs, and starts on
  No; with no terminal or `--yes` it needs `--demote-current`. In every place that can make a
  coordinator (SPEC-hot-control.md H25). With it, the picker's describe and path-limit offers
  become now, not now or never, and an old no is asked again once (Plan 00201 S-A15).
  lxc-add gets the same two paths (a new `declare lxc-role`, which the image carries): CCY
  3.99.12.4, container 2.77.3. Owner's steps: [ACCEPTANCE.md](ACCEPTANCE.md), the two
  sections after M5. Notes:
  [subagent-reports/261016-coordinator-ux-opus.md](subagent-reports/261016-coordinator-ux-opus.md).

- [ ] 🔄 **Task 3.6** (M6; its commits call it Task 3.5): cc, built on `feat/hot-control-m6`,
  merged; host acceptance to come. `cc --teams` and the new `cc --bus` sessions are hot-controlled like ccy ones (H31,
  H32): a `kind: cc` membership under the session's tmux name, a keeper that follows it, a
  system prompt that names no team, and `ccy-teams session add|remove|move|role|list` (and
  the M5 picker) finding cc sessions on ccy's tmux server. `cc --bus` refuses team messages
  until it is restarted on its first team (Decision 7). Repair is refused for cc (exit 78),
  and the record follows each change. CCY 3.99.12.3, container 2.77.2. Host
  acceptance: [ACCEPTANCE.md](ACCEPTANCE.md) M6, `_acceptance-m6.inc.bash`. As built, and what
  is not: SPEC-hot-control.md H32 (Decision 7). Notes:
  [subagent-reports/261016-m6-opus.md](subagent-reports/261016-m6-opus.md). Fable said
  FIX-FIRST ([subagent-reports/261016-m6-review-fable.md](subagent-reports/261016-m6-review-fable.md)):
  B1 (a membership left under a reused tmux name) is fixed, for cc and for ccy `--no-bus`, with
  the minor points; the re-check said MERGE, with two follow-ups: R1 (a cc session hot-added
  with a `--` in its command line is judged not on the bus) and R2 (an unreadable session
  record is taken as not on the bus instead of failing). Both are fixed on `fix/m6-followups`
  (Go only): a record's launcher words are ccy's up to `--` and all of cc's, `SetTeams`
  writes the seats before the first `--` (which also fixes M3's ccy restore of a command line
  with `--`), and an unreadable record stops `session list`, `add` and the cc listing, naming it.

- [ ] 🔄 **Task 3.5**: the container name race the M2 host run found, built on
  `fix/ccy-name-race`. Two ccy launches started together chose the same lowest free name and
  the second failed at podman's create (exit 125). The launcher now claims its name through
  `ccy-teams internal claim-container-name`: a per-process claim under
  `$XDG_RUNTIME_DIR/ccy/container-names/`, written under a lock held only for the choice.
  `scripts/test-ccy-container-name.bash` races launches over a fake engine. CCY 3.99.10.2,
  container 2.75.1. Host acceptance: the M2 leg of `acceptance.bash`, queued in meta-deploy.
  Notes: [subagent-reports/261016-name-race-opus.md](subagent-reports/261016-name-race-opus.md).

- [ ] 🔄 **Task 3.8** (owner feedback on lxc-add in 3.99.12.10): the Checkout step is a typed
  line with Tab completion from the container's tree (`internal/ui/pathinput`, the first
  Bubble Tea screen a command runs; a hint line says Tab completes), built on
  `fix/lxc-checkout-typed`. The checkout search also covers `/var/www`, `/srv` and `/opt`,
  bounded by depth, count and time, and says when it is partial; what it found is suggested
  under the line. A paste goes in as typed text; control characters and bidirectional
  controls are refused whole, typed or pasted. Preceded by Plan 00204 Task 2.3
  (`project_hidden_bindings` over Bubble Tea). CCY 3.99.12.12, container 2.77.8 (merged
  over F44's 3.99.12.11). Host acceptance: `ccy-teams lxc-add <team>`, a path pasted into the
  Checkout line, after meta-deploy.

- [ ] 🔄 **Task 3.9** (owner feedback; its first commits and journal entries call it Task
  3.8, renumbered because F44's lxc-checkout task holds 3.8): a running session added from
  the teams list, on `feat/teams-add-session-and-restart`. A team's menu has Add a running
  session to `<team>`, and a running seat's menu Add this session to another team, both
  through the Running sessions view's seat and role questions
  ([SPEC-team-add-and-restart.md](SPEC-team-add-and-restart.md) A1-A10). Picker only. Built
  and tested; Fable's FIX-FIRST fixed; host acceptance to come.

- [x] ✅ **Task 3.8** (owner feedback): every member kind has the same roles, built on
  `fix/teams-member-roles`. Enter on an LXC member row offered Park first and no role. Every
  member row (ccy, cc, LXC, host, docker, vm) now offers the same role action, worded by package
  coordinator's two paths, through one role-change path that dispatches by kind only at the
  last step (a declared LXC member's entry is written too, then applied). No destructive
  action is first in any picker menu: a tested rule. `ccy-teams session role` also takes a
  member's handle. The status line shows each team with this session's role,
  `👥 desktop (coordinator), other (worker)`, read on every refresh from the team record the
  role notice reads; `(role?)` where it cannot be read. CCY 3.99.12.15, container 2.77.11
  (merged over F44's 3.99.12.14). A member row with no handle yet (a deferred LXC member, a
  seat ccy has not joined) offers no role and no Park. Fable's FIX-FIRST on
  `fix/teams-member-roles-2` fixed, and `fix/lxc-stopped-member-3` merged, on
  `fix/teams-member-roles-3`. Deployed in F44 9b9e50e0; the owner confirmed on the host that a
  running LXC member can be made a team's coordinator. Notes:
  [subagent-reports/261016-member-roles-opus.md](subagent-reports/261016-member-roles-opus.md),
  [subagent-reports/261019-member-roles-remerge-opus.md](subagent-reports/261019-member-roles-remerge-opus.md).

- [ ] 🔄 **Task 3.10** (was 3.9): each session's conversation recorded by the pingbus
  SessionStart hook beside its report, read on the host only for a record whose launch
  carried the bus (SPEC C1-C4), on `feat/teams-restart`. `conversation.json` on every
  SessionStart (startup, resume, clear, compact), and through `pingbus hook conversation` in a
  session holding no seat (Decision 12); `internal/conversation` reads it, resolving the
  transcript by the recorded id. Pingbus plugin 1.4.0. Built and tested; review and host
  acceptance to come.

- [ ] 🔄 **Task 3.11** (was 3.9's restore and 3.11): restore after a reboot and the new
  `ccy-teams restart <team>` (with the team menu's Restart the team's desktop sessions)
  resume each session by its recorded id, never `--continue` (SPEC C5, C6, B1-B11,
  Decisions 8, 9 and 10), on `feat/teams-restart-2`. `registry.ResumeArgs`;
  `internal/teams/restart`; `membership.WriteRestart`/`WithdrawRestart` (only a restart's
  request is withdrawn); lifecycle plugin 1.2.0 names the restart. The uncalled bash restore
  is deleted. Built and tested; review and host acceptance to come.

- [ ] 🔄 **Task 3.12** (was 3.10): Claude Code's agent view off in the ccy image (SPEC C7,
  Decision 11): `ENV CLAUDE_CODE_DISABLE_AGENT_VIEW=1`, the name confirmed in the Claude Code
  2.1.303 build. CCY 3.99.12.16, container 2.77.12 (merged over
  `fix/teams-member-roles-3`'s 3.99.12.15, on `feat/teams-restart-3`). Built and tested; host
  acceptance to come.

- [ ] 🔄 **Task 3.13** (owner feedback): ccy-teams opens fast, on `fix/teams-open-fast`. The
  board's agent-bus calls (each checkout's `seat list`, each team's `list`, then each person's
  `human devices`) ran one after another; they now run together, eight at most, with what each
  says kept and written out in the old order, and a checkout known by two names is read once.
  The declare helper runs on `/usr/bin/python3`, not through the pyenv shim. The output is
  unchanged. On fakes at 0.4 s a call: 6.1 s down to 1.3-1.7 s. Built and tested; host
  measurement to come. Next, separately: one agent-bus call for the whole board
  ([report](subagent-reports/261019-teams-open-speed-opus.md)).

- [ ] 🔄 **Task 3.14** (owner request): wind a team down.
  `ccy-teams wind-down <team> [--yes]` and the team menu's Wind down the team's sessions: every
  running desktop session of the team, the coordinator included, gets `/goal clear` typed into
  its pane at an idle moment, then the wind-down instruction (save and push work in progress,
  stop sub-agents, record where unfinished work stands, then stop), typed too, so it is the
  owner's own turn and never a bus message. An LXC member in a running container gets the same
  in its service unit's `agent-bus-claude` tmux session, through `sudo lxc-attach` as its user
  (`lxcfreeze.Lister.InContainer` under `tmuxccy`). A stopped or held-back container, members
  elsewhere and unnamed held seats are out of reach and fail the run, named with the text to give
  them by hand ([SPEC-team-wind-down.md](SPEC-team-wind-down.md) W1-W13). Fable's FIX-FIRST
  (the LXC route, the supervisor interleave named, a poll of grace before a second Enter) fixed.
  ccy-teams only: no CCY or image change. Host acceptance to come. Known limits (SPEC section
  4): the ccy supervisor can type a new `/goal` afterwards, and types at the same idle moments.

- [ ] ⬜ **Task 3.15** (follow-up from 3.14's review): `ccy-teams shutdown` reaches LXC members
  the way wind-down does: `/exit` typed into a running container's `agent-bus-claude` session
  through `lxcfreeze.Lister.InContainer`, instead of naming every LXC member as not reachable.
  A `service: true` member's unit has `Restart=always`
  (`helpers/agent_bus_lxc/members.py` `render_member_unit`), so a typed `/exit` alone is
  followed by systemd starting a fresh conversation: after the `/exit` (and after the hard
  stop), the shutdown runs `systemctl stop` of that member's unit in the container, and the
  member counts as ended only when the unit is inactive. A container stopped at the shutdown
  is offline: named, not a failure.

Tasks 3.16 to 3.19 are the owner's whole-team lifecycle request. Each is spec first: numbered
expectations in the named SPEC, a Fable review, then the build, tests first. Decision 13 is
the owner's; Decisions 14 to 23 are open, each with the default it runs on.

**Offline members, in all four tasks** (the owner's rule, CLAUDE/AgentNotes.md "An offline
team member never blocks anything"): a member whose container is stopped, or that the last
apply held back, is shown as `offline (container <name> stopped)` and blocks nothing; the rest
of the operation goes on and its exit code ignores it. Only an operation that explicitly
targets that one member (Attach on its row, `ccy-teams resume <team> --member <handle>`)
reports it, as exit 69 (`EX_UNAVAILABLE`, a new `term` exit code). Offline is not "failed":
a member that is online and does not start, or cannot be typed to, is still a failure (exit 1).

**One lifecycle operation per team at a time.** `wind-down`, `shutdown`, `restart` and
`resume` each take a per-team lock (an `flock` on a file in ccy-teams' state directory, held
by the process doing the work for its whole run, so a dead process frees it). A second one
for the same team is refused, exit 1, naming what holds it and since when; while a timed
wind-down (3.16) is pending it also names `ccy-teams wind-down --cancel <team>`. So a pending
wind-down can never kill sessions that a restart or resume started after it began.

- [ ] ⬜ **Task 3.16** (owner request; extends 3.14 and 3.15): a timed wind-down that ends in a
  forced shutdown. `ccy-teams wind-down <team> --then-shutdown[=<minutes>]` and a team menu
  line, Wind down and shut down the team: an outer window (default 15 minutes, 30 offered in
  the picker) and an inner coordinator deadline inside it (Decision 14). 3.14's typed path
  reaches the coordinator first, then every other member (Decision 15); each text names the
  inner deadline as a clock time and says the team is shut down by force at it. Enforcement is
  on the host, never left to the coordinator: at the inner deadline (or earlier, Decision 16)
  Plan 00201's `ccy-teams shutdown` path (SPEC-shutdown.md SD10 to SD12: `/exit` typed, a busy
  turn interrupted, then `tmux kill-session`) runs on every session still open, its waits cut
  so that it ends inside the outer window. The wait runs outside the owner's terminal unless
  the team has an online LXC member (Decision 17) and can be cancelled. LXC members are wound
  down by 3.14's route; they are forced only once 3.15 lands (its `systemctl stop` included),
  and are named as not forced until then. An offline member is shown as offline, is not typed
  to or forced, and changes no exit code. 3.17's last-shutdown record is written at the
  wind-down's start, from the members found then, and refreshed just before the forced
  shutdown, so a member that ends during the window is still in it. **Forced means forced**: at
  the inner deadline the cut shutdown departs from SD10 and SD11 for this mode only. A session
  whose input box holds unsent text is ended by `tmux kill-session` after the record is
  written, and the report names it with the unsent text read from the pane. A session left at
  the launcher's compose-stop question is ended by `kill-session` too, the report naming its
  compose services as left as they were. `--cancel` before the deadline stops the unit and
  says what was already typed; once the forced phase has begun it stops before the next
  session not yet typed to or killed, and the report names which sessions ended and which were
  left running (the record already written stays). Plan 00201's SPEC-shutdown.md carries a
  line pointing here, so SD10 to SD14 and this mode do not drift. Spec: a new section of
  [SPEC-team-wind-down.md](SPEC-team-wind-down.md).
- [ ] ⬜ **Task 3.17** (owner request; extends 3.11): resume the whole team as it was at its last
  shutdown, coordinator first. A clean `/exit` removes the session's registry record
  (SPEC-shutdown.md, "What ends after `/exit`"), so after a team shutdown `ccy-teams restart`
  finds nothing to start (SPEC-team-add-and-restart.md B3). `ccy-teams shutdown`, and 3.16's
  forced one, first write a last-shutdown record for the team: each session's registry record,
  its seats and role, and its conversation **id copied in** (C4; not a pointer to
  `conversation.json`, which is keyed by session name, so a new session reusing that tmux
  name would overwrite it); and each LXC member reached, with its container, user, seats,
  role and working directory. A new verb, `ccy-teams resume <team> [--member <handle>]` and a
  team menu line, Resume the team as at its last shutdown (Decision 19), consumes the record
  (Decision 21); `restart` stays 3.11's and never reads it. The resume first checks each entry
  against the team's current roster: a seat removed or parked since, or a member now on
  another role, is named and not started (a role change is applied as the roster says). A
  member already running is named and left as it is. A desktop session whose tmux name is now
  held by another session fails, naming it, and keeps its entry. It starts the coordinator
  first and waits for its seats to be held (B6), then the rest together (Decision 18). A
  host ccy coordinator's seats are read with `agent-bus seat list` in its checkout; an LXC
  coordinator's are read by the same command run in the container through 3.18's route, as
  the member's user, in its working directory. An offline coordinator is shown as offline and
  the rest start without waiting for it (exit 0). Host ccy and cc sessions resume by their
  recorded conversation id, never `--continue`; LXC members resume by 3.18's resume command,
  whose default is `ccy --continue` in the member's working directory (Decision 13). Spec: a
  new section of [SPEC-team-add-and-restart.md](SPEC-team-add-and-restart.md).
- [ ] ⬜ **Task 3.18** (owner request; extends 3.15 and 3.17; cross-reference Plan 00205): how an
  LXC member is reached and resumed, set per team with a per-seat override and an LXC default.
  The owner reaches a member by `ssh` or `lxc-attach`, a `cd` into its recorded checkout, then
  a command; some teams use a per-container shell alias (`<alias>`) that starts the container
  and opens an ssh session. A `connect` and a `resume` setting on the `agent_bus_teams` entry,
  overridable on each `agent_bus_lxc_members` entry (Decision 20). The resume is the connect
  command, a `cd` into the member's recorded working directory, then the resume command,
  whose LXC default is `ccy --continue` (ccy in the container is a claude wrapper; Decision
  13); the default connect is today's route (`sudo lxc-attach` as the member's user). No
  conversation id is recorded in the container. The expectations:
  - a member without a service is started in a named tmux session in the container, so the
    resumed agent outlives the `lxc-attach` (or ssh) that started it;
  - a `service: true` member is resumed through its unit, never by a second claude beside it:
    the unit (stopped by 3.15's shutdown) is started with a one-shot `--continue` that
    `agent-bus-claude` (whose last line is `exec claude ... "$@"`) receives for that start
    only, so a later `Restart=always` restart is not changed by it;
  - `--continue` takes the newest conversation in that directory, so it is exact only while
    each LXC member of a team has its own working directory: the resume refuses every member
    that shares one with another member of the team (named with the other, exit 1, the rest
    still resumed), unless the seat has its own resume override (Decision 23);
  - the settings are used by 3.17's resume, by 3.14's and 3.15's paths, and by Attach on an LXC member row, which
    is new: Attach refuses such a row today ("its checkout is not known here" / "it has no
    running session", `src-go/internal/teams/picker/menu.go` `attachRefusal`). The new keys are
    refused by both declare parsers today (`helpers/agent_bus/declare.py` and
    `src-go/internal/bus/declare`, whose parity oracle pins it with
    `testdata/parity/lxc/unknown-keys.yml`), so both parsers, `helpers/agent_bus_lxc/members.py`
    (the unit) and the parity fixtures change together;
  - Plan 00205 (ssh members; research done, nothing built) owns ssh as a provisioning
    route, with ssh-config `Host` aliases as destinations; this task only names a command for an
    already provisioned member and takes 00205's destination as its ssh form once that is
    built, never a second ssh model. Spec: a new SPEC-lxc-connect-and-resume.md in this plan.
- [ ] ⬜ **Task 3.19** (owner request; cross-reference Plan 00196): the coordinator's start text.
  When a session starts as a team's coordinator, and when a hot role change makes it one
  (3.8's role notice), it is told: it is the coordinator; it cannot start, add or provision
  team members itself, only the owner can, from `ccy-teams` on the host; when the work needs
  members who are not present, it asks the human to provision them, saying how many and for
  what, so the work can be delegated to them. **This plan owns it**: it extends the
  `role_coordinator` text in `helpers/pingbus/hooks.py` that Plan 00196 Task 1.5 wrote, whose
  build is ticked and waits only on host acceptance, so reopening 00196 would mix a new
  request into that acceptance. One edit reaches every place the text is shown: all go
  through `role_lines` (SessionStart, the ccy membership notice, and 3.8's `role_notice`).
  `helpers/pingbus/` is built into the image's pingbus zipapp, so the change takes the CCY
  and container version bump (CLAUDE/ContainerRules.md) as well as the pingbus plugin bump;
  LXC members get the new text only once play-agent-bus refreshes their kit. Spec: a new
  section of [SPEC-hot-control.md](SPEC-hot-control.md); the wording is Decision 22.

## Decisions

These were settled by the coordinator under the owner's standing "do not gate on me". **The owner
may reverse any of them.** Full detail: SPEC-hot-control.md section 8.

1. The bus kit is loaded in every ccy session started through ccy's tmux, with a `--no-bus`
   opt-out. This holds only if the inert contract is met and tested in M2:
   - a flag-file guard in `hooks.json`;
   - no per-second Python status line;
   - no `crossSessionInbound` accept unless the session is on a team. Claude Code reads it only
     at launch (the M2 spike), and binds its inbox socket in every session, so a session
     launched on no team is launched with `refuse`: the socket drops every message. Its
     first join relaunches it once, automatically, through the restart path; every later
     change is hot (SPEC H22, H34);
   - exactly one resident process, the keeper.
2. Hot control is control, not isolation, within a checkout. **A remove does not revoke the
   token.** `--revoke` is deferred behind audit F7.
3. cc's weaker authority is accepted (the session can write its own membership file), and so
   is the new `cc --bus`.
4. `PINGBUS_SEATS` and the `ccy-seats` label are retired in M2, together with the explicit
   `PINGBUS_MEMBERSHIP` marker.

### Open: for the owner (from Fable's M4 review)

These are not settled. Each runs on the interim default below until the owner decides.

5. **N2: a coordinator removed while its homeserver is down.** Interim default: it keeps the
   role. The remove releases the seat, and the goodbye and status stay owed (H26). The role is
   not checked and is not owed, so the team record names a coordinator whose seat is free.
   The re-run that sends the goodbye does not return to the role. The alternatives:

   - the role is owed too: a `role` on the farewell entry, demoted on the re-run (what H19
     would expect);
   - such a remove is refused without `--coordinator=demote`, which then stays owed.

6. **N4: "live" means different things in ccy-teams and agent-bus.** Interim default: they
   stay different.

   - ccy-teams counts a session on ccy's tmux server, and honours a stopped session's
     restorable record.
   - `agent-bus seat take` and `seat choose` count a running `ccy-session` container, and read
     no records.

   So the launcher's chooser can offer a seat that a stopped restorable session will replay.
   A `ccy --teams` launched while another session is in its age-restart or first-join
   relaunch (tmux alive, container down) can take that session's seat; the returning keeper
   is then refused `held-elsewhere`. The alternative: agent-bus applies ccy-teams' two tests,
   excluding by name the session being launched.

### Open: for the owner (from M6)

7. **A `cc --bus` session on no team refuses cross-session inbound until it is on a team.**
   Interim default, set by the coordinator to match Decision 1: `cc --bus` starts with
   `crossSessionInbound: refuse`. cc has no supervisor to do the relaunch a ccy session gets,
   so its first `session add` keeps the seat, the desired file and the record, and exits 1
   saying plainly that cc must be ended and started again from `ccy-sessions`, which starts it
   in that seat, accepting. The alternative: `cc --bus` accepts from launch (cc's settings
   already do for `--teams`), so its first join is hot with no restart, and until then any
   process of the owner's uid can post into it.

### Open: for the owner (team add and restart)

Each runs as stated until the owner decides; detail in SPEC-team-add-and-restart.md section 5.

08. **A running ccy member of a restarted team restarts in place**, at its next idle moment,
    through its own supervisor, resuming its exact conversation on the current image. The
    alternative: running members are skipped and only stopped ones started.
09. **A running cc member is left running** (cc has no supervisor). The alternative: `/exit`
    typed into it as `shutdown` does, then started again with its recorded conversation.
10. **Restore and restart resume by recorded conversation id only, never `--continue`.** A
    session without the bus kit (`ccy --no-bus`, a plain `cc`) records none, so after a reboot
    it is named and not restored. The alternative: a recording hook in every session.
    **This changes `ccy-sessions restore` for existing records.** Before: every record was
    started with its arguments plus `--continue` (the newest conversation in its folder,
    which several sessions share, or a refusal behind a stale background record). After:
    each is started with `--resume <its recorded id>`; a `--no-bus` or plain-`cc` record, or
    one whose hook never ran, is not started: the restore names it as a failure (exit 1),
    says to run `ccy --resume` in its folder, and keeps its record.
11. **Claude Code's agent view is off in the ccy image's environment**
    (`CLAUDE_CODE_DISABLE_AGENT_VIEW=1`), background sub-agents and shell tasks left on.
12. **A session on no team records its conversation too: the one exception to the inert
    contract (Decision 1).** Found while building Task 3.10: the plugin's hooks run only while
    a seat is held, so under Decision 10 every ccy session on no team (most of them) would
    record nothing and not come back after a reboot. Now its SessionStart, and only that,
    runs `pingbus hook conversation`: one short Python process at startup, `/clear`, a resume
    or a compaction, which writes `conversation.json` and answers nothing. Every other event
    stays inert. The alternative: keep the contract whole, and such sessions are named and
    not restored after a reboot (`ccy --resume` by hand).

### The whole-team lifecycle (Tasks 3.16 to 3.19)

13. **Settled by the owner, 2026-10-26: the whole-team resume resumes each host session by its
    recorded conversation id, never `--continue`; an LXC member resumes with `--continue`.** The owner's request said `--continue`
    (today each member is relaunched by hand with `ccy --continue --teams`); asked, the owner
    ruled that resuming by recorded id is better. Decision 10 and SPEC-team-add-and-restart.md
    F4 and F5 give the reasons: sessions in one checkout share one transcript folder, so
    `--continue` takes the newest conversation of any of them, and one stale background record
    from another PID namespace makes it refuse for good. **LXC members are the exception**
    (the owner's ruling, 2026-10-26): inside a container `ccy --continue` resumes a session
    cleanly and is what the owner uses today, so an LXC member is resumed by 3.18's connect
    command, a `cd` into its recorded working directory and its resume command, default
    `ccy --continue`; no conversation id is recorded in the container. The limit:
    `--continue` takes the newest conversation in that directory, so it is exact only while
    each LXC member has its own working directory (Decision 23).

### Open: for the owner (the whole-team lifecycle)

Each runs on its default until the owner decides.

14. **The inner deadline.** Default: the outer window less 3 minutes (15 gives 12, 30 gives
    27), and an outer window under 5 minutes is refused. The 3 minutes cover the forced
    shutdown's own waits, all sessions together: 30 s for a prompt, 60 s for an exit, and
    SD14's 30 s seat wait, 2 minutes in all, with the rest for the LXC route and its
    `systemctl stop`. The wind-down's own bounds are capped at the inner deadline: W6's 300 s
    wait for idle ends there (with a 5-minute window the inner deadline is 2 minutes away), and
    a member still waiting then has had nothing typed and is forced with the rest. The
    alternative: a separate `--coordinator-deadline`.
15. **Who is told.** Default: the host types the wind-down into every member, the coordinator
    first and the others once the coordinator's line has gone in; the workers' text says the
    coordinator leads it. Only a typed line can clear a `/goal` (SPEC-team-wind-down.md F1), and
    the coordinator cannot type into a worker. The alternative: only the coordinator is typed
    to, and it tells the workers over the bus.
16. **Shutting down before the deadline.** Default: the forced shutdown starts early once every
    session has been idle with an empty box for 2 minutes after its wind-down turn. The
    alternative: it always waits for the inner deadline.
17. **Where the wait runs.** Default: a transient systemd user unit, so closing the terminal or
    the picker does not cancel it; `ccy-teams wind-down --cancel <team>` stops it and says what
    was already typed. **Except for a team with an online LXC member**: the LXC route is
    `sudo -n lxc-attach`, which needs a sudo timestamp, and sudo's timestamp belongs to the
    owner's terminal, which a unit does not have (and 12 minutes later it would have expired).
    Such a team's wind-down stays in the foreground with a countdown, keeping the timestamp
    fresh with `sudo -n -v` each minute; a refresh that fails makes every LXC member not yet
    forced a failure, named with the text to give it by hand (exit 1), and the desktop sessions
    are still forced. Closing that terminal ends the countdown and frees the lock: what was
    typed stays and nothing is forced, so the owner runs `ccy-teams shutdown <team>`. The
    alternatives: a root timer scheduled in each
    container in the foreground (`systemd-run --on-active` through `lxc-attach`), which needs a
    guest-side stop program; or the foreground countdown for every team.
18. **A coordinator that does not start on resume.** Default: a coordinator that is online and
    fails to start is ✗ with why, the rest are still started, and the run exits 1. An offline
    coordinator (its container stopped) is not a failure: it is shown `-` offline, the rest are
    started without waiting for its seats, and the run exits 0; only `resume --member` naming
    it exits 69. The alternative: nothing more is started after a coordinator that failed.
19. **One verb or two.** Default: two. `ccy-teams resume <team>` (and its menu line) reads and
    consumes the last-shutdown record, checking each entry against the current roster;
    `ccy-teams restart <team>` stays 3.11's, restarting what is running and never reading the
    record, so a restart weeks later cannot bring back members the owner has since ended or
    removed. The alternative: `restart` also reads the record, but only when the team has no
    running member.
20. **The form of the connect and resume settings.** Default: a word list with placeholders
    (`{container}`, `{user}`, `{checkout}`, the last being the member's recorded working
    directory), run with no shell; there is no conversation placeholder, since an LXC member
    records none (Decision 13). A shell alias lives only in the owner's interactive shell, so a
    team that uses one names a script or the alias's expansion. The alternative: the setting
    is run by an interactive `bash -ic`, so the alias works as typed, along with everything
    else the owner's shell start-up does.
21. **The last-shutdown record's life.** Default: consumed. Each entry is removed once its
    member has been started, or named as gone from the roster; an entry whose start failed, or
    whose member was offline, is kept, so a second `resume` retries only those. The next
    shutdown replaces the whole record. The alternative: kept whole until the next shutdown,
    every resume reading all of it.
22. **The coordinator's start text**, default wording: "agent-bus: in team {team} you are the
    coordinator. ... You cannot start, add or provision team members yourself; only the owner
    can, from ccy-teams on the host. When the work needs members who are not present, ask the
    human to provision them, saying how many and for what, then delegate to them." (the
    existing first sentences kept where the dots are). The alternative: a shorter text that
    only names the limit ("only the owner can add members, from ccy-teams") and points at a
    coordinator section of the pingbus skill for the rest.
23. **Two LXC members of a team in one working directory.** Default: the resume refuses each
    of them (named with the other, exit 1; the rest are still resumed), since `ccy --continue`
    would give both the same, newest conversation; a seat with its own resume override is
    exempt. The alternative: a warning, and both are resumed with `--continue` anyway.

## Success Criteria

- [ ] The owner adds, removes, moves and re-roles a running ccy session from `ccy-teams`, and the
  session follows without a relaunch.
- [ ] Killing any team-system process, or restarting any homeserver, is recovered automatically.
  Acceptance proves each case on the host.
- [ ] A host reboot brings every team back with no human step.
- [ ] From `ccy-teams` on the host, the owner winds a whole team down and has it shut down
  inside the chosen window, then resumes it, coordinator first, as it was at that shutdown;
  an offline member is shown as offline and blocks neither.

## Delivery & Milestones

- Plan created; audit and design dispatched.
