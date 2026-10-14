# Suite count line (follow-up to B29)

qa-changed reads a suite's count with `passed:[[:space:]]+([0-9]+)` (src-go/qa/changed/summary.go, last matching line, last match on it). A suite exiting 0 without it "proved nothing".

Fixed: each of four suites now also prints `passed: N   failed: M` from real counters, leaving the lines qa-all reads (`Summary:`, `VERDICT: PASS`, `PASSED (library version X)`, `ALL CHECKS PASSED`) untouched.

- test-ccy-info-flags.bash: had `passed`/`failed` counters; added the line (43/0).
- test-ssh-suspend-guard.bash: added a `PASSED` counter in `is` (11/0).
- test-planlib.bash: `pass` counts, `fail` counts into `FAILED_CHECKS` (136/0). Printed before the PASSED line.
- test-ccy-ssh-probe.bash: `pass` counts; line printed on success and before the fail-fast exit. Note: this suite is NOT in qa-all.bash, so qa-changed's population (found by text in qa-all.bash) never selects it; it needs a live host with ~/.ssh keys. Fixed anyway as asked.

Guard: `TestEverySelectableSuitePrintsTheCanonicalCountLine` in src-go/qa/changed/realtree_test.go reads the real population (loadPopulation) and requires each test-\*.bash source to print `passed: <%d|%s|$var>`. Runs nothing. Red first: failed for planlib, info-flags, ssh-suspend-guard; green after.

Verified: shellcheck (only pre-existing planlib SC1091/SC2034 notes), the four suites, test-qa-changed.bash (107/0), test-qa-helper-summary.bash (78/0), go test ./... and golangci-lint run in src-go/qa (0 issues). No separate helper-counts-reader suite exists in scripts/. qa-all.bash not run.
