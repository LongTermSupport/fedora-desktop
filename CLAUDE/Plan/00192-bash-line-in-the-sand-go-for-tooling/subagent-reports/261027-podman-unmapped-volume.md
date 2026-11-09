# reclaim 2.0.2: podman storage with a volume no namespace maps (branch `fix/00192-podman-storage-unmapped-volume`)

## Cause (host run of 2.0.1)

The host has one rootless volume (`<a rootless volume>`) whose `_data` is owned by a uid
outside the user's current subordinate range. `podman system df` therefore fails as a whole,
and so does the `podman unshare` retry. Nothing can read that volume, and reclaim leaves it
alone.

## Fix (SPEC E7a, E22a; `src-go/internal/reclaim/podman.go`)

- Report: if the second read is refused `permission denied` too, the Podman section is shown
  in part and labelled `(partial: ... no totals)`. It names the volume taken from podman's
  `open .../volumes/<name>/_data` error and says why it could not be read. The reason is
  found by checking, not guessed: the volume's owner (lstat) is compared with the account's uid
  and the account's `/etc/subuid` lines. The cases are outside the current range ("probably
  left from an older setup; podman cannot read or size it"), inside the range, the account
  itself, no range, or a lookup that could not be read. The section also gives commands to look
  at the volume without changing it (`podman volume inspect`, `sudo ls -lan`, the subuid
  `grep`). Then come `podman images`, `podman ps --all --size` and `podman volume ls`, none of
  which opens a volume. A listing that fails is named on stderr and marked as unavailable. The
  report still exits 1.
- Option 6 (and the sweep's container step) shows the same information.
  `podman system prune -af` is **not** offered. It removes stopped containers, podman could not
  see all of its storage, and a stopped container may be what still holds the volume. Only
  `podman image prune -af` is offered. It removes only images no container uses, from the list
  just shown, and it neither reads nor removes a volume. If `podman images` failed too, nothing
  is offered.
- New `Env` seams: `Owner`, `SubUIDs`, `Account`, faked in `world_test.go`.

## Proof

Test first: `podman_test.go`. Its five tests failed before the wiring and pass after.
`internal/reclaim` coverage is 93.6% and golangci-lint reports 0 issues. The version is 2.0.2.

## Host side

The `changed-plays` comment in `meta-deploy.list` now describes 2.0.2. After
`play-cli-tools.yml`, the host agent reruns H1's podman section, expecting the partial section
and the unreadable volume named. It then runs option 6 and answers No, expecting only the image
prune to be offered and nothing to run.
