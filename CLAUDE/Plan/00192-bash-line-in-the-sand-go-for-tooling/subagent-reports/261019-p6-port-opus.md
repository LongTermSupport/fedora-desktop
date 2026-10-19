# Plan 00192 Task 3.3: first P6 port (`open`)

Branch `go-port/open`, from F44 `9b9e50e0`. Not pushed, not merged.

## Housekeeping (its own commit)

PLAN.md Task 3.3 now records `go-port/vmtest` (`3474744f`), `go-port/ftp-camera` (`a069fd96`),
`go-port/wsi-watermark` (`8c2ec4cc`) and `go-port/wsi-watermark-lows` (`df5b3b2f`) as merged
into F44 (each checked with `git merge-base --is-ancestor`). The host checks they list are still
pending.

## Which tool, and why

`open`. SPEC-open.md section 1 gives the reasons. It runs as the user, touches no secret,
deletes nothing, and all its work is decisions over data (a MIME type, the XDG application
directories, PATH, a choice), which fakes test well. It has a human-interactive chooser.
`reclaim` needs root and deletes things. `gh-account-setup`, `setup-rclone`, `nord` and
`ssh-with-key` handle credentials. Neither `open` nor `reclaim` had a bash suite, so no suite
was converted or deleted.

## What was done

- Spec `SPEC-open.md`: expectations E1-E28, departures B1-B19, host checks H1-H5.
- Tests first (red recorded in the journal), then `src-go/cmd/open` and `src-go/internal/open`.
  Handlers are an argv table. A handler with a pager runs as two processes joined by an
  `os.Pipe`, with no shell. The chooser is `ui.Picker` with a `Cancel — open nothing` row, or a
  numbered menu on `/dev/tty` when fzf is missing. Desktop apps go through `gio` alone. SIGINT
  and SIGQUIT are caught, so a handler owns Ctrl-C and a prompt is cancelled by it.
- `src-go/qa/rules/table.go`: a `dynamicNames` row for `run.runHandler`, which runs the
  program the person chose. `.golangci.yml`: G304 is allowed for `desktop.go` and the file
  modes for open's tests, each with its reason.
- `play-cli-tools.yml` imports `play-golang.yml` (tagged `open-command`) and builds the command
  with `tasks/build-go-command.yml`. The bash file and its allowlist row are gone, and
  `go-binary-suites.tsv` has a row `open qa-go`. `docs/playbooks.md`, `src-go/CLAUDE.md`,
  Plan 00064 Phase 4 and PLAN.md are updated.
- `meta-deploy.list` queues `play-cli-tools.yml`, with H1-H5 as the owner's checks afterwards.

## Departures worth the owner's attention

- B1: in a graphical session with no default registered, the bash launched the first
  registered app without asking. The Go asks.
- B2: the bash never found symlinked desktop entries, so Flatpak apps were never offered. The
  Go follows symlinks.
- B5/B6: the default is read from, and registered through, `gio mime`, run in `LC_ALL=C.UTF-8`.
  `xdg-mime` is no longer asked for the default. This differs from Plan 00064's Decision 1,
  and H2/H3 check it on the host.
- B14: usage errors exit 64 and a cancel exits 130 (the bash exited 1 for both).

## QA run

`go test -race` over `internal/open` (93% coverage) and `cmd/open`. golangci-lint: 0 issues.
Project rules (`go vet -vettool` over `./...`): clean. `qa/rules` tests pass. Also passing:
`qa-bash-limits.bash`, `qa-hidden-bindings.bash`, `ansible-playbook --syntax-check` on
`play-cli-tools.yml`, and `qa-changed.bash`. `qa-all` was not run, as instructed.

The first `qa-changed` run failed once in a test this branch does not touch:
`qa/cmd/qa-patterns` `TestRealSemgrepScansTestsDirectoriesAndLargeScripts`, where semgrep timed
out on the test's own `big.bash` while the run was under load. The test passed when run on its
own, and the second full `qa-changed` run passed (31 changed files). That timeout is a load
sensitivity of the test, not something this branch caused.

## Left

H1-H5 need a desktop session and a terminal. Merge after review and the owner's host run.

## Review fixes (branch `go-port/open-2`, from `497a23b0`)

`go-port/open` was checked out in another worktree, so the fixes are on `go-port/open-2`. Fable's
review is copied beside this report (`261019-open-port-review-fable.md`, verdict FIX-FIRST). Each
finding was fixed test first; the red and the green are in the 00192 journal.

1. **Signals (blocking).** The coordinator ruled that the person's own Ctrl-C is a cancel, never
   a failure. `execx.ExitError` gains `Signal`, set from the wait status, and its message reads
   `<name> killed by SIGTERM`. New expectations in SPEC-open:

   - E29: a handler that dies of SIGINT, or exits non-zero after a SIGINT reached `open` while
     it ran (mpv's 4), ends `open` quietly with exit 130. A producer killed by Ctrl-C while
     `less` survived counts the same way.
   - E30: any other signal is `open: <label> killed by <SIGNAME>`, exit 128 + n.
   - E31: an interrupt before the handler runs cancels with `open: cancelled — nothing opened.`,
     exit 130, and nothing further runs. That covers a detector, `gio mime`, the
     registration, the choice and the moment before the handler starts. A step killed by
     SIGINT/SIGQUIT counts as interrupted even if `open` has not seen the signal yet. A
     `gio launch` that failed because of an interrupt is a cancel too.

   A handler's interrupts are taken when it ends, so a Ctrl-C that a handler dealt with
   (exit 0) does not cancel the next target. This replaces the numbered prompt's blanket drain.
   §4 exit codes and B20 are added, and H5 checks this on the host.

2. **B1 reworded.** gio's default falls back to the first registered app in `mimeinfo.cache`,
   so the Go opens unasked, as the bash did. It asks only when gio names no default, or when
   the default it names is in no application directory. H2 confirms this.

3. **Plan 00064** PLAN.md now describes `gio mime` in the overview, Task 2.1 and the risk row. A
   note under Decision 1 says the port moved to gio within option C (same `mimeapps.list`),
   pending the owner's confirmation at H3.

4. **Live-tree comparison** recorded in the journal. The Go binary and the bash at `9b9e50e0`
   were run over 11 cases (directory and URL `-l`/`-n`, a missing path, a `-`-leading file, a
   plain file, and the usage cases). The only differences were B11, B12, B13 and B14. No typed
   file could be compared because the container has no detectors.

5. **Missing test** `TestDefaultInNoDirectoryAsks` added. It passed on the old code: the
   behaviour was right and only the test was missing.

6. **Pager failures** now name the pager (`open: less exited 2`, `open: less killed by SIGTERM`).

B14 (exit 64 and 130) is kept.

QA, all passing: `go test -race` over `internal/open`, `cmd/open` and `internal/execx`, plus
`go test ./...` over the module (`execx` is shared). golangci-lint reported 0 issues. The
project rules driver (`go vet -vettool`) was clean, and so were `qa-bash-limits.bash`,
`ansible-playbook --syntax-check` on `play-cli-tools.yml` and `qa-changed.bash`. qa-all was not
run.
