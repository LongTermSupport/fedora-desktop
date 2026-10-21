# Review: Plan 00213 Task 2.1 (Go `pingbus status-line`), branch agent-a4bf8777ab7b35822-956289f7

Reviewer: Opus, standing in for Fable, covering the qa-reviewer checklist. Read-only.
Diff: `git diff F44...agent-a4bf8777ab7b35822-956289f7` (worktree
`/workspace/.claude/worktrees/agent-a4bf8777ab7b35822-956289f7`, HEAD 53bf47a7).

Tests run (targeted, host load high): `go test -count=1 ./internal/execx/... ./internal/pingbus/... ./cmd/pingbus-status-line/` all ok (golden 10.6 s); `src-go/qa: go test ./rules/` ok.
golangci-lint and qa-changed were not re-run here; the completion report says both are clean.

## VERDICT: FIX-FIRST

## Blocking findings

### F1. The golden "cache-\*" scenarios never reach the Go cache, so Task 2.2's cache coverage proves nothing

`golden_test.go:339` calls `statusline.Run` with no `Now`, so the wall clock is used. Every world
is built moments before its step, so every render reads a path whose ctime is within
`racyWindow` (2 s) and `storable` refuses it (`world.racy`). No cache file is ever written in
the oracle. So `cache-fresh-repeat`, `cache-membership-change`, `cache-record-change`,
`cache-down-appears-and-clears`, `cache-bundle-appears`, `cache-role-same-size-change` and the
others pass on the uncached path, and they would still pass if `lookup` ignored the fingerprints
entirely. The SPEC (section 9) and the completion report admit this. But PLAN Task 2.2 says the
oracle "covers a fresh cache ... and an invalidated one", and that claim is not true of the Go.

The unit tests (`cache_test.go`) prove the cached path for one world only: a
non-membership session, with a `team.json` rewrite and a token chmod as the only invalidations
it tries. Nothing cached is invalidated by any of these: the membership desired or applied file
(the owner's stated case, "a membership change shows at once"), the session record, a down mark
appearing, a `home/<team>` link retargeted, or the Element paths. A path class missed by
`w.note` would serve a stale segment for up to 60 s, and no test would see it.

Fix (small): in the in-process renderer, inject `Now` as a settled clock: the newest ctime in
the world, plus `racyWindow`, plus a margin, as `cacheWorld.settled` already does. Then assert
that `statusline-cache.json` exists after a step of a `cache-*` scenario, so a vacuous pass is
impossible. This replays every invalidation scenario through the real cache against the Python
goldens. Down-mark ages shift by about 3 s, which is far inside the 300 s window. Check that no
scenario sits on that boundary. Until this is done, Task 2.2 must not claim cache coverage.

### F2. The branch does not merge cleanly. The version bumps are stale, and the spec requires a re-record

`git merge-tree F44 <branch>` gives conflicts in `files/var/local/claude-yolo/claude-yolo`,
`Dockerfile`, `docs/ccy-changelog.md` and `CLAUDE/Plan/meta-deploy.list`. The branch bumps
3.99.12.19 to 3.99.12.21 (container 2.77.15 to 2.77.17), but F44 is already at 3.99.13.7 /
2.78.7 (Plan 00211). The Task 1.3/1.4 entries need re-bumping above F44, for example 3.99.13.8 /
2.78.8, with the changelog entries renumbered and the meta-deploy rows merged.

SPEC section 8 also says: "Re-record the oracle (`-update`) after [Plan 00211] merges, before the
Go draft is judged". 00211 Phase 1 is now in F44. It changed `protocol.py`, `inbox.py` and
`cli.py`. `config.py`, `statusline.py` and `parse_team_record`'s region look untouched, so
there is probably no golden diff. The spec makes this step mandatory, though: merge F44, run
`go test ./internal/pingbus/statusline/golden -update`, and confirm `git diff` on `golden/` is
empty.

### F3. The test SPEC section 8 promises does not exist

SPEC section 8 says: "Each one [on-disk format] gets a Go test that reads a file the Python
wrote, so drift fails a test." No such test exists. The golden worlds are hand-edited literals
in `scenarios.json`, and no test runs the Python writers (`hooks.record_session_teams`, the
keeper's `applied.json`, `inbox.TeamState` `team.json`, `set_team_down`) to produce input for
the Go readers. A change to a writer would leave every test green. Either add the writer-driven
tests, or amend the spec to say the oracle's literal worlds are the pin and that a writer change
must re-record. That would be a weaker guarantee and should be stated as one.

## Non-blocking findings

- **N1 (execx, fd leak).** `session()` makes the stdin pipe in `wire`, which puts the read end
  in `childEnds` and keeps the write end in `p.stdin`. When `cmd.Start` fails, only
  `closeChildEnds` runs and `feed` never starts, so `p.stdin` is never closed. That leaks one fd
  per failed start (D2's `/bin/sh` missing, or a gone working directory). Close `p.stdin` on the
  start-failure path.
- **N2 (cache write).** `fsx.WriteAtomic` writes `<path>.tmp.<pid>` with `os.WriteFile`, with no
  `O_EXCL` and no `O_NOFOLLOW`. A pre-existing tmp file keeps its old mode, and a planted
  symlink is followed. The risk is low while the home is 0700 and the user's own. Also, the cache
  and any crash-leftover tmp files now live in the seat home (`/tmp/pingbus-home`). Its owner,
  the seat keeper, removes it with a bare `rmdir` (`seat.py _remove_home`) after unlinking a
  fixed list. Today that runs only on a failed start, before any render, so it is safe now.
  Record the new file in the PINGBUS_HOME layout docs (`docs/agent-bus-protocol.md` section 12)
  at cut-over (Task 2.3), and consider adding it to `_remove_home`'s list.
- **N3 (concurrency).** Concurrent sessions sharing `~/.config/pingbus` do a read-modify-write
  with the last writer winning. Entries can be lost, but that only costs a miss. The tmp names
  are per pid, so nothing is torn. Acceptable.
- **N4 (parity target).** The width table, the int-digit limit and the ipaddress forms are
  pinned to Python 3.11.2 / Unicode 14, which is the container's interpreter. If the status line
  also renders under another interpreter (a host or LXC member kit, where Fedora 44 has Python
  3.14), the Go matches 3.11, not that interpreter. The exposure is only in exotic role or
  base_url text. State in the SPEC which interpreter the oracle is.
- **N5 (inherited).** An inner command killed by SIGHUP has status -1, which equals `TIMED_OUT`,
  so it is shown as "timed out after 10 s". The Python has the same collision, so the parity is
  correct. Add it to S38.
- **N6.** After a deadline, a child that exits at the very edge can be reported as timed out,
  because `context.Cause(inner)` is checked after a clean finish. This is a benign race.

## Points checked and found sound

1. **QA rule changes are legitimate.** `shellStrings` is a table row with a reason, which
   GoProjectRules.md names as the only excuse ("a row in a table in `table.go`, with its
   reason"), on the precedent of `dynamicNames`. It is scoped to one function
   (`statusline.runInner`). The fixture is red/green: the neighbour is flagged, `env ... bash -c`
   is handled, and an owned program (`git`) inside an excused function is still flagged, because
   the owner check precedes the shell check in `judgeProgram`. `TestEveryShellStringHasAReason`
   guards the rows, and the docs were updated in both GoProjectRules.md and the analyzer Doc. The
   "shrink, never grow" ratchet applies to the bash allowlist, not to these reasoned tables. The
   gosec exclusion is `_test.go` only (G302/G304/G306/G703), matching the precedent of
   `internal/open/.*_test.go`, `qa/ansible/.*_test.go` and others. No `//nolint` was added.
2. **The execx change is opt-in.** The only change to `OS.Run`'s default path is the
   `if c.NewSession` branch, and `NewSession` has a single setter
   (`statusline.go:241`). There is no regression for other callers.
3. **Timeout, kill, signals and exit status are correct.** Setsid makes pid equal pgid.
   SIGKILL goes to the group from both `cmd.Cancel` and the post-await path, with ESRCH
   tolerated. The output wait is bounded by Grace (3 s), which improves on Python's unbounded
   second `communicate()`. Signals are subscribed before stdin is read, a cancel gives 128 plus
   the signal and empty stdout, and every other path returns 0. The one exception is a failed
   stdout write, which returns 1, as the Python's BrokenPipe would.
4. **The cache's mechanics are right.** It is 0600 via rename, uid/mode-checked and read with
   O_NOFOLLOW on load. The key covers session id, uid, cwd and the S32 variables. Fingerprints
   are lstat plus the link target. Expiry is the earlier of the TTL and the down mark's age-out.
   An entry with a future `Written` is a miss. A clock is injected. An unresolvable home or a
   missing home directory gives no cache, silently, per S36. What is missing is the test
   coverage in F1.
5. **The `go-binary-suites.tsv` row** (`pingbus-status-line qa-go`) matches the file's rule:
   every `src-go/cmd` command needs a row, and qa-go is correct where the oracle is a Go test.
6. **There is no cut-over.** Nothing outside `src-go/` and the TSV names `pingbus-status-line`,
   and the kit still runs `pingbus status-line` (Python).
7. **No install-specific names** appear in the added lines. The test identities use
   `example.org`.
