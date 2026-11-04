# Review: fix/precommit-sigpipe at 58927f4f (merge-base 7fd72106 on F44)

Verdict: **MERGE**. No blocking findings.

Scope reviewed (`git diff 7fd72106..58927f4f`, 6 files, +59/-22):
`scripts/git-hooks/pre-commit`, `scripts/git-hooks/commit-msg`,
`scripts/git-hooks/lib/secret-scan.bash`, `scripts/bash-limits.allowlist`,
`scripts/test-ccy-container-version-hook.bash`, `scripts/test-secret-scan.bash`.
Note: `git diff F44..HEAD` is misleading here — the branch predates the M3 merge on F44,
so that diff shows M3 reversed. The merge-base diff is the real change.

## 1. Equivalence of the matching logic

- **Merged awk vs the grep chain** (pre-commit:175-179). Old: `grep -E '^[+-]' | grep -vE '^[+-]{3}' | grep -vE '^[+-][[:space:]]*(#|$)' | grep -q .`. New: `/^[+-]/ && !/^[+-][+-][+-]/ && !/^[+-][[:space:]]*(#|$)/ { found = 1; exit } END { exit !found }`. Exactly equivalent:
  `[+-]{3}` and `[+-][+-][+-]` accept the same strings (including mixed `+-+`, which both old and
  new also excluded); the three predicates are the same ERE conjunction; awk `exit` in a main rule
  still runs END, and END's `exit !found` is the status. Same shape as the image gate's awk that
  was already in place. `$(...)` strips trailing newlines and the here-string adds one — no
  effect on per-line matching; git diff text carries no NULs ("Binary files differ" lines), so
  holding it in a variable is safe.
- **`--quiet` vs `--name-only | grep -q .`** (pre-commit:155). Both compare index blob OIDs
  against HEAD for the pathspec; equivalent.
- **numstat prefix** (pre-commit:308-309, 428-429). `STAGED_FILE_LIST` is one file per entry
  from `--diff-filter=ACMRT`, so numstat is one line; `[[ "$numstat" == -* ]]` is the same test
  as `grep -q '^-'` on that line.
- **`head -3 <<<`** (commit-msg:119, pre-commit:368). Display only; `head` is now the writer
  into the `while read`, which drains it, so no SIGPIPE on either side.
- **secret-scan here-string** (secret-scan.bash:353). `grep -qwF -- "$token" <<<"$text"` is
  the same match on the same bytes (here-string appends one newline; `-w` on a trailing newline
  cannot create a match).

## 2. Does a git failure now fail loudly?

Yes for every rewritten site. `ccy_diff=$(git diff ...)`, `image_diff=$(git diff ...)`,
`numstat=$(git diff ...)` are plain assignments under `set -e`, so a non-zero git aborts the
hook. Previously a git failure (or SIGPIPE) in these pipelines read as "no match" and the commit
passed. For `! git diff --cached --quiet` see item 4.

## 3. Early-exit pipelines left under pipefail in hooks and libs

None. Remaining pipelines (`grep -oE`, `grep -vE`, `grep -nE`, the masking `awk` with no `exit`
at secret-scan.bash:69-71; commit-msg:45,93; pre-commit:332) all read their input to EOF.

## 4. `! git diff --cached --quiet` semantics

`--quiet` implies `--exit-code`: 0 = no difference, 1 = difference, 128 = error. Under `if !`,
a 128 is read the same as 1 ("launcher staged") and the hook proceeds to
`ccy_staged_src=$(git show ":$CCY_SCRIPT")`, which is a `set -e` assignment and will itself fail
on the same broken git — so the error does fail the hook, one statement later, rather than
passing. Not blocking; semantics are fail-closed.

## 5. Do the tests prove the bypass and fail on the old code?

Yes. Ran both suites in the worktree: container-version hook `passed: 88 failed: 0`;
secret-scan `passed: 30 failed: 0`. Then extracted the merge-base tree (`git archive 7fd72106`)
into `untracked/scratch/sigpipe-red/`, copied in only the two new test files, and ran again:

- container hook on old code: the four new assertions FAIL (`a large helpers/pingbus change without a container bump is rejected → '0' (wanted '1')`, `…says to bump the container version → 'no'`, `a large one-key agent change without a CCY_VERSION bump is rejected → '0'`,
  `…says to bump CCY_VERSION → 'no'`). Four further failures in that run are the suite's
  play-file listing, which needs a git checkout the archive lacks — unrelated to the fix.
- secret-scan on old code: `FAIL: a token near the top of text larger than a pipe buffer is caught`.

The tests exercise the real hooks against a real repo with a ~1 MB diff / 200k-line text, i.e.
the exact production code path, and the secret-scan test's note about needing many lines (grep
reads a whole line before matching) is correct and is why the fixture is shaped that way.

`./scripts/qa-bash-limits.bash` passes (`BASH-LIMITS-OK`), so the three allowlist rows are exact.

## Non-blocking notes

- pre-commit:141-147 — the comment above the CCY_VERSION extraction still narrates the old
  `grep -m1` history; the fix's own comments reference it as "the reason given above", so it
  works, but it is changelog-flavoured.
- pre-commit:314 — `STAGED_CONTENT=$(git show ":$FILE" 2>/dev/null || true)` is pre-existing and
  swallows a git failure as empty content (the pattern scan then sees nothing). Same defect class
  as this fix, outside this diff's scope.
