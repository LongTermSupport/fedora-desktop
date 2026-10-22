FIX-FIRST

# SPEC-n2g review, round 2 (cold read)

Reviewed: `SPEC-n2g.md` at 4047f682f, against C 3.12 (DESIGN-v2-C-host.md:2087-2324), C 3.2.3
(:235-316), the spike notes, the built code under `src-go/internal/teamchat/`, A 5.1
(DESIGN-v2-A-protocol.md:520), B 2.6 step 08 and SPEC-n3 SY1/SY6/SY7 and gap 06.

The engine is well laid out, and most of the kill orders I tried come back safe. Four state
machine holes stop a resume or a revert, or let an empty or unsecured state serve, and one
credential hole is in `--roster-from-backup`. They are cheap to fix in the text.

## High

1. **(high) `--roster-from-backup` revokes nothing that changed after N.** RM11 (:296-307)
   makes the backup roster the base, but RM4-RM7 judge "after N" from the *current* roster's
   `cred_seq` and `revoked_seq`, which are unreadable here. Example: an `active` agent handle
   reissued after N has `cred_seq <= B` in the base, so it is neither revoked nor reissued. The
   restored database then accepts the token the live reissue had revoked. A human whose password
   was reset after N gets the old one back. This breaks C 3.12.2's "no revoked credential is ever
   served" (:2133-2135), and design and spec both say nothing about it.
   *Fix, add to RM11:* "With the current roster unknown, every credential is treated as changed
   after N: every handle not `retired` in the base goes in `revoke`, an `active` one also gets
   `needs: reissue` and goes in `reissue`, and every human in the base goes in `revoke_humans`
   and `password`. `rev` is `base.rev + 1`." Add a test: a handle reissued after N, restored with
   `--roster-from-backup`, has its N-era token refused (RT2's row, run in this mode too).

2. **(high) A kill in step 10 followed by a failed step 09 on resume gets stuck.** RS8 removes
   `db.previous` first (:415). The record is still `start`, so a kill after RS8.1 resumes into
   RS7 then RS8 (RS12 :457). If RS7 then fails (for example a transient `restore.check` error),
   RS11 reverts. RS16 finds `db` alone, refuses it and "the record is left as it is" (:481-482):
   the record is at `revert` with the block in place and the team down. `--resume` refuses again
   for ever, and `--discard` is refused at `revert` (RS13 :462). Nothing in the spec gets out of
   that state. The spec also never says what a failure in step 10 does (RS11 covers 06-09 only).
   *Fix:* add a record step `clean`, written (fsync) before RS8.1. A failure or kill at `clean`
   never reverts; `--resume` at `clean` runs RS8 only. Every RS8 sub-step counts "already absent"
   as done (`RemoveTree` of a missing `db.previous`, RC4 with no `*.previous`). Say what a
   failure of RS8.5 (status refresh) after the record is gone answers: "restored; status refresh
   failed: <e>" (1). Add `clean` to HB2 ("restore interrupted"), RS12 and SM5's kill sweep.

3. **(high) The "stopped" check refuses a failed unit, so offline restore and an online revert
   cannot proceed.** RS6.3 and RS11.3 (:401-402, :440) say the live unit "must be inactive or
   unloaded". systemd keeps a unit `failed` after a stop until it is reset or started again. A
   unit whose start is refused by `AssertPathExists=` also ends up `failed`. So the crash-looping
   unit that RO is for (RO7's own test, :517-518, alternates `activating`/`failed`) fails RS6.3
   every time. An online revert after the restored database crash-loops fails RS11.3 the same
   way. That is a "swap-back failure" (record at `revert`, team down), and every resume repeats
   it.
   *Fix (RS6.3, RS11.3, RS10.1, RS13):* "stopped means `inactive`, `failed`, or not loaded,
   read after `Stop` returns. `activating` (auto-restart) and every other state is refused." Add
   fake tests that report `failed` after `Stop`, both in RO7's crash loop and on the online
   revert path.

4. **(high) A resume at `swap` never removes the block, so RS17 refuses the start.** RS12's
   `swap` row (:455-456) is "block ensured, live stopped, RS16, control swap repeated, then RS7
   and RS8". It leaves out RS6.6 (record to `start`, block removed). RS17 (:485-488) refuses any
   start while `restore.block` exists, so a kill anywhere in RS6.2-RS6.5 can never be resumed.
   *Fix:* "`swap`: RS6 from its step 2 (the record already `swap`; the block written if absent),
   then RS7 and RS8." Make SM5 assert the end state of every `swap`-step kill.

5. **(high) Nothing makes the renames durable before the block goes.** RS6.4 and RS11.4 rename
   inside the state directory, then the record is advanced and the block removed in
   `root/<team>/`, which is another directory and may be another filesystem. `state.WriteFile`
   fsyncs only its own parent (state/state.go:53,63). After a power loss the record can read
   `start` with the block gone while the state directory comes back as `p r`, with no `db`. At
   boot the unit (enabled, no block) then starts Tuwunel, which creates an empty database and
   serves it. RS17 guards only the engine's own starts. This is the failure C 3.12.2 promises
   never happens (:2240-2242).
   *Fix:* "After the RS16 renames, the state directory's fd is `fsync`ed before the record is
   advanced (RS6.4 to RS6.5, RS11.4 to RS11.5)." Add `Fsync` to SM4's `FS` and a fake test that
   the fsync comes before the record write.

## Medium

06. **(medium) RS7's epoch check runs after members can reach the restored database.** RE1
    (:353-365) says no member request "ever reaches" a restored database with another epoch, and
    lists "RS7 reads the served epoch before anything else is done". But `host.Serve` (SM3) starts
    the live unit and waits ready on the team's listen addresses before RC8 runs, so members can
    sync from then on. RS7 only notices a bad epoch. The prevention is RS3's stop, syncfs and
    prefix argument, proved by RT3. SPEC-n3 gap 06 (n3a SPEC-n3.md:725-729) models "no request
    ever sees the old epoch" as a hard guarantee. If RS3 were wrong, the backup's epoch could be
    one members already hold, and they would keep a stale `since` (H-C14).
    *Fix (pick one):*
    (a) Add an RS3 verify step before `staged`: start the staging unit again with RU1's argv
    *without* `--restore-backup` (so RS2's wipe hazard does not apply), run RC8 through RU2, and
    require the merged epoch and rev. Then stop, syncfs, record `staged`.
    (b) Reword RE1: "RS7 detects; it does not prevent. The prevention is RS3 and is proved only by
    RT3." Tell the n3 owner that gap 06 rests on RT3.
    (a) is the one that makes RE1 true as written.

07. **(medium) A resume at `start` fails `restore.check` on a correct restore.** RS7 and RC8
    require the served `rev` to *equal* the merged one (:408-409). The bootstrap that follows
    (`team.apply`) rewrites the roster event whenever its content changes, for example when a
    room created after N is recreated. `planRoster` writes `rev = NextRev(event, file)` and sets
    the file to match (rooms/rooms.go:364, apply/apply.go:286-287). So after a kill once the
    bootstrap has run, the resume's check reads `R+1` and fails, and RS11 reverts a restore that
    had worked.
    *Fix (RC8/RS7):* "`restore_epoch` must equal the merged one, and `rev` must be at least the
    merged one." Add a test: a kill after the bootstrap's roster write, then a resume, completes.

08. **(medium) The epoch can repeat a value that served, so "H is the stronger bound" is
    wrong.** Under RM11 the drawn value `r` is in `[0, 5e8)`, avoiding only the backup's epoch and
    H. RS6.1 writes H only when `r` is above it. So after a `--roster-from-backup` restore with
    `r < H`, the live epoch is `r`, and H no longer bounds it. A later `--roster-from-backup` draw
    may give `r` again, or 0, or any older epoch that served. That contradicts RE1's last line
    (:365) and n2g-D20. A 5.1 makes 0 the default and only restores raise the epoch, so H does
    bound every served value as long as no draw goes below it.
    *Fix (RM11):* "a value from the injected random source in `(max(H, backup epoch), 5e8)`,
    refused when that range is empty". RS6.1 then always raises H, and RE1's "never repeats" holds
    absolutely. Delete "and is the stronger bound".

09. **(medium) RO1 allows `--offline` for a team that is only stopped.** RO1's 30 s
    `ServerVersion` probe (:497-501) cannot tell "the database will not start" from "the unit is
    stopped" (an owner's `systemctl stop`, a boot with the unit disabled). RV7's refusal message
    then points the owner at `--offline`, which gives up M and the export (RO5) on a database that
    was healthy, then deletes it (n2g-D14).
    *Fix (RV7/RO1):* "When the live unit is `inactive` (not `failed`, not crash-looping),
    `restore` first starts it with `host.Serve` (SM3, 60 s). `--offline` is allowed only when that
    start fails, or the unit is `failed`/`activating` with `NRestarts > 0`, and the answer quotes
    the failure." Add a test: a stopped healthy unit with `--offline` is refused (70) and the unit
    ends up serving.

10. **(medium) Seams are missing for expectations that already exist.** SM4 (:96-104) has no
    `Ready` (needed by RV7 and RO1's probes), no reader of the status record (RM11), no
    `Approvals` (RV6's `declare.Hold`), and its `FS` has no `ReadDir` (RV2's `db.restore-*`), no
    `Syncfs` (RS3) and no `Fsync` (item 5). The trust rule requires the base fd to be `fstat`ed
    uid 0 (:55), which a non-root `go test` over a temp directory can never meet, and SM1/SM4
    inject no owner uid. So BT2, HB1 and RS16 cannot be red/green tested as written.
    *Fix:* add to SM4 `Ready`, `StatusRecord func(team) (*status.Record, error)` and `Approvals`.
    Add `ReadDir`, `Syncfs` and `Fsync` to `FS`. Add `StateOwnerUID` to SM1 and SM4 (0 in `wire`,
    the test's uid in tests), as `ReadOwnedFile` already takes a uid.

11. **(medium) The kill sweep cannot reach root's own writes.** SM5 (:105-109) kills at "the
    k-th call" of a fake, but RS1, RS2, RS6.1, RS6.2 and RS11.2 write with `state.WriteFile`
    directly. Those writes are not fake calls, so for example a kill between "record to `swap`"
    and "`restore.block` written" (RS6.2) is never tested, and neither is a kill between H and the
    record.
    *Fix:* "Every file root writes or removes in `root/<team>/` during a restore goes through
    `restore.Deps.FS` (`WriteFile`, `Remove`), so each one is a kill point of SM5."

## Low

12. **(low) RS11's failure text is wrong for a failed step 7.** RS11 says a swap-back failure
    leaves "the block in place" (:448), but the block is removed at step 6, before step 7 starts
    the old database. *Fix:* "A failure at steps 2-6 leaves the block; a failure at step 7 leaves
    the record at `revert` with no block and the unit failed. `--resume` repeats from step 2."

13. **(low) The trust rule contradicts D2.** :47-48 allows only ctl's files as an exception, yet
    BT2, BT4, HB1, BV3 and RV4 decide from the homeserver-written `meta/` (n2g-D2 accepts this).
    *Fix:* add a second exception, "Tuwunel's backup numbers in `backups/meta/` (n2g-D2: a forged
    number costs only that team's own tars)".

14. **(low) Two exit codes are given for one case.** RV7 calls a homeserver that does not
    answer a refusal (70, :342-343). RS15 gives "69 for an unreachable homeserver" (:469). BV1
    has no 69. *Fix:* state which applies: 70 at RV7, 69 only for an export or check that loses
    the server mid-step. Give BV1 the same rule.

15. **(low) RS13's "stops the units" is ambiguous.** At `staged` after an online revert, the
    live unit is serving and must not be stopped. A discard after `pre` is set may also destroy
    the only copy of N (C :2220-2221). *Fix:* "stops the two staging units only. When `pre` is
    set and `meta/<N>` is gone, the answer says the discarded copy was the only one of backup N."

16. **(low) The staging unit's start timeout is not stated.** RU1 drops every `Timeout*` line,
    so the start timeout is systemd's default (90 s). With `--service-type=notify` and no
    `--wait`, the start job completes only at READY, after the restore has finished. *Fix:* give
    RU1 an explicit `TimeoutStartSec=`, sized against H-C1's largest database, with H-C5 to
    confirm READY is sent under `--restore-backup`.

17. **(low) Hazards 2 and 3 have no unit test over fakes.** RS2's planted-path refusal is
    defended only by RT2, which needs the real Tuwunel. *Fix:* add "Tests: `db.restore-<N>`
    planted between RV2 and RU1 is refused and nothing is started; RS3 records `staged` only
    after `Stop` and syncfs (fake call order); RS6 refuses while a staging unit is still
    `active`."

18. **(low) Failures at steps 06/07 and at RS8.5 have no stated answer.** RS11 says only "the
    record stays `staged` and the team serves". *Fix:* "answered 'restore not done, staged:
    resume or discard' (1)".

19. **(low) Names and references are missing.** The tracked spec cites the untracked spike's
    labels "(b)" and "(d)" (:373, :584). Inline those facts instead. The package list (:65-77)
    leaves out `paths`, which is the single source of every v2 name (src-go/CLAUDE.md
    teamchatseparate). Add `paths` with `restore.json`, `restore.block`, `restore-epoch.json`,
    `config.restore.toml`, `db.restore-<N>`, `db.previous`, `backups/last-run.json`, and the
    `teamchat-restore-ctl@` unit name (SD10's `checkUnit` needs it). RS8.5 changes
    `host.Status.Refresh`'s signature (today `Refresh(ctx, team, bool)`, host/apply.go:831), and
    the `host` line should say so.

20. **(low) Design text the spec drops without saying.** RG3 leaves out the human verbs, `lxc apply`, `redact` and `migrate` (C :2227-2229). Those are not built (admin/verbs.go:86-95),
    and RG1's required `Guard` will catch them once they are. Note that RM7's `lock` must cover
    the human `locked` state when `human lock` lands (only `active`/`removed` exist,
    apply/files.go:37-38). Under RM12, clearing a pending `team.apply` bootstraps the *applied*
    `team.json`, so the newer declaration that apply was writing is dropped until the next `team apply`. RS9 should say that.

## Checked and sound

- Gate A (BU1, n2g-D17 as data only), gate B (RC1's random passwords are never stored), DC8
  (RW2 refuses before the lock), DC4 (`backup`, `backups` and `restore` are root's class,
  admin/run.go:131-133, `classOf` uid/euid, class.go:37).
- No secret in argv (RU1 passes a path, RU2 sends the request on stdin). Link-safe `RemoveTree`.
- RS16's table gives the right next step from every two-entry state in both directions. RC2 and
  RC5 are idempotent when killed per file.
- The epoch survives a revert followed by a resume (members drop on any different value,
  SY6 "lower included"), and a discard after an epoch has served (H, n2g-D20).
- BT1's double read narrows the early-SIGUSR2 race, and what is left is a stated BT2 timeout.
  The staging unit's `Restart=no` is tested in its argv.

## Disposition (the coordinator of the review)

All 20 findings are applied to SPEC-n2g.md. The applied forms differ from the proposals in
these places:

- **1.** The proposal is applied as given, and n2g-D21 records it. One more defect turned up
  while applying it. `seq` restarts from `B + 1`, so a backup taken before the restore could
  later be judged against seqs that are reused. RM11 now sets `min_backup` in
  `restore-epoch.json`, and RV4 refuses an older `N`. `M` cannot be taken under
  `--roster-from-backup` (BK4 needs the roster), so RS4 is skipped there and the answer says
  so.
- **2.** The `clean` step is added. For RS16's refusal at `revert`, the one state no step can
  repair, `--discard` is allowed once the record holds `stuck`. It removes only the record and
  keeps the block. This is listed as an open item in the handoff.
- **6.** Option (a) is applied: RS3's verify reopens the copy without `--restore-backup` and
  runs RC8 before `staged`. RE1 now says RS7 detects and RS3 prevents.
- **9.** RV7 starts an `inactive` unit with `host.Serve` before judging it. RO1's probe comes
  after that start.
- **16.** `TimeoutStartSec=15min`, with H-C5 confirming READY and the time.

No round 3 is run: the brief allows two rounds.
