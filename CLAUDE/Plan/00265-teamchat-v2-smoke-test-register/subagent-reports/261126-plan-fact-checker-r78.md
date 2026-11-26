# Fact check: R78 and PLAN.md Task 4.1

Scope: the R78 claims about run.bash and PLAN.md Task 4.1. R76 and R77 were checked earlier and are skipped.

## REFUTED

None.

## Notes (not refuted)

- The cited range `play_batch_warn_dirty` 2079-2090 is slightly wide. The function runs 2079-2088. Lines 2090 and after belong to the next comment and function.
- `play_batch_warn_dirty` also calls `fatal` when `git status` itself fails (2081-2084). It still only warns about dirty paths, so "only warn" holds.
- `--force` is a new flag. `grep -n -- '--force' run.bash` finds nothing, so Task 4.1 adds it.

## Table

| #   | Claim                                                                                                             | Verdict           | Evidence                                                                                                                                                                                  |
| --- | ----------------------------------------------------------------------------------------------------------------- | ----------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `assert_clean_worktree` is at line 1331                                                                           | VERIFIED          | run.bash:1331                                                                                                                                                                             |
| 2   | It is called from `fd_checkout` at 823                                                                            | VERIFIED          | run.bash:823. It is the only caller (grep).                                                                                                                                               |
| 3   | A clean tree is required only before an update or a pin                                                           | VERIFIED          | The call at 823 sits under `elif [[ -n "$ref" \|\| $# -gt 0 ]]` (822).                                                                                                                    |
| 4   | The play runs (`--changed`, `--rerun`) only warn on uncommitted changes, via `play_batch_warn_dirty` at 2079-2090 | VERIFIED          | Defined at 2079-2088. It calls `warning` when dirty (2085-2087). Called at 2160 (`--changed`) and 2282 (`--rerun`).                                                                       |
| 5   | Nothing checks whether the checkout is behind its upstream                                                        | VERIFIED          | `grep -n "behind\|@{u\|rev-list\|left-right" run.bash` finds no such check. The only fetch is at 835, under a `RUN_BASH_GIT_REF`. The `pull` calls update the tree and do not check it.   |
| 6   | run.bash is on the bash-limits ratchet                                                                            | VERIFIED          | scripts/bash-limits.allowlist:65, `run.bash 2771 66 2714 assoc,data`                                                                                                                      |
| 7   | A detached pin (`RUN_BASH_GIT_REF`) has no upstream                                                               | VERIFIED          | A 40-hex ref goes through `checkout --detach` (837-841). A branch-name ref uses `checkout -B ref origin/ref` (846), which does have an upstream, so the claim holds only for the sha pin. |
| 8   | `scripts/test-run-bash-*` exist                                                                                   | VERIFIED          | `git ls-files` lists 13 files, e.g. scripts/test-run-bash-git-ref.bash and scripts/test-run-bash-rerun.bash.                                                                              |
| 9   | PLAN.md Task 4.1 says run.bash may not grow (ratchet)                                                             | VERIFIED          | PLAN.md:89-91, plus claim 6.                                                                                                                                                              |
| 10  | Owner quote, <team> Q18, 26 Nov 08:58Z                                                                            | UNVERIFIABLE-HERE | It is a message from the owner. Only the owner's channel log can settle it.                                                                                                               |
