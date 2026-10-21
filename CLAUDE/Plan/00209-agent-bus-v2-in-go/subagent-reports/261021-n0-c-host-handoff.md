# Plan 00209 N0 area C: handoff after review round 1

The area C designer stopped at its context budget. This file is for the agent that continues.

## State

- Worktree `/workspace/untracked/worktrees/bus-v2-n0-c-host`, branch `feat/bus-v2-n0-c-host`, tip
  `51a42375` or later. Never push, never run `qa-all.bash` or Ansible; `./scripts/qa-changed.bash`
  before each commit; commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- The design: `CLAUDE/Plan/00209-agent-bus-v2-in-go/DESIGN-v2-C-host.md`. The draft is complete
  (sections 1-8). Round 1 review: `subagent-reports/261021-n0-c-host-review.md`, FIX-FIRST, 24
  findings (2 blocker, 9 major, 13 minor). Rules: `/workspace/untracked/scratch/n0-designer-common.txt`;
  frame: `DESIGN-v2-FRAME.md`. Research digests: `untracked/scratch/c-digest-{1..4}-*.md` in the worktree.
- Already done for round 1 (commit `51a42375`): placeholder table completed (finding 21); source key
  `L` renamed `LR` in the source-key paragraph and in rows C17, C30, C31, C32 (finding 20, partly);
  C11 restricted to the board (18); C18 cites A's grammar and says "a restore included" (20, 6);
  C25 carries the host `cc` caveat pointing at a new risk K12 (8); C26 reworded to "live or
  restorable session holds"; C30 says a coordinator can be declared before its member exists (5);
  C36 says a backup comes before any restart that changes binary or config (2); new C41 (nothing a
  container wrote is trusted or handed on), C42 (desired membership, R31/R32), C43 (disconnected
  notice, R29) (1, 7); section 1 says "sibling ccy session".

## Left to do: the fix plan, finding by finding

Still to rename `L ` to `LR ` in: the C21 row ("L 6 item 8"), and text lines with "L 6 item 6",
"L 6 item 8", "(R33, R48, L 1, L 6)", "(L 6 item 4)" (drop with finding 15), "L 6 item 2",
"L 7 item 4", "L 6 item 11". `grep -n -E 'L [0-9]'` finds them.

01. **Blocker, seat release (3.7.1-3.7.3).** Redesign: the authoritative bundle (`seat.json`,
    `token`) lives only in the pool, never mounted: `pool/<checkout key>/<team>/<seat>/`, plus a
    `held.json` (session, since) written by the owner process under the allocation lock = the
    holding record (replaces "moved into the session dir"). Take = host creates
    `sessions/<session>/<team>/` with `mkdirat` (EEXIST refused, named), opens it
    `O_DIRECTORY|O_NOFOLLOW`, checks it is empty and owner-owned, writes copies of `token` then
    `seat.json` with `openat(O_EXCL|O_NOFOLLOW)` 0600 relative to that fd. Release = keeper's seat
    lock free, then the session's seat directory is moved out to `released/<session>/<team>/` (a
    quarantine outside the pool, pruned; its `token` copy unlinked), never back into the pool.
    Member state does NOT travel between sessions (new decision DC15: (a) only the host-held bundle
    travels, state archived, default; (b) state travels after a whole-tree check, rejected because
    inbox content could inject into the next session). Add assumption on B: B resumes from
    server-side state (receipts) and drains its outbox before releasing the seat lock. New decision
    DC16: token reused across sessions (a, default; a released hostile session could keep a copy,
    stated as risk) vs reissue on every take (b, a password per session start). Tests row "seats":
    planted destination, symlink, non-empty dir, release to quarantine.
02. **Blocker, binary swap (3.2.3, 3.5.1 step 4, 6.1).** Base unit has NO `ExecStart=`; the per-team
    drop-in sets `ExecStart=<v2-lib>/tuwunel-<version>` (so a unit without its drop-in cannot start
    at all). The binary changes only when `team apply` rewrites the drop-in, after its backup. Old
    versions pruned by team apply/remove only when no drop-in names them and they are not the pin.
    Fix the 6.1 row about the pre-swap backup (now true: every restart onto a new binary is preceded
    by that team's backup). VM-lab test: a scoped play leaves unscoped teams' drop-ins on the old
    version.
03. **Major, play order (3.5.1).** Build `<v2-host>` first as the login user; run the freshly built
    binary's `declare check` unprivileged (move `declare check` to the "anyone" caller class in 3.3);
    then the root tasks. Install step comes before any `<v2-host>` root step.
04. **Major, LXC absent order (3.5.1 steps 9-11).** Order: `lxc apply --phase absent`, then
    `team remove` for absent teams, then `team apply`, then `lxc apply --phase present`.
05. **Major, coordinator bring-up (3.6.4, 3.6.2 step 5, 3.9.2 step 6, DC6).** `role apply` never
    defers a demotion; a promotion whose handle is not yet in the roster is recorded as
    `coordinator_wanted` in the roster, shown as a status-record problem with its fix, and done at
    the end of the `member add|return` or `lxc apply` that creates that handle. Not a skip: a
    recorded pending state. Put it in DC6's text. VM-lab cases: a new team with an LXC coordinator in
    one play; a coordinator declared before its ccy member exists.
06. **Major, restore roster (3.12.2).** Restore merges: `counters` = max(current, backup); retired =
    union (re-deactivate in bootstrap); handles in the current roster but not the backup's become
    `lost` (their accounts are gone; `return` recreates); active handles whose `token_ms` is after
    the backup's time become "needs reissue". The `ctl/` swap is done by the `<v2-ctl>` child with
    the tar's content passed on its pipe (root never writes `ctl/`). Add to tests and H-C5.
07. **Major, hot control and notices (new subsection 3.7.5).** Per ccy session two mounts:
    `sessions/<session>/` read-write at `<v2-seat-mount>` (seat copies and member state) and
    `control/<session>/` read-only at `<v2-ctl-mount>` holding `desired.json`
    `{v, generation, written_ms, seats:[{team, seat}], farewell:[{team, seat, reason}]}`, written by
    ccy-teams after the session record (00203 H5). Active team = seat present AND desired (R31).
    Leave = farewell entry; B's keeper posts goodbye, drains, releases the seat lock; then the host
    releases (item 1). Disconnected (R29, C43): when the host releases a seat whose session ended
    without a goodbye, the host runs B's `<v2-bin> seat disconnected` with the POOL bundle (never the
    container copy), once per claim; whether a goodbye was said is judged from the room on the
    server, not from container-written state. B owns message content (A) and the keeper side; list
    these as assumptions on B. Add C42/C43 to the sources used.
08. **Major, C25 caveat (done) plus assumptions on B and D**: neither `<v2-seats>` nor the owner
    runtime lock directory may lie under any path ccy mounts. Add risk K12: a host `cc` session reads
    every seat (refusing a coordinator seat while a host-kind member runs in that checkout is the
    option offered; default: state it, as for Element).
09. **Major, DynamicUser (3.2.3, 3.12.2).** Add `User=<v2-hs>-%i` to the base unit and to the restore
    transient unit (same dynamic uid, no chown); `<v2-hs>` at most 6 characters so the name fits in
    31 (team names are at most 24). Transient unit gets `SocketBindAllow=tcp:<staging port>` and
    loopback `IPAddressAllow`. H-C3/H-C5 check the shared uid and no chown.
10. **Major, D-Bus and modules.** New decision (renumber as needed, e.g. DC17 "systemd and YAML
    mechanisms"): (a) add `github.com/godbus/dbus/v5` and `go.yaml.in/yaml/v3` to the production
    module `src-go/go.mod` with Plan 00204 allowlist rows, use D-Bus for host systemd (health unit
    properties, `KillUnit` main SIGUSR2, the board's `ListUnitsByPatterns`) and the guest's private
    socket (godbus p2p dial plus auth, no Hello) - default; (b) `systemctl show -p` key=value blocks
    and `systemctl kill --kill-whom=main` through `execx` by exit status, and DC13 falls back to
    "start at next container start". 3.13's board row then uses `ListUnitsByPatterns` (drop the
    `--output=json` claim). Rewrite DC12's reason: firewall changes are rare and the CLI judged by
    exit status is enough (not "no new dependency").
11. **Major, server_name window (3.5.2).** Step 4 writes `root/<team>/server-name` before the first
    start; step 2 checks against it. Table test.
12. **Minor, health lock (3.4, 3.11.2).** The tick reads the view without the lock; takes the team
    lock without waiting only to restart and to write; writes only if the roster `generation` (add
    to the roster schema, raised on every change) is unchanged since the read. The read-only child
    never rolls `pending` forward.
13. **Minor, restart vs reload (3.5.2 step 7).** `units.json` splits a restart set (binary, config,
    base unit, `ExecStart`) from a reload set (`IPAddressAllow`, `SocketBindAllow`, `MemoryHigh`,
    `After=`/`Wants=`): reload set changes need only `daemon-reload`.
14. **Minor, lxc-attach (3.9).** `--clear-env --set-var HOME= USER= LOGNAME= PATH=` from the host's
    `/etc/passwd` read; H-C10 checks that `-u`/`-g` drop supplementary groups. Keep v1's pre-check
    for `service: true` (container config `lxc.start.auto = 1`, systemd as init, tmux in the guest),
    read host-side.
15. **Minor, one read path (3.9.2 step 3).** Read every running container through
    `/proc/<init pid>/root` (drop the store table and the bind-mount re-implementation); stopped
    containers are not read at all (record only). `lxc-add` of a stopped container defers. Opens use
    `O_NONBLOCK` and `fstat` regular-file checks. C27 for LXC: the host probes the guest seat lock
    through `/proc/<pid>/root`.
16. **Minor, kit retention (3.9.2 step 4, C33).** A digest is deleted only when no process of the
    guest (host `/proc`, filtered by the container's cgroup) runs a binary from it; same rule for the
    host `<v2-kit>` with the owner's processes.
17. **Minor, firewall and sockets (3.2.5, 3.5.2).** `--query-rich-rule` 0 = present, 1 = absent, any
    other code fails; query permanent and runtime separately. Bound addresses and port checks read
    `/proc/net/tcp` and `/proc/net/tcp6` in-process (no `ss`).
18. Done (C11).
19. **Minor, host_vars scope (3.8).** The Go reader decodes the document to `yaml.Node`, applies the
    strict rules only to the subtrees of the v2 top-level keys, ignores every other key whatever it
    holds (vault tags included); a host_vars directory is refused by name.
20. **Minor, seams.** 3.7.4: cite A for the handle grammar and type set; keep only seat numbering
    and allocation. Mark `trust:` shape and the room names in the status-record example as
    assumptions on A / examples. `LR` rename (above).
21. Done (placeholders).
22. **Minor, backup unit (3.12.1).** Give the backup service's sandbox (like the health unit's) and
    say that the backup inside `team apply` runs under the lock the apply already holds (an
    internal function taking the held lock, never a second `flock`).
23. **Minor, resident cost (3.15).** State the tick's calls as an order of growth (teams x rooms x
    (members + humans) per minute) and a process plus a child per team per tick; per member the
    homeserver holds a long-poll sync (memory in the homeserver); drop "three or so teams"; H-C1
    reports per team and per member.
24. **Minor, registration secret (3.2.2).** Drop `ctl/<team>/registration.secret`; root passes the
    secret to the child on its pipe only when registering the admin.

## Then

Commit, spawn the round 2 adversarial reviewer (model opus) over the file and the frame, writing to
`subagent-reports/261021-n0-c-host-review-2.md`; fix and loop, at most 3 rounds in all. Then send
the lead a final message under 1200 characters: branch, tip, file, verdict, the decisions defaulted
(DC1-DC17), and the assumptions on areas A, B and D (section 7).
