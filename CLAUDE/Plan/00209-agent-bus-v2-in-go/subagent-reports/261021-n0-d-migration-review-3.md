Verdict: FIX-FIRST

# Plan 00209 N0 area D: adversarial review, round 3

Reviewed: `DESIGN-v2-D-migration.md` at `17640d8a`, against `DESIGN-v2-FRAME.md`, the common
designer rules, CLAUDE.md and the v1 source. `D:n` is a line of the design. Other paths are
relative to the worktree.

Count: 2 blockers, 4 majors, 8 minors. Most of round 2 is fixed, chiefly by replacing the park
fence with a network fence. Both blockers come from that change. The fence verb cannot be built
where the design puts it. And rollback's state check now runs after the fence is lifted, so it
races the watchers it has just released.

## Round 2 status

| #   | Finding                                     | Status | Evidence                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| --- | ------------------------------------------- | ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `ReturnSeat` cannot write into the seat dir | FIXED  | Fixed by the redesign. Nothing is parked before commit, so nothing is returned: R step 1 says "nothing to re-mint" (D:759-761). The rejection of park cites the wrapper refusal (D:643-645).                                                                                                                                                                                                                                                                                    |
| 2   | LXC return deletes guest `state/`           | FIXED  | Same redesign. The guest's v1 directory is removed only in M6, after commit (D:627, D:897-900), and A8 allows that change (D:856).                                                                                                                                                                                                                                                                                                                                              |
| 3   | Humans have no v1 password after logout     | FIXED  | R step 2 (D:762-764) and M8 (5) (D:629) issue `agent-bus human password`. The verb is in the 5.5 list (D:693), and RK10 is corrected (D:991).                                                                                                                                                                                                                                                                                                                                   |
| 4   | Crash inside M2 leaves no record            | FIXED  | `fence.json` is written as an intent before M2's first act and updated per act (D:535-536, D:623 (a)). R reads `fence.json`, the fence status and `agent-bus list` from any phase (D:755-757, D:780). P1 rolls back from a crash inside M2 (D:921).                                                                                                                                                                                                                             |
| 5   | A8 can never pass with an LXC member        | FIXED  | The allowed difference is generated from M6's `log.jsonl` (D:534, D:856). v1's park changes no room event (`admin.py:779-789`; parked state is not in `_team_record`, `admin.py:530-547`), so A8's "no event after `v1_last_event`" also holds.                                                                                                                                                                                                                                 |
| 6   | No watcher after R                          | FIXED  | The fence leaves tokens alone, so no 401 and no final exit 8. Watchers retry transient faults (verified below). R step 4 checks presence and lists a restart command (D:770-773).                                                                                                                                                                                                                                                                                               |
| 7   | Row 7 timing; launcher skip                 | FIXED  | The rows are moved to the M7 report (D:442, D:510-511, D:628). The launcher skip is gone (D:875-879, D:1050-1058).                                                                                                                                                                                                                                                                                                                                                              |
| 8   | Wrong re-fetch citation; local TIMEOUT path | PARTLY | The citation is corrected to `cli.py:1348-1417` (D:427). The TIMEOUT path is stated, but only for `_deliver`, which is `recv`'s path. A running ccy session delivers through the watcher's injection path, `_candidates` and `_send_delivery` (`cli.py:1907-1921, 2027-2031`), which marks the same `reported` entries. The P1 seed round 2 asked for, a deadline that passes during the fence in a running session, is not in P1 (D:920 seeds only static before/after pings). |
| 9   | v2-native rule; two set records             | PARTLY | The set pointer is now the only route (D:522-525), and v2-native is read from the status directory (D:248-250). But "v2-native = no set pointer" now contradicts R step 5, which removes the pointer (new finding 3).                                                                                                                                                                                                                                                           |
| 10  | R phase range inconsistent                  | FIXED  | The range is "preflighted to carried" throughout (D:754-755, D:779-780).                                                                                                                                                                                                                                                                                                                                                                                                        |
| 11  | `seat remove` needs the server and sudo     | FIXED  | M8 (2) states the sudo park, that a second park is harmless, and the ordering before any archive stop (D:629).                                                                                                                                                                                                                                                                                                                                                                  |
| 12  | `WhoAmI` of LXC tokens                      | FIXED  | Obsolete. The fence proof no longer uses member tokens: it uses a connection attempt and the admin's `whoami` over loopback (D:623 (f), D:677).                                                                                                                                                                                                                                                                                                                                 |
| 13  | Commit wording; lock                        | FIXED  | A temporary file is renamed over the old record, and the checks and the rename both happen under the set lock (D:527-529, D:555-559).                                                                                                                                                                                                                                                                                                                                           |

## The v1 facts the design relies on (task 6)

- **The watcher retries a network fault: VERIFIED.**
  - A refused connection, a timeout or an `OSError` becomes `Unreachable(transient=True)`
    (`helpers/pingbus/matrix.py:221-222`, and 136-137 for a cut read).
  - `_permanent` returns False for a transient `MatrixError` (`cli.py:1722-1728`), and
    `_watch_poll` retries with a backoff "from the stored sync token" (`cli.py:1793-1805`).
  - So both a dropped packet (BPF or nft `drop`) and a TCP reset are survived. The fence's premise
    holds.
- **"A local TIMEOUT is the only state change during the fence": TRUE in effect, CITED WRONG,
  and NOT COMPLETE AS STATED.**
  - Both delivery paths re-fetch inbox items before they emit TIMEOUTs.
    - `_deliver` loops over the items first (`cli.py:1397-1418`).
    - `_candidates` sorts items before TIMEOUTs (`cli.py:1918-1922`).
    - A re-fetch raises `Unreachable`; only `NotFound` is caught (`cli.py:1358-1361`). That aborts
      the call before any TIMEOUT. A TIMEOUT is therefore emitted only when the member's inbox
      is empty.
  - `cmd_send` calls the server (`verify_room`, `cli.py:1036`) before `admit_send` and the outbox
    write (`cli.py:1045-1050`), so a fenced send changes nothing.
  - The live path for ccy sessions is `_send_delivery` (`cli.py:2027-2031`), and the design
    should cite it.
  - Two state changes are reachable during the fence that are not member-local:
    - a new v1 seat, or a hot add or remove, through the unfenced admin loopback (new finding 5);
    - the owner's own routine v1 play (new finding 6).
- **Does systemd apply `IPAddressAllow=` to a running unit? The deferral is UNDER-EVIDENCED.**
  - The fallback is stated: a restart is acceptable (D:704-706).
  - But v1's own installer records the opposite assumption to the design's default. It says
    "the running homeserver only takes its new sandbox on a restart"
    (`files/usr/local/sbin/agent-bus-install:820-821`), and it restarts on every drop-in change
    (`:816`).
  - v1's restore already runs the server loopback-only through a restart with
    `--property=IPAddressAllow=127.0.0.1/32 ::1/128` (`:1063-1072`). That is a precedent for the
    mechanism, applied at start.
  - The design should default to a restart, and treat live apply as an optimisation for P1 to
    prove. The restart is cheap, because watchers survive it (E25).
  - It must also rule out `systemctl set-property`. That writes `50-IPAddressAllow.conf`, which
    sorts before `network.conf`. On the next `daemon-reload` (any dnf transaction, any play),
    `network.conf` would append v1's allow list again and lift the fence silently.

## The fence: a new v1 verb, or no v1 change at all (task 2)

**The new verb is not acceptable as written.**

- **It breaks a HARD RULE.** `files/usr/local/sbin/agent-bus-install` is bash on the ratchet
  allowlist at 988 code lines (`scripts/bash-limits.allowlist:51`). It "may shrink and be ported,
  never grow". Adding `fence|unfence` to it fails `qa-bash-limits` (new finding 1).
- **It goes against the frame.**
  - The installer is shared by every running v1 team. So a migration feature ships new code
    under teams that must "keep working untouched".
  - The frame's sentence "nothing v1 reads or writes is changed" is about the v2 install. A
    migration has to act on the one team being moved, and it does so through v1's existing verbs
    (lock, logout, park, play). Changing v1's software is a different act, and it reaches every
    team.
- **`rooms-read-only` has the same problem in Python.**
  - `agent-bus.pyz` is a standalone Python CLI, and CLAUDE.md says such a CLI is ported to Go
    when it is next reworked.
  - The verb also does not hold, because v1 undoes it (new finding 4).

The options, each against the cost the brief asks about:

| Option                                                                                                                                                         | v1 changed?                                                              | Member state | Watchers                                        | Rollback                                                                                                                                    | Human lockout                                                                               | Verdict                                                                                                                                                                                                                                                               |
| -------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------ | ------------ | ----------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| (a) The new v1 verb (the design)                                                                                                                               | Yes: shared v1 software for every team; grows a ratchet-listed bash file | kept         | survive                                         | `unfence`                                                                                                                                   | lock for all; logout and a lost password where Element is on loopback                       | **Refused** by the bash ratchet                                                                                                                                                                                                                                       |
| (b) A v2-written drop-in `zz-teamchat-fence.conf` in `agent-bus-hs@<team>.service.d/` (`IPAddressAllow=` reset, then loopback), daemon-reload and restart      | No v1 code. One new file in v1's unit directory, a narrow D17 exception  | kept         | survive the restart (E25)                       | delete the file, reload, restart                                                                                                            | as (a)                                                                                      | Works. cgroup BPF matches by address on every interface, so it also cuts the pasta traffic that arrives on `lo` with the bus address as its source. It persists across reboots, and v1's restore precedent is the same mechanism. It bends "touches nothing v1 uses". |
| (c) A v2-owned nftables table `inet teamchat_fence_<team>`: input hook, `tcp dport <v1 port>`, `ip saddr != 127.0.0.0/8` and `ip6 saddr != ::1`, then `reject` | **None.** No v1 file, unit or code                                       | kept         | survive (a reset is transient, `matrix.py:221`) | delete the table and disable the unit                                                                                                       | as (a)                                                                                      | Works, if P1 proves it survives `firewall-cmd --reload`, Docker's iptables-nft and `lxc-docker-user-iptables-reconcile.bash`. It takes effect at once, on established connections too, with no restart. Reboot persistence needs a v2 oneshot unit (cost below).      |
| (d) Only v1's own firewalld rich rules                                                                                                                         | —                                                                        | —            | —                                               | —                                                                                                                                           | —                                                                                           | **Not viable.** firewalld accepts `lo` before zone rules, and ccy seats reach the bus address through pasta as host-local traffic. Also, `sync_firewall` owns v1's rules (`agent-bus-install:588-615`).                                                               |
| (e) Stop the v1 unit                                                                                                                                           | No                                                                       | kept         | survive (connection refused is transient)       | `systemctl start`. But the unit is enabled, so a reboot lifts the fence silently unless it is masked. The health record goes red meanwhile. | **None needed.** Nothing can reach it, so there is no lock, no logout and no lost password. | Best for humans, but M3's export and event fetch, the admin `whoami` and M8 all need a server. Restarting it loopback-only for M3 brings back the problem of Element on loopback.                                                                                     |

**Recommendation: (c), with (b) as the stated fallback if P1 disproves (c).**

- (c) is the only option that keeps both the frame ("nothing v1 uses is changed") and D17
  literally, with no v1 software change and no bash growth.
- `reject` makes a fenced v1 send fail at once rather than after the 15 s request timeout
  (`matrix.py:32`), which is kinder to running agents. The fence proof then checks "refused",
  not "times out".
- The cost to state in section 8 is one oneshot `teamchat-fence@<team>.service` per migrating team
  while it is fenced. It runs `Before=agent-bus-hs@<team>.service` and is WantedBy multi-user, so
  that a reboot cannot lift the fence. A v2 unit that orders itself before a v1 unit changes
  nothing of v1. It resides only during the fence and holds no process.
- The fence verb, its status and its removal move into `teamchat-admin`. The adapter lists `Fence`
  as an `nft` call (it is not a file write, so 4.1 part 2 still holds).
- Human lockout is the same for (a), (b) and (c): lock for everyone, and logout plus a new
  password only where Element is on loopback. (e) is the only route with no lockout. Name that
  in OD form if the owner cares more about humans' passwords than about M3's reads.

## Naming and collisions (task 3)

- **`teamchat` passes** the qa-reviewer naming criterion (`.claude/agents/qa-reviewer.md:106-111`).
  It says what the system does, and it is not a metaphor or a vague word.
- **No collisions found:**
  - `git grep -i 'teamchat|team-chat|team_chat'` outside the plan finds nothing;
  - there is no `teamchat` user or group in the container's `getent`;
  - there is no `team*` binary in `/usr/bin` or `/usr/sbin` here;
  - `_sets` cannot collide with a team name (grammar `^[a-z]`);
  - `teamchat-<team>` fits 32 characters with `{0,22}`.
- **Near-collision (minor 8):** `teamchat0` and the `team` prefix sit beside Fedora's libteam
  family: `teamd`, `teamdctl`, `teamnl`, and NetworkManager's `team` connection type, whose
  devices are conventionally `team0`. A `team*` glob or an `nmcli` filter on "team" matches both.
- **Unverifiable here:** Fedora's package and user namespace. P2's `triage.bash` should assert it
  (`dnf repoquery --whatprovides`, `getent`), rather than trusting this review.

## New findings

### 1. Blocker: `agent-bus-install fence|unfence` cannot be added; it grows a ratchet-listed bash file and changes shared v1 software

- **Location:** D:656, D:690, D:699-706, D:711.
- **Problem.**
  - `agent-bus-install` is on `scripts/bash-limits.allowlist:51` (988 lines, 1156 statements,
    `assoc,data`). The rule in CLAUDE.md is "may shrink and be ported, never grow".
  - The verb also changes v1 code that every running team uses, which the frame excludes (see the
    fence analysis).
- **Fix.**
  - Adopt option (c), with (b) as the fallback. Drop the new v1 verb. Move `Fence`, `Unfence`
    and `FenceStatus` into `teamchat-admin`.
  - Restate D17: "v2 writes no v1 file and changes no v1 code". If (b) is used, add one named
    exception.
  - Update 5.3's idempotency rule, 5.4 M2 (d) and (f), 5.5, RK1, section 8 (the fence unit), and
    P1: survive `firewall-cmd --reload` and a reboot, and be removed by unfence.

### 2. Blocker: R checks member state byte for byte after it has lifted the fence, so it races the watchers it released, and P1 asserts both

- **Location:** D:759-769, D:921.
- **Problem.**
  - Step 1 lifts the fence, and every watcher syncs again. Its `sync.json` gets a new
    `next_batch` as soon as any event or presence update happens (presence renews every 300 s).
    Seeded inbox items are delivered and move to `consumed/`.
  - Step 3 then demands that `sync.json`, `inbox/` and `consumed/` match the snapshot byte for
    byte, and "stops" otherwise.
  - P1 asserts both "every seeded item is delivered by v1 exactly once" after the lift and "state
    matches the snapshot". For a live team, R alarms in the normal case.
  - Because R stops, step 5 (discard v2) never runs, and a re-run alarms again. R cannot finish.
- **Fix.**
  - Run the state check first, while the fence holds, before steps 1 and 2.
  - On a difference, print the carry plan and require an explicit owner flag
    (`--accept-state-diff`) to continue. Never leave R unfinishable.
  - In P1, assert the snapshot comparison before the lift and the deliveries after it.

### 3. Major: after a rollback the team reads as v2-native, so the launcher routes its sessions to a purged v2 team

- **Location:** D:248-250 against D:774-777 and D:523-525.
- **Problem.** 3.9 defines v2-live as a status record "created on v2 (it has no set pointer)".
  R step 5 says "the team's status record loses its set pointer". Unless purge also deletes the
  record, which is not stated, a rolled-back v1 team is then v2-live, and every relaunch of its
  sessions takes the v2 path.
- **Fix.**
  - Mark v2-native positively (`"origin": "v2"`) and never infer it from a missing field.
  - After R, keep the pointer to the rolled-back set, so that it reads as v1-live (the set is not
    `committed`).
  - Or state that `team remove --purge` deletes `<team>.json`.
  - Add a unit test of the decision function over every phase, `rolled-back` included.

### 4. Major: `rooms-read-only` is undone by v1's own play, so the archive becomes writable again

- **Location:** D:629 (3), D:697, D:707-709, D:740-742.
- **Problem.**
  - Every run of `agent-bus-install team` calls `bootstrap` (`agent-bus-install:831`), which
    calls `_sync_team`. That rewrites `m.room.power_levels` to `expected_power_levels(humans)`
    whenever they differ (`helpers/agent_bus/admin.py:584-591`, `:629-645`).
  - An archived team is still in `agent_bus_teams` until v1 retires (D:898). So any routine v1
    play reverts the archive to writable: one without `agent_bus_only_teams`, or one naming the
    team.
  - Humans have v1 passwords again after M8 (5). A post in the archive then reaches no one,
    silently, which is a D9 gap.
  - The verb is also a v1 code change (see the fence analysis).
- **Fix.** Pick one and state it:
  - (i) The read-only state is put into v1's own declaration: an `archived` flag that
    `expected_power_levels` honours. This is a listed v1 change, tested in v1's suite, with the
    frame cost said plainly.
  - (ii) The OD-D6 default becomes (b) or (c): the archive server is stopped at M8, and history
    is served by the export and the pinned `history.md` in v2.
  - (iii) The archive is accepted as writable, and the human posts in it are named as
    unreported.
  - The recommendation is (ii), because it needs no v1 change.

### 5. Major: seat changes during M2 to M5 go round the fence through v1's admin loopback

- **Location:** D:251-253, D:616-617, 5.4 M2 to M5.
- **Problem.**
  - Until commit the team is v1-live, so the launcher and ccy-teams take v1's path.
  - A new session on a new seat runs `seat take`, which calls `sudo agent-bus add-member`
    (`helpers/agent_bus/checkout.py:243-248`). That runs over loopback, which the fence leaves
    open. It creates an account and an invite after `v1_last_event`, so M3 or M5 stops the run.
  - A hot remove runs v1's `seat retire`, which deletes `sync.json` (D:633-634). R's check then
    alarms.
  - These are routine owner actions (a supervisor relaunch, or a new session), not faults.
- **Fix.**
  - The launcher and ccy-teams refuse any v1 seat creation, hot add or hot remove for a team
    whose set is at `fenced` to `carried`, read from the status directory. The refusal names the
    migration. A relaunch of an existing seat, which mints nothing, stays allowed.
  - Add this to section 12's ccy list and to the version bump.
  - Seed it in P1.

### 6. Major: a remote member's `last_send` cut under-carries, which loses items silently

- **Location:** D:436 (row 1), D:798.
- **Problem.**
  - v1 syncs into the inbox and delivers separately, when the session is idle. An agent often
    sends while busy. So items synced before its last send can still be undelivered.
  - Those items are before the cut, so they are not carried. After commit they are never
    delivered: v1 is fenced and the re-fetch fails. That violates D9.
  - `possible_duplicate` covers only the over-carry direction.
- **Fix.**
  - Use the member's `m.read` receipt per room as the cut. v1 posts a public receipt for each
    delivery confirmed in the transcript (`cli.py:2046-2071`, `matrix.py:301-304`). The admin
    reads the receipts over loopback.
  - Fall back to `join` when there is no receipt. Mark every such item `possible_duplicate`.
  - The same receipts also confirm row 4's unconfirmed deliveries for sessions that are not
    running.
  - P1 should verify that the admin can read others' receipts on Tuwunel.

### 7. Minor: the TIMEOUT argument cites only `recv`'s path, and the seed is missing (round 2 #8 residual)

- **Location:** D:428-431, D:920.
- **Fix.**
  - Cite `_candidates` and `_send_delivery` (`cli.py:1907-1921, 2027-2031`) beside `_deliver`.
  - State that a TIMEOUT is emitted only when the member's inbox is empty, because a re-fetch
    raises first (`cli.py:1358-1361, 1943`).
  - Add a P1 seed: a deadline that passes during the fence, in a running session with an empty
    inbox.

### 8. Minor: the fence proof covers one route; loopback members are not checked

- **Location:** D:623 (f), D:621.
- **Problem.**
  - The proof connects from the host's bus address only. The real routes are pasta (host-local,
    on `lo`) from a ccy container and the bridge from an LXC guest.
  - v1 permits a member bundle with `--address 127.0.0.1` (`admin.py:667-672`: `listen_addresses`
    includes loopback). The fence never cuts such a member.
- **Fix.**
  - Probe from one running ccy container and one running LXC guest.
  - Assert the rule set exactly (`nft list table`, or `systemctl show -P IPAddressAllow` for
    option (b)).
  - In M0, refuse any readable bundle whose `base_url` is loopback, rather than inferring it from
    the member kind.

### 9. Minor: two message fates are unstated

- **Location:** 5.2.
- **The cases.**
  - (a) Human messages typed in v1 Element during M2 to M8. Element queues them locally as
    unsent. After M8 they are refused by the read-only rooms, or lost on logout.
  - (b) A v1 send whose request reached the server but whose response was cut by the fence. The
    event exists before `v1_last_event` and is carried to its recipients, but the sender believes
    the send failed and may send it again on v2.
- **Fix.**
  - (a) Report the unsent Element messages. The welcome notice and the M2 owner message tell
    humans to retype in the v2 window.
  - (b) For each member, list its v1 sends within `REQUEST_TIMEOUT_S` before the fence as
    "sender may believe this failed", and mark the carried copies.

### 10. Minor: M2's `backup-now` removes v1 state before commit, and repeats on a re-run

- **Location:** D:623 (b), D:596-599.
- **Problem.** Tuwunel keeps 7 database backups (`render.py:27,83`), so each `backup-now` rotates
  out the oldest v1 backup. A re-run of M2 repeats it, which goes against D7.
- **Fix.** Skip it when `fence.json` records it done, and name the rotation as a cost.

### 11. Minor: after R, the declared move stays queued

- **Location:** D:822-831, D:774-777.
- **Problem.** `teamchat_migrations` still names the team after R. The next meta-deploy run of
  `migrate-declared.bash` offers the same move again ("not yet `accepted`").
- **Fix.** R marks the declaration `rolled-back`, and `--declared` skips it until the owner
  declares it again.

### 12. Minor: the adapter has no read for the inject records of LXC and host members

- **Location:** D:439 (row 4) against D:669-677.
- **Problem.** Row 4 reads LXC and host session homes "with the snapshot". But `ReadGuestState`
  is scoped to `~<user>/.config/pingbus/<team>/state/`.
- **Fix.** Add a confined read of the guest session home's `inject/` (names only), with its path
  cited from v1.

### 13. Minor: `teamchat0` sits beside libteam's `team*` family

See the naming section above.

- **Fix.** Either accept it with a sentence in 3.5, or pick an interface name that does not
  begin with `team`.
- Add P2 triage assertions for the Fedora package and user namespaces.

### 14. Minor: section 8 omits the fence's cost

Whatever fence is chosen, add its per-team cost while it is fenced: the oneshot unit and the nft
table for (c), or the drop-in and one v1 restart at fence and at unfence for (b). Also the v1
health record shows a problem while the fence holds, if option (e) is ever taken.
