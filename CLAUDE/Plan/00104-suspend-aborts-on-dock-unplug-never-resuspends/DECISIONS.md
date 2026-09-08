# Plan 00104 — Technical Decisions

Extracted from [PLAN.md](PLAN.md) to keep the plan document lean (it is re-read in
full every session). The decisions themselves are unchanged; Decision 4 supersedes
Decision 1's priority ordering, and Decision 3's conclusion is superseded while its
reasoning survives — each says so in place.


### Decision 4: The goal is a durable suspend request, not a bounded failure — **supersedes Decision 1's framing**

**Context**: Decisions 1 and 2 both framed the problem as *recovering from* a failed suspend,
and argued over how long the machine may stay awake before a safety net catches it. The
operator rejected the premise outright:

> "there is no acceptable bag time — if i say suspend it should go for suspend. something as
> trivial as unplugging the power should not derail that."

That is the better frame, and it changes the design. The defect is not "the recovery is slow";
it is **that an explicit suspend request was silently not honoured**. A bounded idle timeout
answers the wrong question — it makes the failure shorter rather than making the request hold.

**Decision**: Three layers, in priority order.

1. **Remove the trigger.** F17 shows the AC adapter and both USB-C power-delivery ports ship
   armed as wakeup sources. Pulling the mains therefore fires a wake event, and one landing
   during the s2idle transition aborts the suspend. Disarm them by udev rule — waking a
   laptop is what the lid and the power button are for.
2. **Make the request durable.** A `system-sleep` hook re-issues the suspend when a resume
   happens within 10s of one with the lid still closed. This covers *any* wake source, not
   just the one we know about.
3. **Defence in depth.** GNOME idle-suspend on battery, as originally proposed — demoted from
   primary to backstop.

**What this reverses**: Decision 1 rejected the `system-sleep` hook as "the narrowest option,
covering strictly less". Under the old framing that was true. Under the correct framing the
hook is the layer that actually delivers the requirement, and the idle timeout is the one
covering less — it cannot honour a request, only curtail the consequences of dropping it.
Decision 1's *choice* of setting stands as layer 3; its *reasoning about priority* does not.

**Date**: 2026-09-08

### Decision 1: Restore the battery idle-suspend safety net as the primary fix

**Context**: Something must bound the damage from *any* failure to stay suspended, not just
this one trigger.

**Options considered**:

- **A — `sleep-inactive-battery-type=suspend` in the playbook.** One managed setting.
  Restores GNOME's own default (F11). Catches every variant regardless of what aborted the
  suspend, bounded by the idle timeout. **Known limitation, not an advantage**: while an
  inbound SSH session is established, `ssh-suspend-guard` holds a *block*-mode sleep
  inhibitor (F12), which disables this fix entirely for as long as the session lasts. An
  earlier revision of this plan credited that interaction to Option A as a benefit, which
  inverts what F12 actually says.
- **B — Disarm wakeup on the dock's USB tree.** Treats one trigger. `3-6` is not a stable
  identifier — it enumerated as a hub during the incident and as a keyboard afterwards (F5).
  Leaves every other wakeup source unhandled.
- **C — A `system-sleep` post-resume hook that re-suspends if the lid is still closed.**
  Custom code, and the *narrowest* of the three: it only fires after a resume, so it does
  nothing for H1, where no suspend was ever attempted.

**Decision**: **A**, as the primary fix. It is the smallest change, it restores a default
rather than inventing behaviour, and it is the only one of the three whose coverage does not
depend on which trigger fired. C was the first approach considered in session and is recorded
here because it is the tempting one and it is wrong as a primary: it is more code than A and
covers strictly less.

**Date**: 2026-09-08

### Decision 2: Treat the AC→battery state change separately, and gate it on H1

**Context**: A is bounded by the idle timeout, so a worst case still leaves the machine awake
in a bag for that timeout. Reacting to the actual state change would cut it to seconds.

**Options considered**:

- **A udev rule on `SUBSYSTEM=="power_supply"`** that, on AC going offline with the lid
  closed, triggers a oneshot unit that suspends. Event-driven, covers H1 and the incident
  case alike.
- **Shortening `sleep-inactive-battery-timeout`** instead. No new units, but it is a blunt
  trade against normal on-battery desk use.
- **Setting `HandleLidSwitchDocked=suspend`**. Rejected outright — it would suspend the
  docked workstation on lid close, which the Non-Goals forbid.

**Decision**: Deferred to a decision gate in Phase 3, **after** H1 and P1 are settled on the
host. Building a udev rule for a state transition that has not been demonstrated would be
speculative; if H1 is refuted, Phase 1 alone may be sufficient. Recording the option now so
the gate has something concrete to accept or reject.

**Date**: 2026-09-08

### Decision 3: The setting goes in `play-prevent-ssh-suspend.yml` — ⚠️ SUPERSEDED by Decision 4

> **Superseded.** Its *reasoning* holds and is why the move happened at all — a
> drift-prevention setting must live in a playbook that actually runs. Its *conclusion* does
> not: once Decision 4 added a udev rule and a `system-sleep` hook, the work needed a play
> that owns suspend policy, and `play-prevent-ssh-suspend.yml` is not that. The outcome is
> `imports/play-suspend-and-lid-policy.yml`, which satisfies Decision 3's actual requirement
> (imported by `playbook-main.yml`) without putting a udev rule in an SSH play.

**Context**: Two playbooks could plausibly own `sleep-inactive-battery-type` — the lid/power
one by topic, or the one that already sets its AC sibling.

**The deciding fact**: `play-laptop-lid-power-management.yml` is **imported by no playbook**.
It lives under `imports/optional/hardware-specific/` and `playbook-main.yml` does not pull it
in, so it only runs if invoked by hand. `play-prevent-ssh-suspend.yml` **is** imported, at
`playbook-main.yml:8`.

**Decision**: `play-prevent-ssh-suspend.yml`. Putting a drift-prevention setting into a
playbook that never runs would recreate exactly the failure being fixed — the setting would
be nominally IaC-managed and still absent from the host. Topical tidiness loses to actually
being deployed. It also puts the battery setting beside the AC sibling it must not disturb,
where the next reader sees both together.

**Follow-on**: that `play-laptop-lid-power-management.yml` is unimported is itself a latent
problem — the lid config it deploys (F7) is on this host but nothing would restore it. Out of
scope here; worth its own plan.

**Date**: 2026-09-08

