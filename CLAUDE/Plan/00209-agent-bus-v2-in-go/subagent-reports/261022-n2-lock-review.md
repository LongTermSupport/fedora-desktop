MERGE

# Adversarial review: the unified team lock (1525b3d3), HS15 (363820d3), seatDevice in bc341061

Scope: `src-go/internal/teamchat/teamlock` and its callers (host apply/remove, admin verbs and
exit codes, health tick); the HS15 reload rule in host apply/remove; `apply/member.go`'s device
id after the n2d merge. Judged against DESIGN-v2-C-host.md 3.4, 3.5.2 and 3.5.3, SPEC-n2d.md
section 4 (HS1, HS2, HS15), SPEC-n2e.md TL1, and the 7-step port plan in
`subagent-reports/261022-n2e.md`.

No blocker and no major. The trust boundary holds. One minor is about the deploy slice still
to come, not about this code, and the lows are hardening and drift.

## Findings

1. **minor: nothing provisions `/run/teamchat-admin`, and the sandboxed timers cannot create it**
   (DESIGN-v2-C-host.md:1914 and :2093, against `teamlock/teamlock.go:143`).
   Evidence: the only thing that creates the lock directory is `teamlock.Take`'s `os.Mkdir`.
   The health unit (and the backup unit, which shares its sandbox) runs with
   `ProtectSystem=strict`, which mounts `/run` read-only, and with
   `ReadWritePaths=... /run/teamchat-admin`. That path has no `-` prefix, so systemd refuses to
   start the unit (NAMESPACE) when the path is missing. `/run` is a tmpfs. After every reboot,
   then, the health and backup units fail until some unsandboxed root verb (a play's
   `team apply`, or a seat take's member verb) has made the directory. Also, if a later play
   provisions the directory with any mode other than 0700 root:root (a `tmpfiles.d` default,
   or a `RuntimeDirectory=` of 0755), every root verb fails at `lock`. Failing there is correct
   fail-fast behaviour, but it must not be what the provisioning ships.
   Fix (deploy slice, C 3.5.1 static tasks): ship a `tmpfiles.d` line
   `d /run/teamchat-admin 0700 root root -` from the play, and say so in C 3.4 and TL1. Do not
   use a `-` prefix on `ReadWritePaths=`, because the tick then fails at `Mkdir` on the
   read-only `/run` instead.

2. **low: a cancelled context still takes a free lock** (`teamlock/teamlock.go:167-183`).
   Evidence: `flockWait` looks at ctx only inside `clock.Sleep`. With a ctx that has already
   ended and a free lock, the first `Flock` succeeds and `Take` returns a held lock.
   `TestTakeStopsWhenTheContextEnds` covers only the busy case. The verb then fails at its next
   ctx-aware call, so no harm is done, but TL1 says "ctx ends it".
   Fix: `if err := ctx.Err(); err != nil { return err }` at the top of the loop, plus a test
   with a free lock and a cancelled ctx.

3. **low: a waiter can read an empty line, or a crashed holder's stale one** (`teamlock/teamlock.go:103-107`, `:197-203`).
   Evidence: the holder is written after `flock` succeeds, in two system calls
   (`Truncate(0)`, then `WriteAt`). A waiter that gives up between the flock and the write
   reads one of two things. It may read the empty file and print "an unreadable holder". Or,
   after a holder died without `Release`, it reads the dead holder's line, which names a pid
   that no longer exists. The window is microseconds, and the output is sanitised either way,
   so the only cost is a misleading message.
   Fix (optional): write with one `pwrite` of the line, then `Ftruncate(len(line))`. That
   removes the empty window. Alternatively, document in TL1 that the holder named is the line
   as read, which may lag one hand-over.

4. **low: the busy error comes back wrapped in a join** (`teamlock/teamlock.go:104`).
   Evidence: `errors.Join(err, f.Close())` with a nil close error returns a `*joinError` that
   wraps the `*BusyError`. `errors.Is` and `errors.AsType` see through it, and every caller
   uses them. A plain type assertion would miss it, though, and TL1 says the busy lock "is a
   `*BusyError`".
   Fix: close first, and join only when the close fails
   (`if cerr := f.Close(); cerr != nil { return errors.Join(err, cerr) }; return err`).

5. **low: the tests do not check why a refusal failed, and the owner check runs only through the seam**
   (`teamlock/teamlock_test.go:208-262`).
   Evidence: `TestTakeRefusals` asserts only "not taken, not busy". I mutated each check
   mentally (dropped O_NOFOLLOW on the directory or the file, dropped `IsRegular`, dropped the
   mode test), and each case still fails the test, so the table does discriminate today. A
   future case could still pass for the wrong reason. The owner check is proven only through
   `take(..., euid+1)`. That does exercise the real `Fstat` uid against a non-matching euid,
   so as a seam it is adequate. But the tests run as root, so a stronger proof is free:
   `os.Chown(dir, 65534, 65534)` and the public `Take`, skipped when not root. That covers the
   real `Geteuid` wiring with no seam.
   Fix: give each refusal case an expected substring of the error (`link`, `regular`,
   `0700`, `owned`, `verb`), and add the chown-based owner test.

6. **low: drift in the doc comments** (`host/apply.go:2`, `PLAN.md:237`).
   Evidence: the host package doc cites "SPEC-n2d.md section 4, HS1-HS14", but HS15 now exists
   and is implemented in this package. The PLAN line for slice n2d says the same.
   Fix: change both to HS1-HS15.

## What I checked and found sound

- **Port contract (n2e report, steps 1-7):** every step is done. `Take/Team/Release/BusyError/ErrBusy/Clock/SystemClock`
  live in `teamlock`. `Is` on `BusyError`, the sanitised `holderOf` and the verb regex are
  folded in. The owner check is added. `host.PlayWait` and `admin.SeatWait` stay with their
  callers. Every caller is ported, health's `Releaser` and `TryLock(ctx, team)` included.
  `host/lock.go` and its test are gone, and no `TakeLock`, `host.BusyError` or
  `host/lock.go` reference remains. TL1 is rewritten, and HS1 and HS2 point at it.
- **Trust boundary:** `/run` is root's 0755, so no member (and no container, which has its own
  `/run`) can create `/run/teamchat-admin` first. The directory is opened
  `O_DIRECTORY|O_NOFOLLOW` and checked through its fd for mode exactly 0700 (setgid and sticky
  are refused too) and for owner equal to the euid. The file is `openat`'d relative to that
  same fd, so there is no TOCTOU between the check and the open. The file is opened
  `O_NOFOLLOW` (a dangling link is ELOOP, so `O_CREAT` cannot plant a file elsewhere) and
  must be a regular file (a FIFO is refused; `O_RDWR` on a FIFO does not block on Linux).
  Inside a 0700 root directory only root can place a hardlink, a device or a FIFO, so the
  unchecked nlink and owner of the file are not reachable by a member. The health unit's
  bounding set (no `CAP_DAC_OVERRIDE`) is fine because root owns the directory.
- **Holder line:** what root writes is `verb` (regex `^[a-z][a-z.-]{0,31}$`) + `TeamID`
  (grammar `^[a-z][a-z0-9-]{0,21}$`) + pid + ms, all printable and well under 256 bytes. On
  read, at most 258 bytes are taken, one trailing newline is trimmed, and the result must be
  1-256 bytes of `[ -~]` (Go's `$` without `m` anchors at the end of the text). Otherwise it
  is "an unreadable holder". Escape sequences, embedded newlines, over-long lines and empty
  lines never reach a terminal or the journal, and the tests cover each of these cases.
- **Holder that never releases or crashes:** flock dies with the fd, so a crashed holder
  frees the lock. A holder that hangs is bounded by the caller's wait (60 s, 10 min, or 0 for
  the tick), and the tick drops with `busy: ...` and never waits (wait 0, so no Sleep, which
  `TestTakeBusyCarriesTheHolder` asserts).
- **fd hygiene:** every error path closes what it opened. `lockDirectory` closes on a failed
  `Fstat`, mode or owner check. `openLock` always closes the directory fd, and closes the
  file fd if the directory close fails. `take` closes the file on a failed wait or a failed
  holder write. `Release` evaluates truncate, unlock and close all three, even when one
  fails. A double `Release` errors (on a closed `os.File`) and cannot hit a reused fd.
- **Exit 75:** `verbExit` tests `errors.Is(err, teamlock.ErrBusy)` first. A member verb's
  busy error reaches it directly. `team apply` and `team remove` wrap it in `*host.Error`,
  which unwraps. `printChanged` and `failWith` both go through `ExitCodeOf`. Tested by
  `TestVerbBusy` (member add and role apply, end to end, holder printed on stderr) and by
  `TestExitCodeOfTheHostAndTheChild` (host.Error wrapping a BusyError).
- **No escaping path:** the file name is `filepath.Base(paths.TeamLock(team))` over a
  validated `TeamID` (a zero id panics in `text()`), and `dir` comes from `Deps`, not from
  input. No caller can name another file.
- **Lock order (C 3.4):** each verb takes one team lock. The tick takes it per team, never
  two at once, and `restart` releases before `write` takes it again. The backup gets the
  held `*teamlock.Lock` and never re-opens the file.
- **HS15 (363820d3):** `loaded` is true only for `answerNothing`/`answerWrite`, which both
  need a drop-in present and `units.json` digests equal to the render. Any drop-in write
  clears it, and `reload` runs before every start or restart (step 7, and the backup start in
  step 6). `units.json` and the stamp are written only in step 11. So a failed reload, or a
  crash between the write and the reload, is retried until one succeeds, and is never
  stamped first. An active unit in step 6 needs no reload (the running binary takes the
  backup). Remove reloads on every remove of an installed team, and `installed` stays true
  while `root/<team>/` exists, so a failed reload is finished by the next remove. The six
  tests check the order (`before(daemon-reload, restart)`), that what systemd loaded matches
  what is on disk, and the stamp.
- **seatDevice (bc341061):** the local `const seatDevice = "TCSEAT"` was replaced by
  `paths.SeatDevice`, which is the same constant, `"TCSEAT"`, pinned by
  `paths_test.go:299`. That is the value `seat/credential.go`, `seat/records.go` and
  `health/view` use. The behaviour is identical, and there is now one source.
- **Gates:** `go test -race -cover ./internal/teamchat/... ./cmd/teamchat-admin` passes, and
  every teamchat package is at 90% or above (teamlock 93.4%, host 93.2%, admin 95.1%, health
  91.9%). `cmd/teamchat-admin` is at 50.0%, but that package is unchanged by these commits,
  and the floor is go-qa's per-module one. `golangci-lint run` reports 0 issues with and
  without `--build-tags tuwunel` over `./internal/teamchat/...`. The tests read no wall clock:
  a fake `Clock` drives every wait, and `SystemClock` is tested only with an ended ctx and a
  0 sleep.
