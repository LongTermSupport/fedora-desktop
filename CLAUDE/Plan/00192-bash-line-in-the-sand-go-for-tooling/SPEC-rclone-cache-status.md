# rclone-cache-status: specification of the Go port

Programme P6 of [INVENTORY.md](INVENTORY.md) ("Interactive account/setup tools"), its second
tool after [`open`](SPEC-open.md): `rclone-cache-status`, the report of how much of each rclone
mount's VFS cache is used, and how much of one directory inside a mount is cached. **Method:
the legacy bash is not an oracle.** This document states the intended behaviour as numbered,
testable expectations (`E*`). The bash (`files/home/.local/bin/rclone-cache-status`, 323 code
lines, at `1d2b7aa9`) was read only to find the requirements; where it was wrong, the
expectation states the correct behaviour and section 5 lists the departure (`B*`). The proof is
the Go tests in `src-go/internal/rclonecache` (every flow over a fake runner, a fake `/proc` and
temporary directory trees) and `src-go/cmd/rclone-cache-status`. What only a host with rclone
mounts can prove is section 7.

## 1. Why this one next among P6

The P6 tools left after `open` are `gh-account-setup.bash`, `setup-rclone`, `nord`, the
`ssh-with-key` family (credentials: a fake cannot show a secret never reaches argv or a log on
the real tools), `reclaim` (root and deletions), `nvidia-status` (hardware the container cannot
fake usefully) and the `rclone-cache-*` pair. **`rclone-cache-status`** runs as the user,
deletes nothing and changes nothing: it reads `findmnt`, `/proc`, one RC endpoint and a
directory tree, all of which a fake pins. Its one credential is the RC password, and the Go
client that reads it and passes it in the child's environment already exists and is tested
(`internal/rclonerc`, from the `ftp-camera` port). It is the larger of the pair (323 code lines,
the `data` flag: two embedded Python programs); `rclone-cache-warm` (101 lines, no flag) calls
it and stays bash for now. It had no test suite, so no suite was converted.

## 2. Scope decisions

- **D1. One Go binary at the old path.** `src-go/cmd/rclone-cache-status`, built by
  `tasks/build-go-command.yml` from `play-rclone.yml` (tags `rclone` and
  `rclone-cache-status`), installed at `~/.local/bin/rclone-cache-status`, mode 0755. The play
  imports `play-golang.yml` first with the same two tags, so either installs the toolchain too;
  `--tags rclone-cache-status` installs only the command and leaves the mounts up (a whole run
  stops and restarts them). The bash file is deleted and
  its row leaves `scripts/bash-limits.allowlist`; `scripts/go-binary-suites.tsv` gets
  `rclone-cache-status	qa-go`. The callers (`rclone-cache-warm`, `rclone-tail`) run it by
  name from `PATH` and are unchanged.
- **D2. External programs stay where they are the tool.** `findmnt` lists the rclone mounts;
  `rclone rc` asks a mount for `vfs/stats`. Done in Go: reading the mount processes' arguments
  from `/proc`, decoding the JSON, the directory walk, the byte and age formatting (no Python,
  no `numfmt`).
- **D3. One seam for processes.** Everything runs through `internal/execx`; the RC call through
  `rclonerc.Client.Call`, which keeps the password out of argv. `rclonerc` gains the lookup of
  the mount process serving a mountpoint (`MountProcess`), which `AddrForMount` now shares.
- **D4. A report, not a gate.** A mount whose RC cannot be asked is a line of the report, and
  the run still exits 0 (the callers bracket their own runs with it, under `set -e`). A failure
  that makes the report itself wrong (the mount list cannot be read, the directory asked about
  cannot be read in full) is exit 1.

## 3. Expectations

### Command line

- E1. `rclone-cache-status [--label TEXT] [--no-dir] [TARGET]`; `-l` is `--label`; `-h`/`--help`
  anywhere before `--` prints the usage on stdout and exits 0, whatever else the line holds;
  `--` ends the options. The whole command line is read
  before anything runs.
- E2. An unknown `-...` option, a `--label` with no value, or a second target is a usage error:
  `rclone-cache-status: <text> — see 'rclone-cache-status --help'.` on stderr, exit 64. Every
  error line is `rclone-cache-status: <text>` on stderr.
- E3. `rclone` not on `PATH` is `rclone-cache-status: rclone is not installed; it is installed by play-rclone.yml.`, exit 1, before anything is printed on stdout.

### What is asked about

- E4. A TARGET that is an existing directory (symlinks followed) is a query path, made absolute
  and symlink-free. Any other TARGET is a mount name: the report is filtered to the mounts whose
  name is TARGET.
- E5. No TARGET and no `--no-dir`: when the working directory (symlink-free) is inside an
  rclone mount, it is the query path; otherwise the report covers every mount.
- E6. A query path is inside a mount when it is the mount's target or below it. With nested
  mounts the deepest one is chosen. A query path inside no rclone mount is
  `rclone-cache-status: <path> is not inside any rclone mount`, exit 1. With a query path the
  report covers only the mount that contains it.

### The mounts

- E7. The mounts are `findmnt -J -l -o SOURCE,TARGET -t fuse.rclone`, in findmnt's order. Its
  exit 1 with nothing on stdout is no mount. Any other failure, or output that is not that JSON,
  is `rclone-cache-status: findmnt could not list the rclone mounts: <reason>`, exit 1.
- E8. A mount's name is its SOURCE up to the first `:`; its remote path is the rest, without a
  leading `/`.
- E9. A mount's process is the first `rclone mount` process, in pid order, one of whose
  arguments after the program is exactly the mount's TARGET (`rclonerc.MountProcess`). Its RC
  address is the value of its one `--rc-addr=` argument; its cache directory the value of its
  first `--cache-dir=` argument, else `$HOME/.cache/rclone`. No process, or no `--rc-addr=`,
  is a mount with no RC. More than one `--rc-addr=` is an error line for the mount (E12).
- E10. No mount found prints, after the heading, `  No rclone mount matching '<name>' found.`
  (a name filter) or `  No rclone mounts found.`, and exits 0. (A query path always has its
  mount, E6.)

### The summary

- E11. The heading is `=== rclone cache status ===`, or `=== rclone cache status (<label>) ===`
  with `--label`. Then one line per mount.
- E12. A mount with no RC is `  <name>  (no --rc; cannot query)`. A mount whose RC cannot be
  asked is `  <name>  error: <reason>`, the reason one of: `rc credential missing — run play-rclone.yml` (the credential file is missing or empty; `rclonerc`'s advice follows on
  stderr), `rc rejected credentials (401)` (rclone's output names `401` or `Unauthorized`),
  `rc unreachable` (any other failure of `rclone rc`), in both cases rclone's own output on
  stderr as `rclone-cache-status: <name>: <output>`, `parse failed` (`vfs/stats` answered with
  something that is not one JSON object, or a counter that is not a number; the decoder's
  reason on stderr), or the more-than-one
  `--rc-addr=` refusal of E9.
- E13. `vfs/stats` is read as: `diskCache.bytesUsed`, `files`, `uploadsQueued`,
  `uploadsInProgress`, `erroredFiles`, `outOfSpace`, and `opt.CacheMaxSize` (bytes) and
  `opt.CacheMaxAge` (nanoseconds). A missing or null counter is 0; a number may be written as an
  integer or a float (truncated).
- E14. A queried mount is `  <name>  cache: <usage> · <files> files · max-age <age>`, then, each
  only when not zero, ` · queued: <n>`, ` · uploading: <n>`, ` · <n> errored`,
  ` · OUT OF SPACE`. `<usage>` is `<used> / <max> (<pct>%)` with `pct` = used × 100 / max
  rounded down, or `<used> / ∞` when the max is 0. `<age>` is `off` for 0 or less, whole days
  (`365d`) from a day up, whole hours (`3h`) from an hour, else seconds (`59s`).
- E15. Bytes are written as `numfmt --to=iec --suffix=B` writes them: below 1024 the number and
  `B`; above, the largest power of 1024 (K, M, G, T, P, E) that leaves at least 1, rounded up,
  with one decimal below 10 and none from 10 (`1.1KB`, `10KB`, `400GB`; `1048575` is `1.0MB`).

### The directory

- E16. With a query path, its mount's line is followed by `    └─ scanning <path> ...` and the
  analysis. The cache mirror of the path is `<cache dir>/vfs/<name>[/<remote path>][/<path below the mount>]`.
- E17. The analysis walks the query path without following symlinks. Every entry that is not a
  directory is a file, its size `lstat`'s. A file is cached when its mirror path exists (symlinks
  followed), the cached size being the mirror's `lstat` size.
- E18. It prints `    └─ in <path>`, then `       (no files)` when there are none, else
  `       <n> files · <total> total`,
  `       cached:   <c> files (<fp>%)  ·  <cs> (<sp>%)`,
  `       uncached: <u> files (<100-fp>%)  ·  <us> (<100-sp>%)`, percentages rounded down (0 when
  the total is 0), and when some file is uncached, `       top uncached:` and up to five lines
  `         <size right-aligned to 8>  <path below the mount>`, largest first, equal sizes by path.
- E19. An entry the walk cannot read (a directory it cannot list, a file it cannot `lstat`) is
  named on stderr (`rclone-cache-status: cannot read <path>: <reason>`), the walk goes on, the
  analysis ends with `       incomplete: <n> entries could not be read` (`1 entry`), and the
  run exits 1. A file whose mirror cannot be asked about (an error other than "does not exist"
  or "not a directory") is such an entry: counted as a file, neither cached nor listed as
  uncached. A query path that cannot be read at all is the same, with no counts claimed.

### Output

- E20. Colour (bold heading, cyan names, dim notes, green, yellow and red figures) only when
  stdout is a terminal and `NO_COLOR` is unset or empty; the text is the same either way.
- E21. The report is stdout; every diagnostic is stderr. A failed write to stdout is exit 1.

## 4. Kept as it was

The text of every report line, the order (heading, mounts in findmnt order, the analysis under
its mount), the exit 0 for a report with error lines, the name-or-directory reading of TARGET,
the default cache directory, and the 80% (yellow) and 95% (red) usage colours.

## 4a. Callers

Both callers run under `set -euo pipefail`, so the command's exit status reaches them.
`rclone-tail` snapshots at its start and in its Ctrl-C handler; it passes `--no-dir` (with its
mount filter when it has one), so the snapshot is the mount summary, and E19's exit 1 for an
incomplete directory walk cannot end it when it is started inside a mount. `rclone-cache-warm`
passes the mount's name, so it gets that mount's summary line. A stated limit: a directory of
that same name in its working directory would be read as a directory target (E4), as the bash
read it. `TestRcloneTailNeverWalksADirectory` and `TestRcloneCacheWarmNamesTheMount`
(`internal/rclonecache/callers_test.go`) read both scripts and pin these calls; a change to
either script runs the Go gate (`scripts/qa-routing.tsv`).

## 5. Departures from the bash

- B1. Usage errors exit 64 (the bash: 2), and `--label` with no value is a usage error (the
  bash's `shift 2` ended the run with status 1 and no message). `--` ends the options.
- B2. Errors are `rclone-cache-status: <text>` (the bash: `ERROR: <text>`).
- B3. A `findmnt` that fails is exit 1 naming it (the bash: `2>/dev/null || true`, read as no
  mounts). The mounts are read once, as JSON, so a mountpoint with a space in it is one target
  (the bash split `SOURCE TARGET` on whitespace).
- B4. The mount process matches an argument equal to the target (the bash: the target between
  spaces in the joined command line). More than one `--rc-addr=` is an error line (the bash took
  the first).
- B5. The RC error reasons keep the bash's text, except: the "helper library missing" reason is
  gone (no library: the Go client reads the credential itself), the credential reason names the
  play, and rclone's own output for `rc unreachable` is printed on stderr (the bash discarded
  it).
- B6. With nested mounts the deepest mount contains a path (the bash: the first in findmnt's
  order).
- B7. Unreadable entries are named and make the run exit 1 (the bash's `os.walk` skipped them in
  silence and reported partial counts as whole). A symlink to a directory counts as a file (the
  bash's `os.walk` listed it among the directories and did not count it).
- B8. Equal-sized uncached files are listed by path ascending (the bash: descending).
- B9. Colour honours `NO_COLOR`.
- B10. No rclone mount at all prints `  No rclone mounts found.` and exits 0. The bash could
  never print it: `findmnt` exits 1 when it finds nothing, the mount listing's pipeline failed
  under `pipefail`, and the run ended with status 1 after the heading (found side by side).
- B11. A bare `-` is a TARGET (a mount name, or a directory called `-`), not an unknown option
  (the bash: `ERROR: unknown option: -`, exit 2).

## 6. Proof

`go test -race` over `internal/rclonecache`, `internal/rclonerc` and `cmd/rclone-cache-status`:
every expectation has a test written first and seen to fail (journal). Before cut-over the Go
binary and the bash at `1d2b7aa9` were run side by side in the container over a fake `findmnt`
and `rclone` on `PATH` and a temporary cache tree (journal); every difference is a departure
above.

## 7. Host checks (the owner's, after the play)

- H1. `rclone-cache-status --no-dir` lists every mount with live cache figures, not an error.
- H2. Run from inside a mount directory, the analysis matches what the mount holds.
- H3. `rclone-cache-warm <dir>` and `rclone-tail --once` still bracket their runs with it.

`deploy-rclone-cache-status.bash` (queued in `meta-deploy.list`) runs `play-rclone.yml --tags rclone-cache-status`, which builds and installs the command and leaves the mounts up, then
`acceptance-rclone-cache-status.bash`, which scripts H1 and the installed-program check; H2 and
H3 are by hand.
