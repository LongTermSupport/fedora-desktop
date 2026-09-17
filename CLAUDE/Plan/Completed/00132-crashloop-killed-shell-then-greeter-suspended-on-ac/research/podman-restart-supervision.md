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

`dbus-broker` accounts resources per UID, and the failure message names that accounting
directly:

```
dbus-broker[…]: UID <uid> exceeded its 'bytes' quota on UID <uid>.
dbus-broker[…]: Peer :1.63 is being disconnected as it does not have the resources
                to receive a signal it subscribed to.
```

The victims are **receivers**, not senders. A peer is disconnected because its own receive
allocation could not hold a signal it had subscribed to — so the exhausted resource is a
per-peer receive allowance carved out of the user's budget, not a global ceiling the whole
bus shares.

### The quota is not configurable, and raising it is not available

The launcher does pass `--max-bytes`; the earlier reading that it did not was taken from
the launcher's argv rather than the broker's. The broker's own argv on this host:

| Bus                       | `--max-bytes`                     |
| ------------------------- | --------------------------------- |
| Session (`--scope user`)  | `100000000000000` (1e14, ~100 TB) |
| System (`--scope system`) | `536870912` (512 MiB)             |

Two conclusions follow, and both are load-bearing.

**First, `--max-bytes` is not derived from the XML limits.** `session.conf` declares
`max_incoming_bytes` and `max_outgoing_bytes` as `1000000000` (1e9). The session broker
was launched with 1e14 and the system broker with 512 MiB. Neither equals the configured
1e9, and the two scopes differ from each other while their config files declare the same
byte limits — `/usr/share/defaults/at-spi2/accessibility.conf` also declares 1e9 and its
broker likewise got 1e14. The values are scope-dependent constants, not a reading of the
XML. **Task 2.3 answered: no.** Editing `session.conf` limits would be a textbook inert
fix — a file changed, an assertion passed, and no behaviour altered. This is the same trap
the greeter fix was suspected of and cleared of; here it is real.

**Second, the session quota is already effectively infinite.** At 1e14 bytes, no restart
storm exhausts the global ceiling. So "raise the quota" is not a mitigation that exists:
there is nothing to raise. What was exhausted is the per-peer share the broker derives
internally, which no configuration file or command-line flag on this host exposes.

The precise divisor the broker uses to derive a peer's share is **not** established here,
and deliberately so — it does not change the conclusion. Whatever the formula, the value
is not reachable from configuration, so no defence can be built on adjusting it.

The structural point therefore stands and hardens: a container restart storm and the
user's desktop draw from one shared, quota-limited resource; the storm exhausts it first;
and **the resource cannot be enlarged**. Rate control at the source and detection are the
only levers.

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

| Mitigation                                                    | Where it lives       | Status                                                                                                               |
| ------------------------------------------------------------- | -------------------- | -------------------------------------------------------------------------------------------------------------------- |
| `on-failure:N` instead of `unless-stopped`                    | The external project | Verified as the only capped policy, but outside this repo                                                            |
| Quadlet unit so systemd's `StartLimitBurst` genuinely applies | The external project | This is podman's own recommendation                                                                                  |
| **Restart-rate detection**                                    | This repo            | **The defence this repo can build.** Specified in [detection-gap.md](detection-gap.md)                               |
| ~~Quota headroom monitoring~~                                 | —                    | **Rejected.** The quota is 1e14 and the breach is a per-peer share; there is no headroom figure to threshold against |
| ~~Raising the per-UID D-Bus quota~~                           | —                    | **Rejected.** Not derived from config, already effectively infinite — nothing to raise                               |
