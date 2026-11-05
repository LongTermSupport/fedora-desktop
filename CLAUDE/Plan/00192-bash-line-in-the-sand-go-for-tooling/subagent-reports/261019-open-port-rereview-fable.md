# Plan 00192 Task 3.3: re-review of `go-port/open-2` at `d1c9a6ba`

Narrow re-check of the one fix commit `d1c9a6ba` (on `497a23b0`) against my FIX-FIRST review
`261019-open-port-review-fable.md`, and the fixer's "Review fixes" section in
`261019-p6-port-opus.md`. Checked out detached at `d1c9a6ba` in my own worktree; nothing changed;
the scratch copy used for the red proof is deleted.

## Verdict

**MERGE.** Every finding is fixed as described, each with a test that fails on the old code (or
honestly recorded as a test of existing behaviour). The shared `execx` change alters only the
error *text* for a signal death; no caller, test, golden or bash suite depended on the old text,
and `Code` stays `-1`.

## Mechanical checks (run here)

| Check                                                          | Result                        |
| -------------------------------------------------------------- | ----------------------------- |
| `go test -race -count=1 ./...` (main module)                   | all ok                        |
| `go test -race -count=1 ./...` (nested `qa` module)            | all ok                        |
| `golangci-lint run ./...`                                      | 0 issues                      |
| project rules: `go vet -vettool=<qa/driver>` over both modules | clean                         |
| `scripts/qa-bash-limits.bash`                                  | `BASH-LIMITS-OK`, exit 0      |
| `go mod tidy -diff` (the new `x/sys/unix` import in `execx`)   | empty: `x/sys` already direct |
| worktree after the run                                         | clean                         |

## Red proof (tests fail without the fix)

A copy of `src-go` at `d1c9a6ba` with the production files of `internal/execx` and
`internal/open` (`execx.go`, `app.go`, `choose.go`, `desktop.go`, `launch.go`, `mime.go`)
replaced by their `497a23b0` versions:

- `internal/execx`: build failure (`exit.Signal undefined`) — the test needs the new field,
  which is the fix. `TestOSRunnerNamesTheSignalThatEndedTheCommand` runs a real `sh -c 'kill -TERM $$'` and asserts `Signal == SIGTERM`, `Code == -1`, text `sh killed by SIGTERM`;
  `TestOSRunnerAnExitIsNoSignal` pins that `exit 130` is `Signal == 0`, `Code == 130`.
- `internal/open` with the new `execx` and the old `open` code: nine failures, exactly the nine
  the journal names — `TestPipelineFailures` (finding 6), `TestHandlerEndedByCtrlCIsAQuietCancel`,
  `TestHandlerFailingAfterCtrlCIsAQuietCancel`, `TestPipelineProducerEndedByCtrlCIsAQuietCancel`,
  `TestHandlerKilledByAnotherSignalIsNamed`, `TestInterruptWhileTypingCancels`,
  `TestInterruptDuringGioMimeCancels`, `TestInterruptDuringGioLaunchCancels`,
  `TestInterruptBeforeTheHandlerRunsCancels` (finding 1). `TestDefaultInNoDirectoryAsks`
  (finding 5) passes on the old code, as the fixer and the journal say: behaviour right, test
  missing. `TestInterruptAtThePromptCancels` passes on both (the old drain and the new
  "handler takes its interrupts" give the same answer at that prompt; the test now gets its
  earlier interrupt from a real first-target handler rather than a pre-seeded channel, which is
  the stronger shape).

## Findings 1-6, one by one

1. **Signal death / interrupt outside the handler — fixed.** `execx.OS.Run` reads
   `exitErr.Sys().(syscall.WaitStatus)` and sets `ExitError.Signal` when `Signaled()`.
   `handlerResult` (launch.go) now: SIGINT death or a SIGINT caught while the handler ran →
   `errHandlerInterrupted` (quiet, 130); any other signal → `<name> killed by <SIGNAME>`,
   `128+n`; else `exited <n>`, 1. `step()` wraps every pre-handler program (three detectors,
   `gio mime`, the registration) and `interruptedStep` turns a caught interrupt or a
   SIGINT/SIGQUIT death into `errCancelled`; `typeOf` and `desktopCandidates` propagate it
   instead of trying the next detector or wrapping it; `launch()` checks for a pending
   interrupt before the handler starts; a failing `gio launch` after an interrupt is a cancel.
   `main`'s loop returns on the first error, so E29's "later targets are not opened" holds.
   Spec E29-E31, §4 and B20 added; H5 extended to check it on the host. B20's claim about the
   bash (128+n silent, Ctrl-C included) is right: `bash -c "$spec"` under `set -e`, the outer
   bash also in the foreground group.
2. **B1 reworded — fixed.** The row now says gio's default falls back to `mimeinfo.cache`, the
   Go opens unasked as the bash did, and asks only when gio names no default or the named
   default is in no directory; H2 gained the check.
3. **Plan 00064 drift — fixed.** Overview, Task 2.1 and the risk row now describe `gio mime`;
   Decision 1 carries a note (within option C, same `mimeapps.list`, pending H3). The remaining
   `xdg-mime` mentions in that PLAN.md are the type-detection chain and the historical
   "the bash asked…" clauses, which are correct.
4. **Live-tree comparison recorded — fixed.** Journal 14:54 entry: eleven cases, differences
   B11-B14 only, matching my own run.
5. **Missing test — fixed.** `TestDefaultInNoDirectoryAsks`: gio names `org.gnome.Loupe.desktop`
   (written nowhere), GIMP is the only entry, `len(picker.specs) == 1`, GIMP launched.
6. **Pager named — fixed.** `handlerResult(filepath.Base(c.pager[0]), …)` on a pager failure;
   `TestPipelineFailures` asserts `open: less exited 2`, and the SIGTERM case asserts
   `open: less killed by SIGTERM`, 143. E25 updated.

## The shared `execx` change, traced

Every importer of `internal/execx` was listed (31 production packages) and every use of
`*execx.ExitError` outside `execx` read. Findings:

- **Field use.** Callers read `Code` (`agentbus`, `declare`, `ftpcamera/teardown`, `gitx`,
  `sudo`, `wsitoken` compare it to `1`; `ui`, `vmtest/lab`, `vmtest/guest`, `wsi`, `wsipost`,
  `sessions/reboot` return it as the exit code) or only the type. `Code` is still `-1` on a
  signal death, so none of these changes behaviour. (`term.ExitCode(-1)` → `os.Exit(255)` in
  the "return it" callers is pre-existing and untouched.)
- **Text.** `Error()` differs only when `Signal != 0`: `<name> exited -1` → `<name> killed by <SIGNAME>`. No Go test, golden file or bash suite in the repository contains `exited -1`
  (grepped `src-go`, the four golden/testdata trees — `freeze/golden`, `qa/all`, `qa/changed`,
  `declare`, `qa/rules` — `scripts/test-*.bash` and `tests/`). The ccy-teams/ccy-sessions bash
  suites match no exit text of that form. The nested `qa` module runs programs through
  `qa/internal/proc`, not `execx`, so its `Code: -1` fixtures are unaffected.
- **Where the new text can appear at run time.** A child that `execx` itself ends on context
  cancel (SIGTERM, or `Cmd.Interrupt` such as `wsi`'s SIGINT to `pw-record`) now reads
  `pw-record killed by SIGINT` where it read `pw-record exited -1`. The callers that cancel a
  context (`wsi/record.go` debug log line `Recorder exited: …`; `ftpcamera/server.go`, which
  checks `ctx.Err()` before reporting a stopped follower; `vmtest/guest.go`, which maps
  `DeadlineExceeded` first; `wsi/paste.go`, `wsipost`, `wsitoken`, which report deadline
  first) print it, if at all, as a diagnostic string nobody parses. That is the whole
  behavioural change outside `open`: a clearer diagnostic.
- **Dependency.** `golang.org/x/sys/unix` was already a direct requirement (`go.mod` line 12,
  three other production importers); `go mod tidy -diff` is empty, so `qa-go-deps` is unaffected.
- **Correctness.** `exec.ExitError.ExitCode()` is `-1` for a signal death, and
  `WaitStatus.Signaled()`/`Signal()` are the right readers on Linux; `unix.SignalName` gives
  `SIGINT`/`SIGTERM`/… as the tests pin.

## The SIGINT-only call (SIGQUIT is not a handler cancel) — judged acceptable

While a handler runs, only SIGINT (death by it, or caught while it ran) is the person's cancel;
a SIGQUIT death is `killed by SIGQUIT`, 131, and a SIGQUIT caught while a handler ran counts
as nothing (a non-zero exit after it is `exited <n>`, 1). Before the handler, both cancel (E31).

I agree with the call. Ctrl-\\ is the "this is stuck, dump core" key: no handler in the table
(`less`, `mpv`, `chafa`, `bat`, …) treats SIGQUIT as a graceful quit, so a SIGQUIT death means
the person force-killed the program, and naming it with 131 tells them what happened; the bash
exited 131 silently. Treating the pre-handler Ctrl-\\ as a cancel rather than
`gio mime killed by SIGQUIT` is a kindness for `open`'s own short steps, where the person's
only intent can be "stop", and the asymmetry is written down (E30 names SIGQUIT 131, E31 says
"interrupt or quit") and tested (`TestHandlerKilledByAnotherSignalIsNamed` has SIGQUIT 131).
One edge is unspecified but harmless: a handler that catches SIGQUIT itself and then exits
non-zero is reported `exited <n>`, 1 — true of no handler in the table.

## Minor observations (not blocking, no change asked)

- The mpv rule ("non-zero exit after a SIGINT reached `open` while it ran") relies on `open`'s
  signal goroutine having delivered to the channel by the time `Run` returns. The kernel
  delivers SIGINT to the whole foreground group at once and mpv's shutdown takes far longer
  than the runtime's channel send, so the window is theoretical; the SIGINT-death branch
  covers the handlers that do not catch it, and the spec says so for steps.
- A Ctrl-C during a *successful* `gio launch` of target 1 cancels target 2 (the pending
  interrupt is left for the next `launch()`); the code comment and E31 say this is intended,
  and I agree the person pressed Ctrl-C in `open`'s own time.
- `-h --bogus` et al. and B14 unchanged from my first review; still fine.

## Repository hygiene

No install-specific names in the diff; the journal entries are stamped by `mkplan.bash`
(14:54, 14:55); the review copy in `subagent-reports/` is byte-for-byte my report; PLAN.md
Task 3.3 and `src-go/CLAUDE.md` say E1-E31 / B1-B20.
