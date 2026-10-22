# n2g spec review, round 1 (SPEC-n2g.md at ec516479c)

**Verdict: FIX-FIRST.** Four findings are high: each lets a crash, a restart or a reused
number put a database in front of members that the design says can never be served. All are
fixed in the spec in the same commit as this review.

Read: SPEC-n2g.md, the n2g handoff, the Tuwunel spike digest, C 3.12 in full, C 3.2.3 (the
base unit), and the code the spec leans on. That code is `host/apply.go` (`startForBackup`,
`reload`, `Deps.Backup`), `units/units.go` (`checkUnit`, `hostState`), `teamlock`, `state`,
`apply/files.go` (the roster and human states), `rooms/rooms.go` (`planRoster`, `NextRev`),
`status` (`Restore`, `Backup`), `health` (`NoBackups`) and `admin/class.go` and `run.go`.
The epoch's consumers are A 5.1, B 2.6 step 08, C13 and B44, and SPEC-n3's SY1, SY6, SY7, SY10
and gap 06 (fakehs `RestoreTo`).

## Findings

01. **High: the swap-back has no record step.** RS11 writes the block and renames `db` back
    while the record still says `swap` or `start`. If the process is killed between the two
    renames and the owner runs `--resume`, `start` runs RS7 over a state directory with no
    `db`. That start either fails forever on the block, or, if the block is gone, Tuwunel
    creates an empty database and serves it. **Fixed:** the record goes to `revert` (fsync)
    before RS11 touches anything, and RS12 finishes a `revert`. The renames follow one table of
    the states the directory can be in (RS16), and anything else is refused. RS17 adds a rule:
    the live unit is never started unless `db` is a directory in the state directory.

02. **High: a restart of the staging unit re-wipes the secured copy.** RU1 gives the staging
    unit "each line of `host.BaseSandbox`", and the base unit (C 3.2.3) carries
    `Restart=always`. A crash or a watchdog kill of the staged server after RC1 restarts
    `--restore-backup N` over the non-empty `db.restore-<N>`. The spike's hazard (b) then wipes
    the secured database and restores `N` unsecured, and RS6 would swap it in. **Fixed:** D17
    now defines the sandbox lines and excludes the lifecycle lines. The staging unit sets
    `Restart=no` itself, and RS3 refuses when `NRestarts` is not 0 or the `InvocationID`
    changed while secure ran. Tests cover the argv and a restart fault.

03. **High: the epoch raise is not proven atomic, and N3 relies on it.** SY6 and SY7 (and
    n3a's fakehs `RestoreTo`, gap 06) assume that no request ever sees the restored database
    with the old epoch. In this spec that holds only if RC1's roster write survives the staging
    stop. RS3 accepts any stop, so a stop that ends in a SIGKILL after `TimeoutStopSec` could
    lose the write. The swapped-in database would then serve backup `N`'s roster event, whose
    epoch may equal what members hold (no restore since `N`), and a stale `since` would be
    kept (H-C14). **Fixed:** RE1 states the invariant and names its consumers. The staging stop
    must end with `Result=success`. RS7 reads the served epoch and fails step 9 (swap back) on
    a mismatch. RT3 restarts the staged database plainly on the pinned Tuwunel and reads the
    raised epoch at its first answer.

04. **High: a discarded attempt can reuse an epoch that already served.** If step 9 started
    the restored database (epoch E+1) and a later step failed, RS11 swaps back. After
    `--discard`, the next restore merges `current + 1` = E+1 again, from a different database.
    A member that was paused across the swap-back still holds E+1 and a position from the
    discarded database, so its stale `since` is kept. **Fixed:** RS6 writes a root-owned
    high-water `root/<team>/restore-epoch.json` (fsync) before any secured database can serve.
    RM10 raises from `max(current, high-water)`, and RM11's random draw differs from it.

05. **Medium: leftovers and the fresh path.** RV2 does not name `db.previous` in the state
    directory, so RS6's `RENAME_NOREPLACE` fails half-way on a leftover. Nothing checks
    `db.restore-<N>` again right before the staging start, and nothing asserts that
    `config.restore.toml`'s `database_path` differs from the live one or that
    `database_backup_path` is kept. The spike's (d) says the restore needs that path. **Fixed**
    in RV2 and RS2.

06. **Medium: the staged server could still be running at the renames.** Neither RS6 nor RS12's
    `swap` checks that the staging unit is inactive. Renaming a database a live process writes
    into is the spike's hazard (c) in its worst form. **Fixed:** the renames require both the
    staging unit and the live unit to be inactive.

07. **Medium: the SIGUSR2 race and the preamble's conflation.** The base unit has
    `PrivatePIDs=yes`, so in production an early signal is dropped (PID 1) and only shows as
    BT2's 60 s timeout. In the spike there is no PID namespace, so it kills the process. Both
    remain possible if the unit crash-restarts between BT1's readiness and `KillUnit`.
    **Fixed:** BT1 reads `InvocationID` before readiness and again just before the signal, and
    refuses on a change. The preamble states both outcomes. SD10 adds `InvocationID`,
    `NRestarts` and `Result` to the unit state.

08. **Medium: `O_NOFOLLOW` covers the last component only.** The homeserver's uid owns
    `backups/`, `backups/meta/`, the state directory and everything in `db*`. Opening
    `.../backups/meta` with `O_NOFOLLOW` still follows a link at `backups`. **Fixed:** the
    trust rule now says every path in the homeserver's tree is resolved with
    `openat2(RESOLVE_NO_SYMLINKS|RESOLVE_BENEATH|RESOLVE_NO_XDEV)` from a root-owned fd of
    `/var/lib/private/teamchat`. Removals walk level by level, with no-follow opens and
    `unlinkat`. The rule's first line also wrongly excluded `teamchat-ctl`, whose files root
    reads by I-B6: it is reworded.

09. **Medium: the seams are not named.** `backup.Deps`, `restore.Deps` and `host.Serve` have
    no fields or signatures. `host.Deps.Backup` is `func(ctx, held) error` while `Take` takes
    `keep`, and the handoff's "a kill at every step" has no model. **Fixed:** a "Seams" section
    names every field, gives the adapter, and models a kill as a fake that calls
    `runtime.Goexit`. Because the engine never cleans up in a `defer`, the disk is left as a
    kill leaves it.

10. **Medium: RM4 misses one case.** A handle that is `active` now and `parked` or `lost` in
    the backup, with `cred_seq > B`, holds a credential the restored database never issued.
    The board would not show the fix. **Fixed:** RM4 covers every handle `active` now and
    present in the backup with `cred_seq > B`.

11. **Medium: resume validation.** RV4 requires `meta/<N>`, which `M`'s prune may have
    removed by the time of a `--resume`. **Fixed:** a resume validates `N.tar` (BK3) and the
    record, not `meta/<N>`. `N.tar` survives because BT4 keeps it, and because the guard
    refuses every other backup while the record exists.

12. **Low: BK10 is dangling.** Gate A cites "BK10" for the backup units' text, and there is
    no BK10. **Fixed:** BU1 holds `teamchat-backup.{timer,service}` as data, as n2d did its
    drop-ins. No file is installed.

13. **Low: BK3 is looser than BK1.** It does not refuse a non-zero uid or gid, a user or group
    name, or a member time that differs from the manifest's. **Fixed.**

14. **Low: RC3's truncation is undefined.** Reading backwards, the cap keeps the newest
    events. **Fixed:** that is said, and the answer gains `first_ms`.

15. **Low: a password changed from Element after `N`.** C says it is "in the same list", but
    no roster write records it, so root cannot know it. **Fixed** (n2g-D19): the answer
    carries one fixed line about it, and nothing is guessed.

16. **Low: status codec.** `status.Restore.Pre` is a non-null `int64`, so 0 stands for
    `null`. HB2 adds the `revert` step and the `offline` mode. **Fixed.**

17. **n2g-D14: build the offline restore.** I found no reason it cannot be made safe. Every
    guarantee about credentials comes from the staged copy, which needs no live homeserver.
    The live server provides only `M` (an undo of a restore whose "before" does not start) and
    the export (which cannot be made). **Built** as RO1 to RO7: `--offline` is refused while
    the homeserver answers. It skips `M` and the export, states the loss, resolves a pending
    operation in the safe direction, and never starts the old database on a swap-back. The
    owner confirms only the defaults (n2g-D14).

18. **DC4 is testable, and kept.** The seam is `admin.Process` (`classOf`). BV4's test runs
    `backup`, `backups` and `restore` with the ctl uid and another uid, and expects 77 from
    each. No code inspects `SUDO_*` (D7). The password comes from the absence of any NOPASSWD
    rule, which Task 2.5's footprint gate checks.

19. **Verified, no change:**

    - Gate A: no file is installed; BU1 and D17 are data.
    - Gate B: RC1's random password is never stored, and a pool seat comes back only by
      ccy-teams' `member reissue --credential password`.
    - The roster `rev` window (an event put but not saved after a crash) leaves
      `pending: team.apply`, which RV5 refuses, so `current.rev + 1` is above the live event.
    - RM10's `rev` matches `planRoster`'s convention (the file's `rev` is the event's), so
      step 9's bootstrap writes no second roster event.

## The spike's three hazards, where each is now defended

| Hazard                                      | Defence                                                                                                                   | Test                                                                                                                  |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| `SIGUSR2` before the handler (kill or drop) | BT1 ready first, `InvocationID` unchanged up to the signal; BT2's timeout names it                                        | BT1: no `Kill` call when not ready, or when the invocation changed; RT1 on the pinned Tuwunel                         |
| `--restore-backup` wipes a non-empty target | RV2 (`db.restore-*`, `db.previous`), RS2's fresh check and `database_path` assertion, RU1 `Restart=no`, RS3's `NRestarts` | RV2 per leftover; RS2 with the path planted after validation; RU1 argv; RS3 with `NRestarts=1`; RT2 into a fresh path |
| the restored server goes on serving         | RS3's stop with `Result=success`, RS6's inactive check, RS10's stop, RU1's private network (H-C5)                         | RS3 and RS10 fault rows; RS6 with the staging unit active; RT3 restart and epoch read                                 |
