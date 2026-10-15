# Adversarial review: gate fixes (f8611aba, 8d761435) — read-only

Reviewed `git diff 6affbfec...HEAD` in the worktree. Builder's report:
`261015-gate-fixes-sonnet.md`.

## What was checked

**Commit A, `namesFile` token match.** A differential run of the old substring rule against
the new token rule over every tracked non-`.md` file × every `scripts/*` suite: 1222
(file, suite) selections kept, 244 dropped. Every dropped pair was audited by category; all
are word-internal hits, none of which exercise the file they used to select:

- `files/var/local/claude-code/cc` → 27 `test-ccy-*` suites (hit through `ccy`), plus
  `qa-nokill-containerwatch`, `qa-vmtest-manifest`, `qa-version-pins` (`accept`, `succeed`).
  The suites that really use `cc` (`test-cc-teams`, 6 `test-ccy-*` that invoke `cc`) are
  still selected.
- `files/usr/local/bin/agent-bus` → `test-vmtest-member` (hit through
  `agent-bus-acceptance-u23`, a team name).
- `files/var/local/claude-yolo/claude-yolo` → `test-self-update-cycle`
  (`play-claude-yolo.yml`), `test-planlib`.
- `helpers/self_update/cycle.py` → `test-self-update-cycle` (a comment naming
  `tests/helpers/self_update/test_cycle.py`). That suite runs `helpers.self_update.update`,
  which does not import `cycle`; `cycle.py` stays covered by the `@py` row (ruff, unit-tests).
- `.claude/hooks/stop`, `scripts/lint`, `files/home/.local/bin/open`, plan `deploy.bash`
  files: English words.

The asked-for spellings all still match: `./cc`, `"$DIR/cc"`, `cc;`, `cc)`, `${cc}`,
`cc --teams`. `cc-teams.bash` and `cc_teams` do not match `cc`, which is right: they are other
files. The full relative path is still a substring test, so `files/var/local/claude-code/cc`
still selects a suite that spells out `files/var/local/claude-code/cc-teams.bash` — the
over-selection the comment promised, kept. `go test ./changed/` in `src-go/qa` passes.

**Commit B, parse rewrites.** Each form was run old-vs-new in a differential probe:

| rewrite                                       | proven identical                                                                                                                                                          |
| --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `exec <&"$FD" {FD}<&-` → two `exec`s          | stdin becomes the saved tty in both; `/proc/$$/fd` shows fd 10 closed in both; with `FD=""` both die at the first `exec` with "Bad file descriptor", rc 1, under `set -e` |
| `${v:+ (x)}` → `${v:+" (x)"}`                 | inside `"…"`: same bytes for v empty, `x`, `a b`, `*`, `$(echo hi)`; `${v:-"(none)"}` unquoted also yields one word `(nothing)`                                           |
| `$(( (a-b)/n ))` → `$(( $((a-b))/n ))`        | same value and rc for negative, zero, empty, octal `010`, `08` (both "value too great for base", rc 1), 14-digit; `% width` for `expect_at` empty/0/3/4/`x` same          |
| `if ! { p \| g >f; }` → `if ! p \| g >f`      | pipefail on/off: same lines written, same `FAILPATH` when grep emits nothing or the target is unwritable, same continue under `set -e`                                    |
| heredoc in `if` → `cat … <<'EOF' \|\| { …; }` | in a function under `set -e`: identical body bytes on success, rc 0; on an unwritable dir the same `print_error` text and `return 1`                                      |
| `echo ""; echo "═"` → `printf '\n%s\n'`       | identical bytes; the two-line `printf '%s\n' a b >&2` matches the two `echo … >&2`                                                                                        |

semgrep 1.177.0 `--lang bash --pattern 'echo $X'`: the new launcher parses with zero errors
(745 matches); the base launcher reports `PartialParsing` 17–3994 with 0 matches. common,
docker-health, network-management, restart-request, session-registry, ssh-handling,
token-management: zero errors. session-lifecycle: still "Syntax error" (unchanged, builder
noted it). `./scripts/qa-bash-limits.bash`: BASH-LIMITS-OK; the three allowlist rows only
shrank (2828→2827, 317→316, 1402→1398).

Suites in the worktree: all 34 `scripts/test-ccy*.bash` pass except `test-ccy-ssh-probe`,
which fails its section 3 (no `~/.ssh/github_*` keys in this container); it has no
`qa-gates.tsv` row and is host-only, unrelated to the diff. `test-ccy-restart-request`:
passed 90, failed 0.

## Findings

1. **scripts/test-ccy-restart-request.bash:616–619 — the fd close is no longer asserted.**
   Low. Before, `restore_line` grepped the whole line `exec <&"$FD" {FD}<&-`; an absent close
   made `restore_line` empty and the ordering check fail. Now nothing in the suite checks
   `exec {CCY_RESTART_TTY_FD}<&-` exists, so a later edit that drops the close (leaking the
   tty fd into the container run) passes. Fix: extend the "1 1 1" check to "1 1 1 1" with
   `grep -c -F 'exec {CCY_RESTART_TTY_FD}<&-'`, and require its line number to equal
   `restore_line`.
2. **src-go/qa/changed/suites.go:158–161 — stale doc comment.** Low. `selectSuites` still says
   the basename is "a raw substring, deliberately loose". It is now a token match. Fix: say
   "by path as a substring, or by basename as a token".
3. **No gate holds the parse result.** Medium, pre-existing. The commit restores semgrep's view
   of the launcher, but `printPartial` only warns, so the next `${v:-(x)}` written on the host
   silently re-hides 4,000 lines from every bash rule and nothing goes red. The builder's
   ratchet design is sound and unbuilt. Fix: a follow-up task on Plan 00192 that fails the
   patterns gate for a file whose unparsed union exceeds its allowlist record (or, cheaper, a
   fixed list of files that must parse with zero errors).
4. **docs/ccy-changelog.md:30–31 — unrelated reflow.** Low. The 3.98.2 paragraph had two lines
   joined into one over-long line, in a commit that changes behaviour nowhere. If the
   markdown formatter owns the wrap, leave it; otherwise restore the two lines.
5. **`isNameByte` and `.`.** Info. `.` is not a name byte, so `cc.bash` or `cc.py` in a suite
   would select a `cc` change. Over-selection only, consistent with the stated bias.
6. **`TestEverySelectableSuitePrintsTheCanonicalCountLine` narrowed.** Info. It now skips
   suites with no `count` reader. Population comes from `qa-gates.tsv`, so a suite with a
   `detail` reader is the only kind affected, and `detail_suite_test.go` pins that judge.
   No loss of coverage found.

## Verdict

**MERGE AFTER FIXES** — findings 1 and 2, both one-line. No behaviour difference was found in
any of the five rewritten constructs; the CCY bump (3.98.3) and changelog are present, and the
ratchet only tightened. Finding 3 is a follow-up task, not a blocker for this diff.
