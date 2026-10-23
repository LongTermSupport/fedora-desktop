# Plan fact check: Plan 00192 PLAN.md / PORTS.md diff (F44 tree)

Scope note: only the first 562 of 2093 diff lines were read. The rest is PORTS.md text moved from PLAN.md, which I did not itemise.

## REFUTED (most consequential first)

1. "`rclone-tail` (P6): ported and cut over on `go-port/rclone-tail`, under review, not merged ... its host run `deploy-rclone-tail.bash` (H1) waits for the merge".
   `git merge-base --is-ancestor go-port/rclone-tail HEAD` says merged. HEAD history has commit 1d8bc1037 "Merge Plan 00192 Task 3.3: rclone-tail ported to Go ...". `meta-deploy.list` line 41 already queues `deploy-rclone-tail.bash`. Change: rclone-tail is merged, and its H1 deploy is queued.
2. "`gh-account-setup` (P6): ported on its own branch, under review, not merged".
   No `go-port/gh-account-setup` branch exists, local or remote. `src-go/cmd/gh-account-setup` is in the current tree (commit 4a38295a4 "gh-account-setup ported to Go", WIP). The "own branch, not merged" status contradicts the tree. Change: restate where it landed and whether review finished.
3. "all 22 `qa/cmd` mains call `cmdmain.RefuseClosedStdout`" and "every one of the 22 `qa/cmd` mains uses `cmdmain`" (moved text, seen in the first part of the diff).
   `src-go/qa/cmd` has 28 directories, and 28 files under `qa/cmd` reference `cmdmain.RefuseClosedStdout`. Change: 22 should be 28.
4. "12 gate packages use `rootargs` (9 of them also `emit`)".
   Non-test importers of `qa/internal/rootargs` number 16 directories and of `qa/internal/emit` 12. My counting was rough, so treat this as probably stale. Recount.

## Verified

| Claim                                                                     | Evidence                                                                                                                                                              |
| ------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `qa-go.bash` retired; the Go gate is go-qa-ci via `scripts/qa-go-qa.bash` | `scripts/qa-go.bash` is absent; `scripts/qa-go-qa.bash` exists; `scripts/qa-gates.tsv` line 29 names it; no `qa-go.bash` row in `bash-limits.allowlist`               |
| PORTS.md, INVENTORY.md and every linked SPEC-\*.md exist                  | `ls` of the plan folder                                                                                                                                               |
| `open`: `go-port/open` and `go-port/open-2` merged; deployed `8c2de08c`   | Both are ancestors of HEAD. 8c2de08c is a commit and an ancestor of HEAD. bf673b3b4 records "8c2de08c deployed"                                                       |
| vmtest, ftp-camera, watermark/wsi merged                                  | `go-port/vmtest`, `ftp-camera`, `wsi-watermark` and `wsi-watermark-lows` are all ancestors of HEAD                                                                    |
| All listed `qa-*` ports merged into F44                                   | Every `go-port/qa-*` branch is an ancestor of HEAD, including `qa-cmdmain`                                                                                            |
| `rclone-cache-status` merged and has a deploy script                      | `scripts/deploy-rclone-cache-status.bash` and SPEC exist; the `go-port/rclone-cache-status` branch is not listed, but `src-go/cmd/rclone-cache-status` is in the tree |
| goldmark v1.8.6 in `src-go/qa/go.mod`                                     | go.mod line 7                                                                                                                                                         |
| `src-go/qa/internal` holds cmdmain, emit and rootargs                     | `ls`                                                                                                                                                                  |

## Unverifiable here

- "`rclone-cache-status` deployed and live-checked on the host (`deploy-rclone-cache-status.bash` passed)": host state.
- The `open` H1-H5 host checks being "not queued in `meta-deploy.list`": no `open` entry in `meta-deploy.list` (grep), consistent but not proof of intent.
- Review dispositions and the per-port counts (scenario counts, file counts): the per-port numbers were not re-derived.
