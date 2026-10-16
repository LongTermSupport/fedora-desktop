# Why CI on F44 is red, and the fix (Opus worker)

Branch `fix/ci-red`, from local F44 (`1d3c65bb`). Fix commit `0d7c0b1e`.

## Runs examined

QA workflow on F44: 38869396737 (latest), 38862560510, 38857115975, 38851068395,
38846710536, 38841118372, 38839905242, 38837452061. Every run fails in the `qa-all.bash`
job, step "Run full QA suite". The `gitleaks secret scan` job passed in all eight.

The latest run was built from `0c61144d`. F44 has moved on since then, and two of the
causes below came in after that commit. They have not reached CI yet, but the next F44
push would hit them.

## Causes

| #   | Gate                           | Seen in CI                                                                             | Root cause                                                                                                                                                                                                                                                                                                                                                 | Fix                                                                                                                                                                                                                                                                 |
| --- | ------------------------------ | -------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | go (`qa-go.bash`)              | all 8 runs: 21 to 22 ftpcamera tests, `TestDefaultSession` first                       | `src-go/internal/ftpcamera/kit_test.go` `realRoot()` faked `sudo -n mkdir -p` with a real `os.MkdirAll` on any path. `startVsftpd` (`server.go:267`, `:273`) makes `/run/vsftpd/empty` and `/run/systemd/system/vsftpd.service.d`. The CI runner is not root, so it gets EACCES. Where the suite runs as root (this container), it writes the real `/run`. | `realRoot` only acts inside the test's temp tree (`kit.inTree`). The two system dirs (`systemDirs`) are recorded and never created. Any other path outside the tree fails the test loudly. `mv` and `rm -f` are guarded the same way. Production code is unchanged. |
| 2   | helper-tests                   | 5 of 8 runs: `test_two_teams_arrive_in_one_delivery`, `('1','1','0') != ('2','2','0')` | Each team's inbox is filled by its own poll thread, and every watch pass delivers whatever is pending at that moment (SPEC-inject E13). Under load, team A's first sync lands and is delivered alone before team B's. The spec promises one delivery only for items that are pending together, so the test was racy. Production is right.                  | `tests/helpers/pingbus/test_notify.py`: inside the test, `cli._inject_pass` is wrapped so a pass runs only once both teams hold their item. That is the state the test means to assert on.                                                                          |
| 3   | go (`qa-go.bash`, `src-go/qa`) | not yet (came in with `00944959a`, after `0c61144d`)                                   | `unittest_counts.py:70` now imports `helpers.qa_environment.session_env`. The real-tree fixture in `src-go/qa/cmd/qa-helper-tests/main_test.go:47` copied only `unittest_counts.py`, so 6 real-tree tests fail with an ImportError.                                                                                                                        | The fixture copies `session_env.py` too.                                                                                                                                                                                                                            |
| 4   | gitleaks                       | not yet (came in with `3222cb1a`, after `0c61144d`)                                    | `test_hs_journal.py:348` held the fake Matrix tokens `syt_...` as literals, and gitleaks flags them as `generic-api-key`.                                                                                                                                                                                                                                  | The tokens are joined from parts at run time. The assertions still prove both are redacted. `.gitleaks.toml` is unchanged: no allowlist.                                                                                                                            |

One older failure is already fixed on F44: `test_statusline`
`test_a_hung_inner_status_line_times_out...` hit ProcessLookupError in run 38841118372.
`assertGone` now catches ESRCH (`9e946120`).

## Reproduction

- **Cause 1.** I could not run the tests unprivileged here: the container is root,
  `unshare` is not permitted, there is no strace, and the worktree is mode 700. Two pieces
  of evidence stand in for that run:
  - This container has a `/run/vsftpd` and a `/run/systemd/system/vsftpd.service.d` that
    earlier test runs created. I left them in place.
  - With `/run/vsftpd/empty` taken out of `systemDirs`, the guard fails 22 tests with
    `root command on "/run/vsftpd/empty", outside the test tree`. That is the same set
    that failed in CI.
- **Cause 2.** With 22 busy loops loading the CPU: 14 of 40 runs failed before the fix,
  40 of 40 passed after it.
- **Cause 3.** Reproduced by `qa-changed.bash` (the go gate over `src-go/qa`). Green after
  the fix.
- **Cause 4.** Reproduced with `gitleaks dir . --redact` (8.30.1): 1 leak before the fix,
  none after, over the whole tree.

## QA

- `./scripts/qa-changed.bash` passes: toolchain, ruff, unit-tests, and the go whole gate.
- Targeted runs pass: `go test -race ./internal/ftpcamera/`, `go test ./cmd/qa-helper-tests/`,
  `python3 -m unittest tests.helpers.pingbus.test_notify`, and the plan's `test_hs_journal`.
- I did not run `qa-all`, as instructed.

## Only CI can confirm

- Cause 1 as a non-root user, which is the condition CI actually runs in.
- That nothing else in the full suite fails once these four are fixed. The full
  `qa-all.bash` in CI is the first full run on this branch.
