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

## 6. The executor, and the hole the split hid

`helpers/host_health/probe.py` runs the three probes and hands them to §1's classifier. Two things
it owes that a pure classifier cannot:

**Every route out ends in a `ProbeOutcome`.** A missing command, a non-zero exit, a timeout and an
`OSError` all become one. This runs at the end of a login, so an uncaught exception takes the whole
health surface down — and a user who sees nothing cannot tell that from a healthy host, which is
§3's rule applied to the executor itself. The timeout is there because `systemctl` against an
unreachable bus can hang, and a check that stalls the session gets removed from the session.

**Multi-line stderr is collapsed to one line.** Each finding is one line by contract, because the
notification splits on newlines; real `systemctl` answers an unreachable bus in two.

The split between pure and impure hid a hole for one commit. `build_report` took `ProbeOutcome` for
`dkms` but plain **text** for the two `systemctl` scopes — so a `systemctl` that could not run
contributed an empty failed-unit list, which is byte-identical to a host with nothing failing. §3
was written as though it already covered all three; it covered one. Both scopes now carry an
outcome, each probe is judged independently so one failure cannot mask another's findings, and the
container smoke run demonstrates the difference: with neither `dkms` nor a systemd bus reachable it
produces three findings, two of which the previous shape swallowed silently.

That is worth naming plainly: the defect was in the code whose entire subject is *checks that cannot
fail*, and it survived a design document that asserted the opposite.

## 7. Open decision for Task 3.2: what an offline login should say

The login unit is a `--user` service `After=graphical-session.target`, on the pattern
`play-container-watch.yml` already establishes. It is deliberately **not** delivered before Task
3.2: a unit whose output nothing surfaces is not a deliverable.

The decision 3.2 has to make, and must not make by accident: the play-freshness check answers
`UNTRUSTWORTHY` when its `git fetch` fails, because reporting "nothing is stale" from refs that may
be weeks old is the wrong answer. Offline at login is **ordinary** — a train, a hotel, a laptop that
woke early. So the two rules this plan keeps invoking point in opposite directions here: *"I could
not look" must never render as "nothing is wrong"*, and *a check that speaks on every login gets
muted*.

Neither rule wins on its own terms. The question is whether an unreachable remote is a fact about
the **host** (which this surface reports) or about the **network** (which it does not), and it should
be settled in writing before the unit exists — not discovered from whichever behaviour the first
implementation happened to have.

## 8. Settled: what an offline login says

§7 left this open deliberately, so it would be decided in writing rather than inherited from
whichever behaviour the first draft happened to have. Decided.

The two rules genuinely do conflict as stated. *"I could not look" must never render as "nothing
is wrong"* says report it; *a check that speaks on every login gets muted* says do not, because
offline at login is ordinary — a train, a hotel, a laptop that woke before the wifi did.

Neither wins, because the question was posed wrongly. "Can I reach the remote **right now**" is a
fact about the network. **"How long is it since I last could"** is a fact about this host, and it
is the one that matters: a freshness answer computed from refs fetched an hour ago is worth
having, and the same answer computed from refs three weeks old is not.

So the check records the timestamp of each **successful** fetch, and an offline run:

| Time since the last successful fetch | Behaviour                                     |
| ------------------------------------ | --------------------------------------------- |
| within the staleness bound           | judge against the refs on hand, and say nothing if clean |
| beyond it                            | **a finding**, naming how long it has been    |
| never (no record at all)             | **a finding** — nothing has ever been checked here |

This keeps both rules. Nothing is emitted on an ordinary offline login, and *"I have not been able
to check for two weeks"* is reported as what it is: a fact about this machine, not about the cafe's
wifi. It also removes the worst outcome available under either simple answer — a host that quietly
stops being checked at all, which is this plan's subject in its purest form.

The bound is a declared constant, not a guess buried in a branch, and the never-fetched case is
**not** folded into "beyond the bound": a host that has never successfully fetched has a different
problem from one that fetched last month, and one message for both would describe neither.

`check_freshness.EXIT_UNTRUSTWORTHY` stays. It is the right answer for *"the ledger is BROKEN"* and
for *"a ledgered commit cannot be resolved"* — states where no verdict can be given at all. What
changes is that a failed fetch alone stops being one of them.
