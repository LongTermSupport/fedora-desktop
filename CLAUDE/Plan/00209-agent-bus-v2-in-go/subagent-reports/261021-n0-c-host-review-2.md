FIX-FIRST

# Plan 00209 N0 area C (host side): adversarial review, round 2

Reviewed: `DESIGN-v2-C-host.md` (the whole file, at the branch tip) against `DESIGN-v2-FRAME.md`, the N0
common rules and the round 1 findings. I spot-checked `src-go/go.mod` (neither godbus nor any YAML
module is there yet, so DC17 is a real decision, as the design now says), v1's
`helpers/agent_bus_lxc/members.py` (v1 only *checked* `lxc.start.auto`; it did not set it, so round
1's item 14 overstated that part), and the designer's digest 3.

The round 1 fixes are good. Both blockers are closed properly: the pool holds the authoritative
bundle and is never mounted, and the binary is pinned per drop-in. No blocker remains. Six new
majors remain. Four are in restore and the seat flows the fixes added, one is a race in the status
record that DC4 rests on, and one is a reissue path that cannot be placed. No install-specific
names and no time estimates were found; the durations in the text are runtime parameters.

## Round 1 findings

| #   | Status   | Evidence                                                                                                                                                                               |
| --- | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | RESOLVED | 3.7.1 pool/sessions/released split, C41, DC15; release moves to quarantine only (3.7.3); hostile cases in the 3.14 "seats" row                                                         |
| 2   | RESOLVED | 3.2.3 `ExecStart=<v2-lib>/tuwunel-<version>` in the drop-in; 3.5.1 step 6 has no shared link; 6.1 row corrected; VM-lab pin-bump case. Edges: #35, #42                                 |
| 3   | RESOLVED | 3.5.1 steps 1-3: build, then the fresh binary's `declare check` unprivileged, then install                                                                                             |
| 4   | RESOLVED | 3.5.1 steps 10-13 and 3.9.2's last paragraph agree: absent, remove, apply, present                                                                                                     |
| 5   | RESOLVED | 3.6.4 bring-up order, `coordinator_wanted` (3.6.1), DC6, two VM-lab cases. Gaps: #37                                                                                                   |
| 6   | RESOLVED | 3.12.2 merged roster (counters max, retired union, `lost`, needing reissue); `restore.swap` through the child. New restore defects: #25-#27                                            |
| 7   | RESOLVED | C42/C43, 3.7.5 desired.json (read-only directory mount), farewell, host-run disconnected notice; section 7 B4/B6. New defects: #30, #33, #34                                           |
| 8   | RESOLVED | C25 caveat, K12, section 7 D4 (mount constraints on `<v2-seats>` and the runtime directory)                                                                                            |
| 9   | RESOLVED | `User=<v2-hs>-%i` (3.2.3), the 24 + 6 + 1 length bound, transient unit identity (3.12.2 step 2), H-C3                                                                                  |
| 10  | RESOLVED | DC17 decides it: godbus and yaml go into the production module, `ListUnitsByPatterns` in 3.13; DC12/DC13 reasoning is consistent with it                                               |
| 11  | RESOLVED | `root/<team>/server-name` written before the first start (3.5.2 step 4); table test in 3.14                                                                                            |
| 12  | PARTLY   | 3.4/3.11.2 move the probes outside the lock and guard the write with the roster's `generation`, but the guard misses every change that does not write the roster (#28)                 |
| 13  | RESOLVED | 3.5.2 step 3 restart set and reload set                                                                                                                                                |
| 14  | RESOLVED | 3.9.1 `--set-var` for HOME/USER/LOGNAME/PATH, a `getgroups()` self-check, H-C10; `lxc.start.auto` checked for `service: true` (3.9.2 step 3), as v1 did                                |
| 15  | RESOLVED | 3.9.2 step 3: one `/proc/<init pid>/root` dirfd, `O_NONBLOCK` + `fstat`; LXC C27 probe in 3.9.2                                                                                        |
| 16  | RESOLVED | 3.9.2 step 4: a digest is kept while any process runs from it, in the guest and on the host                                                                                            |
| 17  | RESOLVED | 3.2.5: exit 0/1/other, `--permanent` and runtime queried separately, `/proc/net/tcp{,6}` read in-process                                                                               |
| 18  | RESOLVED | C11 is now restricted to open and redraw                                                                                                                                               |
| 19  | RESOLVED | 3.8: `yaml.Node` decode, strict rules for the v2 subtrees only, a `host_vars/<host>/` directory refused. Writer issue: #41                                                             |
| 20  | RESOLVED | 3.7.4 cites A; `trust` and the room names are marked as examples; the source key is now `LR`                                                                                           |
| 21  | PARTLY   | most placeholders are added; `<v2>`, `<owner group>`, `<iface>`, `<staging port>` and `<bus_ip>` are still missing, and the `hs/<team>/` location conflicts with `<v2-hs-state>` (#39) |
| 22  | RESOLVED | 3.12.1 sandbox and capabilities; 3.4 "held lock as an argument"                                                                                                                        |
| 23  | RESOLVED | 3.15: the teams × rooms × (members + humans) growth, per-member homeserver cost, H-C1 measures                                                                                         |
| 24  | RESOLVED | 3.2.2: the secret lives in `config.toml` only and reaches the child on stdin for bootstrap step 1                                                                                      |

Open: 0. Partly: 2 (12, 21), both carried below as #28 and #39.

## New findings

### Majors

25. **[major] 3.12.2: a restore brings revoked credentials back to life.**
    The restored database is the database as it was at backup `N`. Every revocation made after `N`
    is undone:

    - a member reissued after `N` (C21, and the remedy K13 relies on against a hostile session that
      kept its token): its pre-reissue token is valid again. The merged roster only *marks* it as
      needing `reissue`;
    - a human whose password was reset, or who had `human logout` run after `N` (the lost-phone
      procedure, 3.6.5): the old password and the lost device's session work again;
    - a member parked after `N` is revoked again (step 5). That is right, and it shows the rule is
      known but applied to one case only.

    Fix: in step 5, revoke (password reset with device logout) every member whose current
    `token_ms` is after the backup time, before marking it needing `reissue`. For humans, either
    record `revoked_ms` per human in the roster and revoke those after `N`, or revoke every
    human (the owner re-runs `human password` per human; say so in the restore answer). Add both
    cases to the 3.14 restore row and to H-C5.

26. **[major] 3.12.2 step 4: restoring the tar's `config.toml` and `team.json` makes restore fail,
    or diverge, after any declaration or pin change.**
    The tar's config was rendered for the declaration and Tuwunel version of backup time. The
    drop-in, `firewall.json`, the `SocketBindAllow=` port, the listen assertion (step 5) and the
    running binary are current. After a port, `listen`, `allow_from` or cache-modifier change, the
    restored config binds a port the drop-in denies, or listens where step 5's assertion fails.
    After a pin bump that drops a key, `error_on_unknown_config_opts` stops the start. Each of these
    ends in the swap-back. Restore, the disaster path, is then impossible after ordinary changes,
    and the staging config (step 2) is not specified at all. Fix: render `config.toml` (and the
    staging config, with its own `database_path` and port) from the *current* declaration, which
    keeps the one registration secret. Keep the tar's `config.toml` and `team.json` only as
    validation evidence (`server_name` must match `root/<team>/server-name`). Then the stamp need
    not be removed for that reason. Add a test: a restore after a port change and after a pin bump.

27. **[major] 3.12.2 steps 4 and 7 break C36 and make a mistaken restore permanent.**
    C36 requires "a backup before any restart that changes the homeserver's binary or config,
    whatever starts it". Restore restarts onto a different database and config without one, then
    deletes `db.previous` and the `*.previous` control files on success (step 7). Restoring the
    wrong `N` loses everything since `N` with no way back. Fix: before step 4, while the live
    homeserver still serves, take a backup through the internal function under the held team
    lock (3.4). Name its number in the restore's answer, so that "restore back" is one verb.

28. **[major] 3.4, 3.11.2 step 5: the status record can go backwards; the `generation` guard
    covers only roster writes.**
    The tick reads first and writes later, if the roster's `generation` is unchanged. Several
    writers of `status.json` change nothing in the roster:

    - `human devices` (the login flow's re-read, 3.11.3);
    - room convergence in `team apply` (`rooms.json`, kicks and invites);
    - `backup`;
    - `human lock`/`unlock`/`logout`/`password` (3.6.5 does not say they write the roster).

    A tick that read before such a write overwrites the fresher record. The new device then
    vanishes from the board, or apply's fixed drift comes back with the fix "`ccy-teams apply`",
    for up to 60 s. That contradicts "a change the owner just made shows at once" (3.11.3), the
    basis of DC4. Fix: give `status.json` its own `seq`, raised by every writer under the team
    lock. The tick records `seq` at its read and writes only if it is unchanged. Test it with
    each writer interleaved.

29. **[major] 3.7.3, 3.6.3, 3.9.2 step 7: `reissue` and `return` cannot be placed, and a held seat's
    reissue is undefined.**
    Placement requires "a fresh `mkdir` in the pool; one that appeared meanwhile is refused" and
    `token`/`seat.json` by `O_CREAT|O_EXCL`. A `reissue`, a `return` of a parked handle, and the
    "claimed directory without a bundle" recovery (3.7.3) all target a pool directory that already
    exists, possibly holding a token, so `O_EXCL` fails. The same holds for `guest place` into an
    existing `~/.config/<v2>/seats/<team>/`. Nor does the design say what happens to a session
    *holding* the seat when it is reissued (K13's remedy): its copy is revoked, and nothing
    delivers the new one. Fix: place a new bundle as a new directory (`<seat>.new-<claim>`) and swap
    it in with `renameat2(RENAME_EXCHANGE)`, the old one going to quarantine. State that a reissue of
    a held seat is a leave followed by a take for that session (desired.json without the seat, then
    with it), or that it is refused while held, naming the release.

30. **[major] 3.7.5, 3.7.3 release step 3: a disconnected notice that cannot be posted wedges the
    seat for ever.**
    "A failure to post fails the release ... `held.json` stays, so the next release retries it."
    The notice uses the pool token, so it can never post when:

    - the handle was parked or retired after the session ended;
    - it became `lost` by a restore;
    - it was reissued with the new bundle not yet in the pool;
    - the team was removed.

    Every retry fails, and the seat cannot be taken again by any route. Fix: a notice is due only
    for a handle the roster (status record) shows `active`. Exactly one of these answers is
    recorded and the release goes on: an `M_UNKNOWN_TOKEN`/`M_FORBIDDEN` answer, a team that is
    absent, or a homeserver that is not installed. The board shows "notice not posted: <why>".
    Only a transient failure (unreachable, 5xx, 429) keeps `held.json`, and the board shows the
    seat as leaving. Add these cases to the 3.14 "seats" row.

### Minors

31. **[minor] 3.7.3 release step 1, 3.13 seat probe: the owner opens container-controlled paths with
    `O_NOFOLLOW` on the last component only.**
    The container can replace `sessions/<session>/<team>` with a symlink. The board's L3 probe then
    follows it to any owner path, and takes `LOCK_SH` there (for example on the play lock or an
    allocation lock in the runtime directory) or opens a device node. Fix: resolve from the
    `sessions/<session>` dirfd with `openat2(RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS)`
    plus `O_NONBLOCK` and `fstat`, as 3.9.2 already does for guests. Add the case to the 3.14 seats
    row.

32. **[minor] 3.11.3 login flow: password sudo stays hot without bound.**
    Each successful `sudo -n` refreshes the timestamp, so a 2 s poll "until a new device appears or
    the owner ends the wait" keeps the owner's sudo usable on that tty indefinitely, which is against
    C10's "password at the time". Fix: bound the poll (for example to the sudo timestamp
    window). On timeout, or when `sudo -n` fails, run `sudo -k`, say that the board falls back to the
    tick, and stop.

33. **[minor] 3.7.3 release, 3.7.5 leave: bounded waits and network calls run under the checkout's
    allocation lock, and a timed-out leave is never retried.**
    Step 1 (waiting for the keeper to drain) and step 3 (a homeserver post) hold the lock. A
    launcher's seat take in the same checkout then times out behind them. A leave that exits 75
    leaves `held.json` naming a *live* session, which no "dead and not restorable" sweep releases.
    Fix: wait and post without the allocation lock, and take it only for the rename and the
    unlink. The board re-runs a release for any seat that is held but not in its session's
    `desired.json`.

34. **[minor] Section 7 D5 against 3.7.1 "restorable counts as held".**
    D5 releases a session's seats when its container exits. 3.7.1 and C26 keep the seats of a
    restorable session held, because a reboot or age restart replays the record. Fix: release at
    exit only when the session record is removed (the session is over), and say who removes it.

35. **[minor] 3.2.3, 3.5.2 step 6: a team removed without `--purge` and brought back after a pin
    bump migrates its database with no backup, and maybe with no old binary.**
    `team remove` deletes the drop-in, so the binary sweep may delete the old `tuwunel-<version>`.
    On re-present the answer is "first start", and step 6 backs up only on "restart". Fix: the
    sweep also counts the version recorded in `units.json` of every unpurged team. Back up whenever
    a database exists and the binary or config differs from `units.json`, starting the old version
    for the backup.

36. **[minor] 3.6.3 `--seat next` against 3.7.3 "claim the pool directory before sudo", and the
    allocation scope.**
    The pool path names the seat, so ccy-teams must know the number before root does. Seat numbers
    are counted per prefix (repo, host, type) across checkouts, but the allocation lock is per
    checkout, so two checkouts of one repo can choose the same number. The loser gets "active,
    naming `reissue`", which is wrong advice. Fix: ccy-teams always passes an explicit seat, with
    `next` only for LXC. Either take the allocation lock per prefix, or have root's refusal
    for a just-claimed handle say "taken by another checkout, choose again".

37. **[minor] 3.6.1, 3.6.3, 3.6.4: state the design relies on but has no schema for.**

    - "needing `reissue`" (3.6.3 roll forward, 3.12.2 merge) has no field in `roster.json`.
    - A declared coordinator that is `lost` is not covered by 3.6.4's cases.
    - A deferred promotion fired by `member add` for a `human_text: false` bundle has no stated
      outcome: does the add fail, or does it succeed with the promotion refused?

    Fix: add a `needs` field (`reissue`), treat `lost` like "not in roster" until `member return`,
    and say that the add succeeds with `coordinator_wanted` kept and a refusal problem recorded.

38. **[minor] 3.5.1 step 3, 3.12.2 steps 4 and 7: root acts on paths a lesser user controls.**

    - Root copies `<v2-host>` from the login user's build output, checked against "the digest the
      build printed". The user controls both, so a symlink there makes root copy any root-only file
      to a 0755 path.
    - Restore renames and deletes entries in the homeserver uid's state directory.

    K9 accepts owner-equals-root at the next play, but the copy should still not be a read
    primitive. Fix: open the build output with `O_NOFOLLOW`, `fstat` it (a regular file owned by the
    login user), and hash the bytes root copies. Do the state-directory renames with
    `renameat2(RENAME_NOREPLACE)` from a dirfd and the deletes with `os.Root`/`RemoveAll`
    semantics, never by path.

39. **[minor] Placeholders and the state layout.**
    `<v2>` (in `play-<v2>.yml` and the guest's `~/.config/<v2>/`), `<owner group>`, `<iface>`,
    `<staging port>` and `<bus_ip>` are used but are not in the table D works from. The 3.2.2 file
    table puts `hs/<team>/` under `<v2-state>`, while `StateDirectory=<v2-hs-state>/%i` puts it
    under `/var/lib/private/<v2-hs-state>/`. Fix: add the five rows and give the homeserver
    state's real location in the file table.

40. **[minor] 3.9.2 step 3: the `lxc.start.auto` miss "names the fix in the container's
    declaration".**
    The design does not say which play owns an LXC container's config. If none does, the message
    asks for a manual edit, which IaC forbids. Fix: name the owning play, or state that LXC
    container configs are outside this repository's IaC and that the message names the setting
    only.

41. **[minor] 3.8: a `yaml.v3` node round-trip is not byte-preserving.**
    Re-encoding the whole file changes indentation, quoting and comment placement in keys v2
    does not own, `!vault` blocks included, so every v2 write produces a whole-file diff in the IaC
    store. Fix: splice only the byte ranges of the v2 top-level keys, and test that the bytes
    outside them are identical before and after a write.

42. **[minor] 3.2.3, 3.5.1 step 7: a change to the base unit text reaches unscoped teams at their
    next crash or health restart, without their apply.**
    The base unit is in the restart set, but it is installed for all teams by the static step, so
    a new hardening directive (K2) first meets an unscoped team at an unplanned restart. Fix: either
    a base-unit change makes the play apply every team (scope ignored, said in the output), or K2
    states this window and H-C2 runs before the static install.

## DC4 against the brief

The status-record choice meets "ccy-teams opens in about 1 s" with margin: file reads, one D-Bus
call, and no sudo or process per item (3.13). Member and device facts are at most about 70 s old
for changes made outside a verb, and are fresh at once after a verb. Online and up come from live
probes. The login flow re-reads through `human devices`. Two defects stop it fully meeting the
brief today: the record can go backwards after any verb that does not write the roster (#28), and
the re-read keeps sudo hot without bound (#32). With those fixed, DC4 (a) stands.

## Coverage

- Blockers: none. Majors: 25-30. Minors: 31-42.
- No requirement was found dropped silently. C36 is contradicted by restore (#27).
- Seams: A (handles, rooms, team-name length) and B (keeper leave, `seat disconnected`, guest verbs)
  are cited, not restated. D4 and D5 carry the mount and exit duties; D5 conflicts with 3.7.1 (#34).
- Fail-fast: the release wedge (#30) is the opposite failure, a fail-stop with no route out. No
  skip-and-warn was found.
