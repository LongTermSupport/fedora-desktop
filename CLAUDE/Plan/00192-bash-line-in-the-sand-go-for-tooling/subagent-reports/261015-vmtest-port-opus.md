# vmtest port (Plan 00192 Task 3.3, P5)

Branch `go-port/vmtest` (from 14058c62), pushed. Not merged.

## Commits

- `07b70a7b` SPEC-vmtest.md: expectations E1-E60, departures B1-B15, scope D1-D5, host-only checks H1-H6.
- `899fec61` `src-go/internal/vmtest` (every verb) and `gitx.RemoteBranchTip`; a path-scoped gosec exclusion for the lab's paths and modes.
- `2c987849` Cut-over: `src-go/cmd/vmtest`, the lab play builds the binary, the scrubber's command front, the bash CLI and four suites removed, `retiredAfterPort` in the qa-all golden, docs and `meta-deploy.list`.
- A last commit with this report, the PLAN.md line and the journal entry.

## What was ported

The whole CLI: fetch, build-base (fast and full, desktop LUKS and secrets on stdin), freshness-status, sweep, nightly, ensure-base, refresh-base, run (host-only gate, run.bash, the desktop session runner, fixture, reboot, checker, evidence, scrub, judge), verify, member-up, member-ssh, member-down. Everything external goes through `execx`; `git` through `gitx`. Clock, sleep, port and randomness are injected.

Kept as they are, by decision: the 21 `helpers/vmtest` Python modules, behind one seam (D1); the scrubber in bash, behind a 0644 front `scripts/lib/run-log-scrub-cli.bash` (D2); the guest scripts (D3).

Departures found during the build and added to the spec: B16 (a desktop host-only run's session `run.env` had no GitHub identity) and B17 (artefact URLs must be https).

## Tests

- `go test -race ./internal/vmtest ./cmd/vmtest`: green, 86.9% coverage. Red came first for each test file, `RemoteBranchTip`, `WithSignals` and the scrub front.
- `test-run-log-scrub.bash`: 27/27, 7 of them new for the front.
- `test-vmtest-prepare-record.bash`: 12 (its CLI case moved to TestE40).
- `qa-changed`: passed on both code commits. That run included qa-go (lint clean, no nolint), the qa-all golden replay with the restored rows, test-qa-changed and bash-limits. qa-all was not run, because this is a sub-agent.

## What remains

- Host proof, owner only (SPEC section 7): H1 fast build, H2 server run and the bridge, H3 desktop build and run, H4 members and Ctrl-C, H5 nightly timer, H6 host-only scrub. The lab play is queued in `meta-deploy.list`.
- Merge after the full `qa-all` and a review.
- The Go tests' comments still name the deleted bash suites as the source of their cases.
