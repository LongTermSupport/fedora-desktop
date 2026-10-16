# Hot control design: the facts from the code, and the options weighed

Plan 00203 Task 1.2. Deliverable: [../SPEC-hot-control.md](../SPEC-hot-control.md). Everything
here was read from the code at F44 `672573ad`; nothing was run on a host. Claims marked
**(verify)** must be proven by the M-acceptance steps, not taken from this note.

## 1. How a session's teams, seats and role are fixed today

### 1.1 Launch, ccy

- `claude-yolo` (around line 3880) calls `ccy_seats_take "$CCY_SEATS"` just before
  `container_cmd run`. `lib/agent-bus-seats.bash`:
  - `ccy_seats_check` runs `agent-bus seat check <list> [--resolved]`. It prints the
    canonical `<seat>@<team>[,...]`.
  - `ccy_seats_take` runs `agent-bus seat take <canonical> [--no-prompt]`. That creates any
    missing seat with one `sudo agent-bus add-member ... --out=-`, whose tar the user-side
    tool unpacks into `<checkout>/.claude/ccy/pingbus/seats/<team>/<seat>/`. It refuses a held
    seat (exit 75).
  - It then fills `CCY_SEAT_RUN_ARGS=(-e PINGBUS_SEATS=<canonical> --label ccy-seats=<canonical>)`
    and prints `ccy-teams describe-seats --block`.
- `files/usr/local/bin/agent-bus`:
  - `seat ...` runs as the caller, never through sudo: root never writes in a checkout.
  - Admin commands run as the `agent-bus` system user through `runuser`.
- `entrypoint.sh`, the AGENT-BUS block (lines 547-600):
  - It refuses any `PINGBUS_*` that `ccy.env` or `ccy.env.local` changed (D62).
  - It refuses `PINGBUS_TEAMS` or `PINGBUS_HOME` from the launcher.
  - Only with `PINGBUS_SEATS`, it links `pingbus` onto PATH, adds
    `--plugin-dir .../plugin/pingbus --settings .../settings.json` to claude, and puts
    `pingbus seat exec --` in front of the final exec (line 748).
- `helpers/pingbus/seat.py` `claim()`:
  - It parses `PINGBUS_SEATS`, checks each seat directory and its `member.json`, and takes
    each `seat.lock` without waiting (kind `seat` and the claim time; never a PID).
  - It builds `/tmp/pingbus-home` (mode 0700). A second claim is refused.
  - It makes one symlink per team pointing at the seat directory.
  - It sets `PINGBUS_HOME` and `PINGBUS_TEAMS` in the child environment, runs
    `config.load_active`, and makes the lock fds inheritable.
  - `cmd_seat_exec` then calls `execvpe` into claude.
  - So the locks are held by claude's process tree.

### 1.2 Launch, cc

- `files/var/local/claude-code/cc-teams.bash` builds:

  ```
  env PINGBUS_SEATS=<seats> <kit>/pingbus seat exec --root <checkout>/.claude/ccy/pingbus/seats
  --home <tmp>/pingbus-home -- claude --plugin-dir <kit>/plugin/pingbus --settings <tmp>/settings.json
  --append-system-prompt "<sentence>"
  ```

  `<sentence>` is `ccy-teams describe-seats --sentence`: "You are <handle> in <team>...".

- `<tmp>` is `mktemp -d $XDG_RUNTIME_DIR/cc-teams.XXXXXX`. The kit's `settings.json` is
  merged with cc's deny settings.

- A `host` seat beside Element needs `~/.config/pingbus/element-accepted`
  (`config.element_accepted`).

### 1.3 The role

- The role lives in the team record: room state `agent_bus.team`, `roles`, user ID mapped to
  `coordinator` or `worker`. It is sent by `admin` only, and a record from anyone else makes
  the room untrusted (protocol §8). The record holds at most 64 role entries.

- The registry `registry.json` (helpers/agent_bus/registry.py) holds one coordinator per team
  (`Registry.coordinator`, the refusal in `set_role`). A parked member keeps its role.

- It is changed by `sudo agent-bus set-role <team> <handle> --role=...`.

  - ccy-teams calls it from `seats.ChangeRole` (the seat menu, `choose-seat`).
  - `src-go/internal/agentbus/agentbus.go:177` validates the role.

- pingbus reads it from the cached `state/team.json`, the last verified record, which the
  watcher's sync refreshes:

  - `hooks._team_view` reads `record.roles.get(member.user_id)` and the coordinators;
  - `cli._addressees` reads it for `--to-coordinator`;
  - `protocol.with_coordinator_copy` reads it for the copy on a worker's message to a human.

  So the role is live in the hooks and the CLI on each call. Only the SessionStart context
  lines (`role_lines`) are said once.

### 1.4 What reads at start, and what reads on each call

| Reader                                              | At start                                                                                             | On each call                                                                                                                                                 |
| --------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| hooks (`pingbus hook ...`, a new process each time) | the environment is claude's, frozen                                                                  | `config.load_active(environ)`: bundles, cached record, inbox, locks. SessionStart alone writes the status-line record and says seats, roles and descriptions |
| watcher (`cmd_watch`)                               | members from `load_active`, one poll thread per team, the socket path and token from the environment | the inject pass. A team whose thread fails is "no longer watched", for good. It exits when no team is left or the socket goes                                |
| status line (`statusline.py`)                       | -                                                                                                    | reads `/tmp/pingbus-sessions-<uid>/<session_id>`, written by SessionStart only                                                                               |
| CLI (`send`, `say`, `recv`, `status`, ...)          | -                                                                                                    | `load_active(environ)` each time                                                                                                                             |

The watcher is spawned with `close_fds=True` (`hooks.spawn_watcher`), so it does **not**
inherit the seat locks. It holds the per-team sync `lock`, kind `watch`.

### 1.5 The homeserver side of a join and a leave

- **The Matrix account.** `admin.add_member` builds the handle `<repo>.<seat>+<host>.<type>`
  (registry), creates the account, invites and joins it to the room, writes the role into
  the registry and the record, and returns the bundle. Its `base_url` is a `listen` address
  of the team (`_member_address`), never pasta's `host.containers.internal`.
- **Leave.**
  - `park-member` revokes the token and keeps the account, the role and the room membership.
  - `remove-member` removes the member for good; the handle is never reused.
  - `agent-bus seat remove` parks the handle and deletes the seat directory.
- **The firewall `allow_from`.**
  - The podman type needs nothing. The container reaches the host's `agentbus0` address
    through pasta, its usual route (docs/ccy.md, "The agent team bus address"). **(verify)**
    The source address as the firewall sees it is not a container subnet: the `podman` row of
    docs/agent-bus.md says "nothing extra", and launches on named networks work today.
  - docker members need the docker subnet. LXC members get the LXC bridge's subnet added by
    the play.
- **The team file and its stamp.**
  - ccy seats are runtime members: the registry, not the host_vars. Adding one changes no
    team file and no stamp, and needs no homeserver restart.
  - LXC members are declared (`agent_bus_lxc_members`), so changing one is a play run.
- **The room.** Members are invited and joined at add-member. A parked member stays in the
  room.

### 1.6 How ccy-teams lists live sessions

`board/collect.go`:

- `collectSessions`: `tmux.List`, names starting with `ccy-` and their paths. Plus the registry
  records `~/.../sessions` with `Name`, `Dir`, `Launcher`, `Prefix`, `Restore`, `Compose`,
  `Args`. The `--teams` value is taken from the record's args (`teamsListOf`).
- `readSeats`: `agent-bus seat list` per checkout. It probes each `seat.lock` from the host;
  `flock` works across the container boundary because it is the same file.
- `seatRow`: the session is the record naming `dir|seat@team`, else `held` or `free`.
- feat/teams-shutdown adds cc session records and the "held seat no record names" case.
- **No record carries the container name.** `CONTAINER_NAME` comes from
  `get_next_container_name "$PROJECT_NAME" yolo` and changes between launches. Hence the
  spec's `ccy-session=<session>` label (H1).

## 2. Every value frozen at launch

The spec's section 1, F1-F11, is the list. The containers' own fixed facts:

- environment: `PINGBUS_SEATS`, `PINGBUS_TEAMS`, `PINGBUS_HOME`, `CLAUDE_CODE_MESSAGING_SOCKET`
  and its token;
- labels: `ccy-seats`;
- mounts: `/workspace` (read-write), `ccy.env.local` (a read-only file mount), SSH keys, the
  gitconfig, display sockets, project mounts;
- container args: `--plugin-dir`, `--settings`, the `seat exec` prefix;
- tmux and the registry: the record's args, and the launcher's in-memory `CCY_ORIG_ARGS` for
  an age restart (prior design note, Plan 00177 Phase 8).

## 3. Options weighed for the source of truth

| Option                                                                                                 | Live?                                                       | Forgeable by the container?              | Verdict                                                                                                                 |
| ------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------- | ---------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| A. A file in the seat directory under `/workspace/.claude/ccy/` (feat/ccy-teams-hot-add's join marker) | yes                                                         | yes: `/workspace` is read-write          | rejected                                                                                                                |
| B. A read-only bind mount of a host directory                                                          | yes (renames inside the directory show)                     | no (`:ro`, no CAP_SYS_ADMIN)             | **chosen**; costs one launch change                                                                                     |
| C. A read-only single-file mount (like `ccy.env.local`)                                                | no: an atomic replace swaps the inode, which the mount pins | no                                       | rejected                                                                                                                |
| D. Signed entries in the checkout                                                                      | yes                                                         | no, if asymmetric                        | rejected: helpers are stdlib-only, and the stdlib has no ed25519                                                        |
| E. `podman exec` pushing into a container-local path                                                   | yes                                                         | yes, afterwards (container root owns it) | rejected as the truth. Kept for Repair (H10) and the route probe (H22), where the host acts and nothing is trusted back |
| F. Roles in the membership file                                                                        | yes                                                         | no                                       | rejected: a second truth beside the admin-sent record                                                                   |

D6, said plainly: any session can read every seat bundle in its own checkout. A forged
membership grants no Matrix capability the session does not already have in that checkout.
So B buys correct allocation, correct display and the owner's control over the session's own
pingbus. It is not a crypto boundary inside a checkout. The spec says so rather than claim
more.

## 4. Why a keeper process

- A launch seat's lock is an inherited open file held by claude's tree (F5).
- To release it on removal, some process holding that open file must call `LOCK_UN`. On a
  shared open file description that releases it for everyone.
- The watcher cannot: it closes inherited fds.
- A process forked by `seat exec` after the claim, before the exec, holds dups. That is the
  keeper.
- It also holds hot-added seats, whose locks must not depend on the watcher, which restarts.
- Lifetime: after the exec, its parent PID is claude. A change in `getppid()` means claude
  ended, and it releases and exits. Python's stdlib can also use `prctl(PR_SET_PDEATHSIG)`
  through ctypes; the poll is simpler and portable.
- If it dies, launch seats are still held by inheritance. Hot seats drop, the hooks report it,
  and the host offers Repair.

## 5. Edge cases: what was found

- **No bus route.** There is no bus podman network to `connect`. `podman network connect` does
  work live on rootless containers (`lib/network-management.bash:465,514`), but no podman
  network carries the bus. The probe from inside (`podman exec ... pingbus reach`) is the honest
  check. **(verify)** That a session on a named rootless podman network reaches `agentbus0`.
  It does today for `--teams` launches, per docs/ccy.md.
- **Homeserver down.** `add-member` and `set-role` need the admin API, so they fail cleanly
  before any write. A remove is local, except its goodbye.
- **Seat limit.** 64 `roles` entries per record. One seat per team per session (`parse_seat_list`).
  Seat names are 1-999999 or short names.
- **Mid-turn.** The socket delivery path already queues mid-turn (Plan 00194 SPEC-inject). The
  keeper and the watcher do not depend on turns.
- **The session exits.** Its locks go with its processes. The host sees the container gone
  and rolls back an add.
- **A team removed.** `ccy-teams remove` today does not look at live sessions in the team; H29
  adds that.
- **The coordinator removed.** A parked member keeps its role (protocol §8), so a removed
  coordinator would stay coordinator. Hence the required `--coordinator` choice in H23.

## 6. The earlier design on feat/ccy-teams-hot-add

Commit `c4c79c83`, not merged into F44. It adds Plan 00177 Phase 8 (Tasks 8.1-8.8) and Plan
00194 Phase 3:

- the supervisor plugin loaded in every supervised session;
- a join marker in the seat's bundle directory naming the container;
- `pingbus join` with a detached lock holder and an env file;
- envelopes telling the agent to run `pingbus` by its full path, because PATH and
  `PINGBUS_HOME` are frozen;
- a Running sessions view;
- the age restart reading the record.

What carries over:

- the Running sessions view;
- the detached holder, which becomes the keeper;
- the age restart reading the record (H5);
- the "plugin extras only at the next start" finding, which becomes D5's one relaunch.

What does not:

- the marker in the writable checkout;
- the env-file and full-path workaround. With `PINGBUS_MEMBERSHIP` and the keeper, the frozen
  `PINGBUS_HOME` path stays valid and the teams are read live, so no command changes form.

## 7. Owner decisions this design asks for

1. Load the pingbus plugin, the settings and the keeper in **every** ccy session (D5, H33),
   with `--no-bus` to opt out. The alternative is that only `--teams` launches can be
   hot-controlled.
2. Accept D6: within one checkout this is control, not isolation.
3. Accept that cc's authority is weaker (H31), and the new `cc --bus`.
4. Retire `PINGBUS_SEATS` and the `ccy-seats` label in M2 (H2).

## 8. Bash limits

The launcher and `lib/agent-bus-seats.bash` are on the ratchet, so M2's launcher change is
one mount argument, one label and one call to `ccy-teams membership init`. Everything else
is in Go (ccy-teams) or Python (pingbus).
