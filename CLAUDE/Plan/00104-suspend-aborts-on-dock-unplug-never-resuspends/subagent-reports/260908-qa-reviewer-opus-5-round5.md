# QA Review — round 5, `40e05b5..302344f` (single commit `302344f`)

**Reviewer**: qa-reviewer (opus-5) · **Date**: 2026-09-08 · **Branch**: F44
**HEAD**: `302344f29b670fcc61516702eedc951172c9ad9c`
**Rounds**: [1](260908-qa-reviewer-opus-5.md) BLOCK · [2](260908-qa-reviewer-opus-5-round2.md) FIX-BEFORE-MERGE · [3](260908-qa-reviewer-opus-5-round3.md) BLOCK · [4](260908-qa-reviewer-opus-5-round4.md) FIX-BEFORE-MERGE

**Working tree**: `git status --short` is **empty**; HEAD is `302344f`. This reviews exactly what would deploy.

**Verdict**: **FIX-BEFORE-MERGE** on `PLAN.md` only. **The machine-facing deliverable is clean — DEPLOY.**

All four round-4 findings are genuinely fixed, verified mechanically. I found **no defect in any artefact that touches the machine**. The two real findings are both false statements in `PLAN.md` on ticked ✅ items, one of them introduced as the *fix* for round-4 F4.

## Blocking

None.

## Should fix (neither affects the deploy)

### R1. `PLAN.md:146-149` states the GNOME probe runs in the preflight block. It does not — this is round-4 F4's defect class, re-instantiated in the same bullet that fixed it

> "Preflight block establishes measured preconditions: sleep capability, upower presence, the systemd sleep-hook directory. **The GNOME schema probe also runs there**, but its **hard fail** is deliberately later…"

Measured via `ansible-playbook --list-tasks`, the probe is task **15 of 19**:

```
Check whether the kernel exposes sleep states          <- preflight
...
Fail if systemd has no system-sleep hook directory     <- end of preflight
Configure logind for laptop lid behavior               <- WRITE
Configure UPower to ignore lid                         <- WRITE
Disarm power-delivery devices as wakeup sources        <- WRITE
Apply pending udev changes before verifying them
Deploy the aborted-suspend recovery hook               <- WRITE
Probe for the GNOME power schema                       <- "runs there"?
```

In the file, `Probe for the GNOME power schema` is at `playbooks/imports/play-suspend-and-lid-policy.yml:174`, under the `# ---- Layer 3 ----` header at `:165`, four section headers past the preflight block (`:30-98`) and after four mutating tasks.

This is not cosmetic. The bullet's entire purpose is to record that preconditions are measured before anything is written. The *fail* placement is deliberate and defensible, and the plan now says so correctly — but the *probe* placement is misdescribed, so a reader trusting the tick believes a safety property the play does not have. That is precisely what round-4 F4 said, about the same bullet.

**Fix**: "The GNOME schema probe sits with the layer-3 task it gates, not in preflight, so both the probe and its hard fail land beside the thing that could not be set."

### R2. `PLAN.md:162` test counts are stale — this range added 10 tests and did not update them

> "- [x] ✅ **15 unit tests**, stdlib `unittest`; `./scripts/qa-helper-tests.bash` green (**240 tests**)"

Measured: `grep -c "def test_"` → `test_core.py:15`, `test_cli.py:10` = **25**; `qa-helper-tests.bash` → **"Ran 250 tests … OK"**. The journal entry committed in the *same commit* says "helper tests ✓ (250, up from 240)" — so `PLAN.md` contradicts its own journal inside one commit.

## Nits

- **`docs/playbooks.md:126` "on every run" was named in round-4 F5 and not fixed.** The COVERAGE task carries `when: not ansible_check_mode` (`:266`, `:271`); the check-mode run confirms `TASK [Report wakeup policy coverage] skipping`. Also the documented format `COVERAGE: n of m power-delivery devices disarmed` is not what a zero-population host prints (`COVERAGE: 0 of 0 — no power-delivery wakeup devices on this host`). Same applies to the reboot banner's new "the COVERAGE line … appears LATER in the output" under `--check`.
- **`core.py:86` treats *anything* that is not `enabled` as disarmed.** Measured against fixtures: `''`, `'   \n'`, `'\n'`, `'potato\n'`, `'ENABLED\n'` all print `COVERAGE: 1 of 1 power-delivery devices disarmed`, exit 0. That is the module's own stated failure mode ("reporting blind as clean", `core.py:10-11`) surviving at per-device granularity. **Honest reachability**: I read all 56 `power/wakeup` attributes on this host — 36 `disabled\n`, 20 `enabled\n`, zero anything else. Not reachable on measured hardware. The safe shape is allow-list `disabled` → disarmed, `enabled` → armed, anything else → `unreadable`.
- **A dangling device symlink is now silently dropped from the population.** Fixture: `os.symlink(T/"nowhere", T/"AC")` → `read_wakeup_states` returns `{}` and the COVERAGE line never mentions `AC`. Round 3 recorded this fixture as `unreadable`; the `FileNotFoundError` split changed it. Defensible (a vanished device has nothing to disarm) and unreachable in real sysfs, but it is an under-match, and under-matches are silent.
- **`cli.main()` prints unconditionally in tests**, so `qa-helper-tests.bash` output now interleaves four stray `COVERAGE:` lines after `OK` — they read like findings. `contextlib.redirect_stdout` in `TestMain` would fix it.
- **`Apply pending udev changes before verifying them` (`:152`) understates what it does.** `meta: flush_handlers` flushes *all* pending handlers; the check-mode run shows `RUNNING HANDLER [restart-upower]` firing there. The name says "udev changes".
- **Review history is embedded in permanent source files**, including a bare commit hash (`core.py:6` — "Plan 00104, `80024ce`") and `test_cli.py:11` — "(Plan 00104, round-4 finding 2)". That is the `R-COMMENT-CHANGELOG` shape. **I checked the density claim before making it**: at 35% comment the play is *within* repo norms (median 17% across 26 `imports/*.yml`, with `play-ZZ-repo-cleanup` 55%, `play-mask-intel-lpmd` 47%, `play-systemd-user-tweaks` 45% above it). Volume is fine; the commit hash and round number are the nit.
- **`force_handlers` is unset in `ansible.cfg`**, so a failure between the `notify: restart-upower` (`:133`) and the flush (`:153`) loses that restart permanently — on re-run `lineinfile` reports `ok` and never re-notifies. Repo-wide Ansible property, negligible blast radius here (a `copy` of a repo file would have to fail), and not made worse by this range.
- Two unconditional `debug` tasks still print on every `playbook-main.yml` run (`:69`, `:268`). Carried nit from rounds 3 and 4.

## Round-4 findings 1–4: verified mechanically

| # | Status | Evidence |
| - | ------ | -------- |
| **F1** verification gates layers 2/3 | **Fixed** | `--list-tasks` shows `Verify the power-delivery wakeup policy applied` and `Report wakeup policy coverage` as tasks **18 and 19 of 19**, after `Deploy the aborted-suspend recovery hook` and all three GNOME tasks. **Partial-state analysis**: a verification failure now occurs with the logind drop-in, `UPower.conf`, the udev rule, the recovery hook and `sleep-inactive-battery-type` all already applied — nothing dangerous is left half-done, and `deploy.bash` would report red for a re-runnable reason. `meta: flush_handlers` at `:153` is still correctly placed and still load-bearing (handlers otherwise flush *after* all tasks, i.e. after the verification). No handler is notified after the flush, so nothing runs behind the verification. |
| **F2** `FileNotFoundError` vs `OSError` | **Fixed, and attacked** | See the four attacks below. |
| **F3** banner wording vs output order | **Fixed** | `:318-321` now reads "The COVERAGE line reporting it appears **LATER** in the output: this banner is a handler flushed mid-play, the verification is one of the last tasks." Matches the measured order. (Check-mode caveat in nits.) |
| **F4** PLAN.md Task 3.5 | **Half fixed** — the false "both hard preconditions moved into preflight" is gone and the new "Verification runs last" bullet is accurate. A **new** false statement replaced it — **R1**. |
| **F5** docs three abort paths | **Fixed** | `docs/playbooks.md:128-138` lists three; the play has exactly three deliberate abort tasks (`:92`, `:223`, `:252`). |

### F2 attacked — four hostile cases, all measured

| case | result | verdict |
| --- | --- | --- |
| device dir with **no `power/` subdir at all** (`ucsi-source-psy-USBC000:001`) | omitted from mapping | correct |
| **EACCES** on `power/wakeup` (mode `000`, target device) | `{dev: None}` → `COVERAGE: 0 of 1 … UNREADABLE: …`, **exit 1** | correct — a real fault still fails |
| **dangling symlink** device entry | omitted | see nit |
| `power/wakeup` is a **directory** (the test fixture) | `IsADirectoryError`, `isinstance(e, FileNotFoundError) → False`, `isinstance(e, OSError) → True` on Python 3.14.7 | the fixture **does** exercise the intended branch on this platform |
| empty / whitespace / garbage attribute value | counted as disarmed, exit 0 | see nit |

### Are the 10 new `test_cli` tests meaningful?

**Proven discriminating, not asserted.** I reinstated the pre-fix `except OSError: states[name] = None` implementation in memory and re-ran `tests.helpers.suspend_wakeup.test_cli`:

```
with the pre-fix implementation: ran=10 failures=4 errors=0
  FAILS: test_exits_zero_when_a_target_device_is_not_wakeup_capable
  FAILS: test_a_target_device_without_the_attribute_is_also_omitted
  FAILS: test_device_without_a_wakeup_attribute_is_omitted_entirely
  FAILS: test_mixed_host_is_classified_correctly
```

Four fail against the defect; `test_unreadable_attribute_is_reported_as_None` passes both ways *by design* — it pins the opposite over-correction (`except OSError: continue`), which is exactly the guard that should exist. None are tautological.

### Does a GNOME probe failure now suppress the verification?

Yes, and it is acceptable, but the play's own principle is applied asymmetrically. `Fail if the GNOME power schema is unreadable` (`:223`) precedes the verification, so on a host where the probe fails the run ends and no COVERAGE line is printed. At that point layers 1, 2 and 3-as-far-as-possible are all deployed, so nothing dangerous is left partial, and the run is **red** — this is not the "green having verified nothing" failure mode. The residue is that the play states "Verification must never be able to prevent the fix it is verifying" (`:240-241`) while a layer-3 fault does prevent layer-1's verification. Diagnostic loss only; not worth restructuring.

## Whole deliverable, stepping back

I looked for structural oddity left by four rounds of incremental fixing and did not find it. Specifically:

- **Placement in the IaC graph is right.** No new play. Layers 1–3 live in the one play that owns suspend policy; verification lives in the play that owns the udev rule; `playbook-main.yml:8-12` imports it adjacent to `play-prevent-ssh-suspend.yml` with the comment correctly stating this is readability, not an ordering dependency.
- **Layer 1 and the helper's population agree, proven.** `sudo udevadm test --action=change -D <repo>/files/etc/udev/rules.d` against the four live devices: `AC` matches rule line 23, both `ucsi-source-psy-USBC000:00{1,2}` match line 24, `BAT0` matches neither. The helper independently enumerates **exactly those three** (`COVERAGE: 0 of 3 … STILL ARMED: AC, ucsi-source-psy-USBC000:001, ucsi-source-psy-USBC000:002`, exit 1). Two independent selectors, same three devices. `udevadm verify` → Success 1, Fail 0.
- **The helper split is right.** Pure `core` + thin `cli`, stdlib only, namespace package, `command:` + `argv:` + `chdir: root_dir`, COVERAGE to stdout as the payload — matches `helpers/CLAUDE.md` and the `helpers/pyenv` precedent.
- **Fail-fast.** One annotated exemption in the play (`:194`), trailing-form, genuine probe-then-fail with the rc consumed by the two tasks below. No `|| true`, no skip-and-warn, no existence-as-generated guard. The hook's `read_int_or_empty` still validates content, not existence.
- **Comment density is within repo norms** — I measured it rather than asserting it (see nits).
- Nothing about the design reads as over-complicated for what it does. Three layers, one play, one helper, one hook, one rule file.

## Public-repo safety

`git diff -U0 40e05b5..302344f` grepped for home paths, emails, RFC1918/private IPs and UUIDs → **no hits**. Device names (`USBC000:001`, `ACPI0003`) are generic ACPI identifiers.

## Plan Commit Rule

Clean. `PLAN.md`, `JOURNAL/00104-Journal-26-09-08.md`, `docs/playbooks.md`, the code and the round-4 report all landed inside `302344f`. Working tree empty. `CLAUDE/Plan/README.md:37` carries the index row. Tasks 4.1/4.2/4.3 correctly still unticked.

## Mechanical gates

Run individually — `qa-all.bash` still exits at `:43` on `qa-python`'s rc=2 (pre-existing ruff pin), per the brief.

| gate | rc | note |
| --- | --- | --- |
| `qa-ansible.bash` | **0** | fail-fast OK; 75 playbooks shebang+exec |
| `qa-ansible-syntax.bash` | 0 | |
| `qa-patterns.bash` | 0 | 200 files |
| `qa-docs.bash` | 0 | |
| `qa-bash.bash` | 1 | **only** `CLAUDE/Plan/00079-…/unit-test-selection.bash` 8× SC2154 — pre-existing, outside the range |
| `qa-python.bash` | 2 | ruff pin 0.16.0 vs installed 0.16.3 — pre-existing. Ran `ruff 0.16.3 check helpers/suspend_wakeup tests/helpers/suspend_wakeup` with repo config → **All checks passed** |
| `qa-helper-tests.bash` | 0 | **Ran 250 tests, OK** |
| `plan-qa --sweep` | 1 | 0 block / 2 advise; both repo-wide nags (staleness list, journal-freshness for Plan 00163). 00104 in neither |
| `ansible-playbook --syntax-check` | 0 | `play-suspend-and-lid-policy.yml`, `playbook-main.yml` |
| `--check --diff` full play | 0 | `ok=13 changed=4 failed=0 skipped=6` |
| `udevadm verify` on the rule | 0 | Success 1, Fail 0 |

**Conditional gates, stated:** `qa-helper-tests.bash` **required** (`helpers/` + `tests/helpers/` in the range) → run, green. `check_extension_compat` and ESLint **not required** — no `extensions/` change. No `files/var/local/claude-yolo/**` change → **no `CCY_VERSION` / `REQUIRED_CONTAINER_VERSION` bump required** (confirmed by `git diff --stat` over those paths, empty).

## DEPLOY call — **DEPLOY**

The machine-facing deliverable is clean. Nothing I found this round can harm this laptop, and the two findings are sentences in a plan file that no machine reads. Fix R1 and R2 (a two-line edit each) — but do not gate the deploy on them.

Measured pre-deploy state, so you have a real before/after:

- `/etc/udev/rules.d/99-suspend-wakeup-policy.rules` — **absent**
- `/usr/lib/systemd/system-sleep/` — only `displaylink.sh` and `nvidia`; the hook is **absent**
- `AC`, `ucsi-source-psy-USBC000:001`, `ucsi-source-psy-USBC000:002` — all read `enabled`; `BAT0` has **no** `power/wakeup` (the F2 case, now handled)
- `/etc/UPower/UPower.conf:39` — `IgnoreLid=false` → will change, **`restart-upower` will fire** (brief battery-indicator re-enumeration under the live session)
- `/etc/systemd/logind.conf.d/laptop-lid.conf` — present and matching; check run reports the task `ok`, so **`warn-reboot-required` will not fire and no reboot is needed**
- `sleep-inactive-battery-type` `'nothing'` at 900s → becomes `suspend`. **Accept knowingly**: on battery, idle 15 min now suspends. GNOME's default, the plan's intent, new on this laptop.

**Watch, in order:**

1. `TASK [Report wakeup policy coverage]` must print `COVERAGE: 3 of 3 power-delivery devices disarmed`. It reads `0 of 3 … STILL ARMED` right now, so this is a genuine before/after, not a tautology. If it fails, the run stops **after** all three layers are installed — you are protected, just re-run.
2. Confirm out of band: `cat /sys/class/power_supply/{AC,ucsi-source-psy-*}/power/wakeup` → three `disabled`.
3. `journalctl -t resuspend-aborted-suspend --no-pager` across the first few deliberate suspends — especially a wake from the external keyboard **inside 10 s with the lid shut**, which is the false-positive case the window bets against. Worst case is three re-suspends over ~15–20 s, then it gives up and logs why.
4. `/run/resuspend-aborted-suspend.count` should be **absent** after any normal long sleep. If it ever holds `3`, layer 2 is dormant until a normal resume or a suspend more than 600 s after the previous one.
5. Task 4.2's reproduction (suspend → unplug the dock within ~3 s → lid closed) is still the only thing that closes this plan. Nothing in this range has executed on the host.
