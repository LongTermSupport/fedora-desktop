# QA Review — commit 4a37331, Plan 00104 filing (plan work only)

**Verdict**: FIX-BEFORE-MERGE

No leak, no fail-fast violation, no IaC mutation. But the committed evidence contains one
histogram that the machine does not produce, and one probe that returns nothing while
reporting success — and PLAN.md builds a task on that probe.

All paths below are relative to `<home>/Projects/fedora-desktop`; the plan folder is
`CLAUDE/Plan/00104-suspend-aborts-on-dock-unplug-never-resuspends/`.

## Should fix

### 1. F2's histogram matches neither of the commands that could have produced it

`TRIAGE-EVIDENCE.md:46-58`

F2 states `Source: journalctl -b -1, entries-per-minute histogram` and gives
23 / 105 / 259 / … / 18, summarised as "~110–260 journal lines per minute". That is exactly
what `show_journal_density -1` (`probe-suspend.bash:73-77`) runs, and its output for the same
minutes sits in the plan's own run directory:

```
triage-runs/20260908-123123/triage-report.md:2920-2945
   2516 2026-09-08T11:40      1792 …11:41      1786 …11:42   …   692 …12:05
```

I also ran the kernel-only variant on the host: 34 / 59 / 77 / … / 11. The transcribed numbers
are neither — off by ~15x from the stated source, and ~2x the other way from the kernel
journal. The **conclusion** (no idle gap, awake for 25 minutes) is fully corroborated by the
real 1500–2500 lines/min series, so nothing downstream collapses. But the figures are quoted
as fact in `JOURNAL/00104-Journal-26-09-08.md:33` and in the commit message, and they are what
Task 1.2 will "reconcile" against. Replace F2's table with the actual probe output, or name
the command that really produced it.

### 2. The "effective HandleLidSwitch*" probe returns nothing and still reports rc=0

`probe-suspend.bash:97-102`

```
triage-report.md:3011-3012
== effective HandleLidSwitch* (including unset defaults)
```

(header, then the closing fence — empty). Confirmed on the host:
`systemctl show systemd-logind --property=HandleLidSwitch --property=HandleLidSwitchDocked
--property=HandleLidSwitchExternalPower` prints **nothing** and exits 0. Those are logind
*manager* properties, not unit properties. A blind check is here indistinguishable from a
clean one, and because it is the last command in the function its emptiness cannot fail
the leg.

This is not cosmetic: `PLAN.md:129-130` makes it the method for settling P1 — "Compare
`systemctl show systemd-logind` lid properties with the dock attached and detached". As
written, Task 2.2 compares nothing to nothing. The working query, which I ran:

```
busctl get-property org.freedesktop.login1 /org/freedesktop/login1 \
  org.freedesktop.login1.Manager HandleLidSwitch HandleLidSwitchDocked HandleLidSwitchExternalPower
s "suspend"   s "ignore"   s "ignore"
```

which incidentally confirms F7's inference that `HandleLidSwitchDocked` is `ignore`. Fix the
probe and Task 2.2 to use `busctl`, and make the probe fail rather than emit an empty section.

### 3. The `-1` boot selector is hardcoded and the report never says which boot that was

`probe-suspend.bash:66-77`, `121-125`

`journalctl --list-boots` on the host now shows the incident boot at index `-1` (ends
12:05:28) with boot `0` already started. One more reboot and `-1` silently points at an
innocent boot; `show_sleep_timeline -1` will then show a normal timeline,
`show_journal_density -1` a normal density, and the report will look **clean rather than
blind** — while `PLAN.md:121` instructs the next agent to "note any divergence from
TRIAGE-EVIDENCE.md", i.e. to attribute a probe-design artefact to the system. Take the boot
selector as a parameter and print `journalctl --list-boots` in the report so the reader can
see which boot was measured.

### 4. The Phase 3 ownership question is answerable from the repo today; deferring it is the wrong call

`PLAN.md:135-137`

`playbooks/playbook-main.yml:3-4` says in terms: "Everything imported here runs by default and
is NOT optional." `play-prevent-ssh-suspend.yml` is imported at line 8.
`play-laptop-lid-power-management.yml` lives under `playbooks/imports/optional/hardware-specific/`
and appears in **no** import list — it runs only when a human runs it by hand. Putting "the
safety net [that] cannot silently drift away again" (`PLAN.md:32-33`) into a playbook the
standard deploy never executes re-creates precisely the drift this plan exists to close.

The scaffolding settles it too: `play-prevent-ssh-suspend.yml:46-67` already carries the
`getent` UID lookup, `become_user: "{{ user_login }}"`, `DBUS_SESSION_BUS_ADDRESS` and the
`provisioning_profile != 'server'` guard; the lid play is `become: true` at play level with
none of them (`play-laptop-lid-power-management.yml:3-8`). The AC and battery keys are one
setting pair with one deployment mechanism.

**Answer: `play-prevent-ssh-suspend.yml`.** Record it as a decided Technical Decision with
that reasoning and delete the "Justify the choice in the JOURNAL rather than defaulting to the
nearer file" sub-task. If the play's SSH-flavoured name then reads wrong for a battery key,
that is a rename discussion, not a reason to scatter the pair across two plays.

### 5. Decision 1 asserts the SSH guard is a non-issue; the plan's own evidence says it disables the fix

`PLAN.md:72-73`

Option A is credited with "Does not fight the SSH guard, which blocks sleep while a session is
live (F12)". F12 says the opposite: `TRIAGE-EVIDENCE.md:233-235` — "While an inbound SSH
session is established on port 22, **all** sleep is blocked — including any idle-suspend a fix
might introduce", and the Risk table at `PLAN.md:176-177` treats it as a live risk. I confirmed
the mechanism at `files/usr/local/bin/ssh-suspend-guard:12-17` (no `--mode`, so `block`) and
`:35-41` (established `sport = :22`, 10s poll). Restate it as the known limitation of Option A,
not an advantage.

## Nits

6. **F7's fenced quote is not the file it names** — `TRIAGE-EVIDENCE.md:136-142` vs
   `triage-report.md:3003-3010`. The deployed drop-in contains two comment lines
   (`# Suspend when lid closed on battery`, `# Don't suspend when lid closed on AC power`) that
   the transcription drops while labelling itself
   `Source: /etc/systemd/logind.conf.d/laptop-lid.conf`. Task 1.2's reconciliation will surface
   it as a divergence.

7. **F6 says "complete set of matches" for a pattern narrower than the plan's own probe** —
   `TRIAGE-EVIDENCE.md:119-126` lists 4 lines; `show_sleep_timeline` (`probe-suspend.bash:68`)
   adds `Performing sleep operation` and returns 5 (`triage-report.md:47-51`). Quote the
   probe's pattern or drop "complete".

8. **F2's "boot 0 begins 12:06:49"** — `TRIAGE-EVIDENCE.md:59-60`. `journalctl --list-boots` on
   the host gives 12:06:53. Not load-bearing, but it is a stated fact the machine contradicts.

## Checked and clean

- **Public-repo safety (the priority check): no findings.** Across all five committed files,
  zero matches for MAC addresses, BSSIDs, SSIDs, private RFC1918 IPs, any IPv4 literal, UUIDs,
  emails, `/home/…` or `/Users/…` paths, or the strings `joseph`/`<gh-user>`/`seat-D`.
  Placeholders used are `<host>`, `<user>`, `<mac>`, `192.0.2.x`, `<client-project>`,
  `<client-db-container>`, `<public-dns>`, `<ethdev>` — all covered by
  `CLAUDE/ExampleValues.md`'s generic `<…>` row and the RFC 5737 row. Retained identifiers
  (`3-6`, `4-1.3.1`, `0000:00:14.0`, `PNP0C0D:00`, `ELAN0676`, `evdi card2/card3`, `r8152`,
  `slack`, UID 1000) are topology, ACPI/PCI model and package names — not identity.
  `**Owner**: joseph` (`PLAN.md:5`) matches 68 of 97 existing `PLAN.md` files, i.e. repo
  convention, not a leak.
- **The raw evidence is properly quarantined.** `triage-runs/20260908-123123/` does contain the
  real hostname and username; `git check-ignore -v` resolves it to `CLAUDE/Plan/.gitignore:16`
  (`*-runs/`) and it is absent from `git show --stat 4a37331`. R4 satisfied.
- **Plan-script standards**: R1 bootstrap verbatim incl. the `.git` bound and both shellcheck
  source directives; R2 `plan_require_host` with a reason; R7 `plan_mode gather` +
  `plan_gather_leg` as a bare top-level statement; R9 renders no verdict; R10 report written to
  `${PLAN_RUN_DIR}`; R11 no `|| true`, no `2>/dev/null`-to-silence, no `set +e`, no
  suppressions — and the SC2016 workaround at `probe-suspend.bash:17-20` is a genuine avoidance,
  not a suppression; R12 both files are `100755` in the index; R13 the only stderr write is the
  closing "Report written" line. Legs are external commands, not local functions. R3/R5/R6/R8
  correctly not exercised by a read-only gather. `shellcheck -x` and `bash -n` clean on both.
- **Facts I independently reproduced on the host and that hold**: F4 (`[s2idle]`;
  `total_hw_sleep 0` / `max_hw_sleep 523986009990`), F5 (`3-6` wakeup `enabled`, and it does
  currently enumerate as a keyboard), F9 (no `handle-lid-switch` inhibitor; every `sleep`
  inhibitor is `delay`), F10 (both types `'nothing'`, 900/900, `idle-delay uint32 0`), F11
  (single repo hit, `play-prevent-ssh-suspend.yml:59`; schema default is `'suspend'` per
  `/usr/share/glib-2.0/schemas/org.gnome.settings-daemon.plugins.power.gschema.xml`), F12, F13
  (all four interfaces present).
- **Epistemic hygiene**: F3 explicitly refuses to read the absent thermal trip as evidence of
  safe temperature, and P1–P3 are labelled as premises with confirm/refute conditions. That is
  the pattern this repo keeps failing at, done right here.
- **Plan workflow**: 187 lines (well under the ~350-line advisory), all template sections
  present, no time estimates, journal filename and `HH:MM · category · REF` grammar conform,
  `README.md` index row added in the same commit, no untracked `CLAUDE/Plan/` dirs. Plan Commit
  Rule satisfied.

## Mechanical gates

- **qa-all.bash: FAIL — but not from this commit.** 8 gating shellcheck warnings in
  `CLAUDE/Plan/00079-podman-container-control/unit-test-selection.bash:111` (last touched by
  `755a608`, pre-dating this commit), plus a ruff pin mismatch (`.ruff-version` 0.16.0, host
  `/usr/bin/ruff` 0.16.3). Both pre-existing. Flagged anyway because `PLAN.md:169` makes "QA
  passes" a Success Criterion this branch cannot currently satisfy — and the ruff mismatch is an
  IaC gap someone owns under CLAUDE.md "Missing Dependencies", not a thing to work around.
- **plan-qa --sweep: 2 advisories, 0 block.** Both are staleness/journal-freshness lists; 00104
  is not named.
- **ansible `--syntax-check`: not triggered** — no playbook changed in this commit. Same for
  `qa-helper-tests.bash`, `check_extension_compat` and the extension ESLint run: no `helpers/`,
  `tests/helpers/` or `extensions/` files in the diff.
