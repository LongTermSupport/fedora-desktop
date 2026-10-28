## 12:25 · action · —   — container-images prune stops at a store with files outside the subuid range (P6a), red then green

P6a, tests first. The first `prune --yes` on the host met a store whose removed layers held
files owned by host uids of an old subuid range; c/storage could not unlink them in
overlay/tempdirs/temp-dir-N, and every later removal and store load failed the same way, so the
run kept going through every target.

RED: `TestAStoreHoldingFilesOutsideTheSubuidRangeStopsTheRunAtOnce` (both error texts the host
saw: the unlinkat during a removal, and "loading primary layer store data: error removing stale
temp dir") failed: removals e, a, f where e, a was wanted. `TestAStoreThatCannotLoadForThatCauseNamesTheRepair`
failed: the stderr had podman's error and no cause.

GREEN: `deniedInTempdirs` (permission denied together with /overlay/tempdirs/temp-dir- or "stale
temp dir") stops the pass after that removal, reports once that the store holds files owned
outside the subuid range and that play-podman.yml repairs it as root, and exits 1; a listing that
fails for that cause names it too. Other failed removals keep P6's behaviour (the existing test).
go test -race passes; the package's coverage is 94.6%; golangci-lint 0 issues.
