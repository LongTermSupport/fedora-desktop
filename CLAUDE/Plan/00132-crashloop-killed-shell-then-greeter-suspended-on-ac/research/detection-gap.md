# Detection gap

Ten hours of runaway restarts, a shared quota driven to exhaustion, and a desktop
destroyed — with no signal anywhere. The detection gap is arguably the more serious of
the two defects, because it is the one that let a small problem run long enough to become
a large one.

## What was observable, and was not observed

Every one of these was visible in real time and nothing was watching any of them:

| Signal                                                   | Value during the incident      | Would have fired how early? |
| -------------------------------------------------------- | ------------------------------ | --------------------------- |
| Container start rate                                     | ~8,800/hour, sustained         | Within minutes              |
| The same container image failing identically, repeatedly | Same traceback every cycle     | Within minutes              |
| A container health check failing without recovery        | Failure streak in the hundreds | Within the hour             |
| Per-UID D-Bus byte accounting approaching its quota      | Rose until breach              | Hours of warning            |
| Battery discharging overnight while nominally idle       | Drained steadily               | Hours                       |

The failure mode is not that the signals were subtle. It is that nothing consumes them.

## The existing surface this belongs in

This repository already owns a host-health reporting mechanism, and a defence should
extend it rather than sit beside it:

| Component                                            | Role                                                |
| ---------------------------------------------------- | --------------------------------------------------- |
| `helpers/host_health/login_report`                   | Reporting-only checks, run at end of login          |
| `host-health.service`                                | `Type=oneshot`, `WantedBy=graphical-session.target` |
| `host-health-collect.timer` / `.service`             | Periodic collection                                 |
| `play-host-health-login-report.yml`                  | The play that deploys it                            |
| `files/home/bashrc-includes/host-health-report.bash` | Shell-visible surface                               |

The design intent recorded in `host-health.service` is directly relevant:

> END of login, not boot. The whole point is that somebody is present to read it: a
> report delivered to an empty greeter is a report nobody sees, and the failure this
> exists for was a broken host nobody was told about.

That is the same failure class as this incident, already articulated. The unit is
explicitly reporting-only and never re-runs a play — a crash-loop check fits that
contract exactly, since the correct response is to tell a human, not to start killing
containers automatically.

A caveat the incident exposes: a login-time report cannot warn about a condition that
**destroys the session before anyone logs in**. The collection timer is the surface that
could have caught this one. Which of the two carries the check, and whether the timer's
findings need a delivery path that survives a dead session, is a design question for
Phase 3.

## The D-Bus accounting interface is available

A monitor could measure the actual quota rather than a proxy for it.
`org.freedesktop.DBus.Debug.Stats` **is exposed** by `dbus-broker` on this host and
returns per-peer accounting under `org.bus1.DBus.Debug.Stats.PeerAccounting` (~43 KB of
data across 83 peers when sampled). Available keys include:

```
Bytes            IncomingBytes     OutgoingBytes
Fds              IncomingFds       OutgoingFds
Matches          MatchBytes
Objects          NameObjects       ReplyObjects
ActivationRequestBytes   ActivationRequestFds
```

This matters because it turns "watch for container restart storms" — a proxy that would
miss any *other* cause of quota pressure — into "watch the headroom on the resource that
actually failed". The former catches one known cause; the latter catches the class.

## The unresolved question before any threshold is set

`dbus-broker`'s quota is per-UID and the broker accepts `--max-bytes BYTES`, but on this
host the launcher runs as `dbus-broker-launch --scope user` with **no `--max-bytes`
argument**. Meanwhile `/usr/share/dbus-1/session.conf` declares
`max_incoming_bytes` and `max_outgoing_bytes` at 1,000,000,000, and
`/etc/dbus-1/session.d/` is empty.

Whether those XML limits are what produced the quota that broke is **unverified**.
`dbus-broker-launch(1)` says only that *"Nearly all of the configuration attributes are
supported"* and names none of them. So:

- A monitoring threshold computed from the XML value may be measured against a limit that
  is not the live one.
- A mitigation that raises the XML value may raise a limit that was never the binding
  constraint.

Both would produce something that looks like a working defence and is not. This is the
same trap identified independently for the greeter dconf path in
[greeter-power-policy.md](greeter-power-policy.md) — a plausible configuration surface
that may not be the one in force. In both cases the discipline is the same: **read the
effective value back and prove it moved**, before writing anything into a play.

Task 2.3 resolves the mechanism; Task 2.4 reads the real figures.

## Defence-before-fix ordering

The crash loop was still live during this research — 66 starts in a 60-second sample.
That is the only genuine, unsimulated instance of the defect available, and stopping the
container destroys it.

So the ordering matters:

1. Build the detection defence **while the loop is live**, and verify it fires. A true
   positive against real data, not a synthetic reproduction.
2. Stop the loop, and verify the detection goes quiet. The true negative.
3. Only then address the fixes.

Reversing this — fixing first — leaves a detection defence that has never been shown to
detect anything, which is indistinguishable from one that does not work. Recorded as
Task 3.4, and as a risk in `PLAN.md`.
