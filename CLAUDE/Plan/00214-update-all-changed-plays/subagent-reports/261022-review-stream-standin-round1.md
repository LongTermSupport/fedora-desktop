# Review: stream-stand-in-not-a-text-stream, round 1 (22e599b3d..be81a2ceb)

Verdict: **FIX-FIRST**

## Verified (no change needed)

- **The new test really drives the real check and fails before the fix.** I ran
  `TestTheRealFreshnessCheckThroughTheSeam` against the old `login_report.py` from
  `git show 22e599b3d`, loaded in memory. Both tests error with
  `AttributeError: '_Sink' object has no attribute 'writelines'`. They pass on the new code,
  and `check_freshness.py:150` and `:238` really do call `writelines`.
- **login_report behaves the same on every path.** `_lines` is `getvalue().splitlines()` with
  blank lines dropped, which is exactly what the old `"".join(chunks)` code did. The order and
  the diagnostics going to stderr are unchanged.
- **`_LockedStream` as `io.TextIOBase`:**
  - Closing on garbage collection neither flushes nor closes the wrapped stream: `_releasing`
    is set before `super().close()`.
  - A write after close still goes through, because `write` is overridden.
  - `writelines` is joined and written under the lock.
  - `isatty` is delegated to the wrapped stream.
  - `err` never reaches a subprocess, so `fileno()` and `encoding` (now None instead of an
    AttributeError) do not matter.
- **Gate and tests:** `qa-speech-to-text-rules.bash` passes. The `test_login_report` and
  pingbus `test_cli` modules pass (153 tests).
- **Rule coverage:** the rule flags the old `login_report.py` (line 470), including the
  third-position tuple. It also catches attribute targets (`obj.err = _Sink()`), a fourth
  tuple position, `redirect_stderr`, `print(file=)`, an annotated assignment, and a class
  with a non-io base.
- **Sweep:** I read every first-party `def write(self` in tracked Python. None of them is a
  missed stream stand-in.
- **Version bump:** CCY 3.100.9 and container 2.79.4 match in the Dockerfile and the script.
  The changelog entry is present.

## Findings

1. **meta-deploy.list is no longer exact (CLAUDE/AgentNotes.md rule).** `helpers/pingbus/cli.py`
   also ships outside the image. `play-agent-bus.yml` builds it into the host's
   `/usr/local/bin/pingbus` and into `kit/pingbus`; see `scripts/test-agent-bus-install.bash:436-439`.
   The kit is copied into LXC members, and the host's ccy-teams runs `kit/pingbus`. Yet the
   00213 entry still says play-agent-bus.yml runs "again (it passed on its own at 67033c224,
   so no change)". That run will now install a new pingbus on the host, in the kit and in
   running LXC members, and the entry needs to say so. The changelog line "The image
   changes" also leaves out the host and kit copies.

2. **The rule has gaps inside its stated scope:**

   - (a) "Assigned over a parameter annotated TextIO" only matches the exact `TextIO`.
     `err: TextIO | None = None` is not caught, and first-party code has 31 such parameters
     (`stdout`, `stderr`, `file`, `err`, `unchecked`).
   - (b) The keyword list misses real first-party TextIO parameter names: `out_stream`
     (`claude-supervise.py:8086`), `err_target`, `diagnostics`, `errors`, `log`. Consider
     matching `(^|_)(std(out|err)|out|err|stream|file|sink|output|target)(_|$)` instead of
     an exact list.

3. **The fixture does not prove several branches.** Removing any of these patterns would
   leave the fixture green:

   - the three-element tuple branch, which is the originating shape
     (`out, diagnostics, not_checked`)
   - `sys.stdout =`
   - every `redirect_stdout`/`redirect_stderr` form

   Each needs a `ruleid:` line.

4. **CLAUDE/QA.md:** the gate-table row now reads `...[test-wall-clock-ceiling-under-a-second](...) [test-deadline-under-ten-steps](...) and [stream-stand-in...]`. The comma, or the "and",
   between the first two links is missing. There is also a double blank line before
   `### stream-stand-in-not-a-text-stream` (around line 851).

5. **Minor, worth a sentence:** `print(file=err)` on `_LockedStream` still makes two locked
   writes (the text, then `"\n"`), so lines from two threads can interleave. QA.md's fix
   advice says `print(file=)` support comes "through write", which reads as if it were safe
   here. Either note that only whole-line `write` calls are atomic, or override nothing and
   document it.
