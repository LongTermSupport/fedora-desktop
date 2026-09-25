# QA review, round 4: per-account signing keys (confirming pass)

- **Reviewer:** `qa-reviewer` (Opus 5.5).
- **Diff:** `8bd46212..8b61cf18` on F44.
- **Saved by the coordinator.** The reviewer is read-only and cannot write files, so this
  is the summary it returned.

**Verdict: PASS.** All four round-3 findings are fixed; one nit.

The reviewer left the repository unchanged: no local `user.*` config, no stray file, one
worktree, a clean status. Every probe ran in throwaway repositories with a fixture HOME and
global config.

## Round-3 items

- **The remedy** (`ssh-handling.bash:1196-1222`): fixed. The real function was run, then
  the printed command from `/`. It removed the key in four cases:
  - a relative include opened from a subdirectory;
  - a linked worktree's subdirectory;
  - a submodule's subdirectory;
  - the not-found case (rc 1), which prints no remedy.
- **`remedy_clears`** (`test-ccy-git-signing.bash:185-201`) really runs the printed line and
  checks the named file. Against the 3.67.2 library it fails the two new cases (43 passed,
  2 failed); at HEAD, 45/45.
- **`qa-js.bash`:** the file set is identical before and after, the same 17 files as
  `git ls-files '*.js' '*.mjs'`. Exclusion is tested on the path relative to the repository
  root, so an excluded name above the checkout cannot exclude it.
- **Check 11** (`acceptance.bash:173-231`): driven with a stub `gh` through seven cases,
  each right. They include a wrong id failing with both ids named, an unreadable account
  giving UNKNOWN, and an upper-case login in the noreply address passing.
- **`signing.py` left unchanged:** the reason holds.

## Nit

1. **In a bare repository the remedy still cannot run** (`ssh-handling.bash:1216-1218`).
   git reports `file:config`, and `--show-toplevel` fails, so its "must be run in a work
   tree" message leaks into the refusal. The printed command exited 5 and left the key. The
   same happens when ccy starts inside a `.git` directory. The refusal itself holds. Fix:
   fall back to `rev-parse --absolute-git-dir`.

## Checked and clean

- CCY 3.67.3 and library 1.6.3, with a changelog entry; meta-deploy updated. No image
  file changed, so no container rebuild is needed.
- PLAN.md matches the journal. No doc shows the old remedy or version.
- No identifiers in the added lines.

## Gates

- `qa-all.bash`: rc 0 (bash 320, python 190, js 17, ansible-syntax 82, helper tests 2182).
- `test-ccy-git-signing.bash`: 45 passed.
- `plan-qa --sweep`: 0 blocking; the only advisory is the accepted journal-order one.
