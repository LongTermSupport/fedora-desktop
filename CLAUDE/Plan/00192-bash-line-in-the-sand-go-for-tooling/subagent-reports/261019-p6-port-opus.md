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
