# Review: commit 903d27702 — the lab's retention moves to Go

Adversarial, read-only review of `src-go/internal/vmtest/retention.go` and its wiring
(`prune.go`, `run.go`, `member.go`, `build.go`, `freshness.go`, `app.go`) against the Python it
replaces (`903d27702^:helpers/vmtest/retention.py`), its tests, and SPEC-vmtest E16, E33, E51,
E56, E61-E64. `go test ./internal/vmtest/...` passes in the worktree.

Verdict: **mergeable after one fix**. 0 BLOCKER, 1 FIX-BEFORE-MERGE, 6 FOLLOW-UP.

## FIX-BEFORE-MERGE

### F1. `--older-than` of 106752 days or more prunes every failed run

`src-go/internal/vmtest/retention.go:84`:

```go
case now.Sub(r.started) > time.Duration(days)*24*time.Hour:
```

`time.Duration` is an int64 of nanoseconds; `24*time.Hour` is 8.64e13, so any `days` of
106752 or more overflows to a negative duration. `now.Sub(started)` is positive for every
run, so the comparison is true for every run that did not pass and has no later pass, and
each is pruned with the reason `did not pass, and is older than 200000 days`. The Python
computed `older_than_days * 86400` in unbounded ints and could not do this.
`parsePruneArgs` (`prune.go:254-258`) accepts any non-negative `Atoi`, so
`vmtest prune-runs --older-than 999999 --yes` (a user meaning "never by age") removes the
overlays of every failed run on the disk. The dry run shows the same wrong plan, so it does
warn first, but the words say the opposite of what happens.

Fix (either): bound the day count in `parsePruneArgs` (a usage error above, say, 36500), or
compute the cutoff without a Duration: `!now.After(r.started.AddDate(0, 0, days))` keeps,
else prunes. Add a case to `TestE61PrunePlanByAge` with `days = 1 << 40` expecting `keep`.

## FOLLOW-UP

### F2. A partial overlay removal says what failed, not what went

`src-go/internal/vmtest/prune.go:332-341` (`removeOverlays`). The log line is written once
per run and names no file; the files are then removed one by one, and on the first failure
the command stops with `cannot remove <path>`. Stops at once: yes. But the `VMTEST-PRUNE-RUN`
line for that run is never printed (`pruneOne` returns before `sayf`), so for a run holding
`overlay.qcow2` and `disk2.qcow2` where the second removal fails, nothing on stdout or in
`retention.log` says the first is gone. Fix: put the file names in the log line, or say each
file as it goes (`VMTEST-PRUNE-RUN <id> removed <file> ...` per file, or print the run's line
before removing with the files it is about to remove).

### F3. No test puts a symlink under `runs/` or `bases/`

`retention.go:144` and `:269` rely on `os.DirEntry.IsDir()` being the entry's own type (a
symlink is not a directory to `ReadDir`, on every Linux filesystem including `DT_UNKNOWN`,
where Go falls back to `Lstat`). I verified this by reading `os.ReadDir`; it is right, and a
symlink `runs/20261001T000000Z-x -> /home/<user>` is skipped, never removed and never
followed. The Python said `is_dir() and not is_symlink()` explicitly and its tests did not
cover it either. The property that guards the owner's disk should have a test: a symlink
named like a passed run beyond the cap pointing at a directory with a file in it, and the
target still there after `sweep`; the same for `bases/foo.build -> bases/server-fast-44`.

### F4. The run and member disk guard measures `<home>`, the old floor measured `runs/`

`prune.go:363` `c.env.FreeBytes(l.home)`; the deleted floor was called with `runs/`,
`members/` or `bases/`. Only the rebuild floor (`retention.go:203`) still asks for `bases/`.
If an owner mounts or symlinks `runs/` onto a larger filesystem, the guard refuses or accepts
on the wrong one. Not supported by the lab today; if it never will be, say so in E64; if it
might be, pass the directory being written.

### F5. Dead branch in `runPassed`

`retention.go:115-121`: after `json.Valid(body)` succeeds, `json.Unmarshal(body, &doc)` into
`any` cannot fail, so the second `failf` is unreachable. Drop `json.Valid` and treat an
`Unmarshal` error as "did not pass" (the Python's `JSONDecodeError` branch), which is one
read and one decision.

### F6. `MemTotal:` with no value reports "no MemTotal line"

`retention.go:175-184`: `len(fields) < 2` breaks to the "has no MemTotal line" error. The
line exists; say `MemTotal has no value`. Tested as "any error" in `TestHostMiB`, so the
text is not pinned.

### F7. `freeBytes` multiplies `Bavail` by `Bsize`; the Python used `f_frsize`

`prune.go:71-87` is unchanged by this commit, but it is now the only free-space call for a
run and a member. POSIX defines `f_bavail` in units of `f_frsize`; Linux reports both equal
on every filesystem the lab runs on, so the figure is right in practice. `syscall.Statfs_t`
has `Frsize`; using it costs nothing and matches the Python the spec was written against.

## Behaviour parity, case by case

| Case                                                             | Python (before)                                              | Go (after)                                                               | Safe?                                                                  |
| ---------------------------------------------------------------- | ------------------------------------------------------------ | ------------------------------------------------------------------------ | ---------------------------------------------------------------------- |
| Name not shaped `<stamp>-<scenario>`                             | keep, "not a run directory name"                             | same (`factsOf` -> `isRun=false`; `planPrune` case 1)                    | yes; tested `scratch`                                                  |
| Shape right, impossible date (`20261399T000000Z-...`)            | keep (ValueError)                                            | keep (`time.Parse` error)                                                | yes; tested                                                            |
| Passed run                                                       | prune "passed"                                               | same                                                                     | yes                                                                    |
| Failed, a later pass of the same scenario                        | prune, names the newest later pass (reverse name order)      | same (`slices.Backward` over name-sorted; `After` strict)                | yes                                                                    |
| Failed, an earlier pass only                                     | keep                                                         | same                                                                     | yes; tested                                                            |
| Failed, older than DAYS (strict `>`)                             | prune                                                        | same, except F1 overflow                                                 | F1                                                                     |
| Failed, younger                                                  | keep with the diagnosis sentence                             | same text                                                                | yes                                                                    |
| Plan order                                                       | by name                                                      | by name                                                                  | yes; tested                                                            |
| Only runs holding `*.qcow2` are reported                         | vmtest's                                                     | unchanged (`prunePlan` skips `len(files)==0`)                            | yes                                                                    |
| `response.json` missing                                          | not passed                                                   | not passed                                                               | yes                                                                    |
| `response.json` invalid JSON / not an object / not finished+pass | not passed                                                   | not passed                                                               | yes; tested                                                            |
| `response.json` unreadable (EACCES, is a directory)              | not passed (OSError)                                         | **error, command stops**                                                 | declared; tested in prune and sweep; fails toward keeping              |
| `response.json` not UTF-8                                        | **uncaught UnicodeDecodeError, Python died**                 | `json.Valid` false, not passed                                           | yes, an improvement                                                    |
| `runs/` absent                                                   | no runs                                                      | no runs                                                                  | yes                                                                    |
| `runs/` or `bases/` is a file                                    | treated as absent                                            | `ENOTDIR` error                                                          | yes (fail fast)                                                        |
| Symlink under `runs/` or `bases/`                                | skipped                                                      | skipped (F3: untested)                                                   | yes                                                                    |
| Sweep keeps newest 10 passing by mtime, every failed             | yes                                                          | same, stable sort, same eviction order                                   | yes; tested with 15 runs                                               |
| Sweep of `*.build`: fast evicted, `desktop-*` kept and logged    | yes                                                          | same text, name order                                                    | yes                                                                    |
| Log written before each removal                                  | yes                                                          | yes, and a log that cannot be opened stops before removing               | yes; tested                                                            |
| Log parent created                                               | `mkdir -p`                                                   | no; the log is `<home>/retention.log` and `<home>/lab.env` was just read | yes                                                                    |
| Bases never touched                                              | only `.build` dirs under `bases/`                            | same; `listBases` only globs                                             | yes                                                                    |
| RAM ceiling `int(host*0.75)`                                     | `host*75/100`                                                | identical for ints; boundary tested (5462 -> 4096)                       | yes                                                                    |
| Run/member disk check                                            | `base_size + margin` under runs/ or members/, plus the guard | guard only (22 GiB under home)                                           | spec'd E33/E56/E64; F4                                                 |
| Rebuild disk check                                               | `2*base + margin` under bases/, plus the guard               | same, plus the guard                                                     | yes; tested with a 13 GiB base                                         |
| Floor output                                                     | `VMTEST-DISK`/`VMTEST-RAM` lines on stderr, then the ERROR   | one ERROR with the figures                                               | declared in the journal; stderr either way                             |
| Sweep done line                                                  | `VMTEST-SWEEP-DONE evicted=<runs+builds+spool>`              | only the spool's `VMTEST-SPOOL-SWEEP-DONE evicted=<spool>`               | spec E51 updated; the lab count is now only the `VMTEST-EVICTED` lines |

## Deletion safety

- Path joins: every name reaches `l.at(runsDir, name)` from `os.ReadDir`, which yields base
  names; `filepath.Join` cannot traverse. The removed `../x` injection test was right to go:
  no helper output is parsed any more.
- Removing while listing: `runDirs` and `sweepBuilds` take the whole listing before any
  removal; `prunePlan` is finished before `pruneOne` starts.
- Dry run: `pruneOne` calls `removeLocked` only with `yes && action == prune`; `record` is
  reached only from `evict` and `removeOverlays`; `diskGuard`'s plan takes and releases each
  run's lock but writes nothing. `--yes` is the only way to remove.
- Partial failure: `evict` and `removeOverlays` return on the first error naming the path;
  earlier removals were each announced (sweep) or not (prune, F2).
- Overlay removal removes a symlink named `*.qcow2`, not its target (`os.Remove`); `allocated`
  uses `Lstat`.
- A run that took its lock between the plan and the removal is reported `live` and left
  (`removeLocked`); the libvirt listing is not re-read, but a run makes its lock before its
  domain, so the lock covers it.

## Errors and output

- No discarded errors: every `err` is returned; `errors.Join` on the lock release and the log
  close. No `failed_when`-style continue.
- stdout carries only the marker lines (`sayf`) and the spool helper's pass-through; progress
  and errors go to stderr (`logf`, `errorLine`). Clean.

## Tests

- `TestE61PrunePlanRule/ByAge/IsInNameOrder`, `TestE61RunPassed`, `TestE51Sweep`,
  `TestE51SweepStops`, `TestE33RAMCeiling`, `TestE16RebuildFloor`, `TestE62PruneRunsRemoves`
  exercise the code paths they name through `vmtest`'s own entry point. The old Python
  cases are all carried over, including the impossible-date name.
- `TestE62`'s second block ("three days and four hours old", `failedRun` 2026-10-12T08 against
  the kit's 2026-10-15T12) does pin the strict day boundary.
- Gaps: F1 (huge day count), F3 (symlinks), F2 (partial removal reporting).
