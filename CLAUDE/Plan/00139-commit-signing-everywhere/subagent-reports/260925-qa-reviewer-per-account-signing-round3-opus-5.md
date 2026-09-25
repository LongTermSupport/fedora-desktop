# QA review, round 3: per-account signing keys (confirming pass)

- **Reviewer:** `qa-reviewer` (Opus 5.5).
- **Diff:** `88cc0a1d..8bd46212` on F44.
- **Saved by the coordinator.** The reviewer is read-only and cannot write files, so this
  is the summary it returned.

**Verdict: FIX-BEFORE-MERGE.** Nothing is blocking.

**The review's own probe left state behind.** Testing `gh-update-git-config` inside the
repository set `user.email` and `user.name` to a fixture identity in its `.git/config`, and
a stray redirect left a file named `0` at the root. The coordinator removed both before
committing anything.

## Should fix

1. **The refusal's remedy is wrong when ccy starts in a subdirectory**
   (`ssh-handling.bash:1210`). git reports `file:.git/config` relative to the repository's
   top level. The printed command named `<repo>/sub dir/.git/config`; run, it exited 255 and
   the key stayed. The refusal itself holds. Fix: build the path from
   `rev-parse --show-toplevel`, and add a subdirectory test.
2. **A path git quotes breaks the remedy** (`ssh-handling.bash:1195,1208`). A non-ASCII path
   comes back as `file:"…\303\251…"` and takes the wrong branch; a `'` breaks the quoting.
   Fix: read with `git config -z` and print the path with `printf %q`.
3. **`scripts/qa-js.bash:65-74` keeps its own exclusion list**, which has drifted from
   `qa-discovery.bash:54-64` again. Fix: use `qa_is_excluded`, as
   `qa-ansible-syntax.bash:79` does.
4. **Acceptance check 11 says "not verified" when it could not look**
   (`acceptance.bash:202-203`). With every account read failing, it still reports that no
   account has the email verified, though the comment promises "unknown".

## Nits

- **Check 11 cannot catch a wrong noreply id** (`acceptance.bash:181`, `signing.py:20-23`).
  A `1+alice@…` typo passes both. Fix: compare with `gh api users/<login> --jq .id`.
- **`acceptance.bash:168` is 111 characters long.**
- **The range has six commits, not five.** The extra one, `ffec1448` (Plan 00109), is clean.

## Round-2 items

- **Item 1:** holds under the owner's ruling. Each launch prints the key it staged, tested,
  and `docs/ccy.md` names the risk.
- **Items 2, 4 and 5:** fixed, apart from 4 above and the nit.
- **Item 3:** fixed. Against a stub `gh`, an unknown alias, a missing token, an API failure
  and running outside a repository each return 1. On success it uses the alias's own token.
- **Item 6:** the code answer holds.
- **The nits:** the remedy is fixed only at a repository's top level (1 and 2 above).
  Refusing when signing is off fails safe, so it is right. The comment, wrapping, table and
  run.bash message are fixed. Leaving the journal-order advisory is right.

## Clean

- CCY 3.67.2 (library 1.6.2) and run.bash 1.26.2 have changelog entries. No image file
  changed, so no container rebuild is needed.
- No identifiers in the added lines.
- Plan state matches the work.

## Gates

- `qa-all.bash`: rc 0 (bash 320, python 190, js 17, helper tests 2182).
- `test-ccy-git-signing.bash`: 38 passed.
- `test_signing`: OK.
- `--syntax-check` on `play-github-cli-multi.yml`: rc 0.
- `plan-qa --sweep`: 0 blocking.
