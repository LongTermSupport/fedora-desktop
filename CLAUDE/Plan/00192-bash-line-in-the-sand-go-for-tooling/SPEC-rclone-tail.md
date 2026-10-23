# rclone-tail: specification of the Go port

Programme P6 of [INVENTORY.md](INVENTORY.md) ("Interactive account/setup tools"), its fourth tool
after [`open`](SPEC-open.md), [`rclone-cache-status`](SPEC-rclone-cache-status.md) and
[`gh-account-setup`](SPEC-gh-account-setup.md): `rclone-tail`, the live monitor of the rclone
mounts' upload activity (queue, uploads in flight, transfer rate, ETA). **Method: the legacy bash
is not an oracle.** This document states the intended behaviour as numbered, testable
expectations (`E*`). The bash (`files/home/.local/bin/rclone-tail`, 226 code lines, the `assoc`
and `data` flags, at `5b5c04ed`) was read only to find the requirements; where it was wrong, the
expectation states the correct behaviour and section 5 lists the departure (`B*`). The proof is
the Go tests in `src-go/internal/rclonetail` (every flow over a fake runner, a fake `/proc`, a
fake clock and a fake signal source), the tests of the shared pieces in `src-go/internal/rclonerc`,
and `src-go/cmd/rclone-tail`. What only a host with live rclone mounts can prove is section 7.

## 1. Why this one next among P6

The P6 tools left after `gh-account-setup` are `setup-rclone` (it writes the rclone
configuration, with credentials, through `rclone config`'s prompts), `nord` (the VPN, root),
`reclaim` (root and deletions), the `ssh-with-key` family (credentials), `nvidia-status`
(hardware the container cannot fake usefully), `rclone-cache-warm` (101 lines, no flag) and
`rclone-tail`. **`rclone-tail`** is a user tool of moderate size that runs as the user and
changes nothing, and it carries two of the features the bash limits forbid outright: two
associative arrays (the per-mount byte counters behind the rate) and an embedded Python program
(the `data` flag). Everything it reads, `findmnt`, `/proc`, two RC endpoints and the clock, a fake
pins. Its one credential is the RC password, which the tested `internal/rclonerc` already keeps
out of argv, and it is the sibling of the last rclone port: it shares that port's mount listing,
mount-process lookup, RC error reasons and byte format, so the port moves them into `rclonerc`
once (D3) instead of keeping a second copy. `PLAN.md` lists it as left after
`rclone-cache-status`. It had no test suite, so no suite was converted.

## 2. Scope decisions

- **D1. One Go binary at the old path.** `src-go/cmd/rclone-tail`, built by
  `tasks/build-go-command.yml` from `play-rclone.yml` (tags `rclone` and `rclone-tail`),
  installed at `~/.local/bin/rclone-tail`, mode 0755. The play's `play-golang.yml` import gains
  the `rclone-tail` tag, so `--tags rclone-tail` installs the toolchain and only this command and
  leaves the mounts up. The bash file is deleted and its row leaves
  `scripts/bash-limits.allowlist`; `scripts/go-binary-suites.tsv` gets `rclone-tail	qa-go`. Its
  caller, `ftp-camera` (`rclone-tail --once <name>`, its exit status read as "status could not be
  read"), is unchanged.
- **D2. The cache snapshot is the rclone-cache-status report, run in process.** The bash ran
  `rclone-cache-status --no-dir [NAME]` by name from `PATH`. The Go command calls
  `rclonecache.App.Run` with the same arguments over its own runner, `/proc` and streams, so the
  snapshot cannot be a different version of the report, nor missing from `PATH`. Its lines and
  diagnostics are that report's own (`rclone-cache-status: ...` on stderr).
- **D3. One copy of what the two rclone reports share.** `rclonerc` gains `Client.Mounts` (the
  rclone mounts from `findmnt -J`, each with its serving process's arguments), `Mount.RCAddr`
  (its one `--rc-addr`, none, or the refusal of several) and `Client.Ask` (an RC call whose
  failure is a `*rclonerc.RCError`: the short reason a report prints and the detail for stderr;
  a call cut short by the run's own stop is the context's error instead), and `Counter` (an RC
  counter as an integer). `rclonecache` is moved onto them, its tests unchanged, and
  `MountProcess`, left with no caller, is removed. The byte format is `rclonecache.IEC`.
- **D4. External programs stay where they are the tool.** `findmnt` lists the mounts; `rclone rc`
  asks each mount for `vfs/stats` and `core/stats`. Done in Go: reading the mount processes'
  arguments from `/proc`, decoding the JSON (no Python), the rate and the formatting (no
  `numfmt`, no `date`).
- **D5. One seam for processes, clocks and signals.** Everything runs through `internal/execx`,
  the RC calls through `rclonerc.Client`, which keeps the password out of argv. The time of day,
  the elapsed time and the wait between ticks are `Env.Now` and `Env.After`; SIGINT and SIGTERM
  reach the run through `Env.OnStop`, which calls its function when one arrives, so a test stops a
  run at an exact point. Every child runs in a session of its own (`execx.Cmd.NewSession`), so
  the terminal's Ctrl-C reaches this run alone: a call in flight is ended by the run's own
  cancel, never killed first by the signal and read as `rc unreachable`, and an opening
  snapshot under way finishes with true figures.

## 3. Expectations

### Command line

- E1. `rclone-tail [--interval N] [--once] [MOUNT_NAME]`; `-i` is `--interval`, `-o` is
  `--once`; `-h`/`--help` anywhere before `--` prints the usage on stdout and exits 0, whatever
  else the line holds; `--` ends the options. The whole command line is read before anything
  runs.
- E2. An unknown `-...` option, an `--interval` with no value, an interval that is not a whole
  number of seconds from 1 to 86400, or a second mount name is a usage error:
  `rclone-tail: <text> — see 'rclone-tail --help'.` on stderr, exit 64. Every error line is
  `rclone-tail: <text>` on stderr.
- E3. `rclone` not on `PATH` is `rclone-tail: rclone is not installed; it is installed by play-rclone.yml.`, exit 1, before anything is printed on stdout.

### The mounts

- E4. The mounts are `rclonerc.Client.Mounts`: `findmnt -J -l -o SOURCE,TARGET -t fuse.rclone`,
  in findmnt's order; its exit 1 with nothing on stdout is no mount; any other failure, or output
  that is not that JSON, is `rclone-tail: findmnt could not list the rclone mounts: <reason>`,
  exit 1. A mount's name is its SOURCE up to the first `:`. With MOUNT_NAME only the mounts of
  that name are shown. They are listed again at every tick, so a mount that comes or goes is seen.
- E5. A mount's process is the first `rclone mount` process, in pid order, one of whose
  arguments after the program is exactly the mount's TARGET. Its RC address is its one
  `--rc-addr=` value; no process or no `--rc-addr=` is a mount with no RC; more than one is an
  error line (E8).
- E6. No mount to show is `rclone-tail: No rclone mount matching '<name>' found.` (with
  MOUNT_NAME) or `rclone-tail: No rclone mounts found.` on stderr, and exit 1, at the first tick
  or any later one (a streaming run then ends without the closing snapshot).

### A tick

- E7. A tick is the heading `=== HH:MM:SS ===` (the local time of day, `Env.Now`), then one line
  per mount, in findmnt's order.
- E8. A mount with no RC is `  <name>  (no --rc; cannot query)`. A mount whose RC cannot be asked
  is `  <name>  error: <reason>`, the reason as the rclone-cache-status report gives it
  (`rclonerc.RCError`): `rc credential missing — run play-rclone.yml` (`rclonerc`'s advice on
  stderr), `rc rejected credentials (401)` (rclone's output names `401` or `Unauthorized`), `rc unreachable` (any other failure; rclone's output on stderr as `rclone-tail: <name>: <output>`),
  `parse failed` (an answer that is not one JSON object, or a counter that is not a number; the
  decoder's reason on stderr as `rclone-tail: <name>: <endpoint>: <reason>`), or the
  more-than-one `--rc-addr=` refusal of E5 (`more than one --rc-addr: <a> <b>`). `vfs/stats` is
  asked first; when it fails `core/stats` is not asked.
- E9. Read: `vfs/stats` `diskCache.uploadsInProgress`, `uploadsQueued`, `erroredFiles`;
  `core/stats` `bytes`, `totalBytes`, `transfers`, `totalTransfers`, `eta` (seconds). A missing
  or null counter is 0, a float is truncated; a missing or null `eta` is no estimate.
- E10. The rate is the growth of `core/stats` `bytes` since the mount's last answered tick,
  divided by the seconds between the two answers (`Env.Now`, fractional), rounded down; the
  mount's first answered tick, a count that fell (rclone restarted) or no time passed is 0. A
  mount is remembered by its TARGET.
- E11. A mount with nothing queued, nothing uploading and a rate of 0 is `  <name>  idle`, then
  ` (<n> error(s))` when `erroredFiles` is not 0.
- E12. Any other mount is
  `  <name>  queued: <q> · active: <a> · done: <d>/<t> (<bytes>/<total bytes>) · <rate>/s · ETA <eta>`, then ` · <n> errored` when `erroredFiles` is not 0. Bytes and the rate are
  `rclonecache.IEC` (numfmt's IEC form: `1.5KB`, `10MB`); the ETA is `<m>m<ss>s` (whole minutes,
  then seconds to two digits: `61m05s`), or `—` when there is no estimate or it is 0.

### The run

- E13. `--once`: one tick, exit 0 (E6: exit 1 when there is no mount). No snapshot.
- E14. Streaming (no `--once`): the snapshot (D2, `--no-dir`, with MOUNT_NAME when given), an
  empty line, then a tick, then a wait of the interval (default 5 seconds) measured from the end
  of that tick, then the next tick, until stopped. A snapshot that exits non-zero ends the run
  with its exit code before the first tick.
- E15. SIGINT or SIGTERM, at any point after the command line is read: the run stops; a tick
  under way stops printing at once (before its next mount line, whether or not a call follows), and the RC call in flight is cancelled and is neither
  printed nor diagnosed; an opening snapshot under way is finished first. Then an empty line and
  the closing snapshot, and the exit is the snapshot's (0 when it was written). With `--once`
  too: the tick stops and the snapshot follows. The signals are released before the closing
  snapshot, so a second Ctrl-C during it ends the process at once.

### Output

- E16. Colour (bold heading, cyan names and figures, dim notes, green rate, yellow ETA and
  no-mount line, red errors) only when stdout is a terminal and `NO_COLOR` is unset or empty;
  the text is the same either way.
- E17. The ticks and the snapshot are stdout; every diagnostic is stderr. A failed write to stdout
  is exit 1 at once (a stream into a closed pipe ends).

## 4. Kept as it was

The flags and their short forms, the mount name filter, the default interval, the heading's
time format, the text of every mount line, the idle collapse (`grep -v idle` still drops the idle
mounts), the opening and closing snapshot of a streaming run, Ctrl-C ending a run with exit 0
after the snapshot, `--once` with no mount exiting 1 (ftp-camera reads it), the RC error
reasons, and the ETA format.

## 5. Departures from the bash

- B1. Usage errors exit 64 (the bash: 2). `--interval` with no value is a usage error (the bash's
  `shift 2` with one argument left ended the run with status 1 and no message), an interval over
  a day is refused, and `--` ends the options.
- B2. Errors are `rclone-tail: <text>` (the bash: `ERROR: <text>`), and the no-mount line is a
  diagnostic on stderr (the bash: on stdout, coloured).
- B3. A `findmnt` that fails is exit 1 naming it (the bash: `2>/dev/null`, read as no mount). The
  mounts are read as JSON, so a mountpoint with a space in it is one target (the bash split
  `SOURCE TARGET` on whitespace).
- B4. The mount process matches an argument equal to the target (the bash: the target between
  spaces in the joined command line), and more than one `--rc-addr=` is an error line (the bash
  took the first).
- B5. The RC error reasons are the rclone-cache-status report's: the "helper library missing"
  reason is gone (no library: the Go client reads the credential itself), the credential reason
  names the play, and rclone's own output for `rc unreachable` is printed on stderr (the bash
  discarded it). The parse failure's reason is on stderr (the bash: `2>/dev/null`).
- B6. The rate's mount is its TARGET (the bash keyed it by name, so two mounts of one remote
  shared one counter and each read the other's bytes). The elapsed time is fractional (the bash
  used whole `$SECONDS`, so a tick that straddled a second boundary could read twice the rate,
  and one within the same second none).
- B7. An active mount's line names its errored files (the bash showed them only when idle).
- B8. A signal during a tick stops it and cancels the RC call in flight (the bash finished the
  tick's call first, up to rclone's own timeout); a call so cancelled is not printed as `rc unreachable`.
- B9. The snapshot is the report run in process (D2; the bash ran whichever `rclone-cache-status`
  was first on `PATH`).
- B10. Colour honours `NO_COLOR`.
- B11. A bare `-` is a mount name, not an unknown option (as in rclone-cache-status).

## 6. Proof

`go test -race` over `internal/rclonetail`, `internal/rclonerc`, `internal/rclonecache` and
`cmd/rclone-tail`: every expectation has a test written first and seen to fail (journal). The
`rclonecache` tests are unchanged by its move onto D3's shared pieces. Before cut-over the Go
binary and the bash at `5b5c04ed` were run side by side in the container over a fake `findmnt`
and `rclone` on `PATH` (journal); every difference is a departure above.

## 7. Host checks (the owner's, after the play)

- H1. `rclone-tail --once` lists every mount as `idle`, an activity line or `(no --rc ...)`,
  never `error:`, and exits 0.
- H2. `rclone-tail` while a file is being written into a mount: the opening snapshot, then ticks
  every 5 seconds whose activity line shows the queue and a rate above `0B/s` while the upload
  runs, and `idle` once it is done.
- H3. Ctrl-C in H2 prints an empty line and the closing snapshot and exits 0
  (`echo $?` prints `0`).
- H4. `ftp-camera`'s closing upload status (`rclone-tail --once <mount>`) still prints.

`deploy-rclone-tail.bash` runs `play-rclone.yml --tags rclone-tail,rclone-cache-status`, which
builds and installs this command, rebuilds `rclone-cache-status` (changed by D3 and its B12) and
leaves the mounts up, then `acceptance-rclone-tail.bash`, which scripts H1 and the
installed-program check; H2 to H4 are by hand.
