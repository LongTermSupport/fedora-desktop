# SPEC: hot control of a running session's teams, seats and roles

Plan 00203, Task 1.2. The owner: "i would like to be able to "hot" control ccy sessions - add
them, remove them, update them into teams, set roles etc all whilst the ccy session is running".

The facts this spec rests on, with file and line references, and the options weighed, are in
[subagent-reports/261016-hot-control-design-opus.md](subagent-reports/261016-hot-control-design-opus.md).
That report also covers the earlier unmerged design on `feat/ccy-teams-hot-add` (Plan 00177
Phase 8, Plan 00194 Phase 3). This spec replaces that design: its join marker lives in the
checkout, which the container can write, so the container could forge it.

**Revision 2** answers the Fable review,
[subagent-reports/261016-hot-control-spec-review-fable.md](subagent-reports/261016-hot-control-spec-review-fable.md)
(REVISE). The changes:

- the keeper is the only holder of every seat lock (D4);
- an inert contract for plain sessions (H33, H34);
- the checkout is part of a seat's identity (3.1, H21);
- the SELinux relabel on the membership mount (H1);
- the `PINGBUS_MEMBERSHIP` marker (H2);
- re-baseline on removal, and a farewell that is never lost (H23, 3.1);
- the milestone order (section 6);
- who can be hot-controlled at all (H35).

The section numbers of revision 1 are kept. The four owner decisions are in section 8.

## 1. What is frozen at launch today

Every item here is fixed when the container or the session starts, and none can change while
it runs.

| #   | Frozen value                                                                                                                                     | Set by                                                                             | Read by                                                                                                          |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| F1  | `PINGBUS_SEATS` (container `-e`)                                                                                                                 | launcher, `ccy_seats_take` (`lib/agent-bus-seats.bash`)                            | `pingbus seat exec` (`seat.claim`)                                                                               |
| F2  | `PINGBUS_TEAMS`, `PINGBUS_HOME` in claude's environment                                                                                          | `seat.claim`, before `execvpe` into claude                                         | every hook, the status line, the watcher and every `pingbus` command, through `config.active_teams` on each call |
| F3  | `--label ccy-seats=<list>` (podman labels cannot change)                                                                                         | launcher                                                                           | people, through `podman ps`. No code reads it                                                                    |
| F4  | `--plugin-dir` and `--settings` (the pingbus plugin, its status line, `crossSessionInbound: accept`), and `pingbus` on PATH                      | entrypoint AGENT-BUS block, **only when `PINGBUS_SEATS` is set**                   | Claude Code, which loads them once, at start                                                                     |
| F5  | the seat locks, inheritable fds of claude's process tree. `Lock.release` truncates and closes, which does not release a shared flock             | `seat.claim`                                                                       | host `agent-bus seat list` (an flock probe). The watcher does not hold them: it is spawned with `close_fds=True` |
| F6  | the session home `/tmp/pingbus-home`, one symlink per seat                                                                                       | `seat.claim._make_home`; a second claim is refused                                 | `config.load_bundle`, `seat.session_seats`                                                                       |
| F7  | the watcher's member list and its per-team threads                                                                                               | `cmd_watch`, at start                                                              | the watcher                                                                                                      |
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
  - Every named ccy launch (H35) gets one new read-only bind mount of a host directory that
    only the owner's tools write, `$XDG_RUNTIME_DIR/ccy/membership/<session>/`, at
    `/run/ccy/membership`. It is relabelled for SELinux (H1).
  - It is a directory mount, not a file mount. A file mount pins the inode, so the container
    would never see an atomic replace on the host. A directory mount shows every rename made
    inside it.
  - No existing mount works:
    - `/workspace` is writable by the container, so the seat directory under `.claude/ccy`
      is forgeable.
    - The other read-only mounts are single files.
  - So hot control needs one launch-time change. After that, every change is live.
- **D2. The container may not write its own membership.** The mount is `:ro`. It is made from
  a more privileged mount namespace, so it carries `MNT_LOCK_READONLY`, and the container has
  no added capabilities anyway.
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
- **D4. The seat keeper is the only lock holder, for every seat.**
  - `pingbus seat exec` forks the keeper, `pingbus seat keep`, before it execs into claude.
  - The keeper takes every seat lock, the launch seats included. Each lock is close-on-exec,
    so nothing is inherited and claude's tree holds none.
  - It also:
    - makes the session home, even with no seat;
    - keeps the flag file (H34; the session's settings are fixed at launch, per the M2 spike);
    - reconciles the read-only file against its locks and the home links;
    - starts the watcher when the first seat is applied (H11).
  - Release is an explicit `flock(LOCK_UN)` before the close. Today's `Lock.release` truncates
    and closes, which is not enough once a lock could be shared.
  - Its end:
    - In a container, the keeper's parent after the exec chain is PID 1 for the container's
      whole life, so the keeper ends with the container.
    - On the host (cc), it ends when its parent changes.
  - A keeper that dies drops every seat. That is visible (H10) and repairable: Repair re-takes
    them all. Allocation stays safe, because H21 refuses a seat that a desired file names.
  - This replaces revision 1's split between inherited launch locks and keeper-held hot locks.
    Under that split a repaired keeper could not release a launch seat. It also closes audit
    gap 7 (a `setsid` child of a `cc --teams` claude kept the seat for good) and absorbs the
    audit's F5.
- **D5. Hot control needs a session launched with the bus kit.** Claude Code loads a plugin
  and its settings only at start (F4).
  - From the version that ships M2, every named ccy session (H35) is launched with the
    pingbus plugin, the session's settings, the keeper and the mount, even with no team.
  - The kit is inert in a session on no team (H33, H34).
  - `--no-bus` opts out. Its sessions, and those launched before M2, are refused with exit 78,
    naming the relaunch.
  - When Plan 00202 M3 moves the pingbus hooks into the `ccy` mod, the mod takes over this
    duty, including the inert contract (section 7).
- **D6. This is a control, not a security boundary inside a checkout.** Any session in a
  checkout can already read every seat's bundle and token in that checkout's
  `.claude/ccy/pingbus/seats/`.
  - **A hot remove does not revoke the token.** A removed session's agent could still post
    with that seat's token by calling the Matrix API directly.
  - Remove is a control over the session's own pingbus, and over what the host shows and
    allocates. It does not stop a hostile agent in the same checkout. Separate checkouts, or
    LXC members, do that.
  - A `--revoke` (rotate the token on remove) is deferred until audit F7 makes `rotate-token`
    refuse a held seat.

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
 "checkout": "<absolute host path of the checkout>",
 "generation": 7,
 "written_ms": 1760000000000,
 "seats": [{"team": "<team>", "seat": "<seat>"}],
 "farewell": [{"team": "<team>", "seat": "<seat>", "reason": "removed"}]}
```

- **A seat's identity is `(checkout, team, seat)`.** `1@alpha` exists once per checkout, with a
  different handle in each. `checkout` is the registry record's `Dir`.
- `generation` rises by one on each write. Equal generations are equal content.
- The host compares a generation **only for equality** with what it wrote, never with `>=`. A
  relaunch writes generation 1 again.
- `seats`: at most one per team, under `seat.parse_seat_list`'s grammar. The order is the
  `--teams` order.
- **`farewell` gives the reason only.** The keeper derives removals from the diff between
  applied and desired. A removal with no farewell entry reads as "removed by the owner".
- Farewell entries accumulate across writes. ccy-teams drops an entry only once that removal
  is complete on the host (seat free, re-baselined, goodbye posted; H23). Two removes in one
  keeper poll therefore lose neither reason.
- Unknown keys, or a `v` other than 1, are refused (exit 78).

### 3.2 The applied membership (keeper-written, private to the session)

`$PINGBUS_HOME/applied.json` (`/tmp/pingbus-home` in ccy).

```json
{"v": 1, "generation": 7,
 "seats": [{"team": "<team>", "seat": "<seat>", "handle": "<handle>", "state": "held"}],
 "refused": [{"team": "<team>", "seat": "<seat>", "why": "<class>", "holder": "<kind>"}],
 "told": {"generation": 7, "roles": [{"team": "<team>", "handle": "<handle>", "role": "worker"}]}}
```

- `why` is one of a closed set: `held-elsewhere`, `no-bundle`, `bundle-invalid`, `not-desired`.
- `holder` names the lock kind holding a `held-elsewhere` seat, as `claim` does today.
- `told` is written by whichever path told the session (H12, H13): the generation and the roles
  it said.
- The active teams of a command are the `held` seats whose `(team, seat)` is also in the
  desired file (H8).

### 3.3 The host-visible report (keeper-written, never authority)

`/workspace/.claude/ccy/pingbus/sessions/<session>/applied.json`. It has the same schema as
3.2.

- The keeper writes it at its start, so a relaunch resets any stale report.
- ccy-teams reads the seat lock first and the report second. The lock is truth; the report
  only explains it (`why`, `told`).

## 4. Requirements

### Source of truth and authority

- **H1.** For every launch with a registry name (H35), bus or not:

  - The run gets a label `ccy-session=<session>`. It is the host's only map from a session to
    its container. The session is the one the launcher IS: its tmux pane was started for it
    (the pane's first process is the launcher, or the registry's trampoline that runs it). A
    launcher run from a shell inside another session's pane, an agent's command there, inherits
    that pane's `TMUX` but is unnamed (found on the host: it took the outer session's name,
    label and membership directory).
  - For a bus launch (not `--no-bus`), the launcher creates the membership directory and
    mounts it `-v <dir>:/run/ccy/membership:ro${CCY_MOUNT_RELABEL:+,Z}`. The relabel is `Z`,
    as for the config import (`claude-yolo:2511`), because the directory is per session.
  - It writes generation 1 from the canonical `--teams` list, with empty `seats` when there is
    none. It does this through `ccy-teams internal membership-init <session> <checkout> <list>`, so no bash grows.
  - Test: `scripts/test-ccy-teams.bash` sees the mount with the relabel token, and the label.
  - As built in M2: a `--teams` launch with no registry name (a plain terminal, H35) still gets
    its seats through a membership directory, named after its container instead; it has no
    `ccy-session` label, so it is not hot-controllable. A plain launch with no name gets
    `PINGBUS_MEMBERSHIP=off`, as `--no-bus` does.

- **H2.** The launcher states the bus choice explicitly:
  `-e PINGBUS_MEMBERSHIP=/run/ccy/membership`, or `-e PINGBUS_MEMBERSHIP=off` for `--no-bus`.
  The entrypoint:

  - refuses `PINGBUS_MEMBERSHIP` unset: "ccy on the host is older than this image";
  - refuses a leftover `PINGBUS_SEATS`, so a mixed deploy fails fast rather than launching a
    teamless session;
  - refuses `/run/ccy/membership` missing when the marker names it;
  - adds `PINGBUS_MEMBERSHIP` to D62's set that `ccy.env` and `ccy.env.local` may not touch.

  `PINGBUS_SEATS` and the `ccy-seats` label are retired.

- **H3.** Inside the container, a write to `/run/ccy/membership` fails with EROFS, and so does
  a remount. Acceptance proves both with `podman exec`. It proves H1's relabel by labels, which
  hold whatever the host's SELinux mode: the bind asks for `Z`, and the mount's directory and file
  carry `container_file_t` at the container's own MCS level, not `user_tmp_t`. On an Enforcing
  host it also proves the container reads them; on a Permissive one (the owner's choice, Plan
  00118\) it says that enforcement was not exercised.

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

- **H8.** `config.active_teams` reads `PINGBUS_MEMBERSHIP` when it is set (and not `off`), on
  every call: applied ∩ desired, in desired order. `PINGBUS_TEAMS` is read only when
  `PINGBUS_MEMBERSHIP` is unset, for `agent-bus-claude` members (LXC, VM, other hosts). Unit
  tests cover both, every refusal, and the empty membership.

- **H9.** The keeper reconciles within 2 s of a change to the desired file, by polling its
  `mtime` and `generation`.

  - For each added seat: the seat directory and bundle are checked as `_check_seat_dir` does,
    the lock is taken without waiting, and the home link is made atomically (a temporary link,
    then a rename). A refusal names the holder's kind.
  - For each removed seat: the link goes and the lock is released with `LOCK_UN`.
  - Then `applied.json`, the report (3.3) and the flag file (H34) are written.
  - A refusal is recorded with its class and does not stop the other seats.
  - `seat exec` with an empty membership is valid: the keeper makes the home and holds nothing.
    Today an empty seat list is a `ConfigError`.
  - Test-first with a fake clock, lock and home.

- **H10.** A dead keeper is reported:

  - the hooks see `keeper.lock` free while the session runs, and say "agent-bus: the seat
    keeper has stopped; every seat is released; the owner can repair it from ccy-teams";
  - ccy-teams shows the session as `unguarded` and offers Repair. Repair runs
    `podman exec -d <container> pingbus seat keep`, with no parent option in a container. It
    re-takes every applied seat.

- **H11.** Only the keeper is resident in a session on no team.

  - The keeper starts the watcher when the first seat is applied. It uses the socket path and
    token that the SessionStart hook records in `$PINGBUS_HOME/socket.json` (mode 0600,
    container-private).
  - The watcher exits when the last team goes. The hooks' respawn (the M0 fix) stays as the
    backstop.
  - While it runs, the watcher reads `applied.json` each tick. It starts a poll thread for a
    team that became active, and stops the thread and releases the sync lock of a team that
    went, within 2 s.
  - A running `pingbus wait` does the same each loop: a dropped team's lock is released, and
    `wait` exits 0 when no team is left.
  - A team that fails for a transient reason is retried by M0's per-team backoff, never
    dropped for good.

- **H12.** The session is told in its own conversation. On each new applied generation the
  watcher writes one **membership notice** through the inbox socket, as a delivery envelope of
  kind `MEMBERSHIP`. It is a fixed template, filled only with names that passed their grammar:

  - for each team joined: the seat, the handle, the role and the team's description line;
  - for each team left: the farewell reason, by default "removed by the owner";
  - the full list of current teams;
  - for a role change seen in a synced record: the new role, using the existing `role_*`
    templates.

  The `MEMBERSHIP` kind is added to `envelope.KINDS` and protocol §15 in M3, with the code.

  - When the first seat is added, the keeper starts the watcher, which then sends the notice
    for that generation.
  - When the last seat is removed, the watcher sends the notice before it exits.

- **H13.** If the notice cannot be delivered (relay mode, socket stalled, delivery missing,
  inbound not yet accepted per H34), the next `UserPromptSubmit` hook adds the same lines as
  context. Either path records what it told in `applied.json`'s `told`, so the session is told
  once per generation. Tests: a delivered notice is not repeated by the hook, and a failed one
  is.

- **H14.** The status line shows the current teams within 2 s. It reads `applied.json` ∩
  desired on each render, not the SessionStart record (F8).

  - `statusline.py` imports no pingbus module, by design, so it holds its own copy of the
    intersection. A test pins that copy equal to `config.active_teams`, as its other copies
    are pinned.
  - The SessionStart record stays for `agent-bus-claude` members only.

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

  `ccy-teams internal membership-init` is the launcher's. It is listed in the usage text's
  internal group, beside `record-seats`, not with these.

  Exit codes:

  | Exit | Meaning                                                    |
  | ---- | ---------------------------------------------------------- |
  | 0    | done                                                       |
  | 1    | a step failed (the report says which)                      |
  | 64   | usage                                                      |
  | 75   | a seat is held elsewhere, or another change is in progress |
  | 78   | the session cannot be hot-controlled (D5, H35)             |

- **H18.** Each command prints one line per step on stderr: `✓`, `✗` or `-` (skipped,
  already so), then the step. The last line is the outcome. stdout carries nothing, except
  `session list`'s table, or `SESSION` lines when stdout is not a terminal.

- **H19.** Idempotent. Re-running a command whose result already holds changes nothing,
  prints every step as `-`, and exits 0. Re-running after a partial failure finishes the
  remaining steps. A goodbye or a re-baseline still owed stays as a farewell entry (3.1) and
  is done on the re-run.

- **H20.** One change at a time per session, and one allocation at a time per checkout.

  - A session change takes `flock` on `$XDG_RUNTIME_DIR/ccy/change/<session>.lock`.
  - A seat allocation takes `flock` on `$XDG_RUNTIME_DIR/ccy/alloc/<checkout digest>.lock`.
  - Either one busy gives exit 75, naming who holds it.
  - Neither lock lives where a container can reach it. `flock` needs no write permission, so
    a process in the session could hold a lock in its read-only membership mount, or in the
    checkout, until the container stops, and freeze the owner out. The membership directory
    carries only files the container may see.

- **H21.** Allocation authority. A seat `(checkout, team, seat)` is free only if its lock is
  free **and** no live session's desired file names the same triple. `agent-bus seat take` and
  `seat choose` both apply this.

  - Tests: a seat named in another session's desired file whose keeper has not locked it yet
    is refused. The same `team, seat` in a different checkout is not refused.
  - As built in M4 (H20, H21):
    - The allocation lock is `$XDG_RUNTIME_DIR/ccy/alloc/<first 32 hex of sha256(resolved checkout path)>.lock`, not `seats/.alloc.lock`: pingbus's `list_seats` refuses any entry
      of `seats/` that is not a team (so every deployed image would break on it), and the
      checkout is writable by every session of it (a symlink there would redirect the host's
      write; a container could hold it for good). `ccy-teams session add` holds it from
      choosing the seat to writing the desired file and runs `agent-bus seat take --alloc-held`; `seat take` otherwise waits for it up to 15 s, then exits 75 naming the
      holder. With no absolute `XDG_RUNTIME_DIR`, `seat take` refuses (exit 78) rather than
      allocate unlocked. The change lock was first built in the membership directory, as this
      spec then said; Fable's M4 review (B1) moved it to `ccy/change/` for the reason above.
    - "Live" is, for ccy-teams, a session on ccy's tmux server (as the board has it), and for
      agent-bus a session whose container runs (its `ccy-session` label). So a launcher's own
      desired file, written just before its `seat take`, is never counted against it.
    - A stopped session's record that a restore replays (`restore=yes`, same checkout) also
      names its seats (Fable's M3 note N4): `session add` refuses them, and the board's
      discovery gives a seat to the live session that wants it over another session's record.
    - `hand-to=<seat>` is resolved in the team's roster: a full handle, or the one active
      member other than the leaving seat whose handle has that seat; several is exit 64,
      asking for the handle. `add --role=coordinator` over another coordinator needs
      `--demote-current`, as `role` does. Interactively, a coordinator's remove asks
      demote-or-not (yes by default); handing over is by the option.
    - `session list` reads a session's roles from its keeper's report (what it was told), so it
      needs no sudo; "unguarded" is no `pingbus seat keep` process in `podman top`.
    - `ccy-teams remove <team>` stays terminal-only (never `--yes`, its own rule): H29's offer
      is a question in that flow, and each session is taken off with `--coordinator=demote`.
    - The in-session `bus: <team>(down)` (H26): while a team's homeserver does not answer, the
      watcher keeps `PINGBUS_HOME/down/<team>` (0600), refreshed on each retry and removed when
      the team syncs again or its thread ends; the status line appends `(down)` for a mark
      younger than 300 s, so one a killed watcher left stops counting. Another kind of sync
      failure (a full disk) is not a homeserver down and is not marked.
    - Not built in M4: docker-engine sessions (H30; only podman sessions carry the
      `ccy-session` label hot control finds them by).

- **H22. Add.** The steps, in order:

  1. Resolve the session to its container (`ccy-session` label). Check that it is running and
     hot-controllable (D5, H35).
  2. Check the team: declared, homeserver up (`agent-bus-hs@<team>` active), roster under
     64 agents (the `roles` limit, protocol §8).
  3. Choose or validate the seat (H21).
  4. Create the seat if new: `agent-bus seat take` (sudo, asked first).
  5. Probe the route from inside the container: `podman exec <c> pingbus reach --team <team>`,
     a versions GET with a 3 s limit.
  6. Set the role if asked (H25).
  7. Write the record (H5), then the desired file.
  8. Wait up to 10 s for the seat lock to read held, then for the report to show this
     generation.
  9. Report what the session will be told.

  **The first join of a session launched on no team relaunches it, once** (H34, Decision 1).
  Such a session was launched with `crossSessionInbound: refuse`, which Claude Code reads only
  at start, so its inbox socket drops every message. Step 7 is then followed by a relaunch through the existing
  restart path (the supervisor's restart request, `lib/restart-request.bash`, which replays
  the record's `--teams`, now naming the new seat): the session comes back in the same tmux
  session, resumed, in its seat, with inbound accepted. Every later change to it is hot. The
  per-step report says so before it happens and when it is done:
  `✓ session: relaunched once to accept team messages (it started on no team): resumed, seat 3 held (14 s)`.
  The keeper's report (3.3) carries `"inbound": true|false`, so ccy-teams knows which path a
  session needs without guessing; until M1's keeper writes it, the launch membership's seats
  (none means no inbound) answer the same question.

  A failure at steps 1 to 6 changes nothing. A failure at 8 restores the record and the
  desired file, and reports the keeper's `why`.

- **H23. Remove.** The steps, in order:

  1. Resolve the session as for add.
  2. If the seat holds the coordinator role, `--coordinator` is required: `demote` sets it to
     worker; `hand-to=<seat>` makes another seat coordinator. Interactively it is asked, with
     demote as the default. Non-interactively its absence is exit 64.
  3. Write the record, then the desired file without the seat, adding a `farewell` entry.
  4. Wait up to 10 s for the seat lock to read free.
  5. **Re-baseline**, from the host, through `pingbus seat retire --root <checkout seats> <seat>@<team>`:
     - clear the seat's `sync_token`, so a later re-add starts as a first sync
       (`timeline.limit: 0`, protocol §9) and nothing posted while it was out is delivered;
     - publish an expired `listening` status, so peers do not see a listener that is gone
       until its 600 s TTL.
  6. Post the goodbye from the host, using the seat's bundle, as the seat's member:
     `<handle> left the team: removed by the owner`. It is addressed to the team's humans and
     coordinator(s), like the session-end goodbye.
  7. Drop the farewell entry (one more desired write).

  The seat and its bundle stay in the checkout, free for a later session. **The token is not
  revoked** (D6). `--park` also parks the handle (`agent-bus seat remove`).

- **H23a. Exactly once across a drop and a re-add.** Items already in the seat's `inbox/` stay
  in the checkout. If a drop falls between "written to the socket" and "consumed", that one
  batch is delivered again on re-add. This window is accepted. Nothing posted while the seat
  was out is delivered (H23 step 5): what the removed team's thread fetches after the remove
  (its long poll in flight, up to 30 s) is consumed undelivered, so this holds from the moment
  of the remove.

  - Acceptance: post to the handle while the seat is out, re-add it, and see no backlog.

- **H24. Move.** Add to the new team (H22), then remove from the old one (H23). If the add
  fails, the old team is untouched. If the remove fails after the add, the report says the
  session is in both teams and names the re-run. A move within one team (seat to seat) is
  refused: remove, then add.

- **H25. Role.** Runs `sudo agent-bus set-role` for the seat's handle.

  - Making a seat coordinator while another holds the role needs `--demote-current`.
    Interactively it is asked, naming the current coordinator. The current one is demoted
    first, as `seats.ChangeRole` does.
  - Every session on the team whose role changed gets the notice (H12) from its own watcher's
    next sync.
  - The command waits up to 10 s for the target session's report (3.3) to show the new role in
    `told.roles`, and reports if it did not.

- **H26. Homeserver down.** Add and role stop at step 2 with "homeserver for `<team>` is down:
  `systemctl status agent-bus-hs@<team>`" and change nothing.

  - Remove still releases the seat (the change is local) and exits 1 with "goodbye and status
    not sent: homeserver down". They stay owed (H19).
  - In the session, the watcher keeps the team and retries it. The status line shows
    `bus: <team>(down)`.

- **H27. Seat limits.** A roster at 64 agents refuses add (exit 1). It names the limit and
  offers `ccy-teams` Ctrl-X (park or remove a member). A second seat in a team the session is
  already in is refused (one seat per team per session). The same seat again is H19's no-op.

- **H28. The session exits during a change.** The container stopping is seen at the wait step.

  - Add restores the record and the desired file, and reports "session ended during the add;
    seat `<s>` left free". A created seat is kept.
  - Remove goes on to steps 5 to 7: the lock went with the session.
  - Neither leaves a desired file for a session that is not running. The launcher writes it
    again on the next launch.

- **H29. A team removed while sessions are on it.** `ccy-teams remove <team>` lists every live
  session in the team, through the shared discovery function (section 7). It offers to remove
  them from the team first, which is H23 for each, and continues only once none is left.
  `--yes` does that without asking. A session whose homeserver vanished anyway keeps the team
  as `down` until the owner removes it (H26).

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

- **H31.** `cc --teams` and `cc --bus` sessions are hot-controllable on the same model:

  - the membership directory is `$XDG_RUNTIME_DIR/pingbus/membership/cc-<id>/`;
  - `seat exec --root --home` forks the keeper there too. It is the only lock holder, and it
    ends when its parent changes;
  - `PINGBUS_MEMBERSHIP` points at it.

  Exactly why it is weaker:

  - The session runs as the owner's own uid, so it **can** write the file. D2 cannot hold on
    the host.
  - `crossSessionInbound`, once accepted (H34), lets any process of that uid post into the
    session.

  This is the same stance cc already takes towards Element ("guidance, not a wall"), and the
  usage text says so.

- **H32.** cc changes:

  - `--append-system-prompt` names no team. It says: "Your agent-bus teams and seats are given
    in the session context and may change while you run; the latest agent-bus membership
    notice is current."
  - A cc session launched without `--teams` or `--bus` is not hot-controllable (the plugin is
    not loaded). `cc --bus` starts one with an empty membership, after the same Element
    consent.
  - As built in M6 (H31, H32):
    - The directory is named for the session's own name in ccy's tmux (`cc-<id>`, as its
      registry record is), written by `ccy-teams internal membership-init --kind=cc`; its
      `membership.json` says `kind: cc`, and every reader refuses a kind that does not match its
      directory. A cc started outside ccy's tmux has no name, so its directory is `cc.<pid>`: it
      follows the file, but no host command can find it (H35).
    - Liveness is the session's pane on ccy's tmux server; the keeper is found in the process
      tree under that pane (`pingbus ... seat keep`). The route check runs on the host:
      `pingbus reach --team T`, "this host reaches T's homeserver".
    - Inbound follows Decision 1, as for ccy (PLAN.md Decision 7): a cc launched with seats
      accepts cross-session inbound; `cc --bus` adds `crossSessionInbound: refuse` to its
      settings, and its keeper reports no inbound, as a ccy keeper on no team does. cc has no
      supervisor to relaunch it, so its first `session add` keeps the seat, the desired file
      and the record, asks no relaunch, and exits 1 saying cc must be ended and started again
      from `ccy-sessions`, which starts it in that seat, accepting. A re-run says the same,
      never "Nothing to do", until it has been restarted.
    - `session repair` refuses a cc session (exit 78): its keeper ends with its claude, and one
      started from the host would outlive the session. The list's state for a cc session with
      no keeper is `unguarded: end it and start it again from ccy-sessions`.
    - The record follows the change (`registry.SetCCTeams`): `--teams <seats>` while it has
      seats, `--bus` when it has none, so a restart from `ccy-sessions` starts where the last
      hot change left it. cc refuses `--bus` with `--teams` (64).
    - The chooser's `choose-teams --type=host --bus` asks the consent alone and prints nothing;
      `describe-seats --sentence` is gone, with the system prompt that named the handles.
    - Not in M6: agent-bus's own H21 liveness (a running container) does not count a cc session,
      so `agent-bus seat take` from another launcher does not see a cc session's desired seat
      until its keeper locks it; `ccy-teams` does. pingbus still reads `PINGBUS_SEATS`, which no
      launcher now sets. The picker (M5) shows cc sessions through `hot.Sessions`, which adds
      the running cc sessions (`discover.RunningCC`), with no change to its UI: a cc row is
      controllable, and never `Unguarded`, so the picker offers no Repair for it.

### The inert contract, and who can be hot-controlled

- **H33. Inert hooks.** The keeper keeps a flag file, `$PINGBUS_HOME/active`, which exists if
  and only if at least one seat is applied. Every hook command in `hooks.json` becomes
  `[ -e "$PINGBUS_HOME/active" ] || exit 0; exec pingbus hook <event>`.
  - As built in M2 the guard is `[ -z "$PINGBUS_MEMBERSHIP" ] || [ -e "$PINGBUS_HOME/active" ] || exit 0`:
    the kit's `hooks.json` is shared with `cc --teams` and `agent-bus-claude` members, which
    set no `PINGBUS_MEMBERSHIP` and keep no flag, and run every hook as before.
    SessionStart's inert branch writes `$PINGBUS_HOME/socket.json` (`{v, socket, token}`,
    mode 0600) with `jq`, reading the socket and token from the environment, never argv.
  - SessionStart is the exception: it still runs, so it can record `socket.json` (H11). It does
    that in the shell, with no Python, when the flag is absent.
  - Test: the plugin contract test runs every hook with the flag absent and sees no `python3`
    started and an empty answer.
- **H34. Inert settings.** The settings file a session is launched with is the keeper's
  `$PINGBUS_HOME/settings.json`, not the kit's file.
  - **Status line.** The command is a shell guard:
    `[ -e "$PINGBUS_HOME/active" ] || exec sh "$PINGBUS_HOME/statusline-inner"; exec pingbus status-line`.
    The keeper resolves the inner status line once, at start, from the user's and the project's
    settings, and writes it to `statusline-inner`. So a plain session starts no per-second
    Python, and keeps the status line it would have had.

  - **Cross-session inbound.** A session launched with at least one seat is launched with
    `crossSessionInbound: accept`, and the watcher, which runs only while a seat is applied
    (H11), is the only writer the kit starts. A session launched on no team is launched with
    **`crossSessionInbound: refuse`**, so nothing in the container can write a turn into it
    (Decision 1, as the owner set it). Its first join relaunches it once
    (H22); a later remove of its last seat leaves inbound accepted until it next ends, with
    the watcher gone, as for any seated session today.

  - **The socket is always there; the setting decides what it delivers** (Claude Code 2.1.301,
    read from its code and seen on the host: every claude process, the host's own included,
    owns `cc-socks/<pid>.sock`). The socket path is derived from the pid and bound whatever
    `crossSessionInbound` says, so the socket's presence proves nothing about inbound. The
    setting is `accept`, `hold` or `refuse`, taken from the first of the managed (policy),
    `--settings` (flag) and user settings that sets it; the project and local settings may
    only tighten it. Unset, a session that bypasses permissions holds a message for the human
    to review (`bypass-default`), and one that prompts accepts it. So an earlier version of
    this section, which said a session launched without the setting "binds no inbox socket",
    was wrong: such a session had a socket that held messages for review. `refuse` drops them,
    and as a `--settings` value it outranks the user's settings, which live in the shared
    checkout the session can write. M2's acceptance (b) proves inbound by the settings claude
    was launched with, not by the socket.

  - **Where the socket is** (probed in a CCY container, Claude Code 2.1.301): Claude Code
    binds `$XDG_RUNTIME_DIR/cc-socks/<pid>.sock`, on the container's own root filesystem, not
    under `/root/.claude` (which is the shared checkout) and not on any mount. So a sibling
    container of the same checkout cannot reach it; only processes in the session's own
    container can.

  - **M2 spike result: Claude Code does not re-read a `--settings` file mid-session** (2.1.301,
    probed in a real interactive session in a CCY container, no prompt sent). A status line in
    the file at start ran each second. An atomic replace (temp file, then rename), an in-place
    rewrite and a removal of the `statusLine` were all ignored for 20-30 s each: the first
    command kept running. A file with no status line at start never gained one when one was
    added, by either kind of write. `crossSessionInbound` could not be observed directly; it is
    read from the same file at the same time. So the settings are fixed at launch:

    - inbound is decided at launch (above): accepted for a seated launch, absent for a
      launch on no team, whose first join relaunches it; the keeper does **not** rewrite
      `settings.json`;
    - the status line command is the guard from launch, so it follows the `active` flag without
      a reload;
    - its `refreshInterval` is fixed at launch too: a seated launch gets 1 s, as before; a
      session launched on no team keeps the inner status line's own interval (none, if it has
      none). Its first join relaunches it seated, so it then gets 1 s.
    - A settings file the inner status line cannot be read from (not JSON, unreadable) does
      not stop the session: the claim names the file on stderr and launches with no
      `statusLine` of its own, so Claude Code's applies and there is no bus segment.
    - In M2 the claim (`pingbus seat exec`) writes `settings.json`, `statusline-inner` and the
      flag, once, before it execs claude; M1's keeper takes the flag over.
- **H33a. One resident process.** A plain session runs exactly one bus process, the keeper.
  - Acceptance (M2): `podman exec <c> ps` shows one `pingbus` process.
  - Acceptance (M2): the session's idle CPU over a minute is unchanged from before M2
    (`top -b -n 2`).
- **H33b. `--no-bus`.** `ccy --no-bus` launches without the plugin, the settings (so no status
  line wrapper and no inbound setting), the keeper and the mount, and with `PINGBUS_MEMBERSHIP=off`.
  Under H33, H33a and H34, neither a plain session nor a `--no-bus` session takes a message on
  its inbox socket as a turn: the plain one refuses it, and the `--no-bus` one, launched in
  bypass mode with no setting, holds it for the human's review (Claude Code's default, unless
  the user's settings say otherwise). They also differ by the mount, the keeper process, the plugin (whose hooks end in
  the shell) and the status line wrapper (which runs the session's own). The usage text says so, so the owner can choose. A `--no-bus` session
  is refused hot control with exit 78.
- **H35. Who can be hot-controlled.** Only a ccy session that ccy-sessions knows: one started
  inside ccy's tmux, which has a registry name and record. A `ccy` run in a plain terminal has
  no name, no record and no membership directory, so it cannot be hot-controlled.
  - ccy-teams says so for such a session (exit 78).
  - The usage text and section 5.1 say it in words.

## 5. UX

### 5.1 The picker: a Running sessions view

`ccy-teams`, then Tab, switches the list between Teams and Running sessions.

- The view lists the sessions ccy-sessions knows (started in ccy's tmux), and cc sessions
  started with `--teams` or `--bus`.
- A `ccy` run in a plain terminal is not listed and cannot be hot-controlled (H35). The view's
  footer says so once, in words.

```
 ccy-teams — Running sessions                                     Tab: Teams   ?: keys
 SESSION              CHECKOUT        TEAMS (seat, role)                         STATE
 ccy-<project-a>-1    <repo-a>        alpha (2, coordinator) · beta (1, worker)  ok
 ccy-<project-a>-2    <repo-a>        -                                          ok
 ccy-<project-b>-1    <repo-b>        alpha (5, worker)                          alpha down
 cc-<id>              <repo-c>        gamma (1, worker)                          ok (cc)
 ccy-<project-c>-1    <repo-c>        cannot be hot-controlled: relaunch         old or --no-bus
 ccy-<project-d>-1    <repo-d>        alpha (3, worker)                          unguarded: Repair
 Only sessions started through ccy's tmux (the ones ccy-sessions lists) can be changed here.
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
 ✓ seat 3: free, not named by any session in <repo-a>
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
 The seat's token stays valid: remove stops this session's pingbus, it does not revoke.
 ✓ role: seat 2 set to worker
 ✓ record: --teams 1@beta
 ✓ membership: generation 5 written (farewell alpha/2)
 ✓ session: seat 2 released (0.8 s)
 ✓ re-baseline: sync position cleared, listening status expired
 ✗ goodbye: not sent: homeserver for alpha did not answer in 4 s
 Removed, but the goodbye is still owed: run the same command again to send it.   (exit 1)
```

### 5.4 Non-interactive equivalents

```
 ccy-teams session add ccy-<project-a>-2 alpha/3 --role=worker --yes
 ccy-teams session remove ccy-<project-a>-1 alpha --coordinator=hand-to=5 --yes
 ccy-teams session move ccy-<project-b>-1 alpha beta --yes
 ccy-teams session role ccy-<project-b>-1 alpha coordinator --demote-current --yes
 ccy-teams session list            # SESSION<TAB>name<TAB>kind<TAB>checkout<TAB>team/seat/role,...<TAB>state
```

The usage text for `session` begins: "Only sessions started through ccy's tmux (listed by
ccy-sessions), and cc sessions started with --teams or --bus, can be changed while they run."

## 6. Milestones

Each milestone is shippable on its own and built test-first. Red and green are recorded in the
plan journal. Each ends with Fable review, qa-reviewer, then the owner's acceptance step on the
host through `meta-deploy.bash`.

| M   | Builds                                                                                                                                                                                                                                                                                                                                                                                                     | Shippable because                                                                                | Acceptance on the host                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| M0  | fix/watcher-survives-homeserver-restart **as it is** (per-team backoff, the first sync in the thread, the `--supervise` parent, the hooks' revive path). Check its SPEC-inject E-numbers once before merge. No runtime start or stop of teams                                                                                                                                                              | fixes a live bug on its own                                                                      | (a) restart a team's homeserver while a session is idle: the next message still arrives. (b) Audit F1: start a session while its homeserver is stopped, then start the homeserver: messages arrive. (c) `pkill` the child watcher: the supervisor restarts it                                                                                                                                                                                                                                         |
| M1  | pingbus: the 3.1 and 3.2 parsers with `checkout`, `active_teams` from `PINGBUS_MEMBERSHIP` (H4, H8), `pingbus seat keep` as the only lock holder with `LOCK_UN` (D4, H9, H10; absorbs audit F5), an empty membership valid in `seat exec`, `pingbus seat retire`, `pingbus reach`. `seat exec` keeps reading `PINGBUS_SEATS` until M2                                                                      | today's launcher still works; the new reader is inert until a launcher sets `PINGBUS_MEMBERSHIP` | (a) `./scripts/qa-helper-tests.bash` green. (b) `ccy --teams 1@<team>` on the new pingbus still claims as before; `agent-bus seat list` shows the seat held, and the keeper is its only holder (`podman exec <c> ls -l /proc/*/fd`)                                                                                                                                                                                                                                                                   |
| M2  | Launcher and entrypoint: the membership dir, the `:ro` mount with the relabel, the `ccy-session` label, the `PINGBUS_MEMBERSHIP` marker, `internal membership-init`, the kit in every named session with the inert contract (H33, H33a, H34), `--no-bus`, PINGBUS_SEATS and the `ccy-seats` label retired (H1, H2, H3, H33b). The H34 settings-reload spike. CCY and container version bumps               | `ccy --teams` behaves as before; plain sessions carry an inert kit                               | On an Enforcing or Permissive host (H3 says what each proves): (a) `ccy --teams 1@<team>` works as before. (b) A plain `ccy`: no bus segment; `podman exec <c> cat /run/ccy/membership/membership.json` reads; `touch` fails with EROFS. (c) A plain ccy prompt starts no `python3`; one `pingbus` process; idle CPU unchanged. (d) An old image with the new launcher is refused by `REQUIRED_CONTAINER_VERSION`. (e) `--no-bus` loads no settings and no keeper. (f) The spike's result is recorded |
| M3  | The watcher's runtime team start and stop, and the keeper starting and ending it (H11, waiters too). The membership notice, with `MEMBERSHIP` in `envelope.KINDS` and protocol §15 (H12), and its hook fallback (H13). The status line (H14), mid-turn behaviour (H15, H16). **The shared session-discovery function** (section 7). `ccy-teams session add` and `remove` with re-baseline, no role options | the first end-to-end hot add and remove                                                          | On a plain running session: `ccy-teams session add <s> <team> --yes`. The conversation shows the notice, the status line shows the team, and a message from Element reaches it. Then `session remove`: the notice; the goodbye in Element; the seat free in `agent-bus seat list`; the listening status gone at once; `pingbus send --team <team>` in the session gives exit 64. Post to the handle while it is out, re-add: no backlog delivered (H23a)                                              |
| M4  | Role and move (the coordinator options of H23, H24, H25), allocation authority keyed on the checkout (H21), locking (H20), idempotence and owed farewells (H19), every edge case (H26 to H30), the age restart reading the record (H5)                                                                                                                                                                     | the full non-interactive surface                                                                 | The script in `ACCEPTANCE-owner.md`: re-role; move; remove the coordinator; stop a homeserver mid-add; `ccy stop` mid-add; re-run each command for the no-op; `pkill` the keeper, Repair, then remove a launch seat; two `session add` at once on one session (exit 75); the same `1@<team>` in two checkouts                                                                                                                                                                                         |
| M5  | The picker's Running sessions view and its menus (5.1 to 5.3), and Repair                                                                                                                                                                                                                                                                                                                                  | the interactive surface                                                                          | the same four operations from the picker, on a terminal                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| M6  | cc: H31, H32, `cc --bus`                                                                                                                                                                                                                                                                                                                                                                                   | cc reaches parity, with its documented weaker authority                                          | (a) `cc --teams <team>`, then `ccy-teams session move cc-<id> ...` from another terminal. (b) Audit F5: `setsid sleep 600` from a `cc --teams` Bash tool, quit, relaunch into the same seat at once: it is free                                                                                                                                                                                                                                                                                       |
| M7  | Docs: docs/agent-bus.md (seats, hot control, "remove does not revoke"), docs/ccy.md (the mount, the label, `--no-bus`, inbound), the usage text (H35), the protocol §12 additions (`PINGBUS_MEMBERSHIP`, `applied.json`)                                                                                                                                                                                   | documentation                                                                                    | qa-reviewer, then the owner reads it                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |

M1 builds on what M2 shipped in pingbus rather than beside it: `seat.parse_membership` (the
strict 3.1 reader), the empty membership in `seat exec`, and the claim's session files
(`settings.json`, `statusline-inner`, the `active` flag). The keeper takes the flag over.

M5 as built (sections 5.1 to 5.3), with the small gaps of the spec settled:

- **One code path.** The picker makes no change itself. Each of its flows only chooses the
  words, then runs `ccy-teams session add|remove|move|role|repair` in the same process with the
  argv a person would type, never `--yes`. So the command asks its own confirmation and prints
  its own steps, and takes the same locks (H20) and rollback. `session list` and the view read
  one `hot.Sessions` and lay it out with one `hot.SessionLines`.
- **The view.** Tab on the teams switches to Running sessions, and Tab again switches back. Esc
  there goes back to the teams; it does not quit. Tab acts on no row, so it is named in the
  header's key line, not in a row's menu. The view lists the sessions `session list` does. cc
  sessions carry no `ccy-session` label, so they join the view with M6.
- **The session menu** offers add, then (when the session is on a team) remove, move and role,
  then attach, then Repair, but only when the session is unguarded. A session that cannot be
  changed (old, `--no-bus`, unreadable membership) offers attach only, and its shortcuts are
  refused with its state and "relaunch it from ccy's tmux". Attach uses the session's tmux,
  with a seat attach's checks, and ends the picker.
- **Add (5.2).**
  - The team question lists every team. One the session is already in, one not installed, or
    one whose homeserver is down is refused with the reason, and the question is asked again,
    3 times at most (InteractiveScripts.md). Then: "Nothing was changed after 3 tries."
  - The seat question offers the lowest free seat first (no seat word: the command chooses
    it, H21), then each seat `agent-bus seat choose` offers that is not held. A seat that
    another session names is refused by the command (75).
  - The role question: worker, or coordinator. When the board's roster names a current
    coordinator, the line says it becomes a worker, and choosing it passes `--demote-current`.
    A worker in a chosen existing seat passes `--role=worker`, so a free seat whose handle
    still holds the role (Decision 5's case) is made a worker explicitly. Otherwise no
    `--role` is passed; a new member is a worker.
- **Remove (5.3) and move.** When the board's roster says the leaving seat is the coordinator,
  the picker asks: "The role goes to no one", the default, or "Hand it to <handle>" for each
  active member. The answer becomes `--coordinator=demote|hand-to=<handle>`. With the roster
  unread (sudo refused, homeserver down), nothing is passed, and the command asks its own
  question. A move asks for the team it leaves (when there are several) and the team it
  joins, from the same refusing team list, but not for a seat: the new team's lowest free
  seat is taken.
- **No host acceptance script for M5.** No tool can observe the picker without a person at it,
  and the view's data path, `session list`, is still checked by M4's acceptance (e). The
  owner's steps are ACCEPTANCE.md M5.

## 7. Interaction with work in flight

- **fix/watcher-survives-homeserver-restart** is M0, shipped as it is. It brings:

  - the per-team retry with backoff;
  - the first sync moved into the thread;
  - a `--supervise` parent;
  - a per-session watch lock under `PINGBUS_HOME/inject/`;
  - the hooks' revive path.

  It does not start or stop a team at runtime. M3 adds that on top. Its watch lock lives under
  `PINGBUS_HOME`, which a plain session has only because M1's keeper makes the home even with
  no seat.

- **feat/teams-shutdown** is merged into F44. Its SD4 finds a team's sessions from the registry
  records only.

  - M3 delivers one shared session-discovery function: desired files, held seats, records,
    keyed on `(checkout, team, seat)`. The board, shutdown, `session add` (H22) and
    `ccy-teams remove` (H29) all use it, so they cannot disagree.
  - M3 is the first milestone that produces a hot seat, so the function belongs there.
  - H29 gives `ccy-teams remove <team>` a gentler first offer than shutdown: take the sessions
    off the team and keep them running.

- **fix/teams-server-robustness** (audit F3, F4) is independent. Merge in any order.

- **Plan 00202, the `ccy` mod.**

  - The mod's team roster band (T1) and `/team who` read `$PINGBUS_HOME/applied.json` ∩ the
    desired file, and the cached `team.json` roles. They never read `PINGBUS_TEAMS`.
  - The schemas in 3.1 and 3.2 are versioned (`v`). Fields are only ever added. The mod
    writes neither file.
  - The mod shows the membership notice's facts in its band, not as model text.
  - Hand-over in either order:
    - If 00202 M3 lands before M2 here, M2 loads nothing extra and relies on the mod.
    - If it lands after, the mod takes over the flag-file guard (H33), the status-line guard
      and the `crossSessionInbound` switching (H34), so the inert contract stays true.
    - This belongs in 00202's Task 3.2.

- **Plan 00177 Phase 8 and Plan 00194 Phase 3** (`feat/ccy-teams-hot-add`, design only, not
  merged).

  - Superseded by this spec. Its join marker is replaced by D1's read-only mount; its
    `pingbus join` by the keeper; its "Running sessions" view by M5.
  - Those tasks should be closed with a pointer here when Phase 2 of this plan is written.

## 8. Owner decisions

Settled by the coordinator under the owner's standing "do not gate on me", and recorded in
PLAN.md and the plan journal. **The owner may reverse any of them.**

1. **The bus kit is loaded in every named ccy session, with a `--no-bus` opt-out.** This holds
   on the condition that the inert contract (H33, H33a, H34) is met and tested in M2. Inbound
   is decided at launch (the M2 spike): none for a session launched on no team, whose first
   join relaunches it once (H22).
2. **Control, not isolation, within a checkout** (D6). A remove does not revoke the token.
   `--revoke` is deferred behind audit F7.
3. **cc's weaker authority (H31) is accepted**, and so is the new `cc --bus`.
4. **`PINGBUS_SEATS` and the `ccy-seats` label are retired in M2**, together with the
   `PINGBUS_MEMBERSHIP` marker (H2).
