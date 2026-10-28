# ccy self-prune: review fixes F1-F3 and follow-ups

Worktree `.claude/worktrees/vmtest-prune`, branch `plan/00235-ccy-self-prune`, from `20216b25f`.
Review: `untracked/scratch/261028-fable-review-ccy-self-prune.md` (untracked). Red and green
are journalled (`JOURNAL/2026-10-28/120453Z-action-7fd317.md`, `120933Z-action-2d2bc0.md`).

## F1: a failed self-prune after a successful build no longer fails the launch

`files/var/local/claude-yolo/lib/image-build-prune.bash`: after the build succeeds, `replaced`
runs as `... || printf 'ccy: the images the build of %s replaced were not removed (container-images exit %s); the daily prune removes them.\n' ... >&2`, annotated
`# FAIL-FAST-OK` (the build succeeded; the daily prune is the backstop). The function then
returns 0, so the launcher call sites (`claude-yolo:2056`, `:3794`, `:1957`) carry on: launch,
restart and stamping. They are unchanged. A failed `before` still returns 1 with nothing built;
a failed build still removes nothing and returns its own status.

## F2: `replaced` keeps everything while another build of the repository holds its lock

- `replaced [--lock <file>] <repository> <ids-file>` (`src-go/internal/containerimages/replaced.go`,
  R6). Once it knows which ids would go, it calls `heldBuildLock(dir, except)`, the prune's
  probe from `prune.go`. That probe now takes the caller's own lock path to skip, and the
  prune passes `""`. The lock directory comes from `App.buildLocks()`, which the two share.
- If another lock is held, each id is reported on stderr as `kept ... another ccy image build is running (<name> is held); the daily prune (container-images-prune.timer) removes it once no build runs`. The exit is 0 and the engine is not asked anything more. Without `--lock`,
  every held lock counts as another build's. A lock it cannot probe stops the run before any
  removal (exit 1, which F1 makes non-fatal).
- How the key reaches it: `image-build-lock.bash:69` now passes its lock file,
  `ccy_image_build_pruned "$file" "${key%%:*}" "$@"`. That is the same statement count, so
  the lock stays at its allowlist record. The lib comment's false claim is corrected.
- Go tests: another lock held (nothing removed, nothing logged, the reason given); only the
  caller's own lock held (the removal goes ahead); no `--lock` with a held lock (nothing
  removed); a lock that is not a file (exit 1, no removal); usage of the new form.

## F3: the lock suite proves the launch's survival through the real path

`scripts/test-ccy-image-build-lock.bash`: the fake podman gains `$TEST_STATE/rm-refuses`, under
which `image rm` exits 2, and `$TEST_STATE/ps-fails`, under which `ps` exits 125. New cases:

- a refused removal: the launch exits 0 on the new image, and stderr reports the refusal and
  names the daily prune;
- `replaced` failing outright (`ps` fails after the build): the launch exits 0 on the new image
  with the warning (`container-images exit 1`), nothing is removed, and an in-place update also
  exits 0 with one commit;
- a held `claude-yolo_proj.lock` (a real flock): the rebuild succeeds, removes nothing and says why.

Red first: 6 of 112 failed (F1 and F2). The refused-removal case already passed, because that
behaviour was right but had no test through this path.

## Follow-ups

- `tasks/claude-yolo-image.yml` builds and installs `container-images` before copying the libs.
- `PLAN.md`: Task 3.1 and 3.2 wording updated, and the docker parent/child "could not remove"
  noise is an unticked FOLLOW-UP line.
- `docs/ccy-changelog.md` 3.102.0 now says a failed cleanup is reported and never fails a
  launch, and mentions the concurrent build and the docker in-place update.
  `docs/containerization.md` is updated to match.
- CCY 3.102.1: the pre-commit gate rejects a `lib/` change without a bump against HEAD, which
  is the case CLAUDE/ContainerRules.md covers (every commit). The 3.102.1 changelog entry
  points at 3.102.0 for the behaviour; `deploy.bash`, `meta-deploy.list` and PLAN.md name 3.102.1.

## Gate

- `golangci-lint run ./internal/containerimages/ ./cmd/container-images/`: 0 issues, and
  `golangci-lint fmt` changed nothing. `selfPruneArgs` went over cyclop's limit, so the
  `replaced` arguments are now parsed in `replacedArgs`.
- `go test -race` on both packages: green, `containerimages` at 95.7% coverage.
- `go test ./internal/ccylaunch/...`: green after re-recording the three goldens with
  `-update`. The only diff is the `replaced` line gaining
  `--lock '{sandbox}/run/ccy/image-builds/claude-yolo_latest.lock'`.
- `bash scripts/test-ccy-image-build-lock.bash`: 112 passed, 0 failed.
- `qa-bash-limits` and shellcheck on both libs: green.
- `qa-changed`: see the commit.
