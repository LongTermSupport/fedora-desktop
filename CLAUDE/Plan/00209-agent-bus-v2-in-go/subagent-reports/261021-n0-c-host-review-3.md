FIX-FIRST

# Plan 00209 N0 area C (host side): adversarial review, round 3 (final)

Reviewed: `DESIGN-v2-C-host.md`, the whole file at `c043071c`, against `DESIGN-v2-FRAME.md`, the
N0 common rules and the round 1 and round 2 reports. I spot-checked v1's
`helpers/agent_bus/render.py` and `files/usr/local/sbin/agent-bus-install`, which confirm that
Tuwunel prunes old database backups (`database_backups_to_keep`) each time it takes a new one, and
that v1 deletes the state tars of the pruned backups.

The round 2 fixes are good and nearly all of them are complete. Two new majors remain, both in
restore and both small to fix:

- the pre-restore backup that #27 added can prune the very backup being restored (#43);
- restore has no crash record, so a kill between its two renames leaves a team that later comes
  up on an empty database (#44).

Everything else is minor and the build session can settle it. I found no install-specific names and
no time estimates. Every duration in the text is a runtime parameter.

## Round 2 findings, and round 1's partly-resolved 12 and 21

| #   | Status   | Evidence                                                                                                                                                                                                                |
| --- | -------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 12  | RESOLVED | 3.4 and 3.11.2 steps 1 and 5: the status record's own `seq`, checked under the lock, which every writer raises (3.11.3)                                                                                                 |
| 21  | RESOLVED | The placeholder table now has `<v2>`, `<owner group>`, `<iface>`, `<bus_ip>` and `<staging port>`. The 3.2.2 table gives `/var/lib/private/<v2-hs-state>/<team>/` as the real location                                  |
| 25  | RESOLVED | 3.6.1 `revoked_ms` and `needs`; 3.12.2 step 6 revokes members marked `needs: reissue` and humans whose `revoked_ms` is after the backup. The cases are in 3.14 and H-C5. Ad-hoc `human lock` is still not covered (#49) |
| 26  | RESOLVED | 3.12.2 preamble and step 3: the current config is re-rendered with only `database_path` and the port changed; the tar is used as evidence only; there is a test for a port change and for a pin bump                    |
| 27  | PARTLY   | 3.12.2 step 2 takes backup `M` and names it in the answer. But taking `M` can prune `N` (#43)                                                                                                                           |
| 28  | RESOLVED | `seq` (3.11.3), the tick's compare-under-lock (3.11.2 step 5), and an interleaving test over every writer (3.14 "locks")                                                                                                |
| 29  | RESOLVED | 3.7.3: a `.new-<claim>` directory, then `RENAME_NOREPLACE`/`RENAME_EXCHANGE`, with the old bundle quarantined; `reissue`/`return` refused while the seat is held. Gaps in the edges: #47                                |
| 30  | RESOLVED | 3.7.3 release step 3 has three outcomes, `notice.json`, and only a transient failure keeps `held.json`; 3.7.5 agrees; the cases are in the 3.14 seats row                                                               |
| 31  | RESOLVED | 3.7.3 step 1 and the 3.13 seat row: `openat2(RESOLVE_BENEATH\|RESOLVE_NO_SYMLINKS\|RESOLVE_NO_MAGICLINKS)` from the session dirfd, then `O_NONBLOCK` and `fstat`                                                        |
| 32  | RESOLVED | 3.11.3: the poll ends on a new device, the owner, 5 minutes or a failed `sudo -n`, then `sudo -k` whichever way it ended                                                                                                |
| 33  | RESOLVED | 3.7.3 release: the waits and the post run outside the allocation lock (taken only for steps 2 and 4); the board re-runs a timed-out leave. Doing that on open conflicts with C39 (#46)                                  |
| 34  | RESOLVED | 3.7.3 "A session is over when ...", and section 7 D5: release only after the record is removed; a crash keeps the session restorable                                                                                    |
| 35  | RESOLVED | 3.2.3 binary sweep counts the `units.json` of unpurged teams; 3.5.2 step 6 backs up a returning team on its old version first. Two record details remain (#49)                                                          |
| 36  | RESOLVED | 3.6.3: ccy-teams always passes `--seat S`, `next` is for LXC only, the refusal names another checkout, and it re-chooses up to three times                                                                              |
| 37  | RESOLVED | `needs` (3.6.1); `lost` is handled like a missing handle for `coordinator_wanted` (3.6.4); a refused promotion still lets the add succeed, and the problem is recorded (3.6.3)                                          |
| 38  | RESOLVED | 3.5.1 step 3 opens with `O_NOFOLLOW\|O_NONBLOCK`, `fstat`s and hashes from one fd; 3.12.2 steps 5 and 8 use dirfd `renameat2(RENAME_NOREPLACE)` and fd-relative removal                                                 |
| 39  | RESOLVED | See row 21                                                                                                                                                                                                              |
| 40  | RESOLVED | 3.9.2 step 3 and section 7 D7: the message names the owning play, or only the setting if D says no play owns the config; v2 never edits a container config                                                              |
| 41  | RESOLVED | 3.8: byte-range splice of the v2 top-level keys, a test that every other byte is unchanged, an `O_EXCL` backup copy and a read-back. Comment edge: #51                                                                  |
| 42  | RESOLVED | 3.2.3 and 3.5.1: a base-unit change applies every present team whatever the scope, and the play prints that it did                                                                                                      |

Of round 2's 18 findings plus round 1's 12 and 21: 19 RESOLVED, 1 PARTLY (27, carried as #43), 0 OPEN.

## New findings

### Majors

43. **[major] 3.12.2 step 2 with 3.12.1 step 5: the pre-restore backup can destroy the backup being
    restored.**
    Tuwunel keeps `database_backups_to_keep` database backups and prunes the oldest each time it
    takes one. The value comes from v1's `render.py` key set; 3.12.1 says "it keeps seven". The
    backup function then deletes the tars of pruned backups. Once the keep count is reached (after
    a week of daily backups, sooner with apply-time backups), `restore --backup N` for the oldest
    `N` goes like this:

    - step 1 validates `N`;
    - step 2 takes `M`, so Tuwunel prunes `N` and backup step 5 deletes `N`'s tar;
    - step 3's `--restore-backup N` then fails.

    The live team survives, but the oldest restore point, which is often the one a slow-noticed
    corruption needs, cannot be restored, and asking for it deletes it. Fix: reorder restore to
    validate (1), stage `N` into `db.restore-<N>` (3), and only then take the pre-restore backup
    `M` (2). Restore already holds tar `N` in memory from step 1, so pruning it afterwards is
    harmless. Alternatively, refuse with a clear message when `N` is the oldest and the keep count
    is reached. Also name `database_backups_to_keep` and its value in 3.2.4's key list. Add to 3.14
    a test that restores the oldest backup with the keep count full.

44. **[major] 3.12.2 steps 5-7: restore has no crash record, and a kill between its renames ends in
    an empty homeserver that no verb can recover.**
    Members (C20, `pending`) and bootstrap (C14) survive a kill at any point. Restore does not. A
    root kill or a power loss after `db` became `db.previous` and before `db.restore-<N>` became
    `db` leaves no `db`. The unit is enabled, so the next boot, `team apply` step 7, or any start
    runs Tuwunel on a missing `database_path`. RocksDB then creates a fresh empty database, and the
    team serves it under its old `server_name`: a silent, wrong state, against fail-fast. Bootstrap
    then refuses ("token lost, naming `restore`"). Restore, run again, fails at step 5 because
    `db.previous` exists (`RENAME_NOREPLACE`), so no verb recovers the team, and the only way back
    is a manual rename, which IaC forbids.

    Fix:

    - write `root/<team>/restore.json` (`N`, `M`, the step) before step 5, and remove it after
      step 8;
    - every root verb on the team (apply, restore, backup, member, health restart) first rolls a
      found record forward or back, or refuses naming `restore --resume`;
    - before step 5, mask the unit at runtime (`systemctl mask --runtime`) so nothing starts it
      mid-swap, and unmask it at step 6 or 7;
    - in step 1, refuse leftover `db.previous`, `db.restore-*` or `*.previous` files with no
      record, naming them.

    Add to 3.14 a kill-at-each-step test for restore, as the member verbs have.

### Minors

45. **[minor] 3.11.3 and 3.13 against 3.9.2: the board cannot probe an LXC member's seat lock.**
    3.11.3 says online-ness "is its seat lock, probed live by the board". For an LXC member the lock
    is reachable only through `/proc/<init pid>/root` of a root-owned privileged container, which
    the owner cannot open, and 3.13 lists only the pool and session probes. Fix: the health tick
    (root) runs the 3.9.2 L3 probe for each LXC member and records `online` in `status.json`. Say
    in 3.11.3 that LXC online-ness is up to about 70 s old, and keep the live probe for ccy seats.

46. **[minor] 3.7.3 and 3.7.5 against C39: a board open runs releases.**
    "The board re-runs the release for every seat whose `held.json` names a live session that no
    longer desires it". Also, "the next take or board open that finds a seat held by a session that
    is over does the same". A release waits up to 60 s on a seat lock and runs `<v2-bin> seat disconnected`, a process making HTTP calls. On the open path that breaks "about 1 s, no HTTP,
    no per-item process" (C39, 3.13). Fix: the board draws from files first, shows such seats as
    "leaving" or "releasing", and then runs the releases in the background (or on the owner's
    action). The open never waits on them. Add the case to H-C7.

47. **[minor] 3.7.3 placement: the held check and the swap are not under one lock, and the
    `.new-` claim excludes nothing.**

    - `reissue`/`return` is refused while `held.json` exists, but nothing says the check and the
      `RENAME_EXCHANGE` happen under the checkout's allocation lock. A take between them writes
      `held.json` into the directory that is then swapped into quarantine, and the session's copy
      holds a token about to be revoked. Fix: the check and the swap are both under the allocation
      lock (the root call is already inside it, 3.4).
    - `<seat>.new-<claim id>` carries a fresh claim id, so the `mkdirat` refuses nothing ("so two
      racing launches cannot both reach the homeserver" is not what guards it; the allocation lock
      does). Fix: say so, or refuse when any `<seat>.new-*` exists, which also stops orphaned
      `.new-` directories piling up. Name who removes a stale one (the `reissue` recovery).
    - A `held.json` or `notice.json` left in the old directory goes to quarantine with it. Say that
      a stale `held.json` is released (with its notice) before a swap.
    - `released/pool/...` shares a namespace with `released/<session>/...`. Use `released/sessions/`
      and `released/pool/`, or say a session id can never be `pool`.
    - 3.9.2 step 7 still says `guest place` "writes `~/.config/<v2>/seats/<team>/`". Make it cite
      the `.new-` swap of 3.7.3.

48. **[minor] 3.7.3 take step 1: a take copies whatever bundle is in the pool, even a dead one.**
    The pool bundle of a handle that is `parked`, `lost`, `retired` or `needs: reissue` holds a
    revoked token, and the take checks only "complete bundle and no `held.json`". Fix: the take
    reads the handle's state from the status record's roster and routes as follows: `parked` and
    `lost` go to `member return`, and `needs: reissue` goes to `reissue`. `retired` is refused, and
    its pool directory goes to quarantine. Only `active` is copied.

49. **[minor] Restore and returning-team record details.**

    - An ad-hoc `human lock` after the backup is undone by restore, because the restored database
      has the account unlocked and `revoked_ms` does not cover locks. Fix: record `locked` per human
      in the roster and re-lock in step 6.
    - `--roster-from-backup` can lower counters and bring back a handle retired later, which C18
      forbids "a restore included". Fix: raise the counters and the retired set from every handle
      named in the status record, the LXC records and the pool, and say that what remains unknown
      is printed.
    - The staging config's file (the `LoadCredential=` source for the transient unit) is not named.
      Name it under `root/<team>/` and remove it in steps 7 and 8.
    - 3.5.2 step 6 re-renders an old drop-in from "the version `units.json` records", but 3.2.2 says
      `units.json` holds digests. Add the version string, the port and the listen set to
      `units.json`. Refuse the old start, naming the port, when another v2 team now holds that port.

50. **[minor] 3.11.2: what the tick enumerates, and its own events.**
    A missing `status.json` counts as `seq` 0. A tick that reads after `team remove` therefore
    passes the step 5 compare and writes a status record for a removed team. Fix: `health --all`
    enumerates teams with a stamp only, and re-checks the stamp under the lock before writing. A
    tick view dropped by the `seq` compare also drops its own "restarted after three missed probes"
    event. Carry tick events in a small root-only file that the next write merges, or let the
    restart write the event under the lock it already holds.

51. **[minor] 3.8 splice: comments at a range edge.**
    "From the key to the next top-level key" sweeps up a comment block that heads the next
    (non-v2) key, and a v2 key's own head comment lies before its start. Fix: end the range at the
    last line of the key's value node, before the next key's head comment, and include the v2 key's
    head comment. Add both to the byte-identity fixtures.

## DC4 against the brief

- **Opening in about 1 s:** met. File reads, one D-Bus call, no sudo, and no process per item
  (3.13). The one exception is #46, the releases run on open; once they run in the background it
  holds.
- **Fresh enough member and device facts:** met.
  - A change verb, apply, backup, restore and `human devices` each rewrite the record at once under
    the team lock.
  - Facts that change with no verb are at most about 70 s old.
  - The record can no longer go backwards (`seq`).
  - Expiry turns a dead timer into a problem the board shows.
  - LXC online-ness needs #45 to be stated honestly.
- **The login flow re-reads after a sign-in:** met. It is a bounded `sudo -n human devices` poll
  that ends in `sudo -k`, and each poll rewrites the human's entry, so the board shows the sign-in
  at once.

DC4 (a) stands.

## Resident cost

3.15 is honest:

- nothing is resident with no teams;
- each team costs one Tuwunel at the measured 100-135 MB, with H-C1 to re-measure;
- the host runs two host-wide timers;
- the per-tick call growth is stated as teams × rooms × (members + humans) and measured by H-C1.

No new resident process came in with the round 2 fixes.

## Coverage

- Blockers: none. Majors: 43, 44. Minors: 45-51.
- Seams:
  - A: handles, rooms, the trust shape and the team-name length are cited, not restated.
  - B: `seat disconnected`, the guest verbs, the keeper's leave and the kit resolution are listed as
    assumptions.
  - D: D4 (mounts), D5 (session end) and D7 (the LXC config play) carry their duties, with no
    contradiction left.
- Privilege: the round 2 fixes stop root and the owner from following links planted by a container
  or a lesser user. Restore's state-directory work is dirfd-relative. No new case was found of root
  acting by path on a writable tree.
- Fail fast: the only new gap is #44 (a silent empty database after a crash). No skip-and-warn was
  found.
- Both majors are local to 3.12.2. Once they are fixed, this area is fit to merge without another
  round.
