# Review: rclone-tail Go port, round 1 (Opus, adversarial)

Branch `go-port/rclone-tail` at `7aa9b4bc1`, diff against F44.

What I ran: `go test -race` over `internal/rclonetail`, `internal/rclonerc`, `internal/rclonecache`,
`cmd/rclone-tail` and `internal/ftpcamera` (all pass; coverage 98.0% and 97.7%); `golangci-lint`
over the four packages (0 issues). I also built the binary and ran it in a process group of its
own over a fake `findmnt`, a fake `rclone` (0.4 s per RC call) and a fake `rclone mount` process,
then sent SIGINT to the whole group, the way a terminal's Ctrl-C does. The scratch files are in
`untracked/scratch/rtrev/`, and no Ansible was run.

## Findings

### M1 (Medium): the deploy leaves the host's `rclone-cache-status` on the old source

`CLAUDE/Plan/00192-bash-line-in-the-sand-go-for-tooling/deploy-rclone-tail.bash:53-54` runs
`play-rclone.yml --tags rclone-tail` only. This branch also changes the `rclone-cache-status`
command: `rclonecache` now runs on `rclonerc.Mounts`/`Ask`, `--no-dir` makes a TARGET a mount
name (SPEC-rclone-cache-status B12), and its `usage.txt` text is new. That binary is built only
by the task tagged `rclone`/`rclone-cache-status` (`play-rclone.yml:482-490`). After the
documented host run, `~/.local/bin/rclone-tail` has B12 (the report runs inside it), but the
standalone `~/.local/bin/rclone-cache-status` is still the F44 build. So the host differs from
the checkout, the spec's B12 does not hold for the command it names, and nothing in
`acceptance-rclone-tail.bash` or in PLAN.md/PORTS.md mentions it. The coordinator's later queueing
will go wrong if it trusts the script's header ("builds src-go/cmd/rclone-tail ... Only the
tagged tasks run").

Fix: run `--tags rclone-tail,rclone-cache-status` in the deploy (both tags leave the mounts up)
and say so in its header, in SPEC-rclone-tail section 7 and in PORTS.md. Optionally have the
acceptance check `rclone-cache-status --help` for the B12 line.

### L1 (Low): Ctrl-C during the opening snapshot puts `error: rc unreachable` in it

`src-go/internal/rclonetail/app.go:157` with `rclonecache` `query`. The opening snapshot runs on
the uncancelled `ctx`, as E15 intends ("finished first"). But the terminal sends SIGINT to the
whole foreground group, so the snapshot's `rclone rc` child is killed too. I reproduced it 5 of 5
times: stdout gets `lib  error: rc unreachable` and stderr gets
`rclone-cache-status: lib: rclone killed by SIGINT`. The same signal arriving during a tick's
call was clean 40 of 40 times, because the stop's cancel wins the race. Nothing guarantees that
win, though: `ask` (`app.go:340`) treats any RCError seen before `ctx.Err()` is set as a real
failure, and `TestStopDuringTick` always calls `stop()` before the fake returns, so it does not
cover the order where the child dies first. The bash had the same artefact, so this is not a
regression, but E15/B8 claim more than the code ensures.

Fix: run the RC children outside the terminal's foreground group (`execx.Cmd.NewSession`, which
kills the group when ctx ends), so only the run's own stop ends them. Or, in `ask`, read an
`execx.ExitError` with `Signal` SIGINT/SIGTERM (or exit code 130/143, which is how rclone's
atexit exits) as a stop. Add a test where the fake returns the signal-killed error before
`stop()` is called.

### L2 (Low): a stop during a tick does not always end it, and `--once` can lose the snapshot

`app.go:265-273, 278-285, 150`. The tick checks for a stop only inside `ask` and after `Mounts`.
If the signal arrives after a mount's last RC call has returned, the later lines are still
printed: `(no --rc; cannot query)`, the more-than-one `--rc-addr` refusal, and the
credential-missing line (`Ask` returns that RCError before it looks at ctx, `mounts.go:95-98`).
If no RC call follows, `tick` returns nil. In streaming mode the `select` then catches the stop.
In `--once` mode, `ended(nil)` exits 0 with no closing snapshot. That contradicts E15: "a tick
under way stops printing at once ... With `--once` too: the tick stops and the snapshot follows."

Fix: check `ctx.Err()` before each mount line in `tick` (return errStopped). In `Run`'s `--once`
branch, treat `stopCtx.Err() != nil` after a nil tick as errStopped, or narrow E15's wording.

## Checked and found sound

- **Argument grammar (E1/E2/B1/B11):** behaves as specified.
- **Rate (E10):** fractional, keyed by TARGET, drop to 0 on a fall, and the test arithmetic
  (11000 B / 5.5 s) is right.
- **Parsing (E9):** null and missing counters, and `eta` absence, are handled.
- **Snapshot (E14):** stop after the opening snapshot works, and signals are released before the
  closing snapshot.
- **Production subscription:** `onSignal` is tested with a real SIGTERM.
- **ftp-camera:** its call (`teardown.go:196`) and its tests are unaffected. Exit 1 on no mount
  is kept, and the no-mount line moving to stderr does not matter because ftp-camera streams both.
- **rclonecache refactor:** no regression in its tests.
- **Play wiring:** the `play-golang.yml` import tag, the build task tags, the
  `go-binary-suites.tsv` row, and the removal of the allowlist/qa-routing/callers_test entries
  are all right.
- **References to the deleted bash:** none left outside plan history and `INVENTORY.md`'s
  snapshot table.
- **Stdout/stderr hygiene:** holds.
- **meta-deploy:** not queued, by stated design (the coordinator queues it after the merge, as
  for gh-account-setup).

VERDICT: FIX FIRST
