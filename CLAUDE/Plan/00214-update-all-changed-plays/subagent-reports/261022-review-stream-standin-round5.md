# Review: stream-stand-in-not-a-text-stream, round 5 (22e599b3d..3c35b1599)

Verdict: **MERGE**

## Round 4 finding

**D. `scripts/qa-speech-to-text-rules.bash` header: resolved.** It now reads "The third and
fourth came from speech-to-text tests..." and "The fifth came from a login report crash: a
stand-in with only write() was handed to a check that called writelines() on it." That
matches the five-rule list above it, and it is true: `helpers/play_ledger/check_freshness.py`
lines 150 and 238 call `writelines` on its streams. The commit changes comment text only.

## Final pass over the whole diff, no issues

- `helpers/host_health/login_report.py`: the three channels are `io.StringIO`, and `_lines`
  keeps the old `_Sink.lines()` filter, so behaviour is the same.
- `helpers/pingbus/cli.py` `_LockedStream(io.TextIOBase)`: `write` returns `int` and
  `writelines` makes one locked write. `flush` is delegated but skipped once `close` has run,
  so collection never flushes the wrapped stream or closes it. `isatty` is delegated and
  `writable` is True. `print(file=)` goes through `write`.
- Versions: CCY 3.100.9 and container 2.79.4 agree in the Dockerfile LABEL,
  `REQUIRED_CONTAINER_VERSION`, docs/ccy-changelog.md and every meta-deploy.list mention.
- meta-deploy.list: the 00214 entry says why it runs again and that the fix is live without
  a play. 00213, which installs the rebuilt pingbus, comes before it.
- PLAN.md Task 4.3b matches the work, and Task 4.4 stays open for the owner.
- Reran: `python3 -m unittest tests.helpers.host_health.test_login_report tests.helpers.pingbus.test_cli`, 153 tests, OK. `./scripts/qa-speech-to-text-rules.bash`,
  rc 0. `./scripts/qa-bash-limits.bash`, rc 0, nothing reported for the wrapper.
