# Task 3.10: the follow-ups of the two vmtest reviews

The FOLLOW-UP findings of `261028-fable-review-vmtest-retention.md` (F2-F7) and
`261028-fable-review-sweep.md` (items 2-6), in `src-go/internal/vmtest`. F1 and the sweep
review's FIX-BEFORE-MERGE were already fixed on this branch (`maxPruneDays`, and
`sweepBuild` asking libvirt). Tests were written first and seen red (journal entries of
2026-10-28, "Task 3.10 review follow-ups: tests red" and "...: green"). Coverage 91.0%;
`golangci-lint run ./internal/vmtest/` reports nothing.

## Fixed

- **F2, a partial overlay removal.** `prune-runs --yes` now logs
  `pruned overlay <run-id> <files>: <reason>` before removing, and a removal that fails stops
  with `cannot remove <path>: <err>; removed so far: <paths>|nothing` and appends
  `failed to remove <path>: <err>` to `retention.log`. One helper, `removeDisks`, does the
  removal for both `prune-runs` and the sweep. Test: `TestE62PruneRunsRemovalFails`.
- **F3, symlinks.** The behaviour was already right; `TestE51SymlinksAreLeft` now pins it: a
  symlink named as the oldest passing run, pointing outside the lab, and
  `bases/server-fast-44.build -> bases/server-fast-44`, survive `sweep` and `prune-runs --yes`.
  It was seen red by a mutation that made `runDirs` take a symlink as a run (prune-runs then
  removed the target's overlay). E51 says a symlink is never followed.
- **F5, the dead branch in `runPassed`.** One decode into a map; a body that does not decode is
  no pass. Written as one boolean expression because `nilerr` forbids
  `if err != nil { return false, nil }`. Non-UTF-8 and empty bodies were added to
  `TestE61RunPassed` (both no pass, before and after: a refactor).
- **F6, `MemTotal:` with no value.** Now `/proc/meminfo has a MemTotal line with no value`;
  `TestHostMiB` pins every message.
- **F7, `Bsize` for `Frsize`.** `freeBytes` calls a new `freeOf`, which multiplies `Bavail` by
  `Frsize` and refuses a zero fragment size; `TestFreeOfCountsFragments` (Bsize 4096,
  Frsize 1024) was red on the old arithmetic.
- **Sweep 2, "evicted" logged before a removal that fails.** The `failed to remove` line above
  (the shared `removeDisks`). The log-first order stays (E51). Test: `TestE51SweepRemovalFails`.
- **Sweep 3, the nightly `kept build desktop-` line.** A build the sweep keeps (a desktop one,
  or one libvirt lists) is no change, so it is now `==> kept build <name>: <why>` on stderr
  only, never in `retention.log`. Tests: `TestE51Sweep`, `TestE51SweepKeepsALiveBuild`.
- **Sweep 4, ranking proven only through mtime.** `TestE51Sweep` now makes the newest passing
  run's directory the oldest by mtime and an evicted one's the newest, and the same two are
  evicted; `age`'s comment no longer says the sweep orders by mtime. There was no mtime code
  left to see this go red against.
- **Sweep 6, nothing removes a run directory.** E51 and `docs/vm-acceptance-testing.md` now say
  so: the sweep and `prune-runs` take only `*.qcow2`, and a run directory stays until the owner
  removes it.

## Declined

- **F4, the guard measures `<home>`.** Kept as is, and E64 now says why: the lab is one
  filesystem, `runs/`, `members/` and `bases/` are its directories, and a mount of their own
  is not supported. Measuring `<home>` also works before `runs/` exists on a first run, which a
  statfs of `runs/` would not.
- **Sweep 5, an empty directory named `*.qcow2` is removed.** Not changed. An empty
  directory holds nothing, so nothing is lost; a non-empty one already fails the removal and
  stops (fail-fast, tested). Making `overlays()` refuse a directory would make the
  `prune-runs` dry run and the disk guard's "would free" figure fail over a harmless entry, and
  skipping it instead would be skip-and-continue. Both are worse than the hygiene gain.

## Also seen, not changed

`listBases` globs `bases/*/base.qcow2`, which follows a symlinked `bases/x.build` and prints
`VMTEST-PRUNE-BASE x.build ...`. It only prints, and never removes.

## Spec, docs, deploy

SPEC-vmtest E51 (kept builds on stderr, the `failed to remove` line, no run directory removed,
symlinks), E62 (the log line names the files; what a failed removal says) and E64 (one
filesystem, fragments). Every reader of the changed lines was searched for: only the spec,
the docs and the tests read them. `meta-deploy.list`: its single `changed-plays` entry
expects `play-vm-test-lab.yml` to rebuild vmtest with these changes.
