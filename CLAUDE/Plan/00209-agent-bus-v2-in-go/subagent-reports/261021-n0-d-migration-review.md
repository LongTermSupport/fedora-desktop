Verdict: FIX-FIRST

# Plan 00209 N0 area D: adversarial review, round 1

Reviewed: `DESIGN-v2-D-migration.md` at `608cbbcb`, against `DESIGN-v2-FRAME.md`, the common
designer rules, CLAUDE.md, the two RESEARCH-v1 digests, and the v1 code itself. Paths below are
relative to the worktree. `D:n` means a line of `DESIGN-v2-D-migration.md`.

Count: 3 blockers, 10 majors, 8 minors.

## Blockers

### 1. Rollback cannot give ccy seats or LXC members a working token: `agent-bus return` does not exist, and neither path the design names re-mints a bundle

- **Evidence.**
  - D:541, D:595-597 and D:520 name the verb `agent-bus return`. v1 has no such verb. The return
    route is `add-member <team> --seat <seat> --repo --host --type --role --address`. It writes
    the new bundle as a tar to `--out` (`helpers/agent_bus/cli.py:87-103, 298-299`; the park help
    text says "add-member --seat returns it").
  - D:595-597 says ccy seats "get their new bundle by v1's seat take when their session is
    restarted". `seat take` only creates seats **that have no directory**, and does nothing for a
    seat that exists (`helpers/agent_bus/checkout.py:224-252`: "none for a seat that exists").
  - The fence parks with `park-member`, not with `seat remove`, so the seat directory is still
    there, holding a bundle whose token has been revoked (`admin.py:779-789`: the password is
    reset with `logout_devices`).
  - So after R the session restarts on a dead token. Its first call gets 401, which is exit 8,
    and TS 7 row 3 calls that permanent.
  - LXC has the same problem. The member is still declared in `agent_bus_lxc_members` and its
    bundle is in place, so v1's play sees nothing to do. TS 7 row 5 says a lost bundle needs an
    absent-then-present cycle.
- **Why this is a blocker.** The fix needs a v1 bundle written into the v1 seat dir, or into the
  guest. That contradicts D:613-615 ("No v1 file is written"), D17, and the 4.1 part 2 analyzer
  (the adapter writes no file). As written, D8 ("rollback restores it") is false for every ccy
  and LXC member.
- **Fix required.**
  - Use the real verb (`add-member --seat` with the member's recorded repo, host, type, role and
    address).
  - Say who places the returned bundle and how: a v1-owned path, as the user for ccy, and v1's
    play with an absent/present cycle for LXC.
  - Restate D17 and the analyzer, so that v1 writes made **through v1's own verbs** are allowed
    and listed.
  - Correct 5.7's safety argument.
  - Add a P1 rollback assertion that every member's next v1 send succeeds after R, not only that
    `state/` is byte-identical.

### 2. The fence destroys the cut point it later needs for remote and unreadable members, and the device check names a verb that does not exist

- **Evidence.**
  - D:368 takes the cut point for a member whose state cannot be read from "the member's device
    `last_seen_ts` ... from the v1 admin device list".
  - `park-member` resets the password with `logout_devices` (`admin.py:779-789`), which deletes
    those devices. M2 (D:503) parks before M3 (D:504) reads.
  - v1's only device listing is `human devices`. It resolves a **human** only
    (`admin.py:904-907`, which calls `_human`), and there is no device verb for an agent member
    (`cli.py:288-313`).
  - The M2 fail-fast check "a v1 member's device still syncs (checked through the admin device
    list)" (D:503) therefore has no verb behind it. The adapter's fixed list (D:528-542) gives it
    no admin-API route either: the admin token is allowed only for `/messages` paging.
- **Result.** For remote members (5.8) and every `server-only` member, item 1 (sent but not yet
  synced) has no defined cut point. That is exactly the "neither carried nor reported" window
  that D9 forbids.
- **Fix required.**
  - Record every member's cut point (the device `last_seen_ts`, or better the last event the
    member is known to have read) in M0 or in M2 before the park, through a v1 read verb that
    exists or is added and listed as a v1 change.
  - Otherwise define the cut as "everything since the member's last v1 send", flagged
    `possible_duplicate`.
  - Replace the M2 device check with one that can be implemented. For example, check that the
    `park-member` output reports the token revoked, and that a request with the old bundle's
    token gets 401.

### 3. Commit is not one atomic act that every reader sees, and the linked-set commit lives in a file ccy cannot read

- **Evidence.**
  - D:465 says commit is the rename of root's `/var/lib/teamchat-admin/<team>/migration/state.json`.
  - D:236-238 and D:241 say ccy-teams and the ccy launcher decide the live system from
    `/var/lib/teamchat-status/<team>.json` `migration.phase`. That is a second file, published
    after the commit (D:453-454).
  - A crash between the two leaves the root record at `committed` and the status record at v1.
    M6's restarts (D:696-698) would then resolve to v1, which is fenced and has revoked tokens.
  - For linked sets, D:635-637 says "its live system is read from the set record"
    `/var/lib/teamchat-admin/_sets/<set-id>.json`. That record is root 0700 (D:165), so the
    launcher, running as the desktop user, cannot read it.
  - Nothing says what a re-run of M5 does when it finds the two records disagree.
- **Fix required.**
  - Name ONE record as the source of truth for "which system is live". It must be readable
    without sudo by the launcher and ccy-teams, and committed by one rename. For a set, that is
    one set record in the status directory that each team's status record points to.
  - Make every other copy derived from it, and have M5 and M6 refuse to go on while the copies
    disagree.
  - State the crash-between-files recovery explicitly, and add it to P1's fault injection.

## Majors

### 4. `human lock` is not shown to end a human's existing Element sessions, so the fence may not stop human posts

- **Evidence.**
  - `human_lock` only sets the `locked` flag (`admin.py:899-901`).
  - v1's own docs say "For a lost phone: lock, then log out all sessions" (`docs/agent-bus.md:836-841`).
    That implies lock alone does not end existing tokens. Tuwunel's behaviour for a locked user
    with a live token is not evidenced in RESEARCH-host or TS.
- **Effect.**
  - If existing sessions survive the lock, humans keep posting into v1 rooms during the fence.
  - M3 stops, which is loud. But between M5 (commit) and M8 (rooms made read-only), a human post
    into a v1 room is neither carried nor reported: A8 runs at M7, and nothing checks M7 to M8.
  - M3 also contradicts itself on this point. "Waits until no v1 event has appeared for 60 s"
    (D:504), against "a v1 event appears after the fence ... stop" (same row). Which events are
    absorbed and which stop the run is undefined.
- **Fix required.**
  - Prove what lock does to live tokens on Tuwunel, with a pilot probe added to P1.
  - Or make the fence `lock` plus `logout-all`, and state the cost: on rollback, the human needs
    a new password through the ccy-teams login flow.
  - Make the M3 rule precise.
  - Add a "no v1 event after `v1_last_event`" check at the start of M8, carrying or reporting
    anything found.

### 5. v1's own session-end and host notices during the fence are not covered

- **Evidence.**
  - v1 posts on its own, outside any agent send:
    - the SessionEnd goodbye (`helpers/pingbus/cli.py:1314-1323`, `hooks.py` `post_goodbye`);
    - `seat disconnected`, run by the ccy launcher once the container has gone
      (`cli.py:2850-2880`; protocol table `docs/agent-bus-protocol.md:946`);
    - `seat farewell` (`cli.py:2838-2846`).
  - M6 restarts every ccy session while its token is revoked. So every goodbye fails, the
    launcher's `seat disconnected` exits non-zero, and `disconnect.log` records failures.
  - The design never mentions these. It does not say whether a failed disconnected step blocks
    the ccy supervisor's relaunch, which is the M6 path.
  - It does not say whether `seat.lock` is left un-emptied, so that after a rollback a later
    `seat disconnected` posts a stale "disconnected" into v1, which is an event after the fence.
  - It does not say whether ccy-teams' hot path treats an owed farewell failure as blocking
    (`src-go/internal/teams/hot/add_test.go:384-392`: "the farewell stays owed").
- **Fix required.**
  - Add a row for each of the three notices to 5.2. Give each a fate, for example: expected to
    fail during the fence, listed in the report, and not carried.
  - Prove in P1 that a failed goodbye or disconnected notice does not block the M6 relaunch.
  - State what R does about owed notices.

### 6. Delivered-but-unconfirmed items in a running ccy container have no read method

- **Evidence.**
  - D:371 says the `inject/` records live in the session home, and that a ccy home "is gone once
    the container exits".
  - At M3, though, the containers are still running: they restart only in M6. The ccy home is
    `/tmp/pingbus-home` inside a rootless podman container (INV 5). D gives no route for root's
    `teamchat-admin` to read it, so these items are silently skipped while they are readable.
  - M6 then destroys them.
- **Fix required.**
  - Specify the read: for example, ccy-teams runs as the user and execs into the container, or
    the v1 kit's own `status`/`inbox` verb is run there.
  - Or name the gap per running session in the report, at M3, not only after the session ended.

### 7. Root reads of agent-writable and guest-controlled trees can follow symlinks

- **Evidence.**
  - D:369 and D:534 have `ReadMemberState` read ccy seat state in `<checkout>` and LXC member
    state "from the container's rootfs as root".
  - A ccy session can write its checkout (the reason v1 keeps locks out of it, LESSON 9), and a
    guest controls its own rootfs. An absolute symlink in a guest rootfs resolves against the
    **host** when it is opened from the host.
  - v1's rule is that root never writes in the checkout (D61, `checkout.py:224-231`). D adds root
    reads with no rule.
  - Risks: reading host files into the carry plan, logs or error text, and a FIFO or device node
    hanging root.
- **Fix required.** Require `openat2(RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS)` (or `O_NOFOLLOW` with
  a dirfd walk), regular files only, a size cap, and no file content in error text. Better, read
  ccy seat state as the owning user, from ccy-teams rather than root. Add a red test with a
  symlinked `inbox/` entry.

### 8. The static gate fails on v2's own units and validator; its exemption set is wrong

- **Evidence.**
  - 4.1 part 1 (D:269-277) forbids every v1 name in `files/**/teamchat*`,
    `playbooks/imports/play-teamchat*.yml` and `src-go/internal/teamchat/` except `migrate/v1`.
    Yet the design requires v1 names in v2 code:
    - D:294-295: v2 units carry `InaccessiblePaths=-/var/lib/agent-bus ...`;
    - D:222: C's port validator reads `agent_bus_teams`;
    - D:185: the LXC subnet logic mirrors v1;
    - D:507 and D:711-714: M6's v2 play and ccy-teams edit `agent_bus_lxc_members`;
    - D:760: the v2 declaration check refuses `agent_bus_*`.
  - ccy-teams (`src-go/internal/teams/`) and the ccy launcher must name both systems, and are not
    in the scope at all.
- **Fix required.**
  - Give the deliberate v1 references one named home each: a generated `v1-paths` constant in
    `migrate/v1` that the unit template and validator import, or an explicit per-line allowlist
    with reasons.
  - Put `src-go/internal/teams` and the ccy launcher in scope, with their allowed v1 references
    listed.
  - Add a red fixture proving that the units still pass.

### 9. v2 changes files v1 reads: the checkout's `.claude/ccy/.gitignore` and the ccy launcher

- **Evidence.**
  - D:191 puts v2 seats under `.claude/ccy/teamchat/`, "ignored by the same `.claude/ccy/.gitignore`
    mechanism".
  - That file is written by v1's `cc-teams.bash:103-115` (content `pingbus/`), and v1's
    `seat take` checks it (`checkout.py:599`).
  - Changing it changes a file v1 reads. The 4.3 footprint has no checkout in its manifest, so
    the proof cannot catch this.
  - Likewise, 3.9 changes the ccy launcher and entrypoint, which v1 sessions use, and `cc --teams`
    (`cc-teams.bash`) has no v2 resolution at all.
- **Fix required.**
  - Either give v2 its own ignore file (for example `.claude/ccy/teamchat/.gitignore` containing
    `*`), or state the exact edit and prove v1's check still passes.
  - Add the checkouts' `.claude/ccy/` trees, the ccy image's v1 files, and v1 launcher behaviour
    (v1 acceptance through ccy and through `cc --teams`) to manifest A/B.
  - Say how `cc --teams` resolves systems, or that it is refused for v2 teams until B or C
    defines it.

### 10. The footprint diff has no LXC guest side, and the image comparison is undefined

- **Evidence.**
  - 4.3 (D:308-329) lists host objects only.
  - The v2 LXC apply writes into the same guest as v1 (`/usr/local/bin`, `/usr/local/lib`, the
    user's `~/.config`, guest units). v1's guest files are `~/.config/pingbus/{env,<team>/}`,
    `/usr/local/share/agent-bus/kit`, `/usr/local/bin/{pingbus,agent-bus-claude}` and
    `agent-bus-claude-<user>.service` (INV 8).
  - "podman image inspect" of an image that the v2 install rebuilds always differs.
- **Fix required.**
  - Add a guest manifest: v1's guest paths, the unit, and the tmux socket.
  - Compare the image by a listing and sha256 of `/opt/claude-yolo/optional/agent-bus/**` and
    the entrypoint's v1 code paths, not by image metadata.

### 11. Declaring a move in host_vars lets a plain v2 play run build the team outside the migration

- **Evidence.**
  - D:652-655: ccy-teams writes a `teamchat_teams` entry with `from_v1: true` when the owner
    picks "move".
  - D:658-659: the plan's `deploy.bash` runs `play-teamchat.yml`, which installs every declared
    team.
  - D:502: M1 writes the same entry and then runs the play.
  - So any v2 software deploy between the declaration and M0 stages the team's server with no
    M0 preflight, no snapshot and no migration state. A later M1 then meets an existing team.
- **Fix required.**
  - Keep migration intent out of `teamchat_teams`, for example in a separate
    `teamchat_migrations` list or a ccy-teams record.
  - Or make `play-teamchat.yml` refuse to build a `from_v1` team unless the migration record
    shows `staged`. Test it.

### 12. The v2 Element profile name can collide with a v1 profile

- **Evidence.**
  - v1's profile is `--profile <team>`, directory `Element-<team>` (D:212, INV 7). The v1 team
    grammar `^[a-z][a-z0-9-]{0,23}$` (D:177) allows a v1 team named `teamchat-x`, whose profile
    is `Element-teamchat-x`. That is exactly v2's profile for team `x`.
  - The "no prefix overlap" rule of 3.1 (D:102) misses names derived from the team name.
- **Fix required.**
  - Make the v2 validator refuse to create or migrate a team `x` while a v1 team `teamchat-x`
    exists.
  - Or use a separator that v1 team names cannot contain, for example `Element-teamchat.<team>`.
  - Extend the 4.1 part 3 unit test to cover names derived from `<team>`.

### 13. An open ping can get a `moved` notice that contradicts an answer already carried

- **Evidence.** D:372 settles every outbox entry whose targets are not in `answered`/`reported`.
  Suppose the target answered on v1 before the fence, and the sender had not yet synced the
  answer. The answer is then an item-1 carry to the sender, and the sender also gets "your ping
  was not answered before the team moved". The carry plan has no cross-check.
- **Fix required.** Build the settle set after the carry set, and drop the `moved` notice for any
  outbox target whose `ack`/`nack`/`done`/`blocked` reply to that ping is among the carried items.
  Add the case to P1's seeded set.

## Minors

### 14. `seat remove` does not run through `sudo agent-bus`

M8 (D:509) runs `seat remove` "through v1's own" verb, and 5.5 (D:537) lists v1 verbs as
`sudo agent-bus ...`. But `seat remove` runs **as the user, inside each checkout**, and deletes
the whole seat directory, `state/` included (`cli.py:337-338, 363-365`). It also refuses a held
seat. Fix:

- name it in the adapter's fixed list as a per-checkout user call;
- say that it destroys v1 member state, which is fine only after the export and after A8;
- state what happens to LXC guests' v1 bundles and state.

### 15. Two confirmations inside the bus-less window

5.10 (D:647-649) asks for one confirmation, before M2, and then runs without stopping. But M3
(D:504) asks again "unless run with `--yes`", inside the bus-less window. Pick one.

### 16. Rollback from `staged`, and rollback re-runs

R from `staged` (D:592) has no fence record and no M3 capture, so step 3 (D:599) has nothing to
compare against. A re-run of R calls `add-member --seat` again, which mints another token and
orphans the previous bundle. Fix: define R per phase, and make the return step idempotent, for
example by checking the registry's `parked` list first.

### 17. Team names over 23 characters

A team name longer than 23 characters cannot migrate under per-team users (D:177-179), yet D:179
says a migrated team keeps its v1 name. M0 must refuse such a team with a named reason, or the
design must require the shared-user option.

### 18. Missing name categories

These have no row in section 3 (INV 0, 2, 4, 6):

- the status-line cache and sessions dir (`/tmp/pingbus-sessions-<uid>`, `statusline-cache.json`);
- v2's stamp group record (D:166 says `<stamp group>` but not whose record: v1's is
  `/etc/agent-bus/stamp-group`, and v2 must not read it);
- the journald/SyslogIdentifier names;
- the units' RuntimeDirectory;
- logrotate, if any.

### 19. Seam overreach

5.2.1 (D:384-386) states the trust rule for `teamchat.carried`, which A owns. 3.8 specifies C's
validator rules, and 4.2 fixes C's unit hardening. Phrase each as an assumption on A or C, which
section 12 partly does already, and remove the normative restatement.

### 20. Inbox items delivered after M3

An inbox item that v1 delivers between M3 and M6 (a session still running locally) is carried
without `possible_duplicate`. The design argues that v1 must re-fetch first, so this cannot
happen. Either state that argument with a code citation, or flag inbox carries as
`possible_duplicate` when the session was running at M3.

### 21. Alternatives not considered

Only one design is given for the commit mechanism, the session move, and the coexistence proof's
structure. The bar is at least two per part. Add at least a rejected alternative for commit, for
example an atomic flip of the host_vars declaration against a status-record rename, with the
reason it was rejected.

## Checked and found sound

These need no change:

- The name has two alternatives, with reasons, as OD-D1 with a default.
- There are no time estimates.
- There are no install-specific names. `teamchat-pilot` and `migration-pilot` are generic.
- Relay mode is not reintroduced.
- `migrate-declared.bash` is a plan-folder script, so it is exempt from the bash limits, and the
  gate wrapper execs Go.
- These v1 verbs exist: `park-member`, `list`, `human lock/unlock/devices/password`,
  `agent-bus-install backup-now` (`agent-bus-install:124, 962, 1174`) and `seat remove`.
- The meta-deploy route is correct in principle.
- The OD-D2 refusal reasoning is sound.
