# Fable final check: feat/stt-token-auto at 5b0f51fe

Third look, narrow: the two FIX-FIRST items of the re-review (ccf89edb) and the note on the
usage-deadline reason. Checked from a detached worktree of 5b0f51fe; nothing in /workspace
was touched.

## What was verified

1. **Merge with F44.** `git merge-tree --write-tree a153aede 5b0f51fe` writes a tree and
   exits 0: no conflicts. a153aede is an ancestor of 5b0f51fe (merge c5a2aa75).
   `CLAUDE/Plan/meta-deploy.list` is the header, the Phase 4 paragraph and the one
   `playbooks/imports/optional/common/play-speech-to-text.yml` entry. The journal day-file
   has its entries in time order (10:27, 10:41, 12:12, 12:46, 13:01, 13:05, 13:24).

2. **wsi-claude-process refactor keeps fail-fast.** `[[ -n "$TRANSCRIPTION" ]] || error_exit`
   and `command -v claude &>/dev/null || error_exit`: the right-hand side exits 1, so neither
   check is inverted and `set -e` has nothing to swallow. The tweak is now
   `TWEAK_LINE="${TWEAK:+$'\n'Additional instruction: $TWEAK}"`, inside double quotes:
   probed with an empty tweak (empty result), a plain one, and one containing `?`, `[a]`,
   `$HOME`, backticks, `}`, `;` and a literal `*`; every character came through verbatim,
   with a real newline in front, no word-splitting, no glob, no expansion. The new pytest
   `test_the_prompt_carries_the_transcription_and_any_tweak_on_a_line_of_its_own` covers the
   same through the real script.

3. **The 1..170 bound.** `^([1-9][0-9]?|1[0-6][0-9]|170)$` probed: accepts 1, 9, 10, 99,
   100, 169, 170; rejects 0, 171, 08, 09, 007, "", 1.5, " 5", "5 ", 1e2, -1. A leading zero
   never reaches `$(( ))`, so there is no octal reading. Note the default changed from
   `${VAR:-170}` to `${VAR-170}`: an exported empty `WSI_CLAUDE_TIMEOUT_SECONDS` is now
   refused rather than silently defaulted. That is the stricter reading and the test asserts
   it; it is the right one.

4. **Env.UsageTimeout is a struct field only.** `NewEnv()` (the production constructor, the
   only one `cmd/wsi-claude-token/main.go` calls) does not set it, and nothing reads it from
   the environment; the only non-test reference is `cmp.Or(a.Env.UsageTimeout, usageTimeout)`
   in `readUsage`. In production the limit is the 60 s constant. The timeout is detected from
   `ctx.Err()`, not from ccy's exit, so a killed ccy is reported as "did not answer within
   60 s" with no exit code and no "re-run play-claude-yolo.yml"; the test asserts both.

5. **Allowlist row** `files/home/.local/bin/wsi-claude-process`: 104/2/113 to 99/2/112.
   Shrank on both counts.

6. **Targeted runs** (in the worktree):

   - `go vet ./internal/wsitoken/` clean; `go test ./internal/wsitoken/` ok.
   - `shellcheck files/home/.local/bin/wsi-claude-process` clean.
   - `./scripts/qa-bash-limits.bash`: BASH-LIMITS-OK.
   - `pytest tests/speech_to_text`: 315 passed, **1 failed**:
     `test_claude_gets_only_the_time_choosing_the_token_left` took 4.66 s against its
     `assertLess(took, 4.5)`. Run alone three times it passes at 4.03-4.10 s. The whole
     suite took 149 s for 316 tests, so the host was loaded; the test's nominal wall time
     is ~4.1 s (2 s chooser + up to 2 s claude budget + two process start-ups), leaving a
     0.4 s margin that load eats. The regex on the line above it (`no answer within [12] s`)
     already proves the budget was cut by the chooser's time; the wall-clock bound only
     needs to tell 4 s from the 6 s a wrong script would take. This test is new on this
     branch (0dcf5a8e), not on F44, and `qa-all.bash` is the push gate, so a flake here
     costs every future push.

## Verdict

One item, test-only, no production code changes:

- `tests/speech_to_text/test_claude_process.py`,
  `test_claude_gets_only_the_time_choosing_the_token_left`: raise the wall-clock bound from
  4.5 to 5.5 s (still below the 6 s of an un-deducted budget), or drop the `assertLess` and
  let the `[12] s` regex carry the proof.

**FIX-FIRST**: the one flaky timing bound above. Everything else is MERGE: the merge is
clean, the refactor is sound, the bound and the seam are as described.
