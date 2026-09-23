# Plan 00134 Phase 3 — triage probes for Tasks 3.1 and 3.2 (opus-5, 2026-09-23)

Scope: write the probes, not run them. Both tasks are "diagnose before changing", and the
evidence is host-only. Everything below is in `triage.bash`, and the raw output goes only
to that run's directory under `untracked/plan-runs/`.

## Task 3.1 — the SELinux denial flood (F7)

What the code says, read before writing the probes:

- `lib/common.bash` `ccy_selinux_mode` sets `CCY_MOUNT_RELABEL=z` unless the verdict is
  `off`. The launcher binds the workspace as `-v "$PWD:/workspace:z"`.
- Extra project mounts and display sockets are never relabelled, as the launcher's
  banner says.
- A session with a forwarded SSH agent runs `--security-opt label=disable`, so it cannot
  produce `container_t` denials.

The probes, in order:

1. `getenforce`.
2. Per engine (podman, and docker when installed), per running container:
   - the process label, mount label and `HostConfig.SecurityOpt`;
   - `HostConfig.Binds`, whose `:z`/`:Z` suffix is the relabel;
   - the mounts, and `ls -dZ` on every bind source.
3. A map from MCS categories to engine and name, taken from each process label, plus the
   de-duplicated list of bind sources.
4. One `sudo -n ausearch -m avc -ts boot` capture into the run directory. A missing
   `ausearch` and an inactive `auditd` are recorded as facts.
5. AVC counts by source type, MCS, target type, class and comm (top 40).
6. AVC counts by the running container that owns each MCS pair. A pair no running
   container carries is printed as such.
7. The 20 most-denied `(dev, ino, name, target type)` tuples.
8. Those inodes resolved to paths, with one `find -xdev` pass per bind source. Each
   gets its current label, its `matchpathcon` policy default and its parent's label.

How to read it: a `:z`-relabelled source that holds `user_home_t` children was either
written after the relabel by something that sets its own label (a file created elsewhere
and moved in keeps its original label), or restored by `restorecon`. The label pair
("now" against "parent") separates the two. A denying MCS that no running container
carries means the container has gone, so re-run with the sessions up.

Limits:

- The audit capture is this boot only. The pre-reboot two-day flood is covered by the
  existing journal probes, which count but do not attribute.
- Inode resolution searches the running containers' bind sources only.

## Task 3.2 — Docker's nftables backend against `DOCKER-USER`

Finding from reading the IaC: Plan 00127 contains no firewall assumption at all. The
dependency is in `play-lxc-install-config.yml`'s header ("inserts rules into Docker's
DOCKER-USER chain") and `lxc-docker-user-iptables-reconcile.bash`, which fails when
`DOCKER-USER` is missing and otherwise inserts two ACCEPTs and a MASQUERADE.

The probes record:

1. The Docker server version, `docker info`'s firewall and backend lines, and
   `/etc/docker/daemon.json`.
2. From iptables:
   - `iptables --version`, which says nf_tables or legacy;
   - `iptables -S FORWARD`, which must show a jump to `DOCKER-USER` for the chain to be
     consulted at all;
   - `iptables -L DOCKER-USER -n -v -x`, whose counters accumulate from the chain's
     creation, so zero means nothing forwarded has passed through it;
   - nat `POSTROUTING`.
3. From nftables:
   - `nft list tables`;
   - every base chain on the forward hook, by table, since each is consulted in
     priority order;
   - every rule naming `DOCKER-USER`;
   - the docker-owned tables in full.
4. The `lxcbr0` address and `systemctl status` of the reconcile unit.
5. firewalld this boot: the `COMMAND_FAILED` count, the first 20 such lines, and the
   `NAME_CONFLICT` lines.

## Verification here

- `shellcheck -x` is clean.
- The three AVC aggregations and the MCS label split were run against a synthetic
  fixture, including an empty container map. That case exposed a real `FNR == NR`
  trap: an empty map file would have swallowed the AVC file. The map is now read in
  `BEGIN`.
- The script itself refuses to run in the container (`plan_require_host`), so no probe
  has been run against a real host.
