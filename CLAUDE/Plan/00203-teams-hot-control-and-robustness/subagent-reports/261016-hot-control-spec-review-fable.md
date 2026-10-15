# Plan 00203: adversarial review of SPEC-hot-control (Fable)

Read-only review of `SPEC-hot-control.md` and `261016-hot-control-design-opus.md` at
`origin/plan/teams-hot-control-spec` (`e45947ae`), checked against the code on `F44`
(`fa29ef5c`), the robustness audit, and the in-flight worktrees. Nothing was run on a host.
Names are placeholders.

**Verdict: REVISE.** The shape is right: a host-written directory mount as the only truth, roles
left in the admin-sent record, no membership from the bus. Four things must change before M1 is
built, because they decide what M1's parsers and the keeper look like; the rest are spec
corrections.

## 1. Can a read-only bind mount be updated live? Yes, with one addition

- **Directory mount, not file mount.** D1 is correct. A bind mount of a directory shares the
  directory's dentry tree, so a file renamed into it on the host appears inside at once. A
  single-file bind mount binds the inode the file had at launch, and an atomic replace swaps
  the inode, so the container keeps the old one: the design is right to reject option C.
  `ccy.env.local` is mounted that way today (`claude-yolo:2534`), which is why it is frozen.
- **Atomic write.** 3.1 says temporary file in the same directory, then rename. Correct, and
  the keeper polls `mtime` and `generation` (H9), so a half-written file is never read.
- **Read permission across the uid map.** `podman run` here passes no `--userns`
  (`Dockerfile:435-437` says so), so container root is the host user, and a 0600 file owned by
  the user is readable inside. Correct.
- **Missing: SELinux.** On an Enforcing host the launcher mounts the workspace `:z` and the
  staged key directory `:Z` (`claude-yolo:1378-1384`, `2511`
  `-v "$CONFIG_TEMP:/tmp/claude-config-import:ro${CCY_MOUNT_RELABEL:+,Z}"`). The membership
  directory lives under `$XDG_RUNTIME_DIR` (`user_tmp_t`), which `container_t` may not read.
  H1 must mount it `:ro${CCY_MOUNT_RELABEL:+,Z}` as the config import does; `:Z` is right
  because the directory is per session and owned by the tool. M2's acceptance (b) must run on
  an Enforcing host, or it proves nothing about the common case.
- **flock across the mount.** H22 step 8 has the host probe `seat.lock` while the container
  holds it. Same inode through a bind mount, so the flock is shared; `agent-bus seat list`
  already relies on it. Fine.

## 2. Authority: can the container or a peer forge membership or a role?

- **The mount.** `:ro` plus no added capabilities (docs/ccy.md line 518; podman's default
  rootless set has no `CAP_SYS_ADMIN`). Stronger than the spec says: a read-only bind
  mount made from a more privileged mount namespace carries `MNT_LOCK_READONLY`, so even a
  process with `CAP_SYS_ADMIN` in the container's user namespace cannot remount it
  read-write. H3's acceptance (write gives EROFS, remount refused) is the right proof.
- **The subset check (H8).** `active_teams` today reads only `PINGBUS_TEAMS`
  (`config.py:142`). The spec's "applied ∩ desired on every call" is sound as long as every
  reader goes through `config.active_teams`; the status line is the exception (it imports no
  pingbus module by design, `statusline.py` docstring) and so needs its own copy of the
  intersection, pinned equal by a test as its other copies are. Say so in H14.
- **Roles.** D3 holds: the role is in the admin-sent record, pingbus refuses a record from
  anyone but `admin` (protocol §8), and `set-role` is `sudo` on the host. No bus path.
- **Tokens in the checkout (D6).** Correct and honestly stated. One consequence the spec
  does not draw: because a removed seat's bundle and token stay in the checkout (H23), a
  removed session can keep posting with that token by calling the Matrix API directly. Hot
  "remove" is therefore a control over pingbus, not a revocation. If the owner wants a
  removal to bite, H23 needs an optional `--revoke` that runs `rotate-token`, which also
  needs audit F7's "refuse on a held seat" first. Recommend stating this under D6 and
  deferring the flag.
- **The report file 3.3** is in the writable checkout, as the spec says, and is checked
  against the locks. Fine. The applied file 3.2 is under `/tmp/pingbus-home`, container
  private: fine.

## 3. Races

### 3.1 The keeper and the inherited locks: the first thing to change

D4 relies on two facts that are both true but that together break H10's Repair:

- A launch seat's lock is an inherited open file description in claude's whole tree
  (`seat.py` `claim`, `set_inheritable`). `LOCK_UN` on any dup releases it for every holder:
  correct flock semantics. Note that today's `Lock.release` does `ftruncate` and `close`,
  which does **not** release a shared flock; the keeper needs an explicit `LOCK_UN`, which D4
  says, so pin it in the keeper's test.
- A keeper restarted by `podman exec` (H10 Repair) is a new process with no dup of those
  descriptors. It cannot release a launch seat. So after any keeper restart, `session remove` of a launch seat waits 10 s for a lock that stays held until the container ends,
  and H23 reports failure for a change the owner is entitled to.

The same inheritance is audit gap 7 on the host (a `setsid` child of a `cc --teams` claude
keeps the seat for good), and the audit's F5 already proposes its fix.

**Recommend:** make the keeper the **only** lock holder for every seat, launch and hot alike.
`seat exec` forks the keeper; the keeper takes the locks (close-on-exec, nothing inherited);
claude's tree holds none. A keeper that dies drops every seat, which is visible (hooks,
board) and repairable (Repair re-takes them all, as it must for hot seats anyway), and
allocation stays safe because H21 refuses a seat a desired file names. One mechanism instead
of two, Repair is complete, and gap 7 closes for cc. The audit's F5 then becomes part of M1.

### 3.2 A change during a turn (H15)

Sound. Each hook is a new process reading two atomically replaced files, the intersection
tolerates reading one new and one old, and the notice rides the inbox socket which already
queues mid-turn (SPEC-inject). One gap: a running `pingbus wait` (`_long_poll`) holds a team
lock and is not told to stop when the team is dropped; H11 covers the watcher only. Say what
happens to a waiter.

### 3.3 The session exits during a change (H28)

Sound for podman. The container's exit releases every lock; the host sees the container gone
at the wait step. Two additions: (a) the keeper writes the report 3.3 into the checkout, which
outlives the container, so a stale report can show the generation the host is waiting for;
have the host check the lock first and the report second, and have the keeper write the
report at claim time so a relaunch resets it; (b) the restore relaunch writes generation 1
(H1), so the host must compare generation for equality with what it wrote, never `>=`.

### 3.4 Two ccy-teams runs at once (H20)

The two flocks are right. One hole: **seat identity**. A seat is `seats/<team>/<seat>/` in a
checkout, so `1@alpha` exists once per checkout, and the handle differs
(`<repo>.<seat>+<host>.podman`). 3.1 carries `session`, `team` and `seat` but no checkout.
H21 ("free only if no live session's desired file names it") therefore refuses `1@alpha` in
checkout B because checkout A's session names it. Add `checkout` (the record's `Dir`) to 3.1
and key H21 on `(checkout, team, seat)`.

### 3.5 A seat keeper restart (H10)

Covered by 3.1 above. Also: in a ccy container the keeper's parent after the exec is PID 1
(`tini` runs the entrypoint, which execs `seat exec`, which execs the supervisor, which
`pty.fork`s claude). `getppid()` never changes while the container lives, so "exits when its
parent changes" is true only on the host (`cc`). Harmless in ccy, because the container ends
with claude, but the spec should say the keeper's end is "the container, or on the host the
parent changing", and Repair's `--parent <claude pid>` should be `--parent <supervisor pid>`
or simply absent in a container.

### 3.6 Exactly-once when a team is dropped and re-added

- Delivery is exactly-once per event through `inbox/` and `consumed/` in the seat's `state/`,
  which lives in the checkout and survives both the drop and the container. A drop between
  "written to the socket" and "consumed" re-delivers that one batch on re-add: a narrow
  window, acceptable, worth a sentence.
- **The backlog is the real question.** The seat's `sync_token` also survives. On re-add, the
  syncer resumes from it and `_fill_gap` pulls everything posted while the seat was out
  (`syncer.py:329-352`), and the account was never parked, so peers kept addressing it. The
  session is then handed a backlog of messages from a team it was told it had left. Decide
  it: recommend the keeper clears the team's `sync_token` on removal, so a re-add starts as
  a first sync (`timeline.limit: 0`, protocol §9) and the gap is never delivered. If the
  owner wants the backlog instead, say so and cap it.
- The removed seat's `listening` status in the room stays until its 600 s TTL. Have the
  keeper publish an expired status on removal, or peers see a listener that is gone.
- **`farewell` can be lost.** Two removes within one keeper poll (2 s) give generation N with
  farewell A, then N+1 with farewell B; the keeper sees only N+1 and drops A from the diff
  with no reason. Derive removals from the applied/desired diff (the spec does) and treat
  `farewell` as the reason only, defaulting to "removed by the owner" when absent; or make
  `farewell` accumulate until the applied generation acknowledges it.

## 4. The one-relaunch limit and the cost of the bus in every session

D5 is unavoidable: Claude Code loads `--plugin-dir` and `--settings` at start. But H33's
"inert" is not true of the kit as it stands, and the spec must say what inert means:

- **`hooks.json` has no shell guard.** Every hook is `pingbus hook <event>` (a zipapp start,
  Python, per event); only the relay PostToolUse hook has an `if`. H33 says the guard reads an
  empty `applied.json` without Python, but a shell cannot parse JSON. Specify the mechanism:
  the keeper maintains a flag file (`$PINGBUS_HOME/active`, present iff any seat is applied),
  and every hook command becomes `[ -e /tmp/pingbus-home/active ] && exec pingbus hook ...`
  (exit 0 otherwise). The H33 test then checks the flag file, not JSON.
- **`settings.json` runs `pingbus status-line` every second** (`refreshInterval: 1`), a Python
  start per second per session, and it replaces the user's and project's `statusLine` with
  its wrapper. In every plain ccy session that is a measurable idle cost. Same fix: a shell
  guard in the command string that `exec`s the inner status line when the flag is absent.
- **`settings.json` sets `crossSessionInbound: accept`** in every session. That is the switch
  that lets the watcher post into the session socket. In a container the only other
  processes are the agent's own, so the exposure is the agent injecting into itself, which it
  could already do; on `cc` it is every process of the owner's uid. It is not "nothing";
  state it under D5 and H31.
- **H11 adds a resident watcher to every session, and D4 a keeper.** Two long-lived Python
  processes per plain session, where today there are none. Recommend one resident: the
  keeper. It starts the watcher when the first seat is applied and the watcher exits when
  the last goes (today's "exit when no team is left" is then right again). The hooks'
  respawn (fix branch) stays as the backstop.
- **`--no-bus` and old launchers are indistinguishable** in the entrypoint once
  `PINGBUS_SEATS` is retired: both arrive with no mount. H2's "refuse a bus launch with no
  `/run/ccy/membership`" needs the launcher to say which it is. Have the launcher pass
  `-e PINGBUS_MEMBERSHIP=/run/ccy/membership` (or `=off` for `--no-bus`), and the entrypoint
  refuse the unset case as "ccy on the host is older than this image", as it does for
  `PINGBUS_TEAMS` today. The variable then also joins D62's protected set naturally.
- What `--no-bus` disables: the plugin, the settings (so the status line and
  `crossSessionInbound`), the keeper and the mount. With the guards above a plain session
  and a `--no-bus` session differ only by the mount and the keeper process. Say that in the
  usage text, so an owner can choose.

## 5. Ordering against in-flight work

Facts first: `fix/watcher-survives-homeserver-restart` has **no commits** past `F44`; its
worktree holds uncommitted implementation (cli.py +220, hooks.py, inject.py, matrix.py, and
the tests) including a `--supervise` parent, a per-session watch lock under
`PINGBUS_HOME/inject/`, and the hooks' revive path. `fix/teams-server-robustness` and
`plan/ccy-mod-gui` have no commits past `F44` and clean worktrees. `feat/teams-shutdown` has
one commit (`6df0b733`).

- **M0 = the watcher fix, committed.** The spec's section 7 describes the fix as "no commits
  yet" and wants a per-team runner that can start and stop. The uncommitted code has the
  per-team retry (`_watch_poll` with `_Backoff`) and the first sync moved into the thread
  (audit gap 2 closed), but **no start/stop of a team at runtime**: `_watch` still takes its
  team list once from `load_active`. So M3's driver is new work on top, which is fine, but
  the spec should say M0 ships the fix as it is and M3 adds the runner, rather than asking
  M0 to be built "to H11's lifecycle". Two further points for M0: the per-session watch lock
  is under `PINGBUS_HOME`, which in a plain session exists only if the keeper makes the home,
  so M2 depends on M1's keeper making the home even with no seat; and the fix cites
  SPEC-inject E25/E26 where the audit saw E20/E21: check the numbers once before merge.
- **feat/teams-shutdown first**, as the spec says. Its SD4 discovers a team's sessions from
  registry records only, and treats "a held seat no record names" as a special case. After
  M3 every hot seat is named by a desired file, not by the record's `--teams` (H5 writes the
  record too, so the record stays true). The shared discovery function in section 7 is the
  right call; name it as an M3 deliverable, not M4, because M3's `session add` is the first
  thing that produces a seat shutdown must find.
- **fix/teams-server-robustness** (the audit's F3, F4) is independent: nothing here reads
  `Restart=` or `seat check`'s wait. Merge in any order.
- **Plan 00202's mod.** Its M3 removes `--plugin-dir` for pingbus and moves the hooks into the
  `ccy` mod. D5 then stops mattering: the mod is in every session anyway. The spec covers
  the case "00202 M3 lands before M2 here", but not the reverse hand-over: when 00202 M3
  lands after, the mod must take over the flag-file guard and `crossSessionInbound` too.
  Add that to section 7 and to 00202's Task 3.2.
- **Plan 00177 Phase 8 / 00194 Phase 3** (`feat/ccy-teams-hot-add`): the spec supersedes
  them. Close them with a pointer when Phase 2 is written, as section 7 says.

## 6. Milestones: shippable, and does the acceptance prove it?

| M   | Shippable?                                                                                                          | Acceptance proves it?                                                                                                                                                                                                                                                                                                                                     |
| --- | ------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| M0  | Yes: the committed watcher fix                                                                                      | Partly. "Restart the homeserver while idle" proves the retry. Add the audit F1 case: start a session while the homeserver is stopped, then start it; and `pkill` the child watcher, which proves the supervisor                                                                                                                                           |
| M1  | Yes if inert. **Not inert as written**: `seat exec` must learn an empty membership, which today is a `ConfigError`  | Unit tests only is fine, but add one host step: a `ccy --teams` launch on the new pingbus still claims as before (the claim code changes in M1)                                                                                                                                                                                                           |
| M2  | **Risky.** It flips every session to the bus, retires `PINGBUS_SEATS`, and needs the launcher and the image in step | (a) and (b) are right. (c) "no python3 started" needs the flag-file guard in section 4 to be true. Add: (d) Enforcing-mode read of the mount; (e) an old image with the new launcher is refused by `REQUIRED_CONTAINER_VERSION`, and a plain session's idle CPU over a minute is unchanged (`pidstat` or `top -b -n 2`); (f) `--no-bus` loads no settings |
| M3  | Yes: the first end-to-end                                                                                           | Good. Add: send from the session with `pingbus send --team <removed>` after remove and see exit 64; post to the handle while it is out, re-add, and confirm the decided backlog behaviour (section 3.6)                                                                                                                                                   |
| M4  | Yes                                                                                                                 | The `ACCEPTANCE-owner.md` script is the right form. Add: Repair after `pkill` of the keeper, then remove a launch seat (the case section 3.1 is about); two `session add` runs at once on one session (exit 75); the same seat from two checkouts (section 3.4)                                                                                           |
| M5  | Yes                                                                                                                 | Fine                                                                                                                                                                                                                                                                                                                                                      |
| M6  | Yes                                                                                                                 | Add the audit F5 case: `setsid sleep` from a `cc --teams` Bash tool, quit, relaunch at once                                                                                                                                                                                                                                                               |
| M7  | Yes                                                                                                                 | Fine                                                                                                                                                                                                                                                                                                                                                      |

Also missing from every milestone: who can be hot-controlled. A registry record exists only
for a session started inside ccy's tmux (`ccy_tmux_current_session`, `RecordSeats` returns 0
silently without one). A `ccy` run in a plain terminal has no name, no record and no
membership directory. H1 says "with a registry name", which is right, but 5.1 and the usage
text must say it in words: hot control is for sessions `ccy-sessions` knows.

## 7. The four owner decisions

1. **Load the bus in every ccy session, with `--no-bus`.** Recommend **yes**, on the condition
   that section 4's inert contract is written into H33 and tested: flag-file guards in
   `hooks.json` and the status-line command, one resident process (the keeper) until the
   first seat, `crossSessionInbound` named in the docs, and an explicit
   `PINGBUS_MEMBERSHIP` marker so the entrypoint can tell `--no-bus` from an old launcher.
   The alternative (only `--teams` launches are controllable) defeats the request, which is
   to add a running plain session.
2. **Accept D6.** Recommend **yes**, with the sentence that a hot remove does not revoke the
   token, and `--revoke` deferred behind audit F7.
3. **cc's weaker authority and `cc --bus`.** Recommend **yes**. It is the same stance cc takes
   on Element, and section 3.1's single-holder keeper removes cc's stale-seat problem at the
   same time.
4. **Retire `PINGBUS_SEATS` and the label in M2.** Recommend **yes**, together with the
   `PINGBUS_MEMBERSHIP` marker, and keep the entrypoint's refusal of a leftover
   `PINGBUS_SEATS` so a mixed deploy fails fast rather than launching a teamless session.

## 8. Smaller corrections

- H25 waits for "the report to show the new generation of told roles", but 3.3 has no roles
  field. Add `told_roles` (team, handle, role) to 3.2 and 3.3, or wait on `told.json`'s
  generation instead.
- H12's `MEMBERSHIP` envelope kind: `envelope.KINDS` is a closed tuple with a fixed field
  count per kind; the protocol §15 addition in M7 must land with M3, not after it.
- H17's `membership init` is listed as a user-facing subcommand; it is the launcher's. Put it
  in a `ccy-teams` internal group as `record-seats` is, so the usage text stays honest.
- H9's "lock is taken without waiting" should name the holder on refusal, as `claim` does,
  so `why: held-elsewhere` can say which kind holds it.
- Section 1's F3 says no code reads the `ccy-seats` label; the new `ccy-session` label is read
  by H22 step 1. Fine, but H1 should say the label is the host's only map from session to
  container and must be set on every launch that has a name, bus or not.
