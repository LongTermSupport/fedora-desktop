# Landing PR 232 (feat/release-version) on F44

**Status:** merged into the work branch off F44 (0da7f5376) and green on targeted QA. Not pushed;
the coordinator merges the work commit. Nothing was tagged or released.

## Commits

- `f6aaebfa9`: `git merge --no-ff origin/feat/release-version`. The PR's commits stay ancestors.
- `7aa0cac58`: qa/toolchain fix for a project rule that landed on F44 after the branch was written (below).
- The last work commit adds the plan journal entry and this report.

## Conflict resolution

| File                           | Resolution                                                                                                                                                                                                                                                                   |
| ------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `CLAUDE/QA.md`                 | F44's gate table kept. It has more rows, and its `qa-private-names` row is already well formed. The branch's only change in that table was a fix to that one cell. The branch's release-workflow section merged cleanly.                                                     |
| `scripts/go-binary-suites.tsv` | Both sides kept: F44's `teamchat-admin` and `teamchat` rows, and the branch's `release-plan` row.                                                                                                                                                                            |
| `claude-yolo`                  | `CCY_VERSION` set to 3.118.2, the next version above F44's 3.118.1. The container stays at 2.84.1: the pre-commit hook accepted the commit without a container bump, and the image is unchanged. The branch's 3.110.1 was never released and does not exist in any tree now. |
| `docs/ccy-changelog.md`        | F44's entries kept. The branch's 3.110.1 entry (four read-only `gitx` questions for `release-plan`) is carried over as the new 3.118.2 entry.                                                                                                                                |

## The old CI failure (ccy-teams "the launch's start")

It does not survive the merge. It was neither this branch's change nor F44's launcher rewrite.

- **Cause:** the branch's merge base (7f988ce) still had the old check. That check bounded the recorded `--since` by two reads of the real clock, so it could flake. In run 41882472850 the 13-digit shape check passed but the window check failed.
- **Already fixed on F44:** commit 25ecc35fa (Plan 00194, 4 Nov 11:59) replaced the check with a fake `date +%s%3N`. The same commit added a rule against bounding a recorded time by the real clock.
- **Merged result:** `scripts/test-ccy-teams.bash` passes 256/256, both run directly and inside qa-changed. The branch does not touch the launcher.

## Red QA found and fixed

`project_walk_open` (issue 191/195) landed on F44 after this branch was written. It flagged `src-go/qa/toolchain/releaseworkflow_test.go`: `releaseWorkflowFindings` read each `action.yml` that `fs.Glob` found with no regular-file check.

- **Class and detector:** the class already has a blocking detector. This was the only instance in the merged tree, since the analyzers stage reported nothing else.
- **Fix:** each globbed action is now checked with `fs.Stat`, and anything that is not a regular file is refused with an error.
- **Proof:** `TestALocalActionThatIsNoRegularFileIsRefused` uses a MapFS named pipe. Without the guard, that pipe reads as empty and passes with no findings.

## QA run

- `go build ./...`. `go test` on `cmd/release-plan`, `internal/release`, `internal/releasever`, `internal/gitx` and `qa/toolchain`: all pass.
- `./scripts/qa-changed.bash --base 0da7f5376` on the merge: every check and suite passed except the Go gate, which failed on the walk-open finding above.
- `./scripts/qa-go-qa.bash --changed --base 0da7f5376` after the fix: every stage passed (fmt, golangci, analyzers, deadcode, the tagged project rules, vuln, mod, build, test, coverage, the teamchat failpoint tests).
- qa-all was not run, as instructed.

## Tooling fault to log (dogfooding)

qa-changed reports `⚠ not covered by qa-changed: .github/actions/release-key/action.yml`. No check in qa-changed routes `.github/actions/*`. The Go gate's `TestTheReleaseWorkflowKeepsItsPromises` does judge that file, but only when `src-go/` changes too. Adding a route in `scripts/qa-routing.tsv` from `.github/actions/` and `.github/workflows/release.yml` to the Go gate would close the gap. I could not file an issue from this seat, so the coordinator should log it.
