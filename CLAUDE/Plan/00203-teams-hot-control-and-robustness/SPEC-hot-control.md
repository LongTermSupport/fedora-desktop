# SPEC: hot control of a running session's teams, seats and roles

Plan 00203, Task 1.2. The owner: "i would like to be able to "hot" control ccy sessions - add
them, remove them, update them into teams, set roles etc all whilst the ccy session is running".

The facts this spec rests on, with file and line references, and the options weighed, are in
[subagent-reports/261016-hot-control-design-opus.md](subagent-reports/261016-hot-control-design-opus.md).
That report also covers the earlier unmerged design on `feat/ccy-teams-hot-add` (Plan 00177
Phase 8, Plan 00194 Phase 3). This spec replaces that design: its join marker lives in the
checkout, which the container can write, so the container could forge it.

## 1. What is frozen at launch today

Every item here is fixed when the container or the session starts, and none can change while
it runs.

| #   | Frozen value                                                                                                                                     | Set by                                                                             | Read by                                                                                                          |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| F1  | `PINGBUS_SEATS` (container `-e`)                                                                                                                 | launcher, `ccy_seats_take` (`lib/agent-bus-seats.bash`)                            | `pingbus seat exec` (`seat.claim`)                                                                               |
| F2  | `PINGBUS_TEAMS`, `PINGBUS_HOME` in claude's environment                                                                                          | `seat.claim`, before `execvpe` into claude                                         | every hook, the status line, the watcher and every `pingbus` command, through `config.active_teams` on each call |
| F3  | `--label ccy-seats=<list>` (podman labels cannot change)                                                                                         | launcher                                                                           | people, through `podman ps`. No code reads it                                                                    |
| F4  | `--plugin-dir` and `--settings` (the pingbus plugin and its status line), and `pingbus` on PATH                                                  | entrypoint AGENT-BUS block, **only when `PINGBUS_SEATS` is set**                   | Claude Code, which loads them once, at start                                                                     |
| F5  | the seat locks, inheritable fds of claude's process tree                                                                                         | `seat.claim`                                                                       | host `agent-bus seat list` (an flock probe). The watcher does not hold them: it is spawned with `close_fds=True` |
| F6  | the session home `/tmp/pingbus-home`, one symlink per seat                                                                                       | `seat.claim._make_home`; a second claim is refused                                 | `config.load_bundle`, `seat.session_seats`                                                                       |
| F7  | the watcher's member list and its per-team threads. A failed team is dropped for good, and the watcher exits when none are left                  | `cmd_watch`, at start                                                              | the watcher                                                                                                      |
| F8  | the status line's record `/tmp/pingbus-sessions-<uid>/<session_id>`                                                                              | SessionStart hook                                                                  | `pingbus status-line`, every second                                                                              |
| F9  | the teams, seats, roles and descriptions in the session's context                                                                                | SessionStart hook                                                                  | the model                                                                                                        |
| F10 | the restart arguments. A reboot restore replays the registry record's `--teams`; an age restart replays the launcher's in-memory `CCY_ORIG_ARGS` | launcher, `ccy-teams record-seats` (which records only for the session it runs in) | `ccy-sessions restore`, `ccy_handle_restart_exit`                                                                |
| F11 | cc: `env PINGBUS_SEATS=...`, the plugin only with `--teams`, and `--append-system-prompt "You are <handle> in <team>..."`                        | `cc-teams.bash`                                                                    | Claude Code, which reads them once, at start                                                                     |

Two things are **not** frozen:

- **The role.** It lives in the admin-sent team record, the room state `agent_bus.team`
  `roles`, and `sudo agent-bus set-role` changes it. The watcher's sync refreshes the cached
  `team.json`, which the hooks read on every call. Only the context lines are frozen (F9).
- **The bus route.** A podman ccy container reaches the team's homeserver at the host's
  `agentbus0` address through pasta, its usual route. The `podman` member type needs nothing
  in `allow_from`, and no podman network is involved.

## 2. Decisions

- **D1. The source of truth is a host-written membership file, mounted read-only.**

  - Every ccy container gets one new read-only bind mount of a host directory that only the
    owner's tools write, `$XDG_RUNTIME_DIR/ccy/membership/<session>/`, at
    `/run/ccy/membership`.
  - It is a directory mount, not a file mount. A file mount pins the inode, so the container
    would never see an atomic replace on the host. A directory mount shows every rename made
    inside it.
  - No existing mount works:
    - `/workspace` is writable by the container, so the seat directory under `.claude/ccy`
      is forgeable.
    - The other read-only mounts are single files.
  - So hot control needs one launch-time change. After that, every change is live.

- **D2. The container may not write its own membership.** The mount is `:ro`, and the
  container has no `CAP_SYS_ADMIN` with which to remount it (docs/ccy.md: no added
  capabilities).

  - What the session applies is written into its own session home. It is never trusted on its
    own: every reader checks that it is a subset of the read-only file (H8).
  - Signed entries were rejected. Helpers are stdlib-only, and the stdlib has no asymmetric
    signature.

- **D3. The role stays where it is**, in the admin-sent team record, set by
  `sudo agent-bus set-role`. The membership file carries no role, so there are never two
  truths.

  - A session cannot raise its own role. Only the team's `admin` sends the record, and pingbus
    already refuses a record from anyone else.
  - No membership change is ever taken from a bus message.

- **D4. The seat keeper.** A small in-container process, `pingbus seat keep`, is forked by
  `seat exec` before the exec into claude. It:

  - reconciles the read-only file against the seat locks and the session home links;
  - holds a hot-added seat's lock;
  - releases a removed seat. `flock(LOCK_UN)` on its dup of a launch seat's inherited open
    file releases that lock for every holder.

  It exits when claude exits, which it sees as its parent changing.

- **D5. Hot control needs a session launched with the bus loaded.** Claude Code loads a plugin
  only at start (F4). From the version that ships M2:

  - every ccy session is launched with the pingbus plugin, the settings, the keeper and the
    mount, even with no team. With no team it does nothing, and its hooks exit in the shell
    without starting Python.
  - A session launched before that version, or with `--no-bus` (H33), is refused, naming the
    relaunch.
  - When Plan 00202 M3 moves the pingbus hooks into the `ccy` mod, which every ccy session
    loads, the mod takes over this duty.

- **D6. This is a control, not a security boundary inside a checkout.** Any session in a
  checkout can already read every seat's bundle and token in that checkout's
  `.claude/ccy/pingbus/seats/`.

  - Hot control decides what the session's own pingbus does, and what the host shows and
    allocates.
  - It does not stop a hostile agent in the same checkout from using a sibling seat's token.
    Separate checkouts, or LXC members, do that.
  - This was already true before hot control. The spec does not make it worse.

## 3. Data model

### 3.1 The desired membership (host-written, read-only in the session)

`$XDG_RUNTIME_DIR/ccy/membership/<session>/membership.json`.

- The directory is mode 0700 and the file mode 0600, owned by the desktop user.
- The file is written by `ccy-teams` only, atomically: a temporary file in the same
  directory, then a rename.
- `<session>` is the session's registry name, the name ccy-sessions shows.
- The directory is on a tmpfs, so it goes at reboot. That is correct: a restore relaunches,
  and the launch writes it again from the record (H5).

```json
{"v": 1,
 "session": "<session>",
 "kind": "ccy",
 "generation": 7,
 "written_ms": 1760000000000,
 "seats": [{"team": "<team>", "seat": "<seat>"}],
 "farewell": [{"team": "<team>", "seat": "<seat>", "reason": "removed"}]}
```

- `generation` rises by one on each write. Equal generations are equal content.
- `seats`: at most one per team, under `seat.parse_seat_list`'s grammar. The order is the
  `--teams` order.
- `farewell` lists the seats removed in this generation, so the session can tell "removed by
  the owner" apart from "never had". It is cleared on the next write.
- Unknown keys, or a `v` other than 1, are refused (exit 78).

### 3.2 The applied membership (keeper-written, private to the session)

`$PINGBUS_HOME/applied.json` (`/tmp/pingbus-home` in ccy).

```json
{"v": 1, "generation": 7,
 "seats": [{"team": "<team>", "seat": "<seat>", "handle": "<handle>", "state": "held"}],
 "refused": [{"team": "<team>", "seat": "<seat>", "why": "<class>"}]}
```

- `why` is one of a closed set: `held-elsewhere`, `no-bundle`, `bundle-invalid`, `not-desired`.
- The active teams of a command are the `held` seats whose `(team, seat)` is also in the
  desired file (H8).

### 3.3 The host-visible report (keeper-written, never authority)

`/workspace/.claude/ccy/pingbus/sessions/<session>/applied.json`. It has the same schema as
3.2. ccy-teams reads it to report per step what the session did. Its truth is checked against
the seat locks, which the host probes itself.

## 4. Requirements

### Source of truth and authority

- **H1.** The launcher creates the membership directory for every ccy launch with a registry
  name, and mounts it read-only at `/run/ccy/membership`. It writes generation 1 from the
  canonical `--teams` list, with empty `seats` when there is none.
  - It does this through `ccy-teams membership init <session> <list>`, so no bash grows.
  - The run gets a label `ccy-session=<session>`, so the host can map a session to its
    container.
  - Test: `scripts/test-ccy-teams.bash` sees the `-v ...:/run/ccy/membership:ro` and the
    label.
- **H2.** `PINGBUS_SEATS` and the `ccy-seats` label are retired.
  - The entrypoint refuses `PINGBUS_SEATS` from a launcher. It also refuses a bus launch with
    no `/run/ccy/membership`, saying "ccy on the host is older than this image", as it does for
    `PINGBUS_TEAMS` today.
  - `PINGBUS_MEMBERSHIP=/run/ccy/membership` is set by the entrypoint, and joins D62's set
    that `ccy.env` and `ccy.env.local` may not touch.
- **H3.** Inside the container, a write to `/run/ccy/membership` fails with EROFS, and so does
  a remount. Acceptance proves both with `podman exec`.
- **H4.** pingbus never acts on a team that is not in the desired file, whatever
  `applied.json` says. Test: a forged `applied.json` naming an extra seat gives "not a member"
  (exit 64) on `send --team`, and no watcher thread for it.
- **H5.** Every hot change writes the session's registry record (`--teams`) **before** the
  membership file. So the record is the persistent truth and the membership file is its live
  copy.
  - A reboot restore and an age restart both replay the record. The age restart reads
    `--teams` from the record (it replaced Plan 00177 Task 8.6), through a ccy-teams filter, so
    no ratchet file grows.
- **H6.** No membership or role change is ever taken from the bus, from a hook's input or from
  any file the container can write. Test: a pingbus unit test feeds each of these and sees no
  change.
- **H7.** The role is changed only through `sudo agent-bus set-role` on the host. A session's
  pingbus has no command that changes a role.

### The session follows

- **H8.** `config.active_teams` reads `PINGBUS_MEMBERSHIP` when it is set, on every call:
  applied ∩ desired, in desired order. `PINGBUS_TEAMS` is read only when `PINGBUS_MEMBERSHIP`
  is unset, for `agent-bus-claude` members (LXC, VM, other hosts). Unit tests cover both,
  every refusal, and the empty membership.
- **H9.** The keeper reconciles within 2 s of a change to the desired file, by polling its
  `mtime` and `generation`.
  - For each added seat: the seat directory and bundle are checked as `_check_seat_dir` does,
    the lock is taken without waiting, and the home link is made atomically (a temporary link,
    then a rename).
  - For each removed seat: the link goes and the lock is released.
  - Then `applied.json` and the report (3.3) are written.
  - A refusal is recorded with its class and does not stop the other seats.
  - Test-first with a fake clock, lock and home.
- **H10.** The keeper exits within 2 s of claude exiting, releasing every lock. A dead keeper
  is reported:
  - the hooks see `keeper.lock` free while the session runs, and say "agent-bus: the seat
    keeper has stopped; the owner can repair it from ccy-teams";
  - ccy-teams shows the session as `unguarded` and offers Repair, which starts
    `podman exec -d <container> pingbus seat keep --parent <claude pid>`.
- **H11.** The watcher runs while the session's socket exists, even with no team. Each tick it
  starts a team's poll thread for each team that became active, and stops and releases the
  sync lock of each team that went.
  - The SessionStart hook starts it in every bus session. The prompt and Stop hooks start it
    again if no watcher holds `watch.lock`.
  - A team that fails for a transient reason (connection refused, a timeout, a 5xx) is retried
    with capped backoff, and is never dropped for good. This is the same per-team lifecycle as
    fix/watcher-survives-homeserver-restart (section 7).
- **H12.** The session is told in its own conversation. On each new applied generation the
  watcher writes one **membership notice** through the inbox socket, as a delivery envelope of
  kind `MEMBERSHIP`. It is a fixed template, filled only with names that passed their grammar:
  - for each team joined: the seat, the handle, the role and the team's description line;
  - for each team left: the reason (`removed by the owner`);
  - the full list of current teams;
  - for a role change seen in a synced record: the new role, using the existing `role_*`
    templates.
- **H13.** If the notice cannot be delivered (relay mode, socket stalled, delivery missing),
  the next `UserPromptSubmit` hook adds the same lines as context. Either path records the
  generation it told in `$PINGBUS_HOME/told.json`, so the session is told once per generation.
  Tests: a delivered notice is not repeated by the hook, and a failed one is.
- **H14.** The status line shows the current teams within 2 s. It reads `applied.json` ∩
  desired on each render, not the SessionStart record (F8). The SessionStart record stays for
  `agent-bus-claude` members only.
- **H15.** Mid-turn:
  - The keeper and the watcher act at once, whatever the model is doing.
  - The notice is queued by Claude Code's socket and shown when the turn allows, as for any
    delivery.
  - A `pingbus` command that already loaded a bundle finishes (bounded by its request timeout).
    The next command against a removed team is refused with exit 64: "not a member of `<team>`
    (removed by the owner)".
- **H16.** The goodbye of a removed seat is never sent twice. The SessionEnd goodbye covers
  only seats still applied.

### Host commands

- **H17.** New non-interactive subcommands. Each takes `--yes` to skip its one confirmation,
  which is required with no terminal:

  - `ccy-teams session list`
  - `ccy-teams session add <session> <team>[/<seat>] [--role=worker|coordinator]`
  - `ccy-teams session remove <session> <team> [--coordinator=demote|hand-to=<seat>]`
  - `ccy-teams session move <session> <from-team> <to-team>[/<seat>]`
  - `ccy-teams session role <session> <team> worker|coordinator [--demote-current]`
  - `ccy-teams session repair <session>`
  - `ccy-teams membership init <session> <list>` (the launcher's)

  Exit codes:

  | Exit | Meaning                                                    |
  | ---- | ---------------------------------------------------------- |
  | 0    | done                                                       |
  | 1    | a step failed (the report says which)                      |
  | 64   | usage                                                      |
  | 75   | a seat is held elsewhere, or another change is in progress |
  | 78   | the session cannot be hot-controlled (D5)                  |

- **H18.** Each command prints one line per step on stderr: `✓`, `✗` or `-` (skipped,
  already so), then the step. The last line is the outcome. stdout carries nothing, except
  `session list`'s table, or `SESSION` lines when stdout is not a terminal.

- **H19.** Idempotent. Re-running a command whose result already holds changes nothing,
  prints every step as `-`, and exits 0. Re-running after a partial failure finishes the
  remaining steps. A goodbye still owed is kept in the host's state and sent on the re-run.

- **H20.** One change at a time per session, and one allocation at a time per checkout.

  - A session change takes `flock` on `<membership dir>/.change.lock`.
  - A seat allocation takes `flock` on `.claude/ccy/pingbus/seats/.alloc.lock`.
  - Either one busy gives exit 75, naming who holds it.

- **H21.** Allocation authority. A seat is free only if its lock is free **and** no live
  session's desired file names it. `agent-bus seat take` and `seat choose` both apply this.
  Test: a seat named in another session's desired file whose keeper has not locked it yet is
  refused.

- **H22. Add.** The steps, in order:

  1. Resolve the session to its container (`ccy-session` label). Check that it is running and
     hot-controllable (D5).
  2. Check the team: declared, homeserver up (`agent-bus-hs@<team>` active), roster under
     64 agents (the `roles` limit, protocol §8).
  3. Choose or validate the seat (H21).
  4. Create the seat if new: `agent-bus seat take` (sudo, asked first).
  5. Probe the route from inside the container: `podman exec <c> pingbus reach --team <team>`,
     a versions GET with a 3 s limit.
  6. Set the role if asked (H25).
  7. Write the record (H5), then the desired file.
  8. Wait up to 10 s for the seat lock to read held and the report to show `held`.
  9. Report what the session will be told.

  A failure at steps 1 to 6 changes nothing. A failure at 8 restores the record and the
  desired file, and reports the keeper's `why`.

- **H23. Remove.** The steps, in order:

  1. Resolve the session as for add.
  2. If the seat holds the coordinator role, `--coordinator` is required: `demote` sets it to
     worker; `hand-to=<seat>` makes another seat coordinator. Interactively it is asked, with
     demote as the default. Non-interactively its absence is exit 64.
  3. Write the record, then the desired file without the seat, with a `farewell` entry.
  4. Wait up to 10 s for the seat lock to read free.
  5. Post the goodbye from the host, using the seat's bundle, as the seat's member:
     `<handle> left the team: removed by the owner`. It is addressed to the team's humans and
     coordinator(s), like the session-end goodbye.

  The seat and its bundle stay in the checkout, free for a later session. `--park` also parks
  the handle (`agent-bus seat remove`).

- **H24. Move.** Add to the new team (H22), then remove from the old one (H23). If the add
  fails, the old team is untouched. If the remove fails after the add, the report says the
  session is in both teams and names the re-run. A move within one team (seat to seat) is
  refused: remove, then add.

- **H25. Role.** Runs `sudo agent-bus set-role` for the seat's handle.

  - Making a seat coordinator while another holds the role needs `--demote-current`.
    Interactively it is asked, naming the current coordinator. The current one is demoted
    first, as `seats.ChangeRole` does.
  - Every session on the team whose role changed gets the notice (H12) from its own watcher's
    next sync. The command waits up to 10 s for the target session's report to show the new
    generation of told roles, and reports if it did not.

- **H26. Homeserver down.** Add and role stop at step 2 with "homeserver for `<team>` is down:
  `systemctl status agent-bus-hs@<team>`" and change nothing.

  - Remove still releases the seat (the change is local) and exits 1 with "goodbye not sent:
    homeserver down". The goodbye stays owed (H19).
  - In the session, the watcher keeps the team and retries it (H11). The status line shows
    `bus: <team>(down)`.

- **H27. Seat limits.** A roster at 64 agents refuses add (exit 1). It names the limit and
  offers `ccy-teams` Ctrl-X (park or remove a member). A second seat in a team the session is
  already in is refused (one seat per team per session). The same seat again is H19's no-op.

- **H28. The session exits during a change.** The container stopping is seen at the wait step.

  - Add restores the record and the desired file, and reports "session ended during the add;
    seat `<s>` left free". A created seat is kept.
  - Remove reports done: the lock went with the session.
  - Neither leaves a desired file for a session that is not running. The launcher writes it
    again on the next launch.

- **H29. A team removed while sessions are on it.** `ccy-teams remove <team>` lists every live
  session in the team (desired files and held seats). It offers to remove them from the team
  first, which is H23 for each, and continues only once none is left. `--yes` does that without
  asking. A session whose homeserver vanished anyway keeps the team as `down` until the owner
  removes it (H26).

- **H30. Bus route.**

  - Podman ccy: no network change, and nothing in `allow_from`. A failed probe (H22 step 5)
    stops the add, naming the base URL and the probe's error.
  - A ccy session on the docker engine reaches the bus through the docker bridge. A failed
    probe there names the docker subnet that the team's `allow_from` needs, and the
    `ccy-teams apply` that adds it (a homeserver restart).
  - `podman network connect` works on a running rootless container (ccy's
    `network-management.bash` already uses it). It is not used, because no podman network
    carries the bus.

### cc host sessions

- **H31.** `cc --teams` sessions are hot-controllable on the same model:

  - the membership directory is `$XDG_RUNTIME_DIR/pingbus/membership/cc-<id>/`;
  - `seat exec --root --home` forks the keeper there too;
  - `PINGBUS_MEMBERSHIP` points at it.

  Exactly why it is weaker: the session runs as the owner's own uid, so it **can** write the
  file. D2 cannot hold on the host. This is the same stance cc already takes towards Element
  ("guidance, not a wall"), and the usage text says so.

- **H32.** cc changes:

  - `--append-system-prompt` names no team. It says: "Your agent-bus teams and seats are given
    in the session context and may change while you run; the latest agent-bus membership
    notice is current."
  - A cc session launched without `--teams` is not hot-controllable (the plugin is not
    loaded). `cc --bus` starts one with an empty membership, after the same Element consent.

### Inert cost and opting out

- **H33.** With an empty membership, each hook exits in its `hooks.json` shell guard without
  starting Python. The status line adds nothing. Test: the plugin contract test runs each hook
  with an empty `applied.json` and sees no `python3` started.
  - `ccy --no-bus` launches without the plugin, the keeper or the mount. Such a session is
    refused hot control with exit 78.

## 5. UX

### 5.1 The picker: a Running sessions view

`ccy-teams`, then Tab, switches the list between Teams and Running sessions.

```
 ccy-teams — Running sessions                                     Tab: Teams   ?: keys
 SESSION              CHECKOUT        TEAMS (seat, role)                     STATE
 ccy-<project-a>-1    <repo-a>        alpha (2, coordinator) · beta (1, worker)  ok
 ccy-<project-a>-2    <repo-a>        -                                       ok
 ccy-<project-b>-1    <repo-b>        alpha (5, worker)                       alpha down
 cc-<id>              <repo-c>        gamma (1, worker)                       ok (cc)
 ccy-<project-c>-1    <repo-c>        cannot be hot-controlled: relaunch       old
 > Enter: menu   Ctrl-A add to team   Ctrl-X remove from team   Ctrl-O role   Esc: back
```

### 5.2 The session menu, then the add flow

```
 ccy-<project-a>-2 — what to do
   Add to a team...                 Ctrl-A
   Attach                           Enter
   Repair the seat keeper           (only when unguarded)
 ─────────────────────────────────────────
 Add ccy-<project-a>-2 to which team?
   alpha   "<description>"   up    12 agents
   beta    "<description>"   up     3 agents
   gamma   "<description>"   down   (cannot add: homeserver down)
 Seat in alpha:   3 (free)  ·  6 (new)            [lowest free]
 Role:            worker  ·  coordinator (alpha's is <handle>; it becomes a worker)
 ─────────────────────────────────────────
 Add ccy-<project-a>-2 to alpha as seat 3 (<repo-a>.3+<host>.podman), worker? [y/N]
 ✓ session running, can be hot-controlled
 ✓ team alpha: homeserver up, 12 of 64 agents
 ✓ seat 3: free, not named by any session
 - seat 3: exists (no sudo needed)
 ✓ route: the container reaches alpha's homeserver
 - role: worker already
 ✓ record: --teams 2@beta,3@alpha
 ✓ membership: generation 4 written
 ✓ session: seat 3 held, applied (1.2 s)
 Done. The session is told: "you joined alpha as <repo-a>.3+<host>.podman (worker)". Nothing restarted.
```

### 5.3 The remove flow, with the coordinator

```
 Remove ccy-<project-a>-1 from alpha?
 Its seat 2 is alpha's coordinator. After it leaves:
   > the role goes to no one (seat 2 becomes a worker)
     hand it to seat 5 (<repo-b>.5+<host>.podman, held by ccy-<project-b>-1)
 ✓ role: seat 2 set to worker
 ✓ record: --teams 1@beta
 ✓ membership: generation 5 written (farewell alpha/2)
 ✓ session: seat 2 released (0.8 s)
 ✗ goodbye: not sent: homeserver for alpha did not answer in 4 s
 Removed, but the goodbye is still owed: run the same command again to send it.   (exit 1)
```

### 5.4 Non-interactive equivalents

```
 ccy-teams session add ccy-<project-a>-2 alpha/3 --role=worker --yes
 ccy-teams session remove ccy-<project-a>-1 alpha --coordinator=hand-to=5 --yes
 ccy-teams session move ccy-<project-b>-1 alpha beta --yes
 ccy-teams session role ccy-<project-b>-1 alpha coordinator --demote-current --yes
 ccy-teams session list            # SESSION<TAB>name<TAB>kind<TAB>team/seat/role,...<TAB>state
```

## 6. Milestones

Each milestone is shippable on its own and built test-first. Red and green are recorded in the
plan journal. Each ends with Fable review, qa-reviewer, then the owner's acceptance step on the
host through `meta-deploy.bash`.

| M   | Builds                                                                                                                                                                                                                                 | Shippable because                                                  | Acceptance on the host                                                                                                                                                                                                                                                                      |
| --- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| M0  | The watcher's per-team lifecycle: start, stop, transient retry with backoff (fix/watcher-survives-homeserver-restart, built to H11's lifecycle)                                                                                        | fixes a live bug on its own                                        | restart a team's homeserver while a session is idle: the next message still arrives, with no relaunch                                                                                                                                                                                       |
| M1  | pingbus: the 3.1 and 3.2 parsers, `active_teams` from `PINGBUS_MEMBERSHIP` (H4, H8), `pingbus seat keep` (H9, H10), `pingbus reach`                                                                                                    | inert until a launcher sets `PINGBUS_MEMBERSHIP`; unit tests only  | `./scripts/qa-helper-tests.bash` green in a host run of meta-deploy's QA step                                                                                                                                                                                                               |
| M2  | Launcher and entrypoint: the membership dir, the `:ro` mount, the `ccy-session` label, `membership init`, the plugin and keeper in every session, PINGBUS_SEATS retired, `--no-bus` (H1, H2, H3, H33); CCY and container version bumps | `ccy --teams` behaves as before; plain sessions carry an inert bus | (a) `ccy --teams 1@<team>` works as before; (b) a plain `ccy` shows no bus segment, and `podman exec <c> touch /run/ccy/membership/x` fails with EROFS; (c) a plain ccy prompt starts no `python3` (H33)                                                                                    |
| M3  | The watcher's live add and drop (H11), the membership notice and its hook fallback (H12, H13), the status line (H14), mid-turn behaviour (H15, H16), and `ccy-teams session add` and `remove` with no role options                     | the first end-to-end hot add and remove                            | on a plain running session: `ccy-teams session add <s> <team> --yes`. The session's conversation shows the notice, the status line shows the team, and a message from Element reaches it. Then `session remove`: the notice, the goodbye in Element, the seat free in `agent-bus seat list` |
| M4  | Role and move (H23 coordinator options, H24, H25), the allocation authority (H21), locking (H20), idempotence and owed goodbyes (H19), every edge case (H26 to H30), the age restart reading the record (H5)                           | the full non-interactive surface                                   | the script in `ACCEPTANCE-owner.md`: re-role, move, remove the coordinator, stop a homeserver mid-add, `ccy stop` mid-add, re-run each command to see the no-op                                                                                                                             |
| M5  | The picker's Running sessions view and its menus (5.1 to 5.3), and Repair                                                                                                                                                              | the interactive surface                                            | the same four operations from the picker, on a terminal                                                                                                                                                                                                                                     |
| M6  | cc: H31, H32, `cc --bus`                                                                                                                                                                                                               | cc reaches parity, with its documented weaker authority            | `cc --teams <team>`, then `ccy-teams session move cc-<id> ...` from another terminal                                                                                                                                                                                                        |
| M7  | Docs: docs/agent-bus.md (seats, hot control), docs/ccy.md (the mount, the label, `--no-bus`), the usage text, the protocol §12 additions (`PINGBUS_MEMBERSHIP`, `applied.json`, the `MEMBERSHIP` envelope kind)                        | documentation                                                      | qa-reviewer, then the owner reads it                                                                                                                                                                                                                                                        |

## 7. Interaction with work in flight

- **fix/watcher-survives-homeserver-restart** (no commits yet). Its retry-with-backoff is
  H11's per-team lifecycle.
  - Build it as M0, with a per-team runner that can start and stop a team. A failed team
    changes state (`up`, `down`, `failed`); it is never removed from the list.
  - M3 then adds only the driver that asks the runner to start or stop teams from
    `applied.json`.
  - Building the fix as "restart the whole watcher" instead would have to be undone by M3.
- **feat/teams-shutdown** (one commit, `ccy-teams shutdown <team>`, SPEC-shutdown SD1-SD16).
  - Its list of a team's live sessions should come from one shared session-discovery function:
    desired files, held seats, records. H22, H29, the board and shutdown all use it, so they
    cannot disagree.
  - After M3, shutdown's "held seat no record names" case shrinks, because every hot seat is
    named by a desired file.
  - H29 gives `ccy-teams remove <team>` a gentler first offer than shutdown: take the sessions
    off the team and keep them running.
  - Merge order: shutdown first. M4 then moves its discovery into the shared function.
- **Plan 00202, the `ccy` mod.**
  - The mod's team roster band (T1) and `/team who` read `$PINGBUS_HOME/applied.json` ∩ the
    desired file, and the cached `team.json` roles. They never read `PINGBUS_TEAMS`.
  - The schemas in 3.1 and 3.2 are versioned (`v`). Fields are only ever added. The mod
    writes neither file.
  - The mod shows the membership notice's facts in its band, not as model text (00202 forbids
    peer text in `{ text }`; the notice is a fixed template and goes to the model through the
    socket as H12 says).
  - D5's always-loaded duty moves to the mod when 00202 M3 absorbs the pingbus hooks. If 00202
    M3 lands before M2 here, M2 loads nothing extra and relies on the mod.
- **Plan 00177 Phase 8 and Plan 00194 Phase 3** (`feat/ccy-teams-hot-add`, design only, not
  merged).
  - Superseded by this spec. Its join marker in the checkout is replaced by D1's read-only
    mount; its `pingbus join` by the keeper; its "Running sessions" view by M5.
  - Its supervisor-plugin carrier is not needed: delivery stays the inbox socket.
  - Those tasks should be closed with a pointer here when Phase 2 of this plan is written.
