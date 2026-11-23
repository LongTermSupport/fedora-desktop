# Stage 1a caps: what was built and what was found

Tasks A.4 to A.6. The caps reach the host with the v2 batch (Plan 00209).

## The limit set

Every ccy and ccc container starts with `--cgroup-parent agents.slice --pids-limit 16384 --memory <RAM less 16 GiB> --memory-reservation <2 GiB below that>`. On a 64 GiB host (MemTotal 67108864 kB) that is `--memory 49152m --memory-reservation 47104m`.

The user unit `~/.config/systemd/user/agents.slice` is `[Slice] MemoryMax=49152M MemoryHigh=48128M`, with no ManagedOOM setting. A launch is refused until the slice is installed.

## systemd-oomd

- **Where the kill came from.** Fedora's `systemd-oomd-defaults` sets `ManagedOOMMemoryPressure=kill`, with `ManagedOOMMemoryPressureLimit=50%`, on the system unit `user@.service` (20 s).
- **Why the earlier fix did nothing.** It wrote `ManagedOOMMemoryPressure=auto` into `~/.config/systemd/user/user.slice.d/`. The user manager has no such unit, so the setting never applied, and its read-back passed without checking anything.
- **Where the containers land.** A rootless container started with `--cgroup-parent agents.slice` lands at `/user.slice/user-UID.slice/user@UID.service/agents.slice/libpod-<id>.scope`. That is inside the cgroup oomd watches. PSI rolls up, so pressure under the slice's MemoryHigh is pressure on `user@UID.service`.
- **How oomd chooses a victim.** It takes the leaf cgroups under the watched one (or those with `memory.oom.group=1`) and orders them by reclaim rate, then by memory use. The trigger is PSI "full" avg10. Any desktop app's scope can be the one killed.
- **Why per-app preferences were rejected.** `ManagedOOMPreference=avoid/omit` is honoured under `user@` only on the candidate's own cgroup. It is not inherited, so setting it on `app.slice` or `session.slice` protects none of the `app-*.scope` leaves.
- **The choice.** A system drop-in, `user@.service.d/50-no-memory-pressure-kill.conf`, sets `ManagedOOMMemoryPressure=auto`. oomd then stops pressure-killing in the session, and `agents.slice`'s MemoryMax makes the kernel kill inside the slice first. The swap-based kill on `-.slice` and the kernel OOM killer stay.

Sources: systemd's `src/oom/oomd-manager.c` and `oomd-util.c`; systemd.resource-control(5); systemd-oomd.service(8); Fedora's EnableSystemdOomd change.

## `--memory-reservation` is a protection, not a throttle

On cgroup v2, crun writes `--memory-reservation` to `memory.low`. That protects memory from reclaim; it does not throttle. Inside `agents.slice`, which has no MemoryLow, it protects nothing.

It is kept as asked, and the docs now say `memory.low` (they said `memory.high`). A real per-container throttle would be `--cgroup-conf memory.high=...`.

## LXC members have no caps

No LXC member has a memory or pids limit. Nothing in the plays or the teamchat LXC code sets `lxc.cgroup2.memory.max` or `lxc.cgroup2.pids.max`; `limits.memory` and `limits.processes` are LXD/Incus keys, and this repository uses classic LXC.

The members are rootful, so they sit under the system's `lxc.payload.<name>`, not under the user's `agents.slice`. The matching caps would be:

- `lxc.cgroup2.memory.max` = RAM less 16 GiB;
- `lxc.cgroup2.memory.high` 1 GiB below that;
- `lxc.cgroup2.pids.max` = 16384.

Capping them together needs a system slice. This is Task A.9.

## The host proof

Task A.7b, a read-only plan script, runs after the cut-over relaunch and checks four things:

- `user@UID.service` reports `ManagedOOMMemoryPressure=auto`;
- `oomctl dump` lists no pressure watch on it (through `sudo -n`; a refusal means it could not establish this);
- `agents.slice` is loaded with its caps;
- every running ccy and ccc container's cgroup is under `user@UID.service/agents.slice/`, with `memory.max` and `pids.max` set.
