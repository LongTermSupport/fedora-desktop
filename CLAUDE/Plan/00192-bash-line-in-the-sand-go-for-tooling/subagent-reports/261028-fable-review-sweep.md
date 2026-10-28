# Review: commit 0c88e81a4, the sweep keeps run evidence

Read-only adversarial review of `src-go/internal/vmtest/retention.go`, `prune.go` and
`retention_test.go` as of 0c88e81a4, against the author's report
`261028-sweep-keeps-evidence.md`. `go test ./internal/vmtest/...` passes.

## Verdict

Mergeable once the one FIX-BEFORE-MERGE below is addressed. No path removes a run
directory or a build directory any more, the run-side ranking and skipping are right, the
second sweep is a no-op, and a failed removal stops the sweep with the list of what went.
The remaining hole is on the build side, where the sweep still cannot tell a failed build
from one that is running.

Counts: 0 BLOCKER, 1 FIX-BEFORE-MERGE, 5 FOLLOW-UP.

## What was checked and holds

- No `os.RemoveAll` is left on a run or build folder in the sweep. The only removals in
  `retention.go` are `os.Remove` of the names `overlays()` returns
  (`retention.go:293-303`). The other `RemoveAll`s in the package (`build.go:129,551`,
  `member.go:292`, `fetch.go:162`) are a build's own directory at its start and end, a
  member's directory on `member-down`, and a fetch's scratch home: none is reached by the
  sweep.
- `overlays()` (`prune.go:168-184`) is `filepath.Glob(dir/*.qcow2)`: one level only, so a
  file in a subdirectory is not reached; `x.qcow2.log` does not match; a symlink named
  `overlay.qcow2` pointing at `bases/<b>/base.qcow2` is sized by `Lstat` and removed with
  `os.Remove`, which unlinks the link and leaves the base.
- Bases: `sweepRuns` reads only `runs/`; `sweepBuilds` takes only directories whose name
  ends `.build`, so `bases/server-fast-44/base.qcow2` is never a candidate (the test checks
  it survives).
- Live run: a run is "passed" only once `response.json` says `finished`/`pass`, and
  `judge()` (`run.go:375-384`) destroys and undefines the domain before `runJudge` writes
  that file. So a passing run under the sweep has no domain, and its lock is released at
  the end of a teardown that already removed the overlay. The sweep's run path therefore
  cannot reach a live guest without checking domains, unlike `prune-runs`, which must,
  because it also takes runs that did not pass. Members live under `members/`, not `runs/`.
- Ranking: `factsOf` (`retention.go:55-67`) rejects an id whose stamp parses as no real
  date, and such a directory is neither evicted nor counted toward the ten (`isRun`
  gate at `retention.go:239`). Two different directories cannot share an id; two ids with
  the same stamp and different scenarios sort stably in `ReadDir` name order, so the
  boundary is deterministic. A passing run with no overlay still counts toward the ten
  (the filter at `:239` is on `passed && isRun`, not on holding a file), so the window
  does not slide onto older runs each night.
- Idempotence: a directory with no `*.qcow2` returns at `retention.go:287` before
  `record` and before `sayf`, so a second sweep logs and prints nothing for runs or fast
  builds. `TestE51Sweep` checks this (second run's stdout is the spool's line only).
  Because the first sweep set the evicted directories' mtime to the wall clock, that
  second-sweep check would fail under the old mtime ordering, so it does prove the
  ordering is by id, if only incidentally (see FOLLOW-UP 4).
- Fail-fast: every error in `evictDisks` is returned; the `_` at `:286` discards the byte
  total, not an error. `removed` is shared across runs and builds (`retention.go:211-216`)
  so the message names every file of this sweep. `TestE51SweepRemovalFails` exercises the
  real path (a non-empty directory named `disk2.qcow2`), and checks the builds and the spool
  were not reached.

## FIX-BEFORE-MERGE

### 1. The sweep removes the disk of a fast-base build that is still running

`retention.go:264-277` (`sweepBuilds`). Every `bases/<name>.build` directory not prefixed
`desktop-` is treated as a failed build and loses its `*.qcow2`. But `build.go:125-141` is
exactly that directory while a build is in progress: `st.dir = bases/<name>.build`, the
installer's target is `st.dir/disk.qcow2`, and only a successful build renames it away
(`build.go:505`). Nothing marks the directory as failed; "failed" is inferred from its
existence.

Failure scenario: the owner starts `vmtest build-base server-fast-44` (or `ensure-base`
or `refresh-base` when they build) in the evening, and `vmtest-nightly.timer` fires
`vmtest nightly` while the installer is running. `sweepBuilds` unlinks `disk.qcow2` under
qemu. The install carries on into the unlinked inode, then `build.go:505` fails on the
rename with ENOENT, the build is reported failed after the full install time, and the
retention log says `evicted build-disk server-fast-44.build disk.qcow2: failed fast-base build`. The brief asks whether a live domain is excluded as `prune.go` excludes it
(`prune.go:216-229`, `markLive`): on the build side it is not. This was also true before
the commit (`RemoveAll` of the same directory), but the commit rewrites this function and
its spec text ("a leftover `bases/*.build` directory") as if it could only be a leftover.

Fix: in `sweepBuilds`, before `evictDisks`, skip (and `record` as `kept build <name> build in progress, libvirt has the domain vmtest-build-<name>`) any `.build` whose domain
`c.listDomains(ctx)` lists; `listDomains` already exists (`member.go:45`) and `prunePlan`
already uses it the same way, so `sweep` grows a `ctx` it already has. Add a test: a
fast `.build` with its domain in the fake's `virsh list --all --name` output keeps its
disk. Update E51 to say the build sweep lists domains first and a failed listing stops
the sweep before any removal, as E61 does.

## FOLLOW-UP

### 2. `retention.log` records an eviction that then fails

`retention.go:290-300`. `record` writes `evicted <kind> <name> <files>: <why>` and then the
loop removes; when a removal fails, the log has already said the file was evicted. The
"log first" order is deliberate (E51 says so) and the sweep's stderr tells the truth, but a
later reader of the log alone is misled. Fix: either word the pre-removal line as
`evicting ...` and append `evicted ...` after the loop, or append a `failed to remove <path>`
line in the error branch before returning.

### 3. The build sweep appends a `kept build desktop-...` line every night

`retention.go:269-273`. A failed desktop build is logged as kept on every sweep, for as
long as it exists, so the log grows a no-op line per night, and the sweep is not silent
when it changes nothing (the test's second-sweep assertion covers stdout only, not the
log). Pre-existing. Fix: say it once, or only in stdout, or not at all.

### 4. The ranking test relies on a side effect

`retention_test.go` (`sweepKit`, `age`, `TestE51Sweep`). The kit still sets every run
directory's mtime to agree with its id, and `age`'s doc comment says the sweep orders
runs by it, which is now the opposite of the behaviour the commit introduces. The
id-not-mtime ordering is proven only through the second-sweep check, where the real wall
clock makes the evicted directories newest. Fix: drop the comment's claim, and make the
proof direct, giving the newest passing run's directory the oldest mtime (and an old one
the newest) before the first sweep and asserting the same two are evicted.

### 5. An empty directory named `*.qcow2` is removed

`retention.go:295`. `os.Remove` also removes an empty directory, so an empty
`runs/<id>/x.qcow2/` goes without the fail-fast the non-empty case gets. Nothing creates
such a thing and nothing is lost, so this is hygiene only. Fix: have `overlays()` return
only regular files and symlinks (it already `Lstat`s each path in `allocated`), so a
directory of that name is a reported error rather than a removal.

### 6. The spec and docs no longer say what the sweep does with a run that did not pass and holds no overlay

Nothing wrong in the code: such a run is untouched. But E51's new text reads "a run that
did not pass is left to `prune-runs` (E61)" while `docs/vm-acceptance-testing.md` says "its
overlay and console are the diagnosis". Both are true; neither says a kept run directory
is kept for ever by the sweep (only `prune-runs` ever removes anything from it, and only
its overlay). One sentence in E51 would close the question the owner will ask after the
incident: what, if anything, ever removes a run directory now (answer: nothing in vmtest).

## Tests that claim more than they exercise

None found beyond item 4. `TestE51Sweep`'s claims (every directory and its transcript and
response kept; only the overlays of runs 0 and 3 gone; `by-hand` untouched; the fast
build's disk gone and its console kept; the desktop build whole; the base untouched; the
exact stdout; the log lines; the spool call; a silent second sweep) are each asserted.
`TestE51SweepRemovalFails` asserts the exit code, both halves of the error message, the
untouched later files, and that the spool was not called.
