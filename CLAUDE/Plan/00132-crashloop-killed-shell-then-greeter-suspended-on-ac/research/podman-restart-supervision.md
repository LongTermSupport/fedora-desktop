# Podman restart supervision

Why a container that can never start successfully was allowed to retry roughly
1–2 times per second for over ten hours. Verified against podman 5.8.4 and the live unit
state on this host.

## Three properties combine

### 1. `unless-stopped` has no retry cap

From `podman-run(1)` on this host:

| Policy                     | Retry behaviour                                                            |
| -------------------------- | -------------------------------------------------------------------------- |
| `no` / `never`             | Do not restart                                                             |
| `on-failure[:max_retries]` | "retrying indefinitely **or until the optional max_retries count is hit**" |
| `always`                   | "retrying **indefinitely**"                                                |
| `unless-stopped`           | Same, indefinite                                                           |

`on-failure` is the **only** policy that accepts a cap. The compose file used
`restart: unless-stopped` — the one setting that is both unbounded and has no syntax for
expressing a bound.

### 2. There is no backoff, at all

Searching the whole of `podman-run(1)` for `backoff`, `restart-sec` and `restart-delay`
returns **zero matches**. There is no knob to configure and no implicit delay applied.
This is what permits ~2.4 restarts per second rather than a rate that decays as failures
accumulate, and it is a real divergence from Docker, which inserts a growing delay
between attempts precisely so that a container which cannot start does not saturate the
host.

The practical effect is that podman treats "failed instantly" and "ran for an hour then
died" identically, so a container in a permanent failure state is retried at the maximum
rate the machine can sustain.

### 3. systemd's start-rate limiter does not apply

This is the property that turned a noisy loop into a desktop outage.

Podman registers each container with the systemd user manager as a **scope**:

```
Started libpod-<id>.scope - libcrun container
```

From `systemd.scope(5)`: *"Unlike service units, scope units manage externally created
processes."* systemd does not start a scope — it is told about a process something else
created, and it tracks the cgroup. Consequently `Restart=`,
`StartLimitBurst=` and `StartLimitIntervalSec=` — the entire mechanism systemd provides
for exactly this failure mode — **are not in the path**. Podman performs the restart
itself, out of band, and registers the result afterwards.

`systemctl --user list-units 'podman*'` confirms nothing systemd-side was supervising the
stack: only `podman.service`, `podman.socket` and a pause scope are present. No compose
service unit exists.

Podman's own documentation anticipates this. `podman-run(1)` states:

> When running containers in systemd services, use the restart functionality provided by
> systemd. In other words, do not use this option in a container unit, instead set the
> `Restart=` systemd directive.

The advice is correct. `podman-compose` does not follow it — it maps compose's `restart:`
key onto podman's own policy, which keeps the container on the no-backoff, no-limit,
no-rate-limiter path.

## Why the host was not isolated from it

Rootless podman drives the **session** D-Bus and the systemd **user** manager. Those are
the same instances the desktop uses. There is no separate bus, no separate quota, and no
priority distinction between a container churning and a compositor delivering input
events.

`dbus-broker` accounts resources per UID. The broker exposes
`--max-bytes BYTES`, documented as "Maximum number of bytes **each user** may allocate in
the broker" — a per-user quota, which is exactly what the failure message names. On this
host the launcher runs as `dbus-broker-launch --scope user` with no `--max-bytes`
passed, so whatever quota applied was a default or was derived from configuration; which
of those is true is unresolved and recorded in [detection-gap.md](detection-gap.md).

Either way the structural point stands: a container restart storm and the user's desktop
draw from one shared, quota-limited resource, and the storm exhausts it first.

## Measured rate

| Window                         | Container starts |
| ------------------------------ | ---------------- |
| Each hour, 20:00–05:00         | 8,517 – 8,890    |
| 5-minute sample during triage  | 326 (~65/min)    |
| 60-second sample during triage | 66               |

Roughly 85,000 restart cycles across the night, and **still running** during
investigation.

## What this does and does not imply for this repository

The crash-looping container belongs to an unrelated project and its compose file is not
ours to own. What *is* in scope here is that the host had no defence of any kind: no rate
limit that applied, no quota headroom alarm, and no detection. A second such loop, from
any project, would produce the same outcome.

The candidate mitigations are recorded rather than applied, per this plan's Non-Goals:

| Mitigation                                                    | Where it lives                                  | Note                                                      |
| ------------------------------------------------------------- | ----------------------------------------------- | --------------------------------------------------------- |
| `on-failure:N` instead of `unless-stopped`                    | The external project                            | Verified as the only capped policy, but outside this repo |
| Quadlet unit so systemd's `StartLimitBurst` genuinely applies | The external project                            | This is podman's own recommendation                       |
| Quota headroom monitoring                                     | This repo, via the existing host-health surface | Requires the quota's real value first — Task 2.4          |
| Raising the per-UID D-Bus quota                               | This repo                                       | **Unverified path** — see Task 2.3 before specifying      |
