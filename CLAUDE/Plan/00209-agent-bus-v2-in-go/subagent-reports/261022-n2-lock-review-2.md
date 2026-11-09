MERGE

# Adversarial review, round 2: the team lock review lows (4bf447e0)

Scope: whether 4bf447e0 fixes round 1's findings 2-6 (`261022-n2-lock-review.md`) correctly and
with fail-first tests, whether the fixes add a defect, and whether the trust boundary of
`src-go/internal/teamchat/teamlock/teamlock.go` and the gates still hold. Round 1's finding 1
(provisioning `/run/teamchat-admin`) stays open for the deploy slice, as intended.

All five fixes are correct and none adds a defect. The two findings below are both low. Both are
about what the holder-line fix claims, not about lock correctness.

## Findings

1. **low: the holder-line comment and TL1 claim more than the code does**
   (`src-go/internal/teamchat/teamlock/teamlock.go:203-204`, `SPEC-n2e.md:53-54`).
   Evidence: the comment says "a waiter never reads an empty file between the two", and TL1 says
   "the file is never empty while held". Neither is true in the normal case. `Release` truncates
   the file to 0 (`:214`), so the next `take` gets its flock on an empty file. Between that
   flock (`:176`) and `WriteAt` (`:206`), a waiter whose own flock fails reads 0 bytes and
   prints "an unreadable holder". The fix removes the empty window in one case only: a stale
   line left by a holder that crashed without `Release`.
   The question asked was whether a mixed old/new line can pass the printable-line check and
   name the wrong holder. There are three windows, all in the stale-line case:

   - **Between flock and `WriteAt`:** the waiter reads the dead holder's whole line. It passes
     the check and names a pid that no longer exists. The old code (`Truncate(0)` first) had
     the same window, so the fix did not add it.
   - **Between `WriteAt` and `Truncate`, new line shorter than the stale one:** the file is the
     new line, its `\n`, and then the old tail. That newline is inside the text, so `holderRe`
     (`[ -~]`, no newline) rejects it and the waiter prints "an unreadable holder", not a wrong
     name. If the new line is the same length or longer, the file is just the new line.
   - **During the copy inside `WriteAt`:** a read can tear, because a regular-file read on
     tmpfs does not take the inode lock against a concurrent write. In theory that gives a
     prefix of the new line followed by the old suffix and its `\n`, all printable, which
     names a mixed holder. The window is nanoseconds, needs a crashed earlier holder, and only
     affects a message.

   Does it matter? No. The holder text appears only in a busy message that has already been
   sanitised, and exit 75, the flock itself and every decision are unaffected.
   Fix: make the wording match the code. In the comment: "writes the line over the old one and
   then cuts what is left, so a crashed holder's longer line is never left behind". In TL1, drop
   "never empty while held" and say instead that the holder named is the line as read, which may
   be empty or one hand-over old.

2. **low: `TestTakeReplacesAStaleLongerLine` is not a fail-first test**
   (`src-go/internal/teamchat/teamlock/teamlock_test.go:304`).
   Evidence: against 1525b3d3's `writeHolder` (`Truncate(0)`, then `WriteAt`), the file ends up
   as exactly the new line too, so the test passes on the old code. It does pin a real property:
   a later edit that dropped the `Truncate` would fail it. But it does not prove the behaviour
   the commit claims to change (the window), and no deterministic test can show a race that
   short. That is acceptable.
   Fix: none needed in the code. Describe the test as pinning the end state, not as proof of
   finding 3's fix (which goes with finding 1's rewording).

## Per finding of round 1

- **F2, ctx check (`teamlock.go:173-175`): fixed.** `ctx.Err()` is checked before each `Flock`,
  so an ended ctx takes no lock, even a free one. The check runs again after every `Sleep`,
  which is harmless. `TestTakeRefusesAnEndedContextOnAFreeLock` (`:271`) fails against
  1525b3d3: there the first `Flock` succeeds on a free lock and `Take` returns a held lock with
  a nil error, so `errors.Is(err, context.Canceled)` is false. Its second `Take` proves that the
  refused call left the lock free. Ctx stays a pre-check only, as TL1 says: a free lock is never
  taken under an ended ctx, and nothing blocks, because the call is LOCK_NB.

- **F4, busy error unwrapped (`teamlock.go:103-108`): fixed.** When `Close` succeeds, the
  `*BusyError` (or a ctx error, or the lock error) comes back as itself, and a close failure is
  still joined, so nothing is hidden. Every caller uses `errors.Is` or `AsType`, which see both
  shapes. `TestTakeBusyIsTheBusyErrorItself` (`:289`) fails against 1525b3d3, where `%T` was
  `*errors.joinError`.
  The cost: the close-failure branch (`:104-106`) is uncovered, and teamlock coverage drops
  from 93.4% to 91.1%. That is still at or above 90%, and a failing `Close` on a valid fd cannot
  be produced without a seam.

- **F3, holder write (`teamlock.go:205-210`): fixed, with the wording issue above.** One
  `pwrite` of the line, then `ftruncate(len)`. Every error path still closes the fd (`:111`).
  The line is at most 32+1+22+5+10+7+13+1 bytes, under `maxHolder`.

- **F5, refusal reasons and the chown test: fixed.** Each case in `TestTakeRefusals` (`:209`)
  now matches a substring of the error. I checked each substring against the message that
  actually fires:

  - `symbolic links`: openat on the link returns ELOOP.
  - `not a regular file`: the FIFO case.
  - `mode 0700, not 0600`.
  - `must be a directory, not a link`: the O_NOFOLLOW open of the directory link.
  - `not a directory`: Mkdir gets EEXIST, then the open gets ENOTDIR.
  - `cannot make the lock directory`: the parent is missing, so Mkdir fails with ENOENT.
  - `verb`: the verb check runs before any filesystem call.

  Each case now fails only for its own reason. These strings were already in 1525b3d3, so this
  part strengthens the tests and has no fail-first form; that is expected. Two of the
  substrings (`symbolic links`, `not a directory`) are errno text, not the program's own
  wording, which is acceptable.
  `TestTakeRefusesAChownedDirectory` (`:257`) runs here as root (euid 0, confirmed). It chowns
  the directory to 65534 and calls the public `Take`, so it exercises the real `os.Geteuid()`
  wiring, and it passes with the expected "owned by uid 65534, not by uid 0". When the tests
  are not run as root it skips, pointing at the seam test, which exists (`:322`).

- **F6, drift: fixed.** `host/apply.go:2` and `PLAN.md:237` now say HS1-HS15.

## Trust boundary, re-confirmed (`teamlock.go`)

The diff did not touch `openLock` or `lockDirectory`, and every property holds:

- `Mkdir` 0700.
- The directory is opened `O_RDONLY|O_DIRECTORY|O_NOFOLLOW|O_CLOEXEC`.
- `Fstat` on that fd requires mode `&0o7777 == 0o700` and uid == euid, and closes the fd on
  refusal.
- The file is `openat`'d relative to the same directory fd, with
  `O_RDWR|O_CREAT|O_NOFOLLOW|O_CLOEXEC` 0600.
- The file must be a regular file, checked by `Stat` on the opened fd.
- The file name is `filepath.Base(paths.TeamLock(team))` over a validated `TeamID`.

## Gates

- `go test -race -cover ./internal/teamchat/...` passes. Every package is at 90% or above:
  teamlock 91.1%, seat 90.3% (the lowest), notice 90.8%, state 90.7%.
- The five tests named above pass with `-count=1 -race`.
- `golangci-lint run ./internal/teamchat/...` reports 0 issues, with and without
  `--build-tags tuwunel`.
