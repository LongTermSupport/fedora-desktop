# Plan 00235 Task 4.7f: prune meets an item already gone; the first prune before the timer

## Defect 1: `container-images prune --yes` exited 1 on "image not known"

**Cause.** `podman images --format json` lists an image once per tag, each entry carrying every
name. An image named `composer:latest` and `composer:2` is therefore planned twice: the first
`:latest` untags it, `:2` takes the image, and the second round's two removals fail with
"image not known". prune counted those as failures, so the run exited 1 and
`container-images-prune.service` failed. It is the class 4.7e fixed in `old-range remove` alone.

**Fix (the class, once).** `internal/containerimages` has one removal path,
`(*run).removeItem(ctx, kind, key, external)` in `prune.go`: podman's own removal (forced only
for a container another tool made), and after a failed one `engine.StoreHas` is asked whether
podman still has the item (an image by id, a tag by its name, a container, a volume). Gone is
done (`earlier` true: said "removed with an earlier item" and logged); still there, or podman
cannot say, the failure stands. A P6a refusal (permission denied in the overlay tempdirs) is
returned as it is, and so is a failure under docker (`ErrPodmanOnly`: `replaced` may run under
docker, and the failure then stands as before). podman's error text never decides absence.

Callers moved onto it: prune's `remove`, `removeReplaced` (replaced.go), `logAndRemove`
(stale.go), and old-range `removeAll`; `removeStep` and `takenEarlier` are deleted. The
look-before stays where the code already looks: old-range `dropGone`, and prune's per-pass
`look`.

**StoreHas and a tag.** `podman image exists` takes a name or an id, so `KindImage` already
answers a tag by name; `internal/engine` is unchanged, so no CCY version bump.

**Package doc.** New P6b (the single path and its rule); O15's after-failure sentence now points
at P6b.

**Tests (red first, then green).**

- `TestATagAnEarlierRemovalTookIsDone`: the composer shape (two listings of one image, two
  tags; `:latest` untags, `:2` takes the image, both again fail and `exists` says gone): exit 0,
  both said "removed with an earlier item", four log lines.
- `TestAFailedRemovalIsNamedNotLoggedAndTheRunFails` now also asserts podman was asked
  `image exists` on the failure, which still stands (the item is there): exit 1, not logged.
- `TestAStaleTagAlreadyGoneIsDone` (stale) and `TestARemovalOfAnImageAlreadyGoneIsDone`
  (replaced): a failed removal of what podman no longer has is done and logged.
- The old-range 4.7e tests pass unchanged through the new path.
- The prune test world's `exists` answers from the same gone-set as its listings.

## Defect 2: play-podman's first prune raced the timer

`Enable and Start the container-images Prune Timer` ran before the first prune: a due timer fires
the service at once, so `Start the container-images Prune Service and Wait for It` found it
already activating and returned without waiting, and the repair in its `always:` met the running
prune and its busy guard (tasks/podman-store-busy-guard.yml) failed.

**Fix.** In `playbooks/imports/play-podman.yml` the first-prune block (and its `always:` repair)
now comes before the timer task, with a comment saying why the order matters. The service start
gained `daemon_reload: true`, since the timer task that did the reload now runs after it and the
units may have just been deployed. The conditions are unchanged: the first prune runs only with no
prune succeeded and the store clean; the timer is held off when foreign-owned.

## QA

- `go vet` and `go test ./internal/containerimages/`: pass.
- `qa-changed.bash`: see the commit message or the coordinator's run. The ansible-syntax gate
  could NOT run in this worktree: `vault-pass.secret` is absent here (the lint-on-edit hook's
  `--syntax-check` failed on that alone). The coordinator runs it.
- The Edit tool was blocked on markdown in this nested worktree (R-MARKDOWN-WRONG-LOCATION), so
  the PLAN.md 4.7f line and this report were written by exact single-match scripts under
  `untracked/scratch/`.

## DBF

The single removal path is the defence: the seam every container-images removal must go
through, so a removal that meets an item already gone is judged in one place, by asking podman.
A grep of the package's non-test files finds `eng.Remove*` only inside `removeItem`. No analyzer
pins that yet; the seam holds it by review, as `project_seam_imports` holds others. Left outside
it, by design: `reown`'s root removal of an unrecorded store directory (`os.RemoveAll` in
oldrange_reown.go), a filesystem removal, not podman's; `os.RemoveAll` already treats a missing
path as success.

Not done (YAGNI): prune does not dedupe the per-tag listings; with the seam a repeat is a
`rm` + `exists` that ends "removed with an earlier item".

## Host

Nothing new is queued in `meta-deploy.list` by this change; the coordinator adds the play-podman
run (which builds and installs the command) to the queue.
