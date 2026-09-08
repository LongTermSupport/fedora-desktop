# QA Review — round 2, commit `70458b0` "Plan 00104: fix qa-reviewer BLOCK before deploying"

**Reviewer**: qa-reviewer (opus-5) · **Date**: 2026-09-08 · **Branch**: F44
**Round 1 report**: [260908-qa-reviewer-opus-5.md](260908-qa-reviewer-opus-5.md) (verdict BLOCK, on `34d9879`)

**Verdict**: FIX-BEFORE-MERGE — **DO NOT DEPLOY RIGHT NOW**

All ten round-1 findings are genuinely fixed; each was verified mechanically rather than by
reading the diff. The commit introduces no blocking defect. It does introduce four pieces of
drift and leaves one behavioural hole. And the working tree has diverged mid-review.

## 0. Read this first — the working tree is not the commit

`git status --short` was clean when this review started and is now:

```
 M playbooks/imports/play-suspend-and-lid-policy.yml     (+119 / -24)
```

A concurrent editor rewrote the play *during* this review. The working-tree version has 16
tasks: a preflight `slurp` of `/sys/power/state`, `meta: end_play`, a `stat`-based UPower gate
replacing the profile gate, a `gsettings get` schema probe with `failed_when: false`, and two
"Report that X was skipped" `debug` tasks. None of that is in `70458b0` and none of it is
reviewed.

Two consequences:

- The `--check --diff` run against the real profile reflects the **working tree**, not the
  commit, and was discarded. The `--check -e provisioning_profile=server` run *did* execute
  against the committed file — its 6-task output matches `70458b0` exactly — so finding 1's
  verification stands.
- `deploy.bash` runs `plan_ansible_playbook playbooks/imports/play-suspend-and-lid-policy.yml`,
  i.e. **the working tree**. Deploying now deploys unreviewed code.

One thing in that uncommitted work is flagged now because it is a HARD RULE and will otherwise
land: lines 109-114 and 196-202 are the prohibited *skip-and-warn* pattern — `CLAUDE.md`:
"❌ 'Skip and warn' pattern — NEVER use `debug` to warn and continue". Line 196 prints a
reassuring "Layers 1 and 2 are unaffected" whenever the gsettings probe returns non-zero **for
any reason**, so a transient session or D-Bus failure on a desktop silently drops layer 3 and
reports it as expected. That needs its own review pass.

## Verification of round-1 findings 1-10

| # | Status | Evidence |
| - | ------ | -------- |
| 1 | **Fixed** | `ansible-playbook play-suspend-and-lid-policy.yml --check -e provisioning_profile=server` → `TASK [Configure UPower to ignore lid] skipping: [localhost]`, `failed=0`, rc 0. The `restart-upower` **handler needs no separate gate**: a skipped task does not notify, and the handler did not appear in the server run's `RUNNING HANDLER` section. |
| 2 | **Fixed** | `--collect` present at `resuspend-aborted-suspend:136`; the call is wrapped in `if ! ...; then log ...` so a scheduling failure is reported instead of killing the hook under `set -e`. |
| 3 | **Fixed — trigger proven not to be a no-op** | See detail below. |
| 4 | **Fixed** | `diff <(imports from playbook-main.yml) <(numbered rows in architecture.md)` → identical **and in order**, 31 = 31. Set-diffed, not count-compared. |
| 5 | **Fixed** | Three-valued `lid_closed` (`yes`/`no`/`unknown`); `unknown` logs explicitly and exits without acting. `busctl … LidClosed` verified on this host as user and root → `b false`, agreeing with `/proc/acpi/button/lid/LID/state` → `open`. |
| 6 | **Fixed** | `rm -f "$COUNT"` removed from the give-up branch (`:117-124`). Only resets are the out-of-window path (`:77`) and the lid-open path (`:105`). See N4 for the consequence. |
| 7 | **Fixed** | `RESUSPEND_WINDOW=10` at `:36` with the F1 derivation written into the file. But see N1 — three documents still say 30. |
| 8 | **Fixed** | `99-suspend-wakeup-policy.rules:10-14` states the scope of the claim, names P2, and says the first-logged candidate was the USB tree it leaves armed. |
| 9 | **Fixed** | Unconditional `debug` gone; `warn-reboot-required` handler at `:136`, notified only by the logind `blockinfile`. On this host `/etc/systemd/logind.conf.d/laptop-lid.conf` already matches the block byte-for-byte, so the task is `ok` and the banner will **not** fire. |
| 10 | **Fixed** | F-range → F1-F17; Dependencies rewritten to state it does *not* touch `play-prevent-ssh-suspend.yml`; Decision 3 header carries `⚠️ SUPERSEDED by Decision 4` with a blockquote explaining which half survived. |

### Finding 3 in detail — the trigger genuinely re-applies the attribute

The displaylink precedent was not taken on trust.

1. `man udevadm` line 270: `--action` — "The default value is `change`". The rule has no
   `ACTION==` match, so it is eligible for a change event.
2. `strings /usr/bin/udevadm` contains
   `Running in test mode, skipping writing "%s" to sysfs attribute "%s"` — proving both that
   `udevadm test` is non-destructive (hence safe to run here) and that the real worker writes
   `ATTR{}` to sysfs during rule application.
3. Simulated the actual repo rule against the actual devices with
   `udevadm test --action=change -D <repo>/files/etc/udev/rules.d <syspath>`:
   - `AC` → `99-suspend-wakeup-policy.rules:23 ATTR{power/wakeup}="disabled": Running in test mode, skipping writing "disabled" to sysfs attribute "power/wakeup".`
   - `ucsi-source-psy-USBC000:001` and `:002` → same, from rule line 24
   - `BAT0` → no match from this rule file
4. `udevadm trigger --dry-run --verbose --subsystem-match=power_supply` enumerates exactly
   those four devices, so the selector reaches all three targets.

Current on-host state is `enabled` on all three, so the change is real and applicable now.

## Should fix (new, introduced or left by this commit)

### N1. `RESUSPEND_WINDOW` 30 → 10 was not propagated; three documents still say 30s

- `docs/playbooks.md:122` — "re-issues the suspend if it aborts within **30s**"
- `CLAUDE/Plan/00104-.../PLAN.md:83` — "a resume happens within **30s**"
- `CLAUDE/Plan/00104-.../PLAN.md:227` — Task 3.3, ticked ✅, "a resume lands within **30s**"

`docs/playbooks.md` is the user-facing doc and PLAN.md:227 is a *completed* task whose
description is now false. The commit changed a documented number and updated only the code.

### N2. `deploy.bash` still says the udev policy needs a reboot, contradicting the play

`CLAUDE/Plan/00104-.../deploy.bash:14-16`:

> "The udev policy applies to devices as they are re-added. So a reboot is required before the
> change is fully in effect"

versus `playbooks/imports/play-suspend-and-lid-policy.yml:145-146`:

> "(The udev wakeup policy does NOT need a reboot — the handler above runs `udevadm trigger`,
> so it applies immediately.)"

`deploy.bash`'s header is what the operator reads before typing `y` at the change gate. It is
also wrong about the reboot in general on this host: the logind drop-in already matches, so
`warn-reboot-required` will not fire and **no reboot is required at all** for this deploy.

### N3. Nothing verifies that the udev policy actually applied — and the banner asserts it did

`udevadm trigger` is invoked without `-w`/`--settle`
(`play-suspend-and-lid-policy.yml:128-134`), so it returns before udevd has processed the
events, and no task reads `/sys/class/power_supply/*/power/wakeup` back. The
`warn-reboot-required` handler states "so it applies immediately" as fact with no observation
behind it. This is the same dimension-C shape round 1 criticised in the udev *comment*, moved
into a *handler*.

**Fix**: add `--settle` to the trigger, and follow it with a handler that asserts
`power/wakeup` reads `disabled` on `AC` and both `ucsi-source-psy-*`.

### N4. The now-sticky give-up can leave layer 2 inert for exactly the incident it exists to catch

`COUNT` is cleared in only two places: a resume outside the window (`:77`) and an observed
**open** lid (`:105`). The operator's machine is docked lid-shut, so `:105` never fires.

1. Operator suspends deliberately, wakes from the external keyboard within 10s. Lid shut →
   hook re-suspends. Repeat → `COUNT` reaches 3, hook gives up.
2. `COUNT=3` persists in `/run` until a resume with `elapsed > 10`, or a reboot.
3. Later that day the *real* failure happens — suspend, unplug the dock, abort at 3s. The hook
   reads `attempts=3`, logs "giving up", and does nothing. The laptop cooks in the bag.

The stickiness is what round 1 asked for and the comment's reasoning is right; the missing half
is an expiry. **Fix**: in the `pre` branch, if the existing `$STAMP` is much older than the
window (the machine was awake a long time between suspend requests), `rm -f "$COUNT"` — a new
suspend after hours awake is a fresh incident, whereas the retry's own `pre` fires 5-8s later
and would not clear it.

### N5. The lid probe consults the live kernel state only as a fallback; the IPC path has a stale-read race and an unbounded timeout

`resuspend-aborted-suspend:86-100` asks logind first and only falls back to
`/proc/acpi/button/lid/*/state`. On this host `/proc/acpi/button/lid/LID/state` exists and is a
live ACPI read; logind's `LidClosed` is a cached property updated from an evdev `SW_LID` event.
On a lid-open wake, whether logind has processed that event by the time the `post` hooks run is
unverified — if it has not, `LidClosed` is still `true`, `elapsed` is a few seconds, and **the
hook re-suspends the machine the operator just opened**. Reversing the order removes both the
race and the IPC on any machine with the ACPI interface.

Secondary: `busctl` has a 25s default method-call timeout and is called from inside a hook that
`systemd-suspend.service` blocks on. If logind is not responsive mid-sleep-transition (not
verifiable without performing a suspend), the hook stalls the resume for up to 25s before
falling back. Add `--timeout=2` regardless of ordering, and confirm logind responsiveness from
inside the hook in Phase 4 rather than assuming it.

## Nits

- `resuspend-aborted-suspend:102-112` — the `case` has no `yes)` arm and no `*)` default;
  re-suspending is reached by *falling out of the case*. Correct today because `lid_closed`
  only takes three values, but the most dangerous branch should be the explicit one. Add
  `yes) ;;` and `*) log …; exit 0 ;;`.
- `:73` `elapsed=$(( $(date +%s) - slept_at ))` can be **negative** if the clock steps backwards
  across resume; `(( elapsed > RESUSPEND_WINDOW ))` is then false and the hook proceeds on
  nonsense data. Guard `(( elapsed < 0 ))` into the same branch as the unknown-lid case.
- `date` is the one unqualified command left (`:61`, `:73`) now that `logger`, `busctl`,
  `systemd-run` and `systemctl` are absolute.
- The `reload-udev-rules → trigger-udev` chain works only because `trigger-udev` is *defined
  after* its notifier in the handler list. That ordering is load-bearing and undocumented; one
  comment line would stop a future tidy-up breaking layer 1 silently.
- `RESUSPEND_DELAY=5` versus the other `post` hooks (`displaylink.sh` and `nvidia` are both
  present in `/usr/lib/systemd/system-sleep/`) is still an unverified residual from round 1.
  Phase 4 material.

## Checked and clean

- **`read_int_or_empty` under `set -e`.** Exercised the exact function in a sandbox against
  missing / empty / non-numeric / valid files: all four return rc 0, `[]`/`[12345]` as
  appropriate, `: "${attempts:=0}"` defaults correctly, the arithmetic survives, and the script
  reaches the end. Round-1 nit 1 is genuinely fixed, not merely commented.
- **busctl as root during resume.** `/usr/bin/busctl` exists; `sudo -n busctl get-property …
  LidClosed` → `b false`, matching `/proc`. Correct property, correct parse (`*true*` before
  `*false*`; output is `b true`/`b false`). The `if …; then` wrapper means a failure cannot trip
  `set -e`. Residual is the responsiveness/staleness question in N5, not the invocation.
- **Silently-inert paths in layer 2.** All exits enumerated: `$2` not a sleep verb (silent,
  correct); `$1` not pre/post (silent, correct); no usable stamp (**now logged**); `elapsed >
  window` (silent, correct — the normal wake); lid open (logged); lid unknown (**now logged**);
  attempts capped (logged); `systemd-run` failed (**now logged**). Only N4 leaves a path where
  layer 2 is off for a reason the operator would not expect.
- **UPower key validity.** `IgnoreLid` is a real, documented key in the shipped
  `upower-1.91.3-1.fc44` config (`IgnoreLid=false` currently present), so
  `regexp: '^IgnoreLid='` replaces a live line rather than appending a dead one.
- **Fail-fast.** No `failed_when:` / `ignore_errors:` / skip-and-warn anywhere in `70458b0`'s
  diff. (Both appear in the *uncommitted* rework — see §0.)
- **Public-repo safety.** Nothing personal in the three deployed files, `deploy.bash`, the docs
  hunks or the plan diff. Hardware IDs are generic ACPI identifiers.
- **Plan Commit Rule.** `PLAN.md`, `TRIAGE-EVIDENCE.md` and today's `JOURNAL/` entry all landed
  inside `70458b0`. The journal is honest — it records what was verified and how, and does not
  claim the deploy happened.
- **Naming.** `reload-udev` → `reload-udev-rules` now matches the sibling; `warn-reboot-required`
  and `trigger-udev` are action-named. No `: -x` patterns in unquoted task names.

## Mechanical gates

- **`scripts/qa-all.bash`**: rc=2 — the two pre-existing failures only
  (`CLAUDE/Plan/00079-podman-container-control/unit-test-selection.bash`, 8× SC2154; ruff pin
  0.16.0 vs installed 0.16.3). Neither file is in this commit. Bash discovery is now 200 files
  (was 199 — `deploy.bash`), and neither the hook nor `deploy.bash` produced a finding.
- **`hooks-daemon plan-qa --sweep`**: rc=1, 0 block / 2 advise. Both are repo-wide
  staleness/journal nags; Plan 00104 appears in neither list.
- **`ansible-playbook --syntax-check`**: `play-suspend-and-lid-policy.yml` rc=0,
  `playbook-main.yml` rc=0. `bash -n` on the hook: clean.
- **Conditional gates — checked, not triggered**: no `helpers/` or `tests/helpers/` change →
  `qa-helper-tests.bash` not required; no `extensions/` change → `check_extension_compat` and
  ESLint not required; no `files/var/local/claude-yolo/**` change → no `CCY_VERSION` /
  `REQUIRED_CONTAINER_VERSION` bump required.

## Deploy call: DO NOT DEPLOY RIGHT NOW

One procedural reason, decisive on its own:

**`deploy.bash` runs the working tree, and the working tree is 119 uncommitted lines away from
the reviewed commit.** What would land on the laptop is not what passed this review, has not
passed `qa-all.bash`, and contains at least one HARD RULE violation (the skip-and-warn `debug`
tasks). Commit it, then have it re-reviewed.

If the tree is restored to `70458b0`, the risk profile for this operator (docked, lid shut,
external monitors) is:

**Safe:**

- **Layer 1 (udev)** — verified by simulation to apply to exactly the three intended devices and
  nothing else. Trivially reversible (delete the rule, re-trigger). The only capability lost is
  "plug in mains to wake", now documented.
- **The logind drop-in** — already byte-identical on this host, so a no-op, and **no reboot is
  required**, contrary to `deploy.bash`.
- **`restart-upower`** — will fire (host currently has `IgnoreLid=false`). Restarting upower
  under a live GNOME session briefly re-enumerates the battery indicator. Low impact.

**Behaviour change to accept knowingly:**

- **Layer 3** — `sleep-inactive-battery-type` goes `nothing` → `suspend` at the existing 900s.
  On battery, an idle machine suspends after 15 minutes. That is GNOME's default and the plan's
  intent, but it is new behaviour on this laptop.

**The one carrying real operational risk on this exact profile: layer 2.**

- The 10s window is bounded and the attempt cap is 3, so worst case the operator fights it for
  ~15s and it stops. Tolerable on its own.
- But **N5** means a lid-open wake within 10s could be re-suspended off a stale `LidClosed`, and
  **N4** means a single false-positive burst can leave layer 2 inert for the next genuine
  incident with nothing but a `logger` line to say so. Both are cheap to fix before deploying,
  and both are things that would only be discovered the hard way.

**Recommended order**: commit and re-review the working tree; fix N1-N5; then deploy. Layers 1
and 3 are independently safe, but the play deploys all three together and splitting it by hand
is not an option under strict IaC.
