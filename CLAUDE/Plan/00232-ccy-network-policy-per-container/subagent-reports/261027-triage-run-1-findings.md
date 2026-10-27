# Plan 00232: host triage run 1, read

Worker report for the coordinator. The facts are F17 to F30 in [PLAN.md](../PLAN.md); the
answers per question are DESIGN.md section 9. This file keeps only what explains the fixes.

## Why P9 printed `FAIL nft parses ...`

The harness. `nft --check` still opens a netlink cache, which an unprivileged user may not do
in the host's own namespace: `cache initialization failed: Operation not permitted`. The test
now runs the check in `unshare --user --map-root-user --net`, an empty namespace of its own.
The test also never fed nft overlapping input, so it would have passed the broken ruleset.

## Why no enforcement probe measured a policy

The ruleset itself. Every apply failed with `conflicting intervals specified`: the host's own
global addresses (added to the block set) lie inside the private ranges, and the allow set held
the bus address twice (from `agentbus0` and from a seat bundle). `nft -f` is atomic, so no
table existed, and `probe` reported the failure and carried on. A3 to A9 all ran unfiltered.

Fixes: `auto-merge` on every set; a unit case with the host's shape; a failed apply now stops
the run (`must`).

## Probes that could not prove anything as written

- A4: the lift attempts ran `nft` and `ip` from the image, which has neither (rc 127). Now the
  host's tools run in the container's namespace under `setpriv` with exactly the container's
  bounding set, beside a control that adds `net_admin`.
- host-lan: the outbound interface's address is copied into the rootless namespace by pasta,
  so it is refused even with no policy. The triage and the acceptance gate now use
  `host.containers.internal` and an address on another host interface.
- A5 now also checks that exit status 75 reaches `podman start -ai`'s caller.

## Not verifiable in the container

nft is not installed in the ccy container, so the nft parse of the fixed ruleset is proved
only by the host rerun (P9, queued in `meta-deploy.list`).
