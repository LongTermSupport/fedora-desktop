# Plan 00192 Task 3.3: adversarial review of `go-port/open` at `497a23b0`

Reviewed: `5a82e6b9` (housekeeping) and `497a23b0` (the port) on F44 `9b9e50e0`, against
`SPEC-open.md` (E1-E28, B1-B19, H1-H5), the builder's `261019-p6-port-opus.md`, `src-go/CLAUDE.md`
"How a port is built", `CLAUDE/GoProjectRules.md` and `CLAUDE/InteractiveScripts.md`. Nothing
was changed. Scratch artefacts are under `untracked/scratch/open-review*`.

## Verdict

**FIX-FIRST** — one defect in the Ctrl-C path the spec calls out as the port's point (finding 1);
everything else is non-blocking.

## Mechanical checks (all run here, as root in the container)

| Check                                                       | Result                                                                                        |
| ----------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `go test -race -count=1 ./internal/open/... ./cmd/open/...` | ok (E9's unreadable-directory test skips under root; it runs on the host and in CI as a user) |
| `golangci-lint run ./...`                                   | 0 issues                                                                                      |
| project rules (`go vet -vettool=<qa/driver> ./...`)         | clean                                                                                         |
| `scripts/qa-bash-limits.bash`                               | exit 0                                                                                        |
| `ansible-playbook --syntax-check play-cli-tools.yml`        | exit 0                                                                                        |
| `scripts/bash-limits.allowlist`                             | only shrank: the one `files/home/.local/bin/open` row removed                                 |
| `scripts/go-binary-suites.tsv`                              | `open	qa-go` added                                                                                               |
| `src-go/qa/rules/table.go`                                  | one `dynamicNames` row, `internal/open` / `run.runHandler`, reason given                      |
| `CLAUDE/Plan/meta-deploy.list` header                       | lines 1-21 byte-identical to F44 (md5 match); the branch only appends one entry               |
| callers of the deleted bash                                 | none in the repo; INVENTORY.md row 210's "callers" are prose uses of the word `open`          |
| install-specific names                                      | none found in the diff                                                                        |

Live-tree comparison (Go binary vs the bash at `9b9e50e0`, both in scratch, `-t -l` and `-t -n`
over a directory, a URL, a missing path, a `-`-leading path and the usage cases): identical
except the spec'd departures (B13 dry-run form, B14 exit 64 and `-h` precedence, B11's
per-detector reasons). The container has no `mimetype`/`xdg-mime`/`file`, so file-typed targets
could not be compared here; both tools fail the same way on them.

## Numbered findings

1. **FIX-FIRST — a handler ended by a signal is reported as `exited -1`, and an interrupt
   outside the handler-owned window is misreported rather than cancelling.**
   `execx.OS.Run` maps a signal death to `ExitError{Code: -1}` (`exec.ExitError.ExitCode()` is
   -1 for a signal), and `handlerResult` (`src-go/internal/open/launch.go`) turns every
   `ExitError` into `open: <label> exited <n>`, exit 1. So on the H5 check: Ctrl-C in `chafa`,
   `pdftotext | less` (the producer dies, `less` survives; quit `less` and `producerFirst` is
   true) or any pager-less handler without its own SIGINT handler prints
   `open: chafa (terminal image) exited -1` and exits 1. `mpv` exits 4 on Ctrl-C and gets
   `exited 4`, exit 1, after a normal quit. The bash died with 130 and said nothing. E25/E26
   do not specify a signal death; no test covers one (every fake exit is a positive code).
   Related: `main.go` subscribes to SIGINT/SIGQUIT before anything runs, so a Ctrl-C while a
   detector or `gio mime` runs kills that child and `open` carries on to the next detector
   (`...; trying the next detector`), and one during `gio launch` (D-Bus activation can take a
   second) is `open: gio launch failed: gio exited -1`, exit 1 — not a cancel.
   Fix: in the spec, say what a handler ended by SIGINT/SIGQUIT means (the natural answer is
   exit 130 with `cancelled`, or at least `<label> was interrupted`), pin it with a test whose
   fake returns a signal death (`ExitError{Code: -1}`, or expose the signal in `execx`), and
   decide what an interrupt before the handler owns the terminal does (cancel, not continue).
   Whether `mpv`'s exit 4 after Ctrl-C should be a failure is the owner's call (see below).

2. **Spec accuracy, B1 (non-blocking).** B1 says the bash launched "the first registered app"
   with no default set and the Go "asks". `gio mime`'s `Default application for …` line comes
   from `g_app_info_get_default_for_type`, which falls back to the first registered app from
   `mimeinfo.cache` when `mimeapps.list` names none — so in practice gio names a default
   whenever any app is registered, and the Go opens it unasked exactly as the bash did. The
   "asks instead" case only arises when gio lists registered apps and names no default, which
   gio's lookup makes rare. The code is fine (E15 is the right rule); the departure table
   overstates the change the owner would see. Reword B1, and let H2 confirm.

3. **Plan 00064 drift (non-blocking).** The branch adds a Phase 4 note that the default is now
   read and set through `gio` (B5/B6), but `CLAUDE/Plan/00064-.../PLAN.md` lines 22-23, 88
   and the risk row at line 190 still describe `xdg-mime query default` as the default path.
   Update them in the same change, or the plan contradicts itself.

4. **Method step not evidenced (non-blocking).** `src-go/CLAUDE.md` requires that "before
   cut-over, the Go tool runs on the live tree and any difference from the old tool's result is
   explained". Neither the journal nor the report records such a run; the red recorded is a
   compile failure (`undefined: App`), which proves the tests preceded the code but not that
   each E's test fails without its behaviour. My own comparison (above) found only the spec'd
   departures, so this is a record gap, not a defect — add the comparison to the journal.

5. **Test gap (non-blocking).** `desktopCandidates` sets `hasDefault` only when the default ID
   is the first entry found; a default gio names whose entry is in no directory (E9's "found in
   no directory is not offered") therefore makes the run ask. That path has no test (the
   `display-im7` case covers a non-default missing ID only). One test with the default missing
   from the index and `len(picker.specs) == 1` pins it.

6. **Minor message quality (non-blocking).** `TestPipelineFailures` and the code report a
   pager failure as `<handler label> exited <n>` (e.g. `pdftotext (text) exited 2` when `less`
   exited 2) — the label names the producer, not the program that failed. E25 permits it;
   consider naming the pager.

## What was checked and found right

- **Every E has a test** (E1-E28 each cited in a test comment and exercised through the fakes);
  the tests are behavioural, not replays: `TestNoDefaultAsksEvenWithRegisteredApps`,
  `TestQuittingThePagerEarlyIsNotAFailure`, `TestInterruptAtThePromptCancels` (with a stale
  interrupt that must be drained), `TestHelpAnywherePrintsTheUsageOnStdout`,
  `TestDashPathIsPassedWithADotSlash`, `TestDesktopEntrySymlinksAreFollowed`,
  `TestDesktopIDIncludesTheSubdirectory` and `TestGioMimeRunsInTheCLocale` each fail without
  the behaviour they name.
- **Departures are genuine bash bugs**: B2 (`find -type f` skipped symlinks: Flatpak exports),
  B3 (basename match at any depth), B4 (first `Name=` anywhere), B9 (pager not probed), B10
  (status lost in `bash -c` pipeline), B11 (detector failure silent), B12 (`--list` on stderr),
  B15 (`*://*` anywhere), B16 (`-`-leading path to `jq`/`sqlite3`/`bsdtar`), B17 (advice that
  needs the missing terminal), B18 (two equal fzf labels), B19 (Esc-only exit). Each is
  confirmed against the bash at `9b9e50e0`.
- **Pipeline (D4/E24/E25)**: `os.Pipe`; producer's stdout is the write end passed as an
  `*os.File` (os/exec hands it to the child directly, no copy goroutine, so `WaitDelay` never
  cuts a long `less` short); the parent holds the write end until the producer's `Run` returns,
  then closes it, so the pager's EOF comes only after the producer exits; `producerDone` is set
  before that close; the pager has the real stdin/stdout; after the pager exits the read end is
  closed so a still-writing producer gets EPIPE/SIGPIPE (SIGPIPE is default in the child, since
  exec resets caught signals); the pager's status wins, the producer's counts only when it
  finished first. No shell anywhere. `cmd.Stdin = nil` is `/dev/null` under os/exec.
- **Signals (E26)**: `signal.Notify` on SIGINT/SIGQUIT before anything runs; children get the
  default disposition (caught signals reset on exec), so `less`/`mpv`/fzf own Ctrl-C and `open`
  waits for them. At the numbered prompt the read is raced against the signal channel (E19);
  stale interrupts are drained first. fzf's 130 is `ui.ErrLeave`, exit 130 (E17).
- **fzf-absent fallback (E18)**: `ui.ErrFzfMissing` → numbered menu on stderr, read from
  `/dev/tty`, bounded at 3 (`maxTries`), `q`/`Q`/`quit`/EOF cancel, `+N` and `-N` rejected,
  heading drawn with the house `ui.Heading` at `tput cols` width (InteractiveScripts rules 2,
  3, 5, 8, 9, 13, 14, 15).
- **Desktop entries**: Exec is never parsed — `gio launch <file> <target>` does it, which is
  D2 and sidesteps field codes and quoting entirely. ID search is spec-correct (path below the
  directory, `/`→`-`, first directory wins, symlinks followed, dangling link and directory
  skipped, unreadable directory a failure). `Name` is the `[Desktop Entry]` group's unlocalised
  key.
- **`execx.Cmd.Env` extends** the parent environment (`ChildEnv(os.Environ(), …)`), so
  `LC_ALL=C.UTF-8` for `gio mime` keeps HOME/XDG and gio reads the user's `mimeapps.list`.
  `gio mime`'s C-locale strings (`Default application for “%s”: %s`, `Registered applications:`, `Recommended applications:`, tab-indented IDs) match glib's
  `gio-tool-mime.c`; gio is not in this container so this is from the source, not a run.
- **Cut-over**: `import_playbook: ../../play-golang.yml` with `tags: open-command` — tags on an
  `import_playbook` reach every imported task, so `--tags open-command` installs the toolchain
  and a full run runs everything; `play-golang.yml` has no tags of its own to collide with. The
  `~/.local/bin` directory task is `tags: always`. `build-go-command.yml` is `import_tasks`
  (tags propagate), builds as the login user with `-mod=vendor`, installs with a
  checksum-compared `copy` at `{{ user_bin_dir }}/open`, mode 0755, owner `user_login` by
  default — the same path, owner and mode as the deleted `copy` task. `docs/playbooks.md`,
  `src-go/CLAUDE.md` and PLAN.md are updated; `.golangci.yml` G304/G30x exclusions are scoped
  to `internal/open/desktop.go` and the package's tests with reasons.
- **B14 exit codes**: no repository caller invokes `open` (the only hits are agent-browser's
  `open` subcommand and prose), so nothing depends on exit 1 for usage or cancel.

## OWNER DECISIONS (not defects)

- **B5/B6 — `gio mime` replaces `xdg-mime query default` / `xdg-mime default`.** Plan 00064
  Decision 1 chose "wrap `mimetype`/`xdg-mime`/`gio`" over reimplementing; swapping which
  packaged tool answers the default stays inside that decision's option C, and both read and
  write the same `~/.config/mimeapps.list`, so the port may take it on technical grounds. It
  does change a recorded decision's named tools, so the owner should confirm (H3 proves it on
  the host) and Plan 00064's text be brought into line (finding 3).
- **B1 — asking when gio names no default.** Not a real behaviour change for the reason in
  finding 2; no decision needed beyond accepting the reworded spec.
- **B14 — exit 64 for usage, 130 for cancel.** No caller depends on 1; this is the module's
  `term` convention. Accept or revert; nothing technical hangs on it.
- **Finding 1's `mpv` case** — whether a program that exits non-zero after the person's own
  Ctrl-C (mpv's 4) should be reported as a failure, or whether a Ctrl-C-ended handler is a
  cancel (130) however the program encodes it. The signal-death message (`exited -1`) is a
  defect either way.
