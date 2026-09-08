# QA Review — Plan 00104, round 6, `fd5ae42..45f2b9f` (single commit `45f2b9f`)

**Reviewer**: qa-reviewer (opus-5) · **Date**: 2026-09-08 · **Branch**: F44
**HEAD**: `45f2b9f`
**Rounds**: [1](260908-qa-reviewer-opus-5.md) BLOCK · [2](260908-qa-reviewer-opus-5-round2.md) FIX-BEFORE-MERGE · [3](260908-qa-reviewer-opus-5-round3.md) BLOCK · [4](260908-qa-reviewer-opus-5-round4.md) FIX-BEFORE-MERGE · [5](260908-qa-reviewer-opus-5-round5.md) FIX-BEFORE-MERGE (PLAN.md only)

**Working tree**: `git status --short` is **empty**. This reviews exactly what would deploy.

**Verdict**: **PASS WITH NITS** — two documentation/plan lines to fix. **The machine-facing deliverable is clean: DEPLOY.**

**On the headline question: the allow-list is correct and changes nothing on any conformant host. Not a blocker.**

## Blocking

**None.**

## The allow-list — settled against the kernel, not against this machine

Per the kernel's own documented ABI, no legitimate third value exists.

`Documentation/ABI/testing/sysfs-devices-power` (fetched from `torvalds/linux` master):

> Such devices have **one of the following two values** for the sysfs power/wakeup file:
> + `"enabled\n"` to issue the events;
> + `"disabled\n"` not to do so;
> For the devices that are **not capable** of generating system wakeup events **this file is not present**.

That is precisely the shape `evaluate()` now implements — two words, plus `FileNotFoundError -> omitted` in `cli.py:66-67` for the not-capable case. The contract and the code agree exactly.

There *is* a third literal in the implementation. `drivers/base/power/sysfs.c:323-329`:

```c
static ssize_t wakeup_show(struct device *dev, struct device_attribute *attr, char *buf)
{
	return sysfs_emit(buf, "%s\n", device_can_wakeup(dev)
			  ? (device_may_wakeup(dev) ? _enabled : _disabled)
			  : "");
}
```

The `""` branch requires `device_can_wakeup(dev)` false **while the attribute is still present** — and the attribute's presence is gated on exactly that predicate at both ends: `dpm_sysfs_add()` (`sysfs.c:709`, `if (device_can_wakeup(dev)) sysfs_merge_group(&pm_wakeup_attr_group)`) and `device_set_wakeup_capable()` (`wakeup.c:471-487`, which sets `can_wakeup` then immediately calls `wakeup_sysfs_remove()`). The only window is the handful of instructions between those two statements, on a device toggling wakeup capability at that instant. Not reachable for `AC` (ACPI0003) or the UCSI source PSYs, which do not toggle capability.

Measured corroboration: **75** `power/wakeup` attributes across `/sys/class/*/*` and `/sys/bus/*/devices/*` on this host — **46 `'disabled\n'`, 29 `'enabled\n'`, nothing else**. Zero empty, zero garbage.

**The allow-list produces byte-identical behaviour to the deny-list on every host that follows the documented ABI, and is strictly safer on one that does not.**

**The tests discriminate** — I reinstated the deny-list implementation in memory and re-ran both modules:

```
DENY-LIST mutation: ran=30 failures=8 errors=0
   TestEvaluate.test_unrecognised_value_is_unverifiable_not_disarmed  (all 6 subTests)
   TestSummary.test_summary_distinguishes_still_armed_from_unverifiable
   TestMain.test_exits_nonzero_on_a_value_it_cannot_interpret
NO-EXISTS-GUARD mutation: ran=12 failures=1
   test_a_dangling_device_symlink_is_unreadable_not_dropped
```

Every new assertion fails against the code it replaced. None are tautological.

## The `unreadable` -> `unverifiable` rename — the call was right

I disagree with round 5's suggestion, not with the author.

- **The word is more accurate, and accuracy is this plan's entire subject.** `UNREADABLE: AC` on a device that read cleanly and returned `potato` is a false statement about the measurement. Six rounds of this review have been about exactly that failure class; folding a new case into a name that misdescribes it would have been the cheap fix, not the correct one.
- **`unverifiable` still covers the original `None` case** without loss — a read that failed is also a state we cannot vouch for. The superset relation holds, so nothing was mis-labelled by widening it.
- **The rename is complete.** Grepped the whole repo (`--include=*.py,*.yml,*.md,*.bash,*.sh`, excluding `.git` and `subagent-reports/`): every surviving `unreadable`/`UNREADABLE` hit is an unrelated file, prose about the genuinely-unreadable case, or a test method name that still describes a `None` fixture accurately. `docs/playbooks.md:136` is a different sentence (the GNOME schema).
- **No consumer parses the COVERAGE line.** `play-suspend-and-lid-policy.yml:269-272` passes `wakeup_coverage.stdout` straight to `debug`; `changed_when: false`; the verdict travels by exit code. `deploy.bash:22,48` mentions the line in prose only. Nothing anywhere greps `UNREADABLE`. Zero blast radius.

Churn cost was ~10 lines in one module plus its tests. Worth it.

## The `device.exists()` guard — correct on real sysfs, including the odd cases

Probed against real filesystems, not reasoned about:

| case | result | verdict |
| --- | --- | --- |
| real `/sys/class/power_supply` (all 4 entries are symlinks) | `{'AC': 'enabled\n', 'ucsi-source-psy-USBC000:001': 'enabled\n', 'ucsi-source-psy-USBC000:002': 'enabled\n'}` — `BAT0` correctly omitted | correct; `exists()` follows the class symlink to the device dir |
| device dir mode `000` (EACCES on the attribute) | `{'AC': None}` | correct — a real fault still fails |
| base dir `r--` (EACCES on `stat` of children) | `{'AC': None}`, **no traceback** | `Path.exists()` in 3.12+ swallows all `OSError`, so this degrades to unverifiable rather than crashing |
| self-referential symlink (`ELOOP`) | `{'AC': None}` | correct |
| dangling symlink | `{'AC': None}` — the round-5 nit, now fixed | correct |
| `/sys` absent or masked (container) | `base.is_dir()` false -> `{}` -> `COVERAGE: 0 of 0`, exit 0 | correct; and moot, since Ansible never runs in the CCY container |

`python3 -m helpers.suspend_wakeup.cli` on the live host: `COVERAGE: 0 of 3 power-delivery devices disarmed — STILL ARMED: AC, ucsi-source-psy-USBC000:001, ucsi-source-psy-USBC000:002`, rc 1. Genuine before/after still available.

## Round-5 R1 — genuinely fixed, verified against the play

`ansible-playbook --list-tasks`, 19 tasks:

```
 9  Fail if systemd has no system-sleep hook directory     <- end of preflight
...
15  Probe for the GNOME power schema                       <- layer 3, as PLAN.md now says
18  Verify the power-delivery wakeup policy applied
19  Report wakeup policy coverage
```

`PLAN.md:146-149` now reads "The GNOME schema probe **sits with the layer-3 task it gates, not in preflight**". Matches the measured play. Fixed, not merely edited.

## Should fix

### S1. `PLAN.md:162` — the test counts were corrected to numbers this same commit made stale. Round-5 R2, third instance

```
- [x] ✅ 25 unit tests, stdlib `unittest`; `./scripts/qa-helper-tests.bash` green (250 tests)
```

Measured at `45f2b9f`:

```
tests/helpers/suspend_wakeup/test_core.py 18
tests/helpers/suspend_wakeup/test_cli.py  12
python3 -m unittest ... test_core test_cli   -> Ran 30 tests ... OK
./scripts/qa-helper-tests.bash               -> Ran 255 tests ... OK
```

**30 and 255, not 25 and 250.** This commit added 3 `test_core` methods and 2 `test_cli` methods; `250` was the pre-commit total. Worse, the journal entry committed in the *same commit* says "helper tests **255** (was 250)" — so `PLAN.md` contradicts its own journal inside one commit again, verbatim what round 5 flagged.

**Fix**: `30 unit tests … green (255 tests)`. Durable lesson: a hardcoded count on a ✅ line goes stale on the commit that fixes it — either re-measure at commit time or drop the suite total.

### S2. `docs/playbooks.md:138-140` — the third abort case no longer describes what aborts

```
- the wakeup policy did not apply, or a targeted device's `power/wakeup` exists but cannot be
  read. This check runs **last**, deliberately, …
```

This commit widened that condition twice and the bullet did not move with it. `Result.ok` (`core.py:55`) is now false when a target's value is **read successfully but not recognised** (`core.py:98-99`), and when the **device entry does not resolve** (`cli.py:61-63`). Neither is "exists but cannot be read" — by the author's own argument for the rename, "unreadable" is the wrong word for a device that read fine and said `potato`. Round-4 F5 exists because this bullet is the only place a reader learns what will abort their provisioning run.

**Fix**: `…or a targeted device's power/wakeup could not be read, held a value that is neither enabled nor disabled, or its device entry no longer resolves.`

## Nits

- **`tests/helpers/suspend_wakeup/test_core.py:11` still carries `(Plan 00104, round-3 finding B2)`.** The commit removed the review-history references from `core.py:6` and `test_cli.py:11` and left the third instance six lines away in the sibling file. That is the "fixed the instance, never asked what else has the same shape" pattern. Bare `Plan 00104` provenance elsewhere is fine; the *round number* is the artefact.
- **`test_cli.py` `TestMain` docstring says "four stray COVERAGE lines"** — the class now has five test methods, all of which call `main()`. The sentence also describes a state that no longer exists (`R-COMMENT-CHANGELOG` shape). Say what the capture is *for*, not what the output used to look like.
- **`cli.py:62` comment is narrower than its branch.** `# entry does not resolve — cannot vouch for it`. Measured: a base dir with `r--` (EACCES on `stat`) also lands here, and that entry resolves perfectly well. The verdict is right in both cases; the comment describes only one.
- **New failure mode, deliberate and worth recording.** A `ucsi-source-psy-*` entry that vanishes between `sorted(base.iterdir())` and its `stat()` — a USB-C disconnect landing in that microsecond window — now yields `UNVERIFIABLE` -> exit 1 -> `any_errors_fatal` abort, where the old code silently dropped it. This is the correct fail-fast trade (a silent under-match is worse than a loud re-runnable failure, and the abort lands after all three layers are installed), but it is a behaviour change the round-5 nit did not spell out. Not worth changing.

## Whole deliverable, stepping back

**The shape is good. I looked specifically for accumulated structural oddity and did not find any.**

- **IaC placement**: no new play. Layers 1–3 and their verification live in the one play that owns suspend policy, imported from `playbook-main.yml` adjacent to `play-prevent-ssh-suspend.yml`. No ordering dependency claimed or needed.
- **Two independent selectors still agree.** The udev rule (`99-suspend-wakeup-policy.rules:23-24`, `KERNEL=="AC"` / `KERNEL=="ucsi-source-psy-*"`) and `core.is_policy_target` (`_EXACT_TARGETS={"AC"}` + `startswith("ucsi-source-psy-")`) select the identical three devices on this host, by different mechanisms. The glob and the prefix test are semantically equivalent (udev's `*` matches zero or more).
- **The verification states its population, its buckets and its coverage** — `COVERAGE: n of m`, `STILL ARMED:`, `UNVERIFIABLE:`, and words rather than a bare pass at `n=0`. A gate whose only visible output is a failure is indistinguishable from one that never ran; this one always prints. That is the single most-recurrent defect in this repo's history, closed properly here.
- **Fail-fast**: one annotated exemption (`:195`, genuine probe-then-fail, rc consumed at `:211` and `:235`). No `|| true`, no skip-and-warn, no existence-as-generated guard. `evaluate` has no path that reports clean without evidence.
- **Helper split** matches `helpers/CLAUDE.md`: pure `core` + thin `cli`, stdlib only, namespace package, `command:` + `argv:` + `chdir: root_dir`, payload on stdout.
- Nothing reads as over-engineered. Three layers, one play, one helper, one rule file, one hook.

## Checked and clean

- **Public-repo safety**: `git diff -U0 fd5ae42..45f2b9f` scanned for `/home/<user>` paths, emails, RFC1918 addresses, UUIDs and `.local/.lan/.home` hostnames -> **zero hits**. Device names (`USBC000:001`, `ACPI0003`) are generic ACPI identifiers.
- **CCY version bump**: `git diff --stat fd5ae42..45f2b9f -- files/var/local/claude-yolo/` is **empty** -> no `CCY_VERSION` / `REQUIRED_CONTAINER_VERSION` bump required.
- **Plan Commit Rule**: code, `PLAN.md`, `JOURNAL/`, `docs/` and the round-5 report all landed in `45f2b9f`. Working tree empty. `CLAUDE/Plan/README.md:37` carries the index row. Tasks 4.1/4.2/4.3 correctly still unticked; `**Status**: In Progress` is honest.
- **Stray-output claim**: `qa-helper-tests.bash` now ends `Ran 255 tests … OK` with **no** trailing `COVERAGE:` lines.
- **Play task-name parse**: no unquoted `: -x` pattern; `--syntax-check` clean on the play and `playbook-main.yml`.
- **Check-mode banner**: `--check` run confirms `Report wakeup policy coverage` skipping, matching the corrected banner text at `:322-323`.

## Mechanical gates

`qa-all.bash` **rc=2**, short-circuiting at `qa-python`'s ruff pin (`expected 0.16.0 (.ruff-version), found 0.16.3`) — pre-existing and out of scope per the brief, so each gate was run individually.

| gate | rc | note |
| --- | --- | --- |
| `qa-ansible.bash` | **0** | |
| `qa-ansible-syntax.bash` | **0** | |
| `qa-docs.bash` | **0** | |
| `qa-patterns.bash` | **0** | |
| `qa-bash.bash` | 1 | **only** `CLAUDE/Plan/00079-…/unit-test-selection.bash` 8× SC2154 — pre-existing, outside the range |
| `qa-python.bash` | 2 | ruff pin, pre-existing. `ruff 0.16.3 check helpers/suspend_wakeup tests/helpers/suspend_wakeup` with repo config -> **All checks passed!** |
| `qa-helper-tests.bash` | **0** | **Ran 255 tests, OK** (16 modules) |
| `plan-qa --sweep` | 1 | 0 block / 2 advise, both repo-wide nags (staleness list, journal-freshness). **00104 appears in neither.** |
| `ansible-playbook --syntax-check` | **0** | play + `playbook-main.yml` |
| `--check --diff` full play | **0** | `ok=13 changed=4 failed=0 skipped=6` — unchanged from round 5 |
| `--list-tasks` | — | 19 tasks; verification at 18–19; probe at 15 |

**Conditional gates, stated explicitly**: `qa-helper-tests.bash` **required** (`helpers/` and `tests/helpers/` both in the range) -> run, green. `check_extension_compat` and `eslint` **not required** — no `extensions/` change in the range (`git diff --stat` over that path is empty).

## DEPLOY call — **DEPLOY**

The machine-facing deliverable is clean, without hedging. The allow-list is correct against the kernel's documented ABI and changes nothing on a conformant host; the rename is complete and the better call; the `exists()` guard behaves correctly on real sysfs and adds no crash path; round 5's R1 is genuinely fixed against the measured play.

The two Should-fix items are sentences — one in `PLAN.md`, one in `docs/`. Fix them, but do not gate the deploy on them.

Unchanged from round 5, and still the only things that close this plan:

1. `TASK [Report wakeup policy coverage]` must flip from `COVERAGE: 0 of 3 … STILL ARMED` to `COVERAGE: 3 of 3 power-delivery devices disarmed`.
2. Confirm out of band: `cat /sys/class/power_supply/{AC,ucsi-source-psy-*}/power/wakeup` -> three `disabled`.
3. Task 4.2's reproduction (suspend -> unplug the dock within ~3 s -> lid closed) has not been executed on the host by anything in this range.
