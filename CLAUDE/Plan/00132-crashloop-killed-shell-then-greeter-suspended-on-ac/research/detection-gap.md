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

> **Resolved in Phase 3, and against this section.** Neither host-health surface carries
> the check. The collection timer runs **daily**, and a loop that kills the desktop within
> ten hours can begin and end between two of its ticks; the login report has the caveat
> above. Plan 00055's watchdog runs every **two minutes** and already has the attribution,
> report, signal and notification path. The full argument is in
> [the confirmed course of action](#where-it-belongs-extend-plan-00055-do-not-build-anything-new)
> below. This section is kept because the *reasoning* about reporting-only contracts still
> holds and is what made 00055 the obvious home once its cadence was compared.

## The D-Bus accounting interface is available — but reports usage, not limits

This section records what the interface *does* expose. The conclusion drawn from it
further down is that this is **not enough** to monitor on; read the two together rather
than stopping here.

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

The hope was that this turns "watch for container restart storms" — a proxy that would
miss any *other* cause of quota pressure — into "watch the headroom on the resource that
actually failed". The former catches one known cause; the latter catches the class.

**That hope did not survive measurement, and the proxy wins.** See below.

## Quota headroom monitoring: investigated and rejected

Task 2.4 set out to read the real quota and headroom so a threshold could be measured
against the resource that actually failed. The measurement disqualifies the approach.

The session broker runs with `--max-bytes 100000000000000` — 1e14 bytes, about 100 TB, and
not derived from any configuration file (the derivation is worked through in
[podman-restart-supervision.md](podman-restart-supervision.md), which answers Task 2.3 in
the negative). Against a ceiling of 1e14, **headroom is not a meaningful quantity**: the
live `PeerAccounting` figures sit in the tens of kilobytes per peer, so the ratio is
permanently and uninformatively near zero. A threshold on it would never fire — including
during the incident, because the global ceiling was never what broke.

What broke was a *per-peer receive allowance* that `dbus-broker` derives internally and
exposes through no flag, file, or bus method. `Debug.Stats` reports each peer's current
usage but not the limit that usage is charged against, so the ratio the defence needs
cannot be computed from the interface that is available.

So the class-level defence is unavailable, and this is the honest reason why — not a
preference for the simpler option. **Task 2.4 answered: there is no headroom figure to
threshold against.**

One accounting signal remains genuinely useful, but only as a *post-mortem*: the broker's
own `UID … exceeded its 'bytes' quota` log line names the failure unambiguously. It was
emitted **4 milliseconds** before GNOME Shell began shutting down. As a warning it is
worthless; as the thing that turns a baffling overnight desktop disappearance into a
diagnosed incident it is valuable, and it should be what the report *cites* once the
restart-rate signal has already fired.

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

## The confirmed course of action (Task 3.3)

### Where it belongs: extend plan 00055, do not build anything new

Plan 00055 (Container Process Watchdog, Dormant) already owns and has deployed every part
of this that is expensive to build:

| Piece it already has                                                                                                                                                              | Why this defence needs exactly that                                                                                                                                                                                             |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A `systemd --user` timer on a **2-minute** cadence                                                                                                                                | Fast enough to catch a storm hours before it exhausts anything                                                                                                                                                                  |
| **Verification caveat** — 00055's L3 panel/notification pass is **outstanding** (its Task 4.2 is 🔄, and its "report reachable from the panel and the CLI" criterion is unticked) | Its L2 host run is green for podman, docker and lxc, so attribution and reporting are verified. The **delivery** leg is not — and that is the leg this design leans on hardest. Task 5.1 must confirm it rather than inherit it |
| Container attribution across Podman, Docker and LXC                                                                                                                               | The finding must name which container, or it is not actionable                                                                                                                                                                  |
| Atomic `report.json` + a `FindingsChanged` D-Bus signal                                                                                                                           | The delivery surface, already written                                                                                                                                                                                           |
| GNOME panel indicator + deduped notification                                                                                                                                      | Reaches a logged-in human without waiting for a login                                                                                                                                                                           |
| An allowlist config shape                                                                                                                                                         | Deliberate restart churn during a deploy must be suppressible                                                                                                                                                                   |
| A reporting-only design decision, gated by a static QA check                                                                                                                      | This defence must not kill a container either — see below                                                                                                                                                                       |

The alternative home, `host-health-collect.timer`, is **wrong** and the reason is
structural rather than aesthetic: it runs **daily**. A loop that goes from start to killing
the desktop inside ten hours can begin and finish between two ticks. Worse, its consumer is
`login_message` — a report delivered at login, and this incident's defining property is
that it destroys the session *before anyone logs in*. A login-time report cannot warn about
a condition whose first symptom is that there is nobody to warn.

So: a new probe inside `helpers/containerwatch`, a new finding type in the existing
`report.json`, and no new timer, unit, extension or delivery path.

### What it measures: `RestartCount` delta per tick

Podman and Docker both track a container's cumulative restarts, readable per container:

```
podman inspect --format '{{.RestartCount}}' <container>
```

Measured live on this host while the loop was running, across every container present:

| Container             | `RestartCount` |
| --------------------- | -------------- |
| The crash-looping one | **124,873**    |
| Next highest          | 19             |
| Third                 | 1              |
| All others            | 0              |

The separation is four orders of magnitude. This is not a metric needing careful tuning to
discriminate — healthy containers sit at zero and stay there.

Two conditions, because they catch different situations:

1. **Rate** — `RestartCount` delta between consecutive 2-minute ticks. The observed loop
   ran at roughly 100 restarts/minute, so ~200 per tick. A threshold of **10 per tick**
   sits twenty times below the offender and far above any legitimate deploy churn.
   Consecutive-sample deltas are an existing pattern in this helper: `cpu_delta_pct`
   already does exactly this, PID-reuse guard included.
2. **Absolute** — `RestartCount` above a high floor (order 1,000) on a *single* tick.
   This exists because the rate test needs two samples, and the most likely real-world
   case is the one live right now: a loop already running when the watchdog starts. Without
   this, a reboot into an in-progress storm is silent until the second tick.

`RestartCount` is cumulative and never resets for the life of the container, so the
absolute test degrades gracefully into "this container has restarted an absurd number of
times", which is worth reporting regardless of current rate.

**LXC has no equivalent field.** The probe must report Podman and Docker and explicitly
record LXC as out of scope, rather than silently returning nothing for it and appearing to
have checked.

### What it does when it fires: reports, and does not kill

Plan 00055's decision D3 — reporting only, no kill and no throttle, enforced by the
`scripts/qa-nokill-containerwatch.bash` static gate — applies unchanged and for a stronger
reason here. The crash-looping container belongs to an unrelated project. Stopping
someone's container from this repository's watchdog would be this host making a decision
about another project's workload on the strength of a heuristic.

The finding should carry the `exec_hint` the existing code already builds, so the human is
pointed *inside* the container, and cite the broker's quota log line as the explanation of
why restart churn is a desktop-stability problem and not merely untidy.

### How it gets falsified (Task 3.4)

| Claim                              | Observation that refutes it                                                                               |
| ---------------------------------- | --------------------------------------------------------------------------------------------------------- |
| The probe detects the live loop    | Run it now, with the loop running, and get **no** finding → the probe does not work                       |
| It does not fire on a healthy host | Stop the loop; a finding that persists past the absolute-test container disappearing is a false positive  |
| The threshold is not merely lucky  | The 10-per-tick bound must sit between the observed legitimate maximum and the offender — 0 and ~200 here |
| The notification reaches a human   | Trigger a finding and confirm the panel and notification appear, not just that `report.json` changed      |

The first row is the one that must be executed **before** the loop is stopped, per the
ordering above. It is the only true positive available without synthesising one.

## The true positive, captured

**This has now been done, and the result is recorded here because it cannot be
reproduced once the container is stopped.**

`detect-crashloop.bash` in this plan folder implements the algorithm above exactly and was
run against the live loop. It is read-only and anonymises container names, which is what
makes its output quotable in this tracked, public repository.

```
sample interval   : 45s
rate threshold    : 3 restart(s) per 45s  (= 10 per 120s production tick)
absolute threshold: 1000 cumulative restarts

ok       container-A  cumulative=0      delta=0
ok       container-B  cumulative=1      delta=0
…
ok       container-J  cumulative=19     delta=0
FLAGGED  container-L  RATE (98 restarts in 45s, threshold 3)
                    + ABSOLUTE (128505 cumulative, threshold 1000)
ok       container-M  cumulative=0      delta=0

RESULT: 1 container(s) FLAGGED — detection fired.
```

Thirteen containers, **one** flagged, **zero** false positives. Three properties of that
result matter more than the fact that it fired:

1. **Both conditions triggered independently.** The rate test and the absolute test each
   caught it on their own, so neither is load-bearing alone — which is what makes the
   absolute test a genuine first-tick safety net rather than decoration.
2. **`container-J` was not flagged.** It carries 19 lifetime restarts — real churn from
   real failures — and sat correctly below both thresholds with a zero delta. This is the
   nearest thing on the host to a borderline case, and the thresholds cleared it by a wide
   margin. The gap between 19 and 128,505 is where the threshold lives, and it is enormous.
3. **Every other container returned a delta of exactly zero.** The discriminator is not
   "the offender is high"; it is that a healthy host is *completely* static on this metric.

The rate had risen to ~130 restarts/minute by this sample, from ~66/minute when the
incident was first triaged. The loop is not steady, it is accelerating.

**What is still owed:** this is a prototype, not the shipped defence. Task 5.1 must port
the algorithm into `helpers/containerwatch` with its tests, and the **true negative** (Task
5.5 — stop the loop, confirm the detection goes quiet) is the half of the validation pair
that has not been observed. The true positive was the perishable half, and it is now
banked.

## The true negative — and the design defect it caught

The loop was stopped with `podman stop` (its policy is `unless-stopped`, so an explicit
stop is definitive and no policy edit was needed). It stayed stopped, and the scope churn
went from ~50 starts per 40 s to **zero**. Final count: **131,377** restarts.

Re-running the detector was supposed to be a formality. It was not:

```
FLAGGED  container-L  ABSOLUTE (131377 cumulative, threshold 1000)
RESULT: 1 container(s) FLAGGED — detection fired.
```

The **rate** test correctly went silent — delta zero, no finding. The **absolute** test kept
firing, because `RestartCount` is cumulative and never resets. As originally specified, that
threshold would have alarmed **for ever** on a container already dealt with.

That is worse than a missing alarm. An alarm that cannot be cleared is one an operator
learns to ignore, and it would have trained the human to ignore the exact signal this whole
plan exists to deliver.

**The fix: gate the absolute test on the container actually running.** A container that is
not running cannot be looping. With the gate in place the same host returns:

```
ok       container-L  cumulative=131377 delta=0

Historic, deliberately NOT flagged:
  container-L has 131377 cumulative restarts but is not running -- historic, not looping
RESULT: no container flagged.
```

The count is still *reported*, just not *flagged* — the suppression is visible rather than
silent, so a reader can see the history without being alarmed by it.

**This is the argument for the validation pair in one paragraph.** The true positive proved
the detector fires. Only the true negative proved it also *stops* firing, and that is where
the defect was. A plan that had shipped after the positive alone would have shipped a
permanent false alarm. **Task 5.1 must port the running-gate, not just the thresholds.**

Also recorded from the final inspection: the container exits with code **0**. It was not
crashing — it completed successfully and `unless-stopped` restarted it regardless, which is
what that policy does. "Crash loop" is the right name for the effect and the wrong name for
the cause.

## Teeth: detection alone would not have saved the session

Everything above is **detection**, and detection is not protection. This has to be said
plainly because the plan reads as though it were:

> The watchdog's report is delivered to a logged-in human. In this incident **nobody was
> logged in** — that is the incident's defining property. A reporting-only defence would
> have observed the loop for ten hours, written a finding nobody read, and watched the
> compositor die on schedule.

So the design needs a layer that acts without a human. Three candidates, weakest to
strongest.

### Layer 1 — automatic stop at a restart threshold (the decided approach)

Stop a container that has restarted more than **100 times within a rolling window**.

The margin is what makes this defensible rather than reckless:

| Quantity                                   | Value                                |
| ------------------------------------------ | ------------------------------------ |
| Restarts needed to exhaust the D-Bus quota | ~85,000 (≈10 h at the observed rate) |
| Enforcement threshold                      | 100                                  |
| Proportion of the way to failure           | **0.1%**                             |
| Time to trip at the observed ~130/min      | ~45 seconds                          |

Three orders of magnitude of headroom. The action fires roughly ten hours before the
desktop would die, and the nearest legitimate container on this host sits at 19 restarts
*for its whole lifetime*.

**The window matters.** A *cumulative* bar of 100 would eventually fire on a container that
restarted slowly over months, which is normal operation, not a storm. A *windowed* count
fires only on a loop.

**Where it must NOT live.** Plan 00055's watchdog is reporting-only by explicit decision
(D3), enforced by `scripts/qa-nokill-containerwatch.bash`. Putting an enforcement action
there contradicts that plan's own contract.

**Why `podman stop` is nevertheless not what D3 rejected.** The gate's forbidden patterns
are *signal* kills — `os.kill`, `os.killpg`, `pkill`, `kill -<sig>`, `Gio.Subprocess.force_exit`
— scanned across `helpers/containerwatch/*.py` and the extension. D3 rejected hard-killing
processes and capping CPU as symptom-hiding. An orderly engine-level
`podman stop` is a different act: reversible, signal-free, and it addresses the loop at its
source rather than masking its effects. The container's policy is `unless-stopped`, which
means *restart unless explicitly stopped*, so a plain stop is definitive and needs no
policy edit — and therefore leaves the external project's configuration untouched.

**Home:** plan 00079 (Podman container control), which already exists for container
lifecycle actions, not 00055.

### Layer 2 — remove the coupling (UNVERIFIED, needs a probe)

The session-bus traffic exists because podman uses the **systemd** cgroup manager
(`podman info` reports `cgroupManager=systemd`), registering every container as a transient
`libpod-*.scope` with the **user** systemd manager — 20 such scopes are live right now. Each
start and stop makes that manager emit unit and job signals on the session bus, which is
where the bytes come from.

`cgroup_manager = "cgroupfs"` in `containers.conf` would stop scope creation, and with it
the signal storm.

**This is a hypothesis, not a recommendation.** It is not established here that cgroupfs
eliminates the signals, and it trades away systemd's cgroup integration and resource
accounting. It needs its own probe before anyone writes it into a play — precisely the
inert-fix discipline this plan has already been caught by twice.

### Layer 3 — containment by UID (the strongest, and the most disruptive)

The broker accounts per-UID, and the failure message names it:

```
UID <uid> exceeded its 'bytes' quota on UID <uid>.
```

Rootless podman runs as the **same UID as the desktop**. Workload containers under a
*different* UID could not exhaust the desktop's budget at all — no threshold, no timing, no
tuning, and it holds against causes nobody has thought of yet, which is what none of the
layers above can claim. [independent-witness-agent-session.md](independent-witness-agent-session.md)
is the existing evidence: a container-hosted session survived the outage untouched because
its blast radius stopped at the boundary.

The cost is a real change to how work is run on this host, so it is recorded as the
structural option rather than proposed for immediate adoption.

### And the greeter fix is teeth too

Worth restating, because it is easy to file as tidiness: the greeter change prevents the
**machine** being suspended even when the session dies. Layer 1 protects the session;
the greeter fix protects everything still running outside it — which, as the tmux evidence
shows, is where the surviving work actually lives.
