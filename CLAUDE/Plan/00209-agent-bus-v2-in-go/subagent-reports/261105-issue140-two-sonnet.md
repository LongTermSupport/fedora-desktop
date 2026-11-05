# Issue 140: the two failures owned here (TestE2EQueuedAtStart, TestOSReadDir)

Branch `fix/teamchat-intermittent-140`, made from F44 at 24d68aa0f. Test-only changes plus one
new project rule; no production code under `src-go/internal` was touched. The hang in
`member/engine` was not touched.

## 1. TestE2EQueuedAtStart

### The two questions

**Does an empty link.log mean the queued send never reached the link? No.** Line 225 of
`e2e_sends_test.go`, where CI failed, is `eventually(... again.down(teamName) ...)`: the wait for
the restarted link to show its team down. The queued send is issued two lines later (227), so the
failing run never sent anything. link.log was empty because the link logs only `down: ...` and
`back up`, and the team had not been marked down yet. Every run measured below answered the send
`queued` in 44 to 144 ms and posted it exactly once. No production defect was found in this path.

**Is a 30 s wall-clock bound what makes it a flake? Yes, but the bound is shorter than the
production schedule it waits on, which is worse than load.** `teamsync.backoff` marks a team down
at the first failed retry once the failure streak is `downAfter` (15 s) old. The retry delays are
1, 2, 4, 8, 16 s, each less a jitter of up to a quarter (`Jitter` is `d/4` at most). With any
jitter the fourth retry falls short of 15 s, so the fifth wait (12 to 16 s) is the one that ends
the streak: down is marked 23.25 to 31 s after the first failure, mean 27 s. `settle` is 30 s.

Evidence, all with the wiring test binary built `-race -tags busfailpoints`:

- A probe copy of the test with 150 s bounds, 8 runs on an idle 8-core machine, time from the
  seat being in to `down` shown: 29.4, 26.5, 27.3, 26.2, 25.5, 30.2, 27.6, 26.2 s. One of eight is
  over the 30 s bound.
- The same probe, time from the server coming back to the queued send posted: 57.2, 24.5, 23.2,
  58.2, 25.8, 54.7, 59.0, 62.3 s. The bound there was `backoffCap + settle` = 60 s, so one of eight
  is over. Two modes: about 24 s (the team resumes on its next retry), and 55 to 62 s (that retry
  and then the engine sender's `retryHold` of 30 s, which a newer sync lifts only if one arrives).
- The unmodified test, 4 copies in parallel with 5 runs each (20 runs, no extra load): 20 pass. So
  the failure is not reproducible at a rate that shows in 20 runs here; the margin is 0 to 5 s on
  the first wait and 0 to 5 s on the second, and CI ran it `-count=2` on many branches.
- Deterministic proof of the schedule, `TestAJitterPutsTheDownMarkAfterTheFifthWait` in
  `member/teamsync/run_test.go` (fake clock, an injected `Jitter` of 1 ns on the first four waits
  and 0 on the fifth): the team is not down at the fifth failure and is marked down by the sixth,
  exactly 31 s less 4 ns after the first. It passes; the existing `TestTransientFailuresBackOff`
  pins the zero-jitter case (down at 15 s).
- Extreme load (16 busy loops plus 3 probe copies on 8 cores, `-cpu 1`) fails everything, including
  setup steps, and also showed `status` answering exit 1 `internal` ("the link failed inside")
  from the engine's `serve.go`. That is a CPU-starvation observation, not the CI failure, and is
  in `member/engine`, which is another agent's: reported, not investigated.

### What was fixed (test-only)

The test owns no event to wait on: the clock that decides `down` is the link's own, in another
process. So the bound is the derived worst case, stated: in `member/wiring/e2e_test.go`,

- `downBound = downAfter + backoffCap + settle` (75 s) and `awaitDown`, used by the three waits
  for `down` (`TestE2EQueuedAtStart`, `TestE2EQueuedWhileDown`, `TestE2EOfflineServer`);
- `deliveredBound = backoffCap + retryHold + settle` (90 s), used by `postedOnce`.

A passing run is not slower: each wait returns when its condition holds. `eventually`, `within`,
`settle` and `backoffCap` are unchanged for every other row.

### Detector

None can be written for this class: whether a bound covers a schedule is a fact of the production
constants and the jitter, not a shape in a test's syntax tree. It is recorded in
`CLAUDE/GoProjectRules.md` under "Defect classes that cannot be a rule", with its fix.

### For the coordinator

- The 55 to 62 s mode: a queued send waited the sender's full `retryHold` after the team was
  back in about half the idle runs. Whether a sync newer than the failure should have lifted the
  hold sooner is the engine owner's question (`member/engine/sender.go` `mayPost`, `holdLocked`);
  I did not read the sender loop far enough to say it is a defect.
- The `status` verb answering `internal` under CPU starvation (above).

## 2. TestOSReadDir

CI: `ReadDir = [{5 other} {2 file}], want [{2 file} {5 other}]`.

**Reproduced deterministically** with the test binary's temp directory on tmpfs
(`TMPDIR=/dev/shm`): the same message, 3 of 3. On this machine's overlay and btrfs it passes.
`restore.OS.ReadDir` reads with `(*os.File).ReadDir(-1)`, which is the filesystem's order (tmpfs
newest first, ext4 by name hash with a per-filesystem seed), unlike `os.ReadDir`, which sorts.

**Does the function promise an order?** No: its interface comment says only "lists a directory; a
missing one is none". Callers: `raiseMinBackup` takes the highest number (order-free);
`leftovers` collects names into one refusal message (the message's order is the directory's, which
is cosmetic). So no caller needs an order, and the test is the defect: it now sorts what it got.

Optional production hardening for the coordinator to decide (not done: production code): make
`OS.ReadDir` sort by name, as `homefs.Root.Entries` and `seat.dir.entries` do ("sorted" in their
doc comments). That would also make the `leftovers` message deterministic.

### Class: a test asserts the order of an unsorted directory listing

Hazard: the order is the filesystem's, so a test passes where it was written and fails on another
runner. Detector `project_listing_order` (`src-go/qa/rules/listingorder.go`): flags
`slices.Equal`, `EqualFunc`, `Compare`, `CompareFunc` and `reflect.DeepEqual` in a `_test.go` file
given a call, or a variable last assigned from a call, of a function that returns a listing in
directory order, with no sort between. A function is marked (an analysis fact, so it crosses
packages, to a fixed point for wrappers) when it calls `(*os.File).ReadDir`/`Readdir`/`Readdirnames`
or `fs.ReadDirFile.ReadDir`, has a slice result and sorts nothing.

- Red, fixture: `testdata/src/fix/internal/badlisting` and `badlistingx` (the originating shape,
  every listing API, a wrapper, a test helper, another package through the fact; sorted, `os.ReadDir`,
  counts, sets and conversions clean). With reporting disabled, 11 `want` lines go red; with the
  sort guard disabled, 2 clean cases go red.
- Red, real tree, before the fix: the sweep reported exactly one finding,
  `internal/teamchat/restore/fs_test.go:139`. (The first sweep also exposed a nil dereference in the
  rule on a call with no function to judge, a conversion or a function value; fixed, with a fixture.)
- Sweep after the fix: `go vet -vettool=<driver>` exit 0 (no finding) over `./...` untagged, over
  `./internal/teamchat/...` with `busfailpoints` and with `tuwunel`, over `./internal/teams/...`
  with `ccyteamstest`, and over the nested `qa` module.
- Not caught: an order asserted through an index, a derived value, an interface method, or a
  comparison helper of the test's own; and the production use of a directory order (the one
  production instance, `OS.ReadDir`, is the coordinator's decision above).
- Blocking: it is in `rules.All()`, which the driver registers; the real tree is clean.

## Tests and gates run

Code commit `f15e4f694`. Run in the worktree:

- `go test -race -shuffle=on -tags busfailpoints -count=1` over `member/wiring` (228 s),
  `member/teamsync` and `restore`: all ok. The fixed `TestE2EQueuedAtStart` alone: ok (85.5 s).
- `TestOSReadDir` fixed, test binary with its temp directory on tmpfs: 3 of 3 pass (red before:
  3 of 3 with the CI message).
- `go test ./rules ./driver` in `src-go/qa`: ok.
- `golangci-lint run` default and `--build-tags busfailpoints` over `wiring`, `restore`,
  `teamsync`: clean; over `qa/rules` and `qa/driver`: clean. `go vet` default and tagged over the
  three packages: clean.
- `./scripts/qa-changed.bash` over the 13 changed files (run with the code staged, before the code
  commit; it queued behind other worktrees' runs on the QA lock): exit 0, with
  `docs: whole gate`, `go: go-qa, both modules (diff mode on a git change)` and
  `teamchat-keeps-off-v1` all passing. The js checks were not planned (no JavaScript changed), so
  issue 135 did not arise. This report was added after that run; `qa-all.bash` was not run (not
  allowed here).
- Not run: the full `./internal/teamchat/...` `-count=2` stage, and the other teamchat packages'
  tests (no file of theirs changed).

The skill `defence-before-fix:dbf` was not available in this session (`Unknown skill`); the method
was followed from CLAUDE.md. The session's context went past its soft budget; the work is
committed and nothing is half built.
