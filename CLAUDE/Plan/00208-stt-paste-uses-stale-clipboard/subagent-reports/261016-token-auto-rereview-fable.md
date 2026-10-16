# Fable re-review: Phase 4, the token with the most allowance left

Branch `feat/stt-token-auto` at 0dcf5a8e, diffed against the previous review commit
(`git diff 3501616a 0dcf5a8e`, the fix commit itself being `b4b48793..0dcf5a8e`) and against
F44 at a153aede. Read-only; nothing on the branch was changed but this file. The previous
review is `261016-token-auto-review-fable.md` (FIX-FIRST: the queue conflict; should-fix 1 the
time budget; should-fix 2 ccy's stderr; the Phase 2 heading).

## Verdict: FIX-FIRST

The two code fixes are real and proven red: the budget is deducted and ccy's first line is
in the reason. The branch again conflicts with F44, in two files, and its queue again carries
an entry F44 has run. One new should-fix from the fix itself (the unbounded override).

### Exact list

1. **Merge F44 (a153aede) into the branch and resolve the two conflicts**
   (`git merge-tree --write-tree F44 0dcf5a8e`: `CONFLICT (content)` in
   `CLAUDE/Plan/meta-deploy.list` and in
   `CLAUDE/Plan/00208-stt-paste-uses-stale-clipboard/JOURNAL/00208-Journal-26-10-16.md`).
   - `meta-deploy.list`: F44 emptied the queue at a153aede (Phase 3 deployed at 7f8b1c23).
     The branch's list (lines 22-38) still carries the Phase 3 paragraph (lines 22-29) above
     the Phase 4 paragraph, both on the one `play-speech-to-text.yml` entry. The correct
     resolved queue is F44's 21 header lines, then **only** the Phase 4 paragraph (the branch's
     lines 30-37, which already ends with "THE EXTENSION CHANGED AGAIN ...: LOG OUT AND BACK IN
     after this run"), then the one entry `playbooks/imports/optional/common/play-speech-to-text.yml`.
     The Phase 3 paragraph goes: it describes a run that has happened.
   - The journal day-file: keep both sides, in time order (branch 12:46, F44 13:01, branch
     13:05). Nothing is lost and nothing is re-stamped.
2. **Bound `WSI_CLAUDE_TIMEOUT_SECONDS`** (`files/home/.local/bin/wsi-claude-process:32`).
   Detail under "1. Time budget" below. Digits only and `1..170`, else `error_exit`; keep the
   allowlist row at 104 or lower by collapsing one or two of the three-line `if` blocks
   (lines 83-85, 88-90, 162-164) into `[[ ... ]] || error_exit "..."`.

## 1. Time budget

- **Deducted, yes.** `CLAUDE_BUDGET=$((CLAUDE_TIMEOUT_SECONDS - SECONDS))`
  (`wsi-claude-process:141`): bash's `SECONDS` counts from the script's start, so it covers
  the chooser (line 99), the prompt read and everything before it. `timeout "$CLAUDE_BUDGET"`
  at line 148. Reverting line 148 to `"$CLAUDE_TIMEOUT_SECONDS"` locally made
  `test_claude_gets_only_the_time_choosing_the_token_left` fail (took 6.0 s, limit 4.5); the
  revert was not committed.
- **Remaining budget \<= 0**: line 142-143 `error_exit` with its own reason and claude is not
  run (`timeout 0` would be no limit; the comment on line 139-140 says so). Tested by
  `test_a_chooser_that_used_up_the_limit_leaves_claude_unrun`.
- **Can Claude still run past 180 s?** Not with the default: 170 s is the whole run. The
  chooser itself is not wrapped in `timeout` by the bash (line 99); its bound is its own
  `usageTimeout = 60 s` (`src-go/internal/wsitoken/app.go:65`) plus one `wsi-setting` read
  (`app.go:357-362`, no context deadline, a local gsettings read). A chooser stuck for the
  whole 170 s is therefore caught only by the callers' 180 s. Acceptable: the only slow step
  inside it (ccy) is bounded, and a chooser that is slow but finishes gets its own reason at
  line 143.
- **The `WSI_CLAUDE_TIMEOUT_SECONDS` override weakens production** (`wsi-claude-process:32`).
  All three callers hand the script their own environment unfiltered (`wsi-stream:900`
  `subprocess.run` without `env=`; `wsipost/app.go` through execx, which inherits
  `os.Environ()`; `wsi-article-window:46`), so a value in the login session's environment
  (e.g. `environment.d`) reaches every post-processing. Probed (`untracked/scratch/`,
  the arithmetic of line 141 in isolation):
  - `abc`, `x[...]`: `set -u` kills the script with `abc: unbound variable` (exit 1, loud,
    no code execution: `$((...))` treats a name as a variable, and `-u` fires first).
  - `1e3`: `value too great for base`, exit 1, loud.
  - `0`, `-5`: budget negative, so line 143 reports "choosing the token took 0 s of the 0 s
    this run has" — fails closed but names the wrong cause.
  - `999999`: claude gets 999997 s; the callers kill the run at 180 s and the diagnosis is
    their generic "could not run: timed out" — exactly what should-fix 1 was about, reopened
    by the seam. Fails loud, but open relative to the design.
    Judgement: keep the seam (the test needs a small limit and the alternative, a stub
    `timeout` on PATH, is worse) but bound it: `[[ "$CLAUDE_TIMEOUT_SECONDS" =~ ^[0-9]+$ ]] && (( CLAUDE_TIMEOUT_SECONDS >= 1 && CLAUDE_TIMEOUT_SECONDS <= 170 )) || error_exit "WSI_CLAUDE_TIMEOUT_SECONDS must be 1-170, not '$CLAUDE_TIMEOUT_SECONDS'"`
    (define `error_exit` first, which it is: line 73). One code line; the ratchet row is 104 and
    may not grow, so collapse one `if` block (see list item 2).
- **60 s versus ccy's own cap.** ccy gives each token's one curl `--max-time "$CCY_USAGE_TIMEOUT"`
  with `CCY_USAGE_TIMEOUT=20` (`files/var/local/claude-yolo/lib/token-management.bash:154,451`),
  the fetches run in parallel and the rest is cache reads and `date`. 60 s is three times the
  worst single fetch; consistent, and the comment at `app.go:65` says why. One gap: when the
  60 s deadline does fire, execx returns an `ExitError` with code -1 (the killed process's
  `ExitCode()`), so the reason reads `exit -1; re-run play-claude-yolo.yml` with no mention
  of the 60 s and a remedy that does not apply (`app.go:252-262`). No test covers the
  deadline. Note, not blocking: name `ctx.Err()` ("no answer within 60 s") when it is set.

## 2. Stderr

- ccy's first non-blank stderr line is appended for every exit other than 0 and 1, and for a
  non-exit failure when there is any (`app.go:255-261`, `firstLine` at 275-282). Reverting
  the append locally made `TestK5CcysOwnFirstLineOfStderrIsInTheReason` fail (stderr lacked
  the line); the test also pins that the second line is not shown. Revert not committed.
- What ccy writes to stderr on `--token-usage --json`: the progress line "Reading usage for
  N account(s)..." (`token-management.bash:893-894`, exit 0/1, so never shown), the unknown
  option line (`:845`, exit 64), and on an old ccy without the option, `print_error`
  (`common-pure.bash:25-27`, which carries ANSI colour codes: the notification would show
  `ERROR:` with escapes around it, cosmetic) or bash's `usage_report: command not found`. In
  JSON mode the per-token error is in the document, not stderr (`:927-931`). None of these
  can carry a token value: the value is read into curl's `--config` on stdin (`:428`) and is
  never echoed. Correct.
- Note the asymmetry that made the old code drop it: `execx.Cmd.Stderr` nil captures into
  `Result.Stderr` and `ExitError.Stderr`; set, it streams and the error is bare
  (`execx.go:109-118`). The fix uses the capture, right.

## 3. Bash limits

- Allowlist row `files/home/.local/bin/wsi-claude-process 105 2 113` → `104 2 113`
  (`scripts/bash-limits.allowlist:49`): shrank by one code line; functions and statements
  unchanged. F44's row is `125 3 138`, so against the merge target it shrinks 21 lines.
  `scripts/qa-bash-limits.bash`: BASH-LIMITS-OK. `shellcheck -x`: clean.
- The merged lines are `log` calls only (lines 80 and 137): debug-log text, no error path
  touched. The `TWEAK_LINE=""` default moved above the `if` (131-134): same value on both
  paths. Nothing hidden.

## 4. Tests

- `go vet`, `go test`, `golangci-lint run` on `internal/wsitoken` and `cmd/wsi-claude-token`:
  ok, 0 issues (21 tests). `pytest tests/speech_to_text`: 313 passed. Red proof by local
  revert for one test on each side, as above.
- The budget test asserts the whole run took < 4.5 s with a 2 s chooser and a 4 s limit; on
  a loaded host a 2 s `sleep` plus bash start-up could brush 4.5 s. It passed here at ~4.0 s.
  Watch it in CI; widen the margin (limit 6, chooser 3, assert < 7) if it flakes. Note only.

## 5. meta-deploy.list

See the exact list, item 1. The branch's content for Phase 4 is right and complete,
including the log-out line; only the stale Phase 3 paragraph has to go, and the merge has to
be made.

## 6. New in the fixes

- No secrets, hostnames or install names: the stub path in the Go test (`/opt/stub/...`) and
  the token names are placeholders; the journal entry (13:05) names files and counts only.
- Fail-fast: every new path exits 1 with a reason on stderr; stdout stays Claude's text only.
- The research heading now reads "Design built (Phase 4)" (`261016-stt-token-auto-research.md:101`).
- PLAN.md Task 4.6 records the fixes and 4.3's line count (104) matches the allowlist.
