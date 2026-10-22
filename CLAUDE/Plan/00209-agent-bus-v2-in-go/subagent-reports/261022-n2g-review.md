Verdict: FIX-FIRST

# n2g adversarial review (bd2ce23f4..HEAD, src-go)

Scope: `git diff bd2ce23f4..HEAD -- src-go` against SPEC-n2g.md. Read in full: restore/engine.go,
validate.go, fs.go, renames.go, merge.go, argv.go, guard.go, records.go; backup/take.go, meta.go,
tar.go (framing and decode); admin/exit.go, restoreverb.go, restoreengine.go, backupverbs.go,
restorechild.go, serverestore.go (RC1/RC3/RC8), tuwunel_test.go; ctl/restore.go; wire.go diff;
health restores.go and probe.go; teamlock diff; restore/engine_kill_test.go.

Runs (all green):

- `go test -race -cover` over restore, backup, admin, admin/wire, ctl, health, host, units,
  teamlock, paths: every package 92.0% to 100% coverage.
- `-tags tuwunel` with FAKEHS_TUWUNEL_BIN: RT1, RT2, RT3 and RT4 pass, and so does the
  tuwunelhs package.
- Two scratch probe tests, written as untracked `zz_review_scratch_test.go` files and then
  removed. `git status` is clean at the end.

## Findings

1. **should-fix**: a restore whose homeserver dies after validation exits 70 at RS4, not 69.

   - Where: `restore/engine.go:388-397` and `admin/exit.go:47`.
   - What is wrong: RV7 proves that the live server answers. RS4 then runs `backup.Take`, whose
     `ready` returns `backup.ErrNotRunning` once the server has gone. `fromStaged` and
     `stillStaged` wrap it with `%w`, and `verbExit` maps ErrNotRunning to `ExitRefused` before
     anything else is checked. The verb therefore reports a refusal (70) for a restore that is now
     recorded `staged`.
   - Spec broken: RS15 says "69 when a homeserver that answered at validation is lost mid-step".
     BT1's 70 is for the `backup` verb only. Exit 70 also tells a caller that nothing was done,
     yet restore.json now exists and the guard blocks every verb.
   - Evidence: a scratch test built
     `fmt.Errorf("restore not done, staged: %w", fmt.Errorf("the pre-restore backup: %w", &backup.Error{Step: "ready", Err: fmt.Errorf("%w: ...", backup.ErrNotRunning)}))`,
     and `ExitCodeOf` gave `exit = 70`. No test covers an RS4 failure's exit code.
   - Fix: map ErrNotRunning from inside the restore engine to 69. One way is for `fromStaged` to
     wrap it as a lost error; another is to check ErrNotRunning only on the `backup` verb's path.

2. **should-fix**: in RT3 the SIGKILL leg does not test what it claims.

   - Where: `admin/tuwunel_test.go:383-389`.
   - What is wrong: the loop stops the secured server with SIGTERM, restarts it, reads it, then
     sends SIGKILL to that second, restarted process. By then the SIGTERM shutdown has already
     flushed RC1's writes. The SIGKILL case therefore never kills a server holding writes that
     the secure op has just made and that no clean stop has flushed.
   - Spec broken: RT3 (and RE1/RS3): "After RT2's secure, the staged server is stopped once by
     SIGTERM and once by SIGKILL". That means two independent runs, each straight after a secure.
     RS3 rests on the claim that "a reopened database that serves the event proves the
     revocations before it are on disk", and a kill straight after the secure is exactly the
     case that tests it.
   - Evidence: `for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL} { s.Stop(sig); s = tuwunelhs.StartWith(...); w.wantServed(...) }`.
     Only the first pass stops the process that `w.secure` wrote to.
   - Fix: give each signal its own fresh stage and secure (a subtest per signal). Then SIGKILL
     hits the server that ran RC1.

3. **should-fix**: the health tick's restart does not re-check restore.json under the lock.

   - Where: `health/probe.go:71` (the check) and `health/probe.go:150-200` (`restart` and
     `restartLocked`).
   - What is wrong: `f.restore` is read in `gather`, without the lock. The two re-probes then
     take ProbeGap x 2 = 20 s, after which `TryLock` and `Units.Restart` run. Inside the lock,
     `restartLocked` re-checks the stamp, for this very race, but never the restore record.
     Suppose a restore takes the lock in that window and leaves a record behind: `staged` after a
     failed RS4 or RS5, or `revert` after RS11 step 7 failed. The lock is released, and the tick
     then restarts or starts the unit. `systemctl restart` starts a stopped unit, and it does so
     without RS17's check.
   - Spec broken: HB3 says the tick "never restarts a team that has a restore.json". RG3 says
     the tick "refuses only its restart".
   - Fix: in `restartLocked`, read `Restores.State` again under the lock, just as the stamp is
     re-read, and do nothing while a record exists. Add a test where the record appears between
     gather and TryLock.

4. **should-fix**: under --offline, a pending `member.retire` of a handle added after N becomes
   `lost` and appears in `return`.

   - Where: `restore/merge.go:167`.
   - What is wrong: in `resolve`, the case
     `!inMerged || !inBak || merged.State == retired -> "left as the merge has it"` comes before
     the `OpMemberRetire` case. For a handle that only the current roster holds, `mergeMember`
     has already set it `lost` and put it in `Return`. The owner was retiring that handle, and the
     board now tells them to run `member return` for it.
   - Spec broken: RM12: "a member `retire` makes its handle `retired`, in `retire`". The rule has
     no exception for a handle that is new since N.
   - Evidence: a scratch test with current `dev-1` (active, cred_seq 8, pending member.retire),
     no `dev-1` in the backup (seq 5), and `Merge(..., Offline: true)` gave `state=lost retire=[] return=[dev-1] pending="member.retire of dev-1: left as the merge has it"`.
   - Fix: test for a retire before the `!inBak` case. It should set `retired` and drop the handle
     from Return. Retire is harmless when the account does not exist, since `serveSecure`'s retire
     skips a missing account.

5. **nit**: RS3's verify reads the current rooms.json, not the tar's.

   - Where: `admin/serverestore.go:410` (`serveCheck` calls `roomsOf(team, o.Deps)`).
   - What is wrong: RC1 writes the roster event into the tar's main room (`a.rooms.Main`). The
     staged `restore.check` reads the main room from `ctl/<team>/rooms.json`, which still holds
     the current rooms until RC2 runs.
   - Effect: if the main room was re-made after N, every restore fails at step 05. It fails safe,
     but no restore can succeed. RC8's args carry no rooms, so the spec is silent here.
   - Fix: pass the tar's rooms to the staged check, or name the limitation as a decision.

6. **nit**: the offline revert's message blames a start that never happened.

   - Where: `restore/engine.go:600-601`.
   - What is wrong: every offline revert says "the restored database did not start", including
     one caused at RS6 (a unit not stopped, a rename, or RC2) before any start was tried. RO6's
     wording fits only the RS7 path.

7. **nit**: `backup --all` waits up to 10 min for each busy team.

   - Where: `admin/backupverbs.go:98`.
   - What is wrong: each team's lock wait is BackupWait (10 min), and BU1's unit has
     `TimeoutStartSec=30min`. Three teams with long-held locks (for example a stuck apply) make
     systemd kill the run, so the teams after them get no backup and no stderr line.
   - Spec: BV2 asks only that a team which is down never stops the others, and a down team does
     not block, so this is a nit. A shorter wait per team under `--all` would close it.

8. **nit**: SM5's kill-at-every-effect runs over the happy path only.

   - Where: `restore/engine_kill_test.go:60`.
   - What is wrong: `TestRunKillAtEveryEffect` iterates over `happyCalls` and `offlineCalls`
     only. The RS11 revert path (block, stop, renames toward unswapped, fsync, RC5, block
     removal, serve, record at staged) has single-failure tests (`TestRevertFailureIsResumed`,
     `TestRevertRecordFailure`) but no kill at each of its effects. SM5 and RS12 say "every call
     of every row".
   - Status: walking each kill point by hand found no defect, since every step is repeatable.
     This is a gap in test coverage, not a behaviour bug.

## Checked and found sound

- RS16 renames: the table rows, refusal of anything that is not a directory, repeat and kill
  safety. RS17's `Startable` runs before both live starts.
- The trust boundary in `restore.OS`: openat2 with RESOLVE_NO_SYMLINKS|BENEATH|NO_XDEV. `Kind`
  uses O_PATH, so it never opens a FIFO. RemoveTree goes level by level with no-follow, and the
  state root's owner is checked.
- backup meta reads are no-follow, O_NONBLOCK, regular only and capped. The BK3 tar parser checks
  headers, padding, end blocks, PAX/GNU, owners and names.
- No secret in an argv: the token goes on stdin through `systemd-run --pipe`. The env is cleared
  (RU3). BK3's errors never quote content.
- BT1's invocation re-read before the signal, BT2's timeout joined with ErrLost (69), and BT5's
  last-run written on every path.
- `backup --all` goes on past a team that fails, and exits 1. A team down is pinned by a test.
- RG1/RG2: a nil guard is refused, and the guard runs under the flock. Resume and discard use
  `Recorded`. RW2: production answers ErrUnproven (70) before the lock.
- RE1: the epoch floor is written before the record reaches `swap`. The verify compares epoch and
  rev before `staged`, and Syncfs runs before `staged`.
- RT1, RT2 and RT4 exercise what they claim: RT2's plant is wiped, L is left untouched, and the
  pool token, the reissued seat and alice's N-era password are all refused.
