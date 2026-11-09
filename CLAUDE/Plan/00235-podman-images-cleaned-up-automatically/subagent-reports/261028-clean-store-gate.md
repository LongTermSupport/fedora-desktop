# Task 4.9: the store-clean gate on ccy's self-prune and a manual prune

Branch `plan/00235-ccy-self-prune`.

## The merge of F44

F44 (the store repair, P6a, the ownership probe and timer gate: Tasks 4.5, 4.6, 4.8) is merged
in. `meta-deploy.list`, `deploy.bash` and the PLAN Status line kept both sides, in run order:
the repair, then the ownership probe (which may fail the play on purpose, Q6), then the first
prune, then `play-claude-yolo.yml` with CCY 3.102.1 and the self-prune. A play-podman.yml that
fails on purpose stops `deploy.bash` (plan_deploy_leg aborts at the first failed leg), so
play-claude-yolo.yml does not run in that deploy, and CCY 3.102.1 then arrives through
changed-plays on a later run. Both files say so.

## The gate

- Marker: `~/.local/state/fedora-desktop/container-images.store-clean`. Its name is
  `storeCleanName` in `src-go/internal/containerimages/prune.go`, beside the log, lock and
  stamp names. Its path is `container_images_store_clean` in `playbooks/imports/play-podman.yml`,
  built from `container_images_state_dir`. The success stamp now uses the same variable. Each
  comment points at the other.
- The play: after `tasks/podman-store-ownership-probe.yml` it makes the state directory (0750,
  as the Go's MkdirAll does; the play runs as the user, so the parents are the user's). With no
  foreign-owned file it writes the marker (the user's, 0644, holding the probe's UTC time).
  With one it removes the marker. Both run before the timer task and before the deliberate
  failure.
- `prune --yes` (P9): without the marker it removes nothing, prints
  `container-images: the podman store has not been confirmed free of old-range files (Plan 00235 Q6); nothing removed; run play-podman.yml`
  once on stderr and exits 1. The check comes after the run lock and before the log is opened,
  so no log is written and no success stamp. A dry run lists as before and notes the missing
  marker once on stderr.
- `replaced` (R7): once some recorded image would go, and before the build-lock probe, a
  missing marker gives the same line and exit 0.
- A marker that cannot be stat'ed (anything but not-exist) is exit 1 in both, before any
  removal.
- Usage text, docs/containerization.md and the 3.102.1 changelog entry say it in one sentence
  each. The CCY lib does not change, so there is no version bump.

## Tests

Five Go tests went red first and were journalled. They cover replaced and prune --yes without
the marker, the dry-run note, and an unreadable marker for each verb. Every test world and
shelf now starts with the marker written. Two existing tests that turned `~/.local` or
`~/.local/state` into a file were adjusted to keep testing what they meant.

`scripts/test-ccy-image-build-lock.bash` never sandboxed HOME, so its self-prune runs were
appending to the real `~/.local/state/fedora-desktop/container-images.log`. HOME is now
`$SCRATCH/home`, set after the Go builds. Each `reset_world` writes the marker, and a new case
removes it: the rebuild succeeds, removes nothing and says why.

Results: `go test -race` on containerimages (coverage 95.3%) and cmd/container-images,
`golangci-lint run` and `fmt` clean, `go test ./internal/ccylaunch/...` passing, the lock suite
at 116 passed and 0 failed.

## Not mine, left as found

The lint hook prints an info-level shellcheck SC2016 on the lock suite's `flock ... bash -c 'echo held >"$1" ...'` line. HEAD already has it, and the line is intended.
