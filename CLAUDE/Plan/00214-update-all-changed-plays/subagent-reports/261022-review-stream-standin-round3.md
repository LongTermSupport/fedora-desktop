# Review: stream-stand-in-not-a-text-stream, round 3 (22e599b3d..7b221d5a4)

Verdict: **FIX-FIRST** (cosmetic only; everything substantive is resolved)

## Round 2 findings

**A. Anchored regexes: resolved.** Probed with semgrep 1.177.0 (probe file
untracked/scratch/probe-r3/p.py, one class `_S` with only `write()`):

- Reported: `log_stream=`, `event_sink=`, `OUT_PATH=`, `error_sink=` through a variable;
  parameters annotated `typing.TextIO`, `Optional[TextIO]`, `"TextIO"`, `IO[str]`,
  `typing.IO[str]`, `"typing.Optional[typing.IO[str]]" = None`, `typing.TextIO | None = None`
  and `io.TextIOBase`.
- Silent: `streamer=`, `sinker=`, `upstream=`, `stream_id=`, a `TextIOBase` subclass passed as
  `log_stream=`, and parameters annotated `IO[bytes]`, `typing.BinaryIO` and `int`.
- The fixture now has a `ruleid:` line for each new branch, plus `ok:` for `streamer=`:
  26 `ruleid` and 4 `ok` lines, which matches the DBF report.
- `./scripts/qa-speech-to-text-rules.bash` passes (rc 0, 396 passed, 0 failed).
- The re-sweep holds. The rule finds nothing in the current tree (197 files). On the pre-fix
  `login_report.py` and `pingbus/cli.py` (from 22e599b3d) it finds exactly 3 sites:
  cli.py:1638, cli.py:2168 and login_report.py:470. The DBF report's "same 3 sites" is true.
- The wording in QA.md, the DBF report and the rule comment now matches what the rule does.

**B. Line wrapping: NOT resolved.**

- CLAUDE/QA.md: the fix moved the overflow instead of reflowing the paragraph. Line 875
  ("threads share the stream, write each line with one `write` call: `print(file=)` makes two
  writes, so") is now 100 characters wide. The paragraph is about 88 wide, and lines 876 and
  877 are left short (62 and 48).
- docs/ccy-changelog.md: resolved.

## New findings

**C. Cosmetic, new in 7b221d5a4.** In `.semgrep/speech-to-text.yml`, line 203 of the rule
comment ("metavariable-regex at the start of the value, so each regex spans the whole of it.
A stand-in defined in one") is 112 characters wide. The rest of the block is about 90 wide.
Reflow it together with lines 202 and 204. The DBF report's Fixture paragraph (line 81, 127
wide) has the same problem; it is plan-local, so fixing it is optional.

**Fix:** reflow the QA.md "How to fix a finding" paragraph and the rule comment at lines
198-204. No logic changes.

## Non-blocking observations

- The annotation regex has no closing boundary. A container annotation such as
  `list[TextIO]` therefore matches, and a write-only object assigned over such a parameter
  would be reported. That is a theoretical false positive, and it fails loud rather than
  silent, so it is acceptable.
- `out_\w*` / `err_\w*` would match names like `out_path=` or `err_code=`. A report also
  needs the value to be a same-file write-only class, so that is acceptable.

## No new issues

These were rechecked independently across the whole diff:

- **login_report.** `_Sink` is replaced by `io.StringIO`, and the `_lines` helper is fine.
- **Tests.** `test_login_report` and pingbus `test_cli` run by module: 153 tests, OK.
- **pingbus `_LockedStream(io.TextIOBase)`.** `writable`, `write` returning `int`, and
  `writelines` all write once under the lock. `flush` is delegated. `close` sets
  `_releasing`, so neither collection nor close flushes or closes the wrapped stream.
  `isatty` is delegated.
- **Versions.** CCY 3.100.9 and container 2.79.4 agree across the Dockerfile LABEL,
  `REQUIRED_CONTAINER_VERSION` and docs/ccy-changelog.md.
- **meta-deploy.list.** The 00213, play-claude-yolo.yml, 00214 and `--changed` entries name
  the new versions and the rebuilt pingbus. The 00214 entry says why it must run again and
  that the fix is live without a play.
- **DBF referral.** The positional and cross-module gaps are correctly out of scope. The
  referral is still accurate.
