# Review: stream-stand-in-not-a-text-stream, round 4 (22e599b3d..95ebdca44)

Verdict: **FIX-FIRST** (one comment sentence; nothing else)

## Round 3 findings

**B. CLAUDE/QA.md "How to fix a finding": resolved.** Lines 872-877 are 87, 84, 82, 89, 89
and 32 characters wide, in line with the surrounding paragraphs (85-90).

**C. `.semgrep/speech-to-text.yml` rule comment: resolved.** Lines 198-204 are 89, 89, 88,
89, 78, 86 and 74 wide. No logic changed in 95ebdca44.

## New finding

**D. `scripts/qa-speech-to-text-rules.bash` header, line 11 is now wrong.** The diff added
a fifth rule to the list (line 8) but left "The last two came from speech-to-text tests that
passed alone and failed in the full gate on a loaded host." With five rules, "the last two"
now means `test-deadline-under-ten-steps` and `stream-stand-in-not-a-text-stream`, and the
second of those came from the login report crash, not from a test. The next line ("The
fifth came from...") contradicts it. Also "given a writelines" reads oddly.

**Fix:** say "The third and fourth came from speech-to-text tests...", and reword line 12,
for example "The fifth came from a login report crash: a stand-in with only write() was
handed to code that called writelines." This is a comment only, so the wrapper stays well
inside the bash limits.

## Rechecked, no issues

- `python3 -m unittest tests.helpers.host_health.test_login_report tests.helpers.pingbus.test_cli`:
  153 tests, OK.
- `./scripts/qa-speech-to-text-rules.bash`: rc 0.
- `_LockedStream(io.TextIOBase)`: `write` returns `int`, `writelines` makes one locked
  write, and `flush` is delegated but skipped once `close` has set `_releasing`, so
  collection never flushes or closes the wrapped stream. `isatty` is delegated.
- `login_report`: the three channels are `io.StringIO` and `_lines` uses `getvalue()`.
  Behaviour is unchanged.
- Versions: CCY 3.100.9 and container 2.79.4 agree across the Dockerfile LABEL,
  `REQUIRED_CONTAINER_VERSION`, docs/ccy-changelog.md and every meta-deploy.list mention.
  pingbus has no package version to bump (none was bumped in 5f86e2700 either).
- meta-deploy.list follows its header. 00213 (play-agent-bus.yml, which installs the
  rebuilt pingbus) comes before 00214. The 00214 entry says why it runs again and that the
  fix is live without a play. The changed-plays description names the login report play.
- PLAN.md Task 4.3b matches the work, and Task 4.4 stays open for the owner's run.
