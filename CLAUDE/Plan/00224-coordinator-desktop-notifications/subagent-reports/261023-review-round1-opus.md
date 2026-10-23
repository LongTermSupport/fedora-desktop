# Plan 00224 review, round 1 (Opus, adversarial)

Scope: `git diff c15620d06..HEAD` (PLAN.md, DESIGN.md, JOURNAL, README row). This is a plan
only, so every finding is about the design or the task list, checked against the repository.

## Verified claims

- The sudoers rule `files/etc/sudoers.d/agent-bus-read-only.j2:9` lets `{{ user_login }}` run
  `/usr/local/bin/agent-bus ^list [a-z][a-z0-9-]*$` without a password. True.
- `agentbus.ParseRoster`, `Roster.Member` and `Roster.Coordinator` are at
  `src-go/internal/agentbus/parse.go:139,79,69`. `Member.Parked` exists (`:46`). True.
- The membership directory is `$XDG_RUNTIME_DIR/ccy/membership/<name>/`. It is mounted
  `ro`, `Z` when relabelling (`files/var/local/claude-yolo/lib/agent-bus-membership.bash:67`).
  Its `Desired` holds session, kind, checkout and seats (`membership/desired.go:90-98`). True.
- `lxcrecord` exists, its `Item` carries Container, User, Handle and Status
  (`lxcrecord.go:32-41`), and it lives under the applied directory, readable by the owner's
  group (`play-agent-bus.yml:211`). True.
- The ssh-agent SELinux measurement is at `lib/ssh-handling.bash:1437-1444`: a socket relabelled
  with `:z` was still denied, so the mount uses `label=disable`. True.
- The kit builds: `pingbus-status-line` is built by `tasks/build-go-command.yml` in
  `play-agent-bus.yml:201-208` and `play-claude-yolo.yml:414-421`. True, but both builds set
  `go_command_static: true`, and the plan does not (see m1).
- LXC members must be privileged. `helpers/agent_bus_lxc/derive.py:215-216` refuses
  `lxc.idmap`, and `docs/agent-bus-lxc-members.md:191` says so. True.
- A ccy container's uid: it runs as uid 0 inside, mapped by the rootless userns to the owner
  (`Dockerfile:471-481`). Files it writes are owned by the owner on the host. This agrees with
  the first unverified premise.
- `stt-watch-failed.service` and the AgentNotes `notify-send` rule (`AgentNotes.md:80-86`)
  exist as described.

## BLOCKER

**B1. A worker can pose as the coordinator: the seat-to-handle mapping the gate relies on can
be written by the container.** DESIGN 3.4 step 2 resolves "the seats in its `membership.json`
... to handles the way `ccy-teams describe-seats` resolves them". `describe-seats` gets the
handle from `Bus.SeatList` (`src-go/internal/teams/choose/choose.go:400-415`). That reads
`seat.List(top, uid)` (`agentbus/agentbus.go:296-308`), which takes the checkout's
`.claude/ccy/pingbus/seats/<team>/<seat>/member.json` and its `user_id`
(`src-go/internal/bus/seat/seat.go:1-7`, `member.go:187`). The checkout is mounted read-write
at `/workspace` (`claude-yolo:2521`), and the seats tree is owned by the container's root.

So a worker container can rewrite its seat's `member.json` `user_id` to the coordinator's
handle. The relay would then resolve the worker's seat to the coordinator and pass the gate.
Two sessions on one checkout make it worse: they share the seats tree, so a worker can read and
copy the coordinator's own `member.json`, token included.

This breaks the plan's central guarantee (PLAN Goals line 33, Success line 153; DESIGN
3.2:90-94, "Nothing the container writes ... can change who the relay thinks is asking").
Plan 00209 already treats seats in the checkout as untrusted (`DESIGN-v2-C-host.md:1013`, "v2
moves seats out of the checkout (C25)").

Fix: the gate may read only host-written sources for seat to handle. One way is for the
launcher or `membership-init` to record each seat's handle in the host-only membership
directory when the seat is taken, checked against the registry, and never read again from the
checkout. Another is a registry query that names the holder of `<seat>@<team>`. The premise at
PLAN:60-61 and Task 2.4 need a test of this attack: a worker whose `member.json` names the
coordinator is refused 77.

## MAJOR

**M1. The bash-limits premise of Task 4.1 is wrong.** Task 4.1 says the logic goes in Go "so the
bash on the ratchet does not grow". `lib/agent-bus-membership.bash` is not on
`scripts/bash-limits.allowlist`, so it is held to the hard limits now. It has 4 functions
already (`:32,47,78,96`, the cap) and about 57 code lines against a cap of 60.

Even with Go printing the mount, the bash still gains lines: the extra `-v`/`-e` arguments, a
wipe at launch of a reused name's spool (the precedent is `:57-59`), and removal at cleanup.
Removal at cleanup belongs in `ccy_bus_disconnected` here, or in `session-lifecycle.bash`, which
is on the ratchet and cannot grow.

Task 4.1 should name the concrete route. For example, `membership-init`, or a sibling
`ccy-teams internal` verb, creates both directories and prints every run argument, so the bash
line count does not change. The task should also name where removal happens, and say that the
spool, like membership, is wiped at launch when a tmux name is reused. Without that wipe, a dead
session's leftover request is attributed to the new holder of the name.

**M2. The LXC mount is new, unowned IaC and can stop a member container starting.** No play
writes an LXC container's config today: `derive.py:233` only reads `lxc.mount.entry`, and no
play edits `/var/lib/lxc/<c>/config`. Task 5.1 adds "one `lxc.mount.entry` per container"
without saying:

- how the play edits a file the owner created (a marked `blockinfile`);
- how the entry is removed when the container's last member leaves;
- that a bind whose source is missing makes `lxc-start` fail. The entry needs
  `optional,create=dir`, or the plan must justify why not, because a notification feature must
  not be able to stop a team member's container booting.

Task 5.1 should also have a test for each of these.

**M3. A bind mount under `/run` in a systemd LXC container is likely hidden.** The mount target
inside the container is `run/agent-notify` (DESIGN 3.2:104-105). systemd in the container mounts
a tmpfs on `/run` at boot unless `/run` is already a mount point, and LXC does not make it one.
That tmpfs would cover the bind. Premise PLAN:57-59 ("a bind of a `/run` directory works") is
worded about the host source and does not name this. Either choose a target outside `/run` (for
example `/var/lib/agent-notify` or `/mnt/agent-notify`), or make the triage measure the
container-side path after the container boots. Name this masking in the premise.

**M4. The result file name is not tied to the request file name.** The relay writes
`<id>.result`, and `id` is a field the container supplies (DESIGN 3.3:112). The check list
(3.3:116-121) and Task 2.2's refusals do not validate `id`. If the relay builds the name from
JSON content, `openat(dirfd, "../../…")` escapes the directory, because `..` resolves through
`openat`. The result is a file created as the owner anywhere the owner can write.

Required: `id` exactly 16 lowercase hex characters, equal to the `.req` file's stem. The
result name is derived from the directory entry's name, never from the content. Add a Task 2.5
test for a traversal `id` and for an `id` that does not match the stem.

**M5. One bad spool entry must not stop the relay for everyone.** The project's fail-fast rule,
applied to the relay process, would let any container stop every notice. A directory, socket,
undeletable entry or unreadable file planted in a spool, or an `EACCES` while pruning, could
make a naive relay exit. A crash loop then hits the unit's start limit, which is a denial of
service by a worker against the coordinators.

The spec (Task 2.1) needs an explicit split. Untrusted per-entry and per-directory faults are
refused and logged, and the relay keeps serving the others. Only the relay's own state (the
limiter file, its watch roots, `notify-send` missing) makes it exit. Pruning must be a
non-recursive `unlinkat`, with `AT_REMOVEDIR` only for an empty directory. Only identity-level
directories are watched (depth 1 under the ccy root, depth 2 under the LXC root); a
subdirectory a container creates inside its spool is never treated as a new identity.

## MINOR

- **m1.** Tasks 3.1 and 4.2 do not say `go_command_static: true`. Both kit precedents need it:
  "Static, because the image is Debian and the host's glibc is newer"
  (`play-claude-yolo.yml:412-421`). LXC members run other distributions.
- **m2.** Missing task rows. A new deployed command needs a row in
  `scripts/go-binary-suites.tsv` (src-go/CLAUDE.md, Build and deploy), a layout line in
  `src-go/CLAUDE.md`, and the Dockerfile `chmod`/`ln -s` into `/usr/local/bin`. The existing kit
  binaries are linked in `Dockerfile:439-440`, not in the entrypoint as Task 4.2 says. If
  `x/sys/unix` is used directly, the reason column of the `go-deps.allowlist` row needs
  updating.
- **m3.** The LXC gate should require `Status == joined` on the `lxcrecord` item (DESIGN 3.4
  step 2). Without it a `left` or `deferred` item's handle stands, and the roster alone has to
  catch it.
- **m4.** The owner-uid ACL in a privileged container. DESIGN 3.2:106 says "Another user in the
  container cannot write a member's directory". That is false when some container account has
  the owner's numeric uid: the ACL grants it `rwx`, so it can delete or read requests and plant
  `<id>.result` before the relay writes it. The file-owner check stops forged requests, but the
  sentence needs correcting. The play could also refuse a container whose passwd has a
  non-member account with the owner's uid.
- **m5.** Route choice (DESIGN 3.1 step 2). LXC has neither `/run/.containerenv` nor `/run/ccy`.
  A member shell started outside `agent-bus-claude` has no `NOTIFY_OWNER_SPOOL`, and its
  systemd user session sets `DBUS_SESSION_BUS_ADDRESS`. That shell would take the "host" route
  against the container's own bus. Detect a container properly (`/run/systemd/container`,
  `container=` in pid 1's environment) and exit 69.
- **m6.** `<session>` should be defined as membership-init's key, `${session:-$container}`
  (`agent-bus-membership.bash:62`), not as "registry name". A session with no tmux name is
  named after its container.
- **m7.** Order of checks. DESIGN 3.4:139 puts the content checks before the pre-gate counter.
  Count first, before parsing, so a flood costs the least.
- **m8.** Escaping. Say whether the summary is escaped. GNOME Shell treats only the body as
  markup, so an escaped `&` in the title would show as `&amp;`. Name the exact zero-width set
  refused ("used as spoofing" is not testable), keeping in mind that ZWJ is needed for emoji
  sequences.
- **m9.** Relay unit details:
  - the relay must start when `$XDG_RUNTIME_DIR/ccy/notify/` or `/run/agent-notify/lxc/` does
    not exist yet, by watching the parent or creating it;
  - `$XDG_STATE_HOME` falls back to `~/.local/state`;
  - the unit must not set `NoNewPrivileges=`, or `sudo` breaks;
  - a refused corrupt limiter file should surface through an `OnFailure` notice, as
    stt-watch's does.
- **m10.** A ccy session holding many seats makes one `sudo` call per team per request, and the
  pre-gate counts requests, not `sudo` calls. Cap the teams looked up per request, or cache the
  roster within a single request (still never across requests).
- **m11.** Task 4.1's alternative, "or the `notify-owner` binary prints the mount", would make
  the ccy launcher depend on `play-agent-bus.yml`'s `/usr/local/bin` install. Prefer
  `ccy-teams`, which `play-claude-yolo.yml` already builds.
- **m12.** The journal holds only the scaffold entry. The design reasoning and decisions D1-D11
  have no `decision` entry yet.

VERDICT: CHANGES
