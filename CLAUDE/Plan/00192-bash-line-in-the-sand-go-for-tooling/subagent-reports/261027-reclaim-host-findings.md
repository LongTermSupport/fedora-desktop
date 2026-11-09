# reclaim host findings (branch `fix/00192-reclaim-host-findings`)

## What was recorded

- PLAN.md's reclaim entry: merged into F44, host checks on 2.0.0 H1-H4 pass, H5 the owner's
  (baobab on the desktop, by screenshot). It also records two limits of that run. B12's "no sudo
  question" was not exercised, because the host user's sudo needs no password. H4's option 6
  podman half read a scratch home's empty storage.
- Journal: a `finding` entry with the host results (anonymised) and an `action` entry with the
  fix, red and green.

## Fixes (reclaim 2.0.1, test first)

- (a) `duComplaints` (`src-go/internal/reclaim/report.go`): du's
  `cannot read directory '<p>': Permission denied` and `cannot access '<p>': Permission denied`
  lines under one root are one line:
  `reclaim: du <root>: <n> entries could not be read (permission denied), the first '<p>'; the size is a lower bound.`
  Any other du stderr line is shown as it is. It is used by the report, the trash size and the
  menu's du listings (SPEC E6, B6).
- (b) `engine.unshared` and `retry` (`containers.go`), and `podmanStorage` (`report.go`): a
  `podman system df` refused `permission denied` is named on stderr and read again as
  `podman unshare podman system df`. Inside podman's user namespace the container uids are
  mapped. Both failing gives `(podman storage unavailable)`, with both reasons on stderr. Option
  6 retries the same way before it offers the prune (SPEC E7, E22).
- Rejected: per-type figures from `podman images` and `podman volume ls`. Summing image sizes
  counts shared layers twice, and the volume list has no sizes.

## Unproven

The unshare retry has not been run on a host. If the volume's owner uid is outside the user's
subuid mapping, the retry fails too, and the report says so with both reasons. That is the
2.0.1 host check.

## Host side

`meta-deploy.list`: the `changed-plays` description now names `play-cli-tools.yml`, which
rebuilds reclaim 2.0.1. Then, by hand, the host agent repeats H1 (`reclaim --version` shows
2.0.1, one du summary line per directory, Podman storage figures or both reasons) and the podman
half of option 6 with the real HOME, answering No. H5 is still the owner's.

`./scripts/qa-changed.bash` passed. Not pushed.
