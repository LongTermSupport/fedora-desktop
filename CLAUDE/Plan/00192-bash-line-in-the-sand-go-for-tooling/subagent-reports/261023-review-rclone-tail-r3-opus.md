# Review: rclone-tail Go port, round 3 (Opus, adversarial)

Branch `go-port/rclone-tail` at `478a613cf`, diff against F44. Earlier rounds:
`261023-review-rclone-tail-r1-opus.md` and `261023-review-rclone-tail-r2-opus.md`.

## What I ran

- `go test -race -count=3` over `internal/rclonetail`, `internal/rclonerc`, `internal/rclonecache`,
  `cmd/rclone-tail` and `internal/ftpcamera`: all pass. `golangci-lint run` over the first four
  packages reports 0 issues.
- **Mutation check.** I used `go test -overlay`, so no file under src-go was changed. The overlays
  are in `untracked/scratch/rtrev3/`. Each one undoes one part of the 478a613cf fix, and every one
  turns the suite red:
  - m1: `untilDone` passes every write through. `TestSecondStopDuringClosingSnapshot` and
    `TestSecondStopDuringOpeningSnapshot` fail.
  - m2: `detached` no longer refuses a command after the context has ended. The same two tests fail.
  - m3: `snapshot` no longer turns a cancel into exit 130. The same two tests fail.
  - m4: the second stop no longer calls `abort`. The same two tests fail.
  - m5: `onSignal` goes back to a single `select`. `TestOnSignal`'s second SIGTERM then has its
    default action and kills the test binary (exit 143).
- **Real processes.** I rebuilt the binary into `untracked/scratch/rtrev2/bin/` and ran it over
  round 2's fakes: an `rclone rc` that takes 4 s, and a fake mount process. Each run was in a
  process group of its own, and the signals went to the whole group (`rtrev3/run.bash`).
  - Round 2's `orphan2.bash` (SIGINT during the opening snapshot, SIGTERM 1.5 s into the closing
    one): exit 130, no `rclone rc` left, and the closing snapshot printed only its heading.
  - `--once`, with a TERM during the tick and a TERM 1 s into the closing snapshot: exit 130 and
    no child left. Stdout was the heading, an empty line and the snapshot heading. Stderr was empty.
  - Streaming, with two TERMs sent back to back during the opening snapshot: exit 130 and no child
    left. The second signal was not lost to the 1-slot channel.
  - Streaming, with one TERM during a tick: exit 0. The full closing snapshot was printed and no
    child was left.

## Round 2 L1 fix (478a613cf): checked

- **Signal goroutine and the run.** The OnStop closure (`app.go:156-161`) runs only on the single
  `onSignal` goroutine, so the check of `stopCtx.Err()` and the `abort()` after it are never
  interleaved with another stop. Context cancel is safe under concurrent use, and `-race` is clean.
  `stopCtx` is a child of the aborted `ctx`, so an abort also ends the ticks.
  - A signal that arrives before `OnStop` subscribes has its default action. No child exists at
    that point, so nothing is left behind.
  - A signal that arrives after `release` has its default action too. By then every child has
    already been waited for.
- **Stop and abort order.**
  - First stop: the tick's calls run on `stopCtx`, so the execx session run kills their groups. The
    snapshots run on `ctx`, so an opening snapshot already under way finishes (E15).
  - Second stop: `ctx` is cancelled. Two things then stop the snapshot. `detached` (`app.go:353-358`)
    refuses any later child, and the execx `runSession` SIGKILLs the group of the call in flight.
    Exec's own `Cancel` hook does the same from its watch goroutine. The run reads `ctx.Err()` and
    returns 130 (`app.go:231-233`).
  - A second stop that comes before the closing snapshot starts gives the empty line and then exit
    130, with no snapshot. That follows from E15.
- **Output leaks.** `untilDone` covers both rclonecache streams.
  - rclonecache's diagnostics after a cancel ("rc unreachable" from `query`, because its `Ask`
    returns ctx's error) are dropped. Its `diagf` sees no error, so `r.failed` is not set.
  - The only output that can still get through is a write that passed the `select` just before the
    abort. That is one line, and it carries real figures, because it was formatted before the
    cancel. This is an unavoidable check-then-write window, not a defect.
  - With `--no-dir`, rclonecache does no directory walk, so nothing slow runs after an abort.
- **Exit codes.**
  - `--once`: tick written gives 0. No mount gives 1. A stop gives the snapshot's exit. A second
    stop gives 130.
  - Streaming: a failing opening snapshot gives its own code. A second stop in it gives 130. A stop
    gives the closing snapshot's exit. A second stop gives 130. A failed write gives 1.
  - These all match E13-E15 and E17.
  - The only ftp-camera call is `teardown.go:197`. It runs on `context.WithoutCancel`, so
    ftp-camera never signals rclone-tail. A terminal Ctrl-C reaches rclone-tail directly, as one
    stop, and the closing snapshot follows as before.
- **The tests prove what they claim.** Each test asserts the exact stdout, an empty stderr, the
  exit code, and the number of calls. The call count is what catches a missing `detached` refusal:
  it would be 6 instead of 5. The mutation check shows that each part of the fix is needed.

## The declined Nit (a stop just after the post-`Mounts` check)

The reason given holds. Between `app.go:265` and the heading or the no-mount return there is only
an in-memory filter. A stop there is no different from a stop that lands just after the last
instruction of any step.

- The heading case still meets E15's "before its next mount line".
- In the no-mount case, the mounts really were absent at that instant.

## Findings

### Nit: the help text's exit status omits 130 and the --once snapshot

Where: `src-go/internal/rclonetail/usage.txt:7, 15-20`.

- It says `--once` prints "with no cache snapshot". But a stop during `--once` prints one (E15).
- The "Exit status" list gives 0 for "ended with Ctrl-C". It does not say that the exit is the
  closing snapshot's, or that a second Ctrl-C gives 130. Code 130 is new in 478a613cf.

Fix: add "130 a second Ctrl-C (or SIGTERM) ended a snapshot", and qualify the `--once` line, for
example "(a Ctrl-C still prints the closing one)".

### Nit: the PORTS.md rclone-tail entry stops at round 1

Where: `PORTS.md:377-379`.

- The entry says "Further rounds: handed back to the coordinator".
- Round 2's fix (one subscription; a second signal kills the snapshot's session groups and gives
  exit 130) is recorded only in the journal and SPEC E15.

Fix: add one sentence when the coordinator merges.

## Nothing else found

I found nothing above Nit. The round 2 Low is fixed, has tests, and was confirmed over real
processes.

VERDICT: MERGE
