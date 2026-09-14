# The post-boot health probe — design

Phase 3's design output. Referenced from `PLAN.md` Task 3.1; this file owns the detail.

The probe answers one question at the end of a login: **did anything about this machine
break while nobody was watching?** The failure it was written after is the shape to keep in
mind — a DKMS module that could not build against a new kernel, so the module simply did not
exist, the monitors it drives stayed dark, and every mechanical check in the repo was green
because none of them looks at the host.

## 1. The split

`helpers/host_health/probe_results.py` is pure: it takes the text the probes produced and
returns findings. The executor runs `dkms status` and `systemctl --failed` for both scopes
and hands the output in. Nothing in the classifier shells out, so every rule is
unit-testable — and the rules, not the shelling, are where this gets decided.

## 2. What counts as broken

| source                      | finding when                                                 |
| --------------------------- | ------------------------------------------------------------ |
| `dkms status`               | a module has no `installed` build for the **running** kernel |
| `systemctl --failed`        | any unit, reported with its scope (system or user)           |
| `systemctl --user --failed` | likewise                                                     |
| Phase 2                     | play-freshness and installed-vs-pinned findings, merged in   |

**Against the running kernel, not merely present.** This is the distinction the incident
turned on. `dkms status` was *not* empty — the module was there, built for the kernel that
had booted the day before. A check for "is the module known to DKMS" would have passed. The
question is whether there is a build for the kernel now running.

**Per module, not per entry.** A module built for several kernels is healthy as long as the
running one is among them, and the stale entries beside it are ordinary. Reporting those is
the noise that gets the whole check muted, and a muted check is not a check.

## 3. A probe that could not run is a finding

`dkms` absent, `systemctl` unavailable, output in a shape the parser cannot read — each
becomes a finding rather than a skip. An unreadable line makes `parse_dkms` **raise**, and
`build_report` converts that into a finding instead of letting it take the whole login-time
probe down and report nothing at all.

This is the same rule the ledger's `BROKEN` sentinel enforces, for the same reason: the
failure this plan exists for was a green report over a broken host, so *"I could not look"*
must never render as *"nothing is wrong"*.

The one thing deliberately **not** guarded: a host with no DKMS modules is a real and
healthy state, and reads as clean.

## 4. Coordinated with Plans 00086 and 00074, which own no reusable probe

`PLAN.md` Task 3.1 requires coordinating rather than duplicating. Checked, and the answer is
that there is nothing to call: Plan 00086 fixed the half-installed-kernel enumeration
*inside* `play-AB-dnf-upgrade.yml`, and Plan 00074 fixed the legacy-grub cgroup step *inside*
`run.bash`. Neither is a component.

So the coordination is negative and worth stating as such: **no kernel-version enumeration
here** (00086's subject, and the place its hard-fail lived) and **no grubby probing** (00074's).
This reports what `dkms` says about the running kernel and nothing cleverer.

`scripts/collect-diagnostics.bash` already captures both `systemctl --failed` views, but for a
human to read afterwards — it classifies nothing, so it is a sibling, not a base to build on.

## 5. Silent when clean

`Report.clean` is true exactly when there are no findings, and Task 3.2's notification fires
only when it is false. Nothing reaches the user on a healthy login.

Phase 2's findings arrive through a single `extra` argument rather than a second report, so a
host with a stale play *and* a failed unit *and* a drifted pin produces **one** notification
listing three things — not three notifications, which is the other way a check gets muted.
