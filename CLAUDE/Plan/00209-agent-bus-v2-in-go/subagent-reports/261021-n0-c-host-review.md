FIX-FIRST

# Plan 00209 N0 area C (host side): adversarial review, round 1

Reviewed: `DESIGN-v2-C-host.md` against `DESIGN-v2-FRAME.md` and the N0 common rules. I
spot-checked these primary sources: `DESIGN.md` section 2 (R27-R33), `RESEARCH-lxc-robustness.md`
sections 1.6-1.8, 3 and 4, `RESEARCH-host.md` (admin endpoints, `--restore-backup`),
`src-go/go.mod` and `src-go/qa/go.mod`, and the author's digests 1 and 3.

The draft is strong. It is concrete, it puts two options against each decision, its resident
costs are mostly honest, and it uses no install-specific names and no time estimates. Two
findings are blockers: a security hole in the new seat store, and an unbacked database upgrade
window. Several majors are internal contradictions, or v1 requirements that were dropped without
being listed in 6.1.

## Blockers

1. **[blocker] 3.7.1 Seat release moves container-written contents into the shared pool.**
   `sessions/<session>/` is mounted read-write into a ccy container. The container's root is the
   owner's uid (K9), so the container controls every byte under it. On release, the host renames
   the directory back into `pool/<checkout key>/` and checks only the top level: that it is a
   directory owned by the owner, with `seat.json` and `token` as regular files. A container can
   therefore:

   - (a) forge `seat.json`, for example with `base_url` pointing at a server it controls, or a
     different `user_id` or `team`, and a token of its own choosing;
   - (b) rename `<team>` directories so that team A's seat is filed as team B's;
   - (c) plant symlinks or FIFOs deeper in B's member state.

   The next session to take that seat runs the forged bundle. That session may be a sibling
   container or a host `cc` session, and a symlink planted inside a container resolves against
   the host filesystem once a host session uses the seat. The result is cross-session injection
   and an owner-file write from a container. This regresses v1's hot-control rules D2/D3
   (RESEARCH-lxc 3.1): "no membership change ever comes from a container-writable file", and the
   desired file was mounted read-only for exactly this reason. The check-after-rename order is
   right, but "refused and left in place" leaves the bad entry in the pool.

   Fix: keep the authoritative bundle outside the mount, for example a pool-side copy of
   `seat.json` and a hash of `token` written at placement. On release, compare byte for byte and
   check that the path's `(team, seat)` matches. Walk the whole tree on release, allowing only
   regular files and directories owned by the owner, no links, no FIFOs and no devices. Move
   anything else to a quarantine directory outside the pool, never into it. Add this as a
   decision with its alternative: never return a container-touched directory to the pool, and
   re-issue the bundle instead. Say which of B's member-state files are allowed to travel
   between sessions. Add hostile-release cases to the 3.14 "seats" row.

2. **[blocker] 3.2.3, 3.5.1 step 4, 6.1: the Tuwunel link swap upgrades homeservers with no
   backup.**
   `ExecStart=<v2-lib>/tuwunel` points at a link, and static step 4 swaps that link before any
   `team apply` runs. Any restart from then on starts the new binary, possibly with a one-way
   database migration, without the pre-restart backup of 3.5.2 step 6. That covers:

   - a crash under `Restart=always`;
   - a watchdog kill;
   - the health tick's restart;
   - a `team apply` that fails before step 6;
   - every team outside `<v2>_only_teams`, whose apply never runs in that play.

   For those teams `units.json` still records the old binary. v1's "back up every running team
   before a software swap" existed for this case, and 6.1 drops it on the false premise that
   step 6 covers it.

   Fix: put the versioned binary path (`<v2-lib>/tuwunel-<version>`) in the per-team drop-in.
   The binary then changes only when `team apply` rewrites the drop-in, after its backup. Keep
   old versions until no drop-in names them. Test: a scoped play leaves the unscoped teams'
   drop-ins naming the old binary.

## Majors

03. **[major] 3.5.1 order: step 1 runs `<v2-host> declare check` before step 5 builds and
    installs `<v2-host>`.**
    On a fresh host there is no binary to run. On an upgrade, the old binary judges the new
    schema. Fix: build first, as the login user, and run the freshly built binary's
    `declare check` without privilege before any root task. Alternatively, move step 1 after
    step 5 and keep the "nothing else runs if it refuses" rule for every task that changes state.

04. **[major] The LXC absent phase is ordered two different ways.**
    3.5.1 step 9 says "For each absent team: team remove. Then lxc apply --phase absent". 3.9.2
    (last paragraph) and v1 (RESEARCH-lxc 1.6) say the absent phase runs before any team is
    removed, so that the member can still be parked while its homeserver runs. Fix 3.5.1 to:
    lxc absent, then team remove, then team apply, then lxc present.

05. **[major] 3.6.4 and 3.9.2 step 6: the declared coordinator cannot be brought up.**
    `role apply` refuses a declared coordinator that is not already an `active` or `parked`
    handle, and `team apply` runs roles (3.6.2 step 5) before `lxc apply --phase present`
    (3.5.1 steps 10-11). An LXC coordinator for a new team, or a newly declared LXC member made
    coordinator, therefore fails the play at `team apply`, and lxc apply's "then every
    promotion" (3.9.2 step 6) is never reached. The same trap catches a ccy member: its handle
    does not exist until its first seat take, so the owner cannot declare it first. D's
    migration also has to declare coordinators. This is v1's L 6 item 6 incident moved
    somewhere else, not fixed.

    Fix: specify the bring-up order. One option is that `team apply` defers a promotion whose
    handle is not yet in the roster, records it in the status record as a problem with its fix,
    and leaves it to lxc apply or to the add that creates the handle; demotions are never
    deferred. That is a recorded pending state, not a skip-and-warn. The other option is that
    ccy-teams and D must add the member before declaring it, and the declaration check says so.
    Pick one, add it as a decision, and add VM-lab cases: a new team with an LXC coordinator in
    one play, and a coordinator declared before its member exists.

06. **[major] 3.12.2 Restore breaks C18 and its own rule on `ctl/`.**

    - (a) The tar restores `roster.json`, so `counters` go down and a handle retired after the
      backup comes back `active`. C18 says neither can happen.
    - (b) Members added, returned or reissued after the backup hold tokens the restored database
      does not know, and the restored roster does not know their handles. The design does not
      say what the board shows or how they are repaired.
    - (c) Step 3, "Replace the control and root state with the tar's", has root write `ctl/`
      directly. 3.2.2 says root writes `ctl/` only through the `<v2-ctl>` child, never directly.

    Fix: on restore, merge in the current roster's counters (take the maximum) and its retired
    set. List the handles whose state changed after the backup. Either mark them needing
    `reissue` or retire them, as a stated rule. Do the `ctl/` swap in the child, or justify an
    exception with a no-follow `openat` under `ctl/`. Add these cases to the 3.14 restore row and
    to H-C5.

07. **[major] Hot control and the host's own notices are dropped without being listed (R29,
    R31, 00203 H22/H23).**
    v1's hot control rested on a desired-membership file the host writes and the container
    cannot write. It had farewells on removal, "record before desired", a route probe from inside
    the container before a join, a goodbye posted from the host as the seat's member on removal,
    and "nothing posted while out is delivered on re-add" (RESEARCH-lxc 3.2-3.3). R29 is a
    host-side requirement: a "disconnected" notice is posted from the host for each seat a
    session held when it ended without a goodbye.

    C replaces "desired" with "the seat directory is in the session directory". That covers a
    hot join, but it says nothing about the following:

    - how a running session is told to leave a team: a release waits for the seat lock to come
      free, but nothing signals the keeper to free it;
    - who posts the farewell, the disconnected notice, or the re-baseline;
    - how C26's "desired membership counts as held" and R31's "active teams = held ∩ desired"
      are honoured.

    None of these appears in section 2, in 6.1, or in section 7 as an assumption on B. Fix: carry
    R29 and R31 with their sources, design the host side of leave and disconnect (a host-written,
    container read-only control file is still the strongest option), or state B's ownership
    explicitly in section 7, and list whatever is dropped in 6.1 with its reason.

08. **[major] C25 and DC5 overstate the isolation, and two unmount assumptions are missing.**
    A host `cc` session runs as the owner on the host and can read every seat in `<v2-seats>`,
    including the coordinator's token in every ccy session directory. C25's "visible only to the
    session that holds the seat" is therefore true only among ccy sessions. 3.10 already treats
    host-kind members as able to read the owner's files for Element. Seats need the same
    treatment.

    C26 also requires the allocation lock (in the owner runtime directory) to be unreachable by
    containers, and C25 requires the same of `<v2-seats>`. Neither is stated as a constraint on
    what ccy mounts.

    Fix: restate C25 with the host-session caveat and add it to the risks (or refuse a
    coordinator seat while a host-kind member runs in that checkout). Add to section 7, for B and
    D, that neither `<v2-seats>` nor the runtime-directory lock paths may lie under any path ccy
    mounts.

09. **[major] 3.2.3 and 3.12.2 step 2: `DynamicUser` has no `User=`.**
    The template sets no `User=`, so systemd derives the dynamic user's name from the unit name.
    `<v2-hs@><team>` contains an `@` and is not a valid user name, so systemd falls back to an
    opaque hashed name. More importantly, restore's `systemd-run` transient unit has a different
    unit name and so gets a different dynamic user. "The same `DynamicUser` state directory" then
    either recursively chowns the database to the transient unit's uid and back, or fails.

    The transient unit also needs `SocketBindAllow=` and `IPAddressAllow=` for its free loopback
    port, because the base unit denies every bind.

    Fix: set `User=<v2-hs>-%i` in the base unit and in the transient unit, bound the team name so
    that the user name stays at 31 characters or fewer, and add the shared uid and the absence of
    a chown to H-C3 and H-C5.

10. **[major] D-Bus is used throughout, yet DC12 rejects it for "no new module dependency".**
    The health tick (3.11.2 step 1), the backup's `KillUnit` (3.12.1 step 2) and the guest
    private socket (3.9.4) all speak D-Bus. `src-go/go.mod` has no D-Bus module, and the
    systemd private socket speaks raw peer-to-peer D-Bus. The same gap applies to
    `go.yaml.in/yaml/v3` (3.8). It is in the nested `src-go/qa` module only, not in the
    production module, so "on the module allowlist" is not true for `<v2-host>` or ccy-teams.

    Fix: decide once and add it as a decision. Option (a): add `godbus` and the YAML module to
    the production module, with Plan 00204 allowlist rows, and then revisit DC12's reasoning.
    Option (b): run `systemctl show` and `systemctl kill --kill-whom=main -s SIGUSR2` through
    `execx`, judged by exit status, which gives up the private-socket design of DC13(a). 3.13
    should use the same mechanism: `systemctl list-units --output=json` is not a JSON listing
    on every systemd. Verify it, or use `ListUnitsByPatterns`.

11. **[major] 3.5.2: C3's check has a window on first apply.**
    `server_name` is checked against `root/<team>/team.json`, which is written only in step 11,
    after bootstrap. Suppose a first apply starts the homeserver, which creates a database with
    `server_name` X, and then fails in step 8, 9 or 10. The next run, with `server_name` Y,
    passes the check and starts Tuwunel on a database that belongs to X. Fix: in step 4, before
    the first start, write a `server_name` record (or `team.json` itself, and the stamp last
    still means fully applied). Add a table test for this case.

## Minors

12. **[minor] 3.4, 3.11.2: what the health tick holds the team lock for is unclear.**
    If the tick holds the lock across its probes (about 30 s) and the child's per-room,
    per-member and per-human reads (each call bounded at 30 s with three tries), a launcher's
    60 s seat take can time out with exit 75. If it does not hold the lock, a view computed
    before a member verb can overwrite that verb's fresher status record. Fix: read the view
    without the lock, then take the lock without waiting only to restart and to write. Write
    only if the roster generation is unchanged since the read; add a generation counter to
    `roster.json`. Also say that the read-only child never rolls `pending` forward.

13. **[minor] 3.5.2 step 7: drop-in changes do not all need a restart.**
    `IPAddressAllow=`, `MemoryHigh=` and `SocketBindAllow=` are cgroup properties. They take
    effect on `daemon-reload`, and `After=`/`Wants=` do not affect a running unit. Restarting for
    a new `allow_from` CIDR drops every member's sync for no reason, which is against C6's
    intent. Fix: split `units.json` into a restart set and a reload set.

14. **[minor] 3.9.1, 3.9.2, 3.9.4: `lxc-attach` details are missing.**
    `--clear-env` leaves `HOME`, `USER` and `LOGNAME` unset, so `guest place` cannot resolve
    `~/.config/...` and `guest check` cannot find the forge CLI's login. Pass them with
    `--set-var`, from the `/etc/passwd` read done on the host side, and say whether `-u`/`-g`
    clear supplementary groups. v1 also set `lxc.start.auto = 1` for `service: true` (digest 3,
    step 06). v2 neither keeps it nor lists it as dropped.

15. **[minor] 3.9.2 step 3: there are two ways to read a running container.**
    `dir` and `btrfs` stores are read directly from the host rootfs, with bind mounts "resolved
    as LXC would", while other stores go through `/proc/<pid>/root`. For a running container,
    `/proc/<init pid>/root` is always the container's real view and makes the mount
    re-implementation unnecessary, so use it for every running container. Also open with
    `O_NONBLOCK` and `fstat` regular-file checks, because a FIFO stalls a no-follow open. The
    design does not say how C27 (held without entering) works for LXC seats in the guest's home.

16. **[minor] 3.9.2 step 4, C33: kit retention can break a running session.**
    Keeping only the last two digests can delete a kit that a running guest session resolved at
    launch. Keep every digest a live session references, and specify retention on the host for
    `<v2-kit>` as well.

17. **[minor] 3.2.5, 3.5.2 steps 2 and 9: firewall and socket checks.**
    `firewall-cmd --query-rich-rule` returns 0 for present and 1 for absent; any other code must
    fail the verb. Query `--permanent` and the runtime separately. The port and bound-address
    checks name `ss`, whose text the design would have to parse. That is the class of bug DC12
    cites against v1. Read `/proc/net/tcp{,6}` or use `sock_diag` in-process.

18. **[minor] C11 and 3.6.5 contradict each other.**
    C11 says "No read runs through sudo", but `human devices` and `backups` are root verbs read
    through password sudo. Restrict C11 to the board's open and redraw.

19. **[minor] 3.8: the strict decode of host_vars must be scoped to the v2 keys.**
    "Known fields only" and "any tag refused" have to apply to the v2 keys only. The same file
    holds other keys, `!vault` values and `yes`/`no` that are not v2's, and host_vars can be a
    directory. Say exactly what is decoded strictly.

20. **[minor] Seams with area A.**

    - 3.7.4 and C18 restate the handle grammar and the `<type>` set, which the frame gives to A
      ("identities and handles"). Cite A, and keep only seat numbering and allocation in C.
    - The shape of the `trust:` block in host_vars and the room names in the status-record
      example (`main`, `reports`, `questions`) should be marked as assumptions on A, or as
      examples.
    - The source key `L` (RESEARCH-lxc) collides with `L<n>` (SPEC-state locks).

21. **[minor] The placeholder table is incomplete.**
    `<v2-lib>`, `<v2-share>`, `<v2-etc>`, `<v2-consent>` and `<v2-hs-state>` are used in the
    text but missing from the table at the top, which area D works from.

22. **[minor] 3.12.1: the backup unit and lock re-entry are not specified.**
    The backup service's sandbox is not given, although the health unit's is. The backup inside
    `team apply` (3.5.2 step 6) "takes the team lock". `flock` locks belong to an open file
    description, so a second open and lock in the same process blocks itself. Say that the
    backup runs under the lock the apply already holds.

23. **[minor] 3.15: the resident cost is understated.**

    - The tick's homeserver calls scale with rooms × (members + humans) per team per minute,
      and each tick starts a process plus a child per team; state this as an order of growth.
    - Each member's long-poll sync costs homeserver memory, so "per member: nothing on the
      host" is not true.
    - "Three or so teams" is not a measured figure; let H-C1 report per member and per team.

24. **[minor] 3.2.2: the registration secret is kept twice.**
    `ctl/<team>/registration.secret` is a second persistent copy of the secret. The child needs
    it only to register the admin, and could receive it on its pipe when needed, which removes a
    secret copy from `ctl/`.

## Requirements and decisions coverage

- Every DC has at least two real options and a justified default. The following need a
  decision and do not have one: the trust model for a released seat (1), the binary pinning
  per team (2), the coordinator bring-up order (5), the restore roster policy (6) and the D-Bus
  mechanism (10).
- Silently dropped or uncarried: R29, R31 and the hot-leave half of 00203 (7); `lxc.start.auto`
  (14); and v1's pre-swap backup, which is listed in 6.1 but on a false premise (2).
- Fail-fast: the health tick's busy exit 0 is acceptable, given the record's expiry and the
  holder's own readiness checks. No other skip-and-continue pattern was found.
- No install-specific names and no time estimates were found.
