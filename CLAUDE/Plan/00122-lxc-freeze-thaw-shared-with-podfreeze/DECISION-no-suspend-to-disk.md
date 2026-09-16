# Decision: `lxcfreeze` gets no suspend-to-disk verb

Plan 00122, Phase 6. **Owner's decision, taken from the spike.** Task state stays in
[PLAN.md](PLAN.md); the reasoning and the evidence are here.

## The decision

No suspend-to-disk verb, and no stop/start verb either. `lxcfreeze` keeps the two things
it already does — freeze and thaw, the cgroup freezer — and anything that must survive a
reboot uses `lxc-stop` and `lxc-start` directly.

## Why CRIU is not the answer

CRIU cannot dump the nested UTS namespace `systemd-logind` creates, and it refuses before
writing an image. The fix for that one blocker is a systemd drop-in
(`ProtectHostname=no`) in **every** container, which only buys the next blocker: behind
it sit mount propagation, cgroup v2 ownership and TCP connection state, each a
per-container concession. The spike stopped at the first refusal for that reason rather
than working down the list.

## Proxmox reaches the same conclusion by avoiding it

Worth stating, because it is the closest comparable and it is not a sample of one:

| Proxmox feature     | How it is actually implemented |
| ------------------- | ------------------------------ |
| container "suspend" | the cgroup freezer             |
| hibernate to disk   | VMs only                       |
| container migration | stop, copy, start              |
| container backup    | filesystem snapshot            |

A mature product with every incentive to offer container hibernation does not, and
implements each adjacent feature the plainer way.

## What to use instead

- **A short RAM-resident hold** — `lxcfreeze` freeze/thaw, which is what it is for.
- **Anything that must survive a reboot** — graceful `lxc-stop` and `lxc-start`.
- **A rollback point** — `lxc-snapshot` on the btrfs rootfs.
- **Running state kept across reboots** — that workload wants a VM, not a container.

## Why `lxcfreeze` does not wrap stop/start

Task 6.2. `lxc-stop` and `lxc-start` are already the interface, and wrapping them would
put a **shutdown** behind a tool whose name says it suspends. The verb the user typed and
the thing that happens to their workload would stop matching, which is worse than one
more command to remember.

## Evidence

The 26-09-16 journal, entries 09:55, 10:00, 10:02 and 10:10 — the spike run, CRIU's
refusal, the reasoning, and the decision as taken.
