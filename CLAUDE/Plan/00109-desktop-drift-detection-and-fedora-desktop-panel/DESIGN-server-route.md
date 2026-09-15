# Design — the server route for the host health report (Task 3.2)

`PLAN.md` carries the task state; this carries the reasoning behind it.

The desktop delivery is `notify-send` from a unit bound to `graphical-session.target`.
A server has no session bus and that target never activates, so the profile where
unattended drift goes unnoticed longest — nobody logs in to see a notification — was the
one profile with no detection at all. Only the *delivery* was ever desktop-bound; the
three checks behind it are profile-agnostic.

## 1. One play, two deliveries

`play-host-health-login-report.yml` is `scope: general` and branches on
`provisioning_profile`. It began as a second playbook and that was wrong: the pair shared
four byte-identical tasks (the PyYAML install, the unit directory, the uid resolution and
its assert) and five comment blocks differing only by line-wrapping, which is the
signature of a copy and the shape that drifts the first time one of them is fixed.

The repo's own test for splitting a play is at `docs/playbooks.md`: the panel earned its
own file because it is "a generic multi-section surface **with a lifecycle of its own**".
This has no such claim available — same `hosts`, same `become`, same opt-in story — and
`scope` is a guard variable evaluated inside the play, so exactly one of the pair would
ever have done anything on any host.

The play keeps a **recognition** assert, which is not a scope guard: both branches key on
`provisioning_profile`, and an unrecognised value would quietly take the desktop branch on
a server rather than failing. `qa-ansible.bash` rejects a real scope guard on a
general-scope play, correctly, so the task is named for what it is.

## 2. The cadence is derived from the consumer's staleness bound

`login_message.STALE_AFTER_DAYS` is 14: past it the consumer reports the document's own
age, because a clean document nobody has refreshed for a fortnight describes the host as
it was a fortnight ago.

**Daily.** It absorbs fourteen consecutive misses, so one failed run, one reboot or a day
powered off is not a false alarm, while a collector that has genuinely stopped is reported
well inside the bound. Weekly allows two attempts inside the same window — the margin
would be a single missed run, and the report would then be crying stale on a healthy host
or silent on a broken one depending on which way the miss fell.

`OnStartupSec=5min` as well, because this plan's incident was a reboot into a new kernel
and a calendar-only trigger would take up to a day to notice that. `RandomizedDelaySec`
applies to every trigger including that one, so the post-boot collection lands 5–35
minutes after the user manager starts. That window is covered from the other end — see §4.

## 3. The interactive guard

bash reads `~/.bashrc` for a **non-interactive** shell too when sshd is what started it,
which is why a `.bashrc` that prints anything breaks `scp`, `sftp` and rsync-over-ssh with
a protocol error: the transfer parses stdout. This snippet is the first thing this repo
puts in `~/.bashrc-includes` that prints at all — the existing four only define aliases.

`scripts/test-host-health-login-snippet.bash` renders the template and sources it in both
a real `bash -i` and a plain `bash -c`. The guard is exercised against a **findings**
document deliberately: with a clean one the snippet is silent for the wrong reason and the
assertion passes with the guard deleted.

Six mutants, all killed: the guard deleted; a `cd` into the checkout instead of a prefixed
`PYTHONPATH`; `PYTHONPATH` exported; the missing-checkout guard deleted; that guard
returning non-zero; and a bare `python3` where a pyenv shim would answer.

The sixth **survived the first round**. The assertion was a `grep` over the whole rendered
file, and the snippet carries a comment explaining why the interpreter is named by path —
so the check was satisfied by its own documentation while the invocation it vouched for
had been mutated. It now selects the non-comment line naming the module and requires
exactly one match.

## 4. A fresh document can be entirely about the previous boot

Only the timer route can outlive a reboot; the desktop producer re-runs at every graphical
login. So a document collected under the previous kernel can be minutes old, well inside
the staleness bound, and say `ok` while every DKMS module on the box is unbuilt for the
kernel that actually booted — this plan's founding incident, behind the mechanism built to
prevent it. Age and boot are independent axes and staleness does not cover it.

`login_message.render` takes the running kernel and reports a mismatch in its own right,
in the **not-checked** group: nothing is known to be broken; what is known is that these
results do not describe the running kernel. Both sides must be known first — the
`unavailable` shape carries `kernel: ""`, and an empty running kernel means "could not
tell", so neither is evidence of a mismatch.

### 4.1 The rule contradicted itself before round 2 caught it

Reporting the mismatch made the report *speak* in the scenario it was written for, and
what it then said was wrong. `dkms_findings` bakes the collecting kernel into its text —
"no DKMS module installed for the running kernel 7.1.9" — written at collection time and
read later, so after a reboot the report carried two consecutive lines with two different
values for "the running kernel", the first presented as a known fault about now:

```
  - evdi: no DKMS module installed for the running kernel 7.1.9-…
  Not checked …
  - these results were collected under kernel 7.1.9-… and this host is now running 7.2.4-…
```

Worse, a test asserted that exact pairing and recorded it as correct, so nothing would
have found it later.

**On a mismatch the boot-scoped section's findings are demoted to the not-checked
group**, under the explanation that causes them. `status_document.BOOT_SCOPED_SECTION`
names the one section this applies to — DKMS state and units that failed during a boot.
The ledger, play freshness and installed-vs-pinned survive a reboot unchanged, so
demoting them would be the mirror image of the same overclaim, and the wording was
narrowed for the same reason: "nothing here describes the running kernel" was false about
three sections out of four. Four mutants: the demotion disabled, every section demoted,
the explanation placed after what it explains, and the wording reverted.

## 5. A healthy server has to be silent, and was not

Found by the `qa-reviewer` pass on 26-09-15 (`subagent-reports/260915-qa-reviewer-00109-t32-opus-5.md`),
measured rather than inferred. Two permanent findings at every interactive login on a
stock server, both from the same root: **`dkms` is installed by exactly two plays, both
optional and both desktop hardware**, so a server has neither the command nor a module
tree.

- **The health probe.** `dkms: command not found` was reported as unchecked. It is now an
  answer when — and only when — there is also no registered module tree under
  `/var/lib/dkms`: a host with no DKMS subsystem cannot have a module missing a build.
  The command absent *with* trees registered is reported, and is a worse state than
  either half, since nothing will rebuild them for the next kernel. An unreadable state
  directory answers `None` and still reports: "we could not tell" is not "it is fine".
- **The pin check.** On a server `dkms()` raises "command not found", so every
  DKMS-resolved pin became *"could not be checked"* for ever. A DKMS-kind pin on a host
  with **no DKMS subsystem** is not answerable here, and that is an answer — driven by
  the same `dkms_registered_modules()` tri-state as the probe, so the two cannot disagree
  about whether this host has DKMS.

### 5.1 The first answer to the pin half silenced the founding incident

Applicability was first taken from the play ledger for the whole population: a pin
belongs to a play, so a play this host has never run installs nothing for the pin to
describe. Round 2 of the review drove it across five ledger states and found the cost.
**Task 1.3 chose no backfill**, so no host has a `play-displaylink.yml` record until that
play next runs — and on every such host `evdi_version (behind): pinned 1.15.0, installed 1.14.16`, the 2026-09-11 state exactly, stopped being reported. Worse, a ledger with rows
but no pinned play left the population empty, which skipped the zero-coverage guard
entirely and produced no output at all: indistinguishable from every pin matching.

Task 1.3's rule was derived for the **freshness** axis, where "has this play been run
here" is the whole question. On the install-state axis it is not: an installed version
that disagrees with the pin is drift whatever the ledger has seen. Transplanting a rule
is not the same as deriving one.

So the ledger acts on **one verdict**, not on the population. `ABSENT` — *"pinned X,
nothing installed"* — is a fault on a host that ran the play and expected on one that
never did. `BEHIND`, `AHEAD` and `UNDETERMINED` all mean the software is present and was
compared, so no ledger state can make them uninteresting, and the zero-coverage guard
counts the whole manifest again, which is what it was always about.

## 6. What the server checkout must provide

The freshness check runs `git fetch`, and `git_history.fetch` sets `GIT_TERMINAL_PROMPT=0`
and `GIT_ASKPASS=""` so nothing blocks on a prompt no login shell could answer. A timer has
no `ssh-agent` — the desktop route only ever worked because it runs inside a session that
has one — so a checkout with an SSH remote and a passphrase-protected key cannot
authenticate, and every run reports "play-freshness has never successfully reached the
remote on this host".

That is a **true** finding with an operator-side remedy, not noise, so it is documented
rather than silenced: give the checkout a remote it can fetch anonymously, or a key usable
without an agent. Deliberately no `Environment=SSH_AUTH_SOCK` in the unit — pointing at a
socket that may not exist would trade a clear finding for a confusing one.

`fetch_clock.offline_finding(last=None)` fires on the **first** login, not after seven
days, so on a server whose fetch never works the line is permanent from the start. The
HOST item therefore confirms the remote rather than assuming it.

## 7. What each branch of the merged play removes

A provisioning profile is not immutable — `-e provisioning_profile=…` is the documented
override and a typo is corrected on the next run. Neither branch removing the other's
artefacts would leave both deliveries installed: two collections and two reports, or a
`notify-send` unit sitting on a box with no session bus.

So each branch removes the other's unit files **and their `.wants/` symlinks** — enabling
a unit writes a symlink that outlives the unit file, so deleting the file alone does not
undo the enable. `state: absent` is used rather than asking systemd, because it is
idempotent and cannot fail on a host that never had them.
