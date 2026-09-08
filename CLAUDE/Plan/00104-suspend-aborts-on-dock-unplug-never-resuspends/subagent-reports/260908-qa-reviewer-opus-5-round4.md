# QA Review — round 4, `80024ce..40e05b5` (single commit `40e05b5`)

**Reviewer**: qa-reviewer (opus-5) · **Date**: 2026-09-08 · **Branch**: F44
**HEAD**: `40e05b5f257d48ffb4fb2317a89e04c28acb8453`
**Rounds**: [1](260908-qa-reviewer-opus-5.md) BLOCK · [2](260908-qa-reviewer-opus-5-round2.md) FIX-BEFORE-MERGE · [3](260908-qa-reviewer-opus-5-round3.md) BLOCK

**Verdict**: **FIX-BEFORE-MERGE** — **not clean**. Nothing here can harm this machine; the
reason is defect count, not danger.

`git status --short` is empty and HEAD is `40e05b5`, so this reviews what would actually
deploy.

**Both round-3 blockers are genuinely fixed. All ten should-fix items are fixed. Four new
defects were introduced by this range**, two of them in the new helper and the new task
placement.

## Blocking

None.

## Should fix

### F1. The new verification task gates layers 2 and 3 on layer 1 — the safety net is no longer deployed if the wakeup check fails

`playbooks/imports/play-suspend-and-lid-policy.yml:165-184` now sits **between** the udev rule
(`:140`) and the recovery hook (`:188`) / GNOME backstop (`:228`). Previously it was a handler,
so every task completed first.

`ansible.cfg:25` is `any_errors_fatal = true`. So if `udevadm trigger --settle` does not apply
the rule, the run aborts having written `laptop-lid.conf`, `UPower.conf` and the udev rule —
and having deployed **neither** `resuspend-aborted-suspend` (layer 2, the actual recovery
mechanism for the reported incident) **nor** `sleep-inactive-battery-type` (layer 3, the
backstop). The operator ends up worse off than before, with the bag scenario fully live.

This contradicts the play's own design statement: `:186` labels layer 2 *"make the request
durable **whatever the wake source was**"* — it has no dependency on layer 1. A postcondition
on layer 1 must not gate the deployment of two independent layers.

The rules file is confirmed absent from the host
(`ls /etc/udev/rules.d/99-suspend-wakeup-policy.rules` → No such file), so this chain fires on
the very next deploy, and the trigger→ATTR write has never been executed end to end.

**Fix**: leave `meta: flush_handlers` at `:152`, and move the two verification tasks
(`:165-184`) to the **end** of `tasks:`, after the "Fail if the GNOME power schema…" task.
Every property the comment at `:155-164` claims — runs every time, judges applied state,
prints COVERAGE — is preserved, and a layer-1 failure stops the run *after* the safety net
exists.

### F2. `read_wakeup_states` treats an absent `power/wakeup` attribute as unreadable, so a target device that is simply **not wakeup-capable** hard-fails the whole provisioning run

`helpers/suspend_wakeup/cli.py:44-56`. Every `OSError` collapses to `None`; `core.Result.ok`
(`core.py:57-63`) counts `unreadable` against the verdict; `cli.main` returns 1; the command
task fails; `any_errors_fatal` aborts `playbook-main.yml`.

`power/wakeup` only exists when the kernel marked the device wakeup-capable. Measured on this
host — `BAT0` is a `power_supply` device with **no `power/wakeup` attribute at all**:

```
AC:                           HAS wakeup = enabled
BAT0:                         NO power/wakeup attribute
ucsi-source-psy-USBC000:001:  HAS wakeup = enabled
ucsi-source-psy-USBC000:002:  HAS wakeup = enabled
```

So a host with an `AC` power supply that has no `_PRW` behaves like `BAT0`. Measured against a
fixture:

```
$ python3 -m helpers.suspend_wakeup.cli --power-supply-dir $FIXTURE   # AC present, no power/wakeup
COVERAGE: 1 of 2 power-delivery devices disarmed — UNREADABLE: AC
EXIT=1
```

That device **cannot wake the machine** — it is already in the state the policy wants — and
udev's `ATTR{power/wakeup}="disabled"` on a nonexistent attribute is a tolerated no-op. This is
the *same defect class as round-3 B2*, narrowed from "host has no AC/UCSI at all" (now
correctly handled) to "host has an AC/UCSI that cannot wake".

The code notices the case and dismisses it with a justification that only covers half of it —
`cli.py:52-54`:

```python
# Includes the common, entirely normal case of a device with no power/wakeup
# attribute at all (e.g. BAT0). core.evaluate() ignores non-targets anyway.
```

`evaluate()` ignores non-targets. It does **not** ignore a *target* with no attribute. This is
`AgentNotes.md`'s "guard the empty case, miss the partial": the whole-host-empty case is
handled and celebrated in three tests; the per-device-absent case is not handled and not
tested.

**Fix**: catch `FileNotFoundError` separately from the rest of `OSError`. An absent attribute
means "not wakeup-capable" — count it toward the population in its own bucket and name it in
the summary (`COVERAGE: 2 of 3 disarmed, 1 not wakeup-capable`), and do not fail on it. Reserve
`unreadable` for `PermissionError`/`IsADirectoryError`/IO, which genuinely leave the state
unknown. Add tests for both.

### F3. `warn-reboot-required` now prints *before* the COVERAGE line it points at

`play-suspend-and-lid-policy.yml:313` still reads *"the COVERAGE line **above** verifies it"*.
But `warn-reboot-required` is notified at `:120`, i.e. before `meta: flush_handlers` at `:152`,
so it runs **at the flush** — before the verification task at `:165`.

Measured with a throwaway probe playbook of the same shape:

```
TASK [flush]
RUNNING HANDLER [reload-udev-rules]
RUNNING HANDLER [warn-reboot-required (says: COVERAGE line above)]
TASK [verify via argv + args.chdir exactly as the play does]
TASK [report] => "COVERAGE TASK SAW: COVERAGE: 0 of 0 — no power-delivery wakeup devices on this host"
```

The banner only fires when the logind drop-in changes, which does not happen on this host today
— but it is operator-facing text that is now false. **Fix**: "the COVERAGE line printed later in
this run verifies it", or move the flush after the verification once F1 is applied.

### F4. `PLAN.md` Task 3.5 asserts a safety property the play does not have

`PLAN.md:146-151`, both bullets ticked ✅:

> "Preflight block establishes measured preconditions: sleep capability, upower presence, the
> systemd sleep-hook directory, **the GNOME schema**"
> "**Both** hard preconditions moved into preflight, so the play aborts **before writing
> anything** rather than part-way through"

The GNOME schema probe is at `:205` and its hard `fail` at `:254` — after the logind drop-in,
`UPower.conf`, the udev rule, the udev reload/trigger, and the recovery hook have all been
written. Only the sleep-hook-directory `fail` moved (`:92-98`). Round-3 S9 named one instance;
the plan now claims both were fixed.

### F5. `docs/playbooks.md` documents two abort paths; there are now three

`docs/playbooks.md:128-134` — "**will abort the run** if either precondition fails" — lists the
sleep-hook directory and the GNOME schema. The new `Verify the power-delivery wakeup policy
applied` task (`:165`) is a third abort path (still-armed device, or F2's unreadable-target
case), added in this range and undocumented. This is round-3 S8's finding re-instantiated for
the new task: the instance was fixed, the lesson was not generalised to the task added in the
same commit.

Also at `:126`: "printing `COVERAGE: …` **on every run**" — it is skipped under `--check`
(`:179`, `:184`), and the empty-population case prints a different string.

## Nits

- **`cli.py` has zero tests.** All 15 tests import `core` only — a repo-wide search of `tests/`
  for the package name returns two hits, both naming `core`. `read_wakeup_states` is where the
  sysfs→model mapping happens, where its documented contract lives (*"Absent from the mapping =
  the device does not exist. Present with `None` = exists but unreadable"*), and where F2 lives.
  `main()`'s exit codes are also unasserted. Per `helpers/CLAUDE.md` the split is pure-logic +
  thin executor, and the executor here is thin enough that this is a nit rather than a rule
  breach — but the untested half is the half with the defect.
- **`test_unreadable_target_is_not_counted_as_disarmed`** (`test_core.py:99-105`) pins the exact
  behaviour F2 says is wrong for the absent-attribute case. It will need splitting when F2 is
  fixed.
- **A non-directory `--power-supply-dir` reports the clean line.** Pointing the flag at a file
  yields `COVERAGE: 0 of 0 — no power-delivery wakeup devices on this host`, exit 0. Not
  reachable from the play (the default is always used) but it is the "blind reported as clean"
  shape the module exists to prevent.
- **Second unconditional `debug` on every `playbook-main.yml` run** (`:181-184`, alongside
  `:69-74`). Round 3 raised this as a nit for one; there are now two.
- **`qa-python.bash` still cannot run** (ruff pin 0.16.0 vs installed 0.16.3, rc=2), so the
  repo's own Python gate has never seen this new code. I ran ruff 0.16.3 manually against both
  new directories with the repo config → **All checks passed**. Pre-existing repo defect,
  correctly raised in round 3 and flagged for its own plan — but this range is the first to add
  Python under a blind gate, which sharpens it.
- **`udevadm trigger --settle` writing the ATTR has still never been executed end to end** on
  this host. `udevadm verify` on the rule file → Success 1, Fail 0, and round 2 proved the write
  fires via `udevadm test --action=change`. First-deploy watch item, not a finding.

## Verification of round-3 findings — mechanical

| # | Status | Evidence |
| - | ------ | -------- |
| **B1** qa-ansible red | **Fixed** | `./scripts/qa-ansible.bash` rc=**0** — *"fail-fast patterns OK; … 75 playbook(s) have correct shebang+exec"*. Trailing form confirmed at `play-suspend-and-lid-policy.yml:225` and `play-podman.yml:127`. |
| **B2** grep regression | **Fixed** (residual F2) | The grep is gone. `core.evaluate({})` and `{"BAT0": "disabled"}` → `ok=True`, `total=0`, prints `COVERAGE: 0 of 0 — no power-delivery wakeup devices on this host`. Live host: `COVERAGE: 0 of 3 … STILL ARMED: AC, ucsi-source-psy-USBC000:001, ucsi-source-psy-USBC000:002`, rc 1 — the true pre-deploy state. |
| — `ok` when `total==0` and `unreadable` non-empty | **Unreachable by construction** | `evaluate()` increments `total` for every target before classifying, so `len(unreadable) ≤ total`; `total==0` ⇒ `unreadable == []`. Not a defect. |
| — em-dash through the command module | **Safe** | `LC_ALL=C` → OK; `LC_ALL=C PYTHONCOERCECLOCALE=0` → OK; through `ansible.builtin.command` with `argv:`+`args.chdir` the debug task rendered `COVERAGE: 0 of 0 — no power-delivery…` intact. Only breaks under `PYTHONIOENCODING=ascii`, which Ansible does not set. Python 3.14.7. |
| — hostile sysfs fixtures | **Handled** | Symlinked device entries (real sysfs shape) → correct. `power/wakeup` a **directory** → `unreadable` (same as F2, arguably right here). Dangling symlink device → `unreadable`. Missing base dir → `0 of 0`. Non-dir base → see nit. |
| **S1** `--help` reboot claim | **Fixed** | `deploy.bash:47-48` — "A reboot is required ONLY if the logind lid drop-in changes". |
| **S2** header overstates | **Fixed** | `deploy.bash:20-23` names the helper, the COVERAGE line, and "does not assume a device count". |
| **S3** `end_play`→`end_host` | **Fixed** | `:85-87`, with the `ansible-doc` rationale at `:81-84`. |
| **S4** unguarded slurp | **Fixed** | `:35-44` `stat` then `slurp` `when: power_state_file.stat.exists`; `:65-67` folds absence into `false` via `default('')`. |
| **S5** verification never ran | **Fixed** | `meta: flush_handlers` `:152` + task `:165` + report `:181`. Check-mode run shows the tasks present in the graph. Skipping under `--check` is defensible and the reason is written down (`:175-178`). |
| **S6** false ✅ Task 3.4 | **Fixed** | `PLAN.md:137-141` now describes the probe gate correctly. (New Task 3.5 introduces F4.) |
| **S7** no plan/journal record | **Fixed** | `JOURNAL/00104-Journal-26-09-08.md:529-619` — three new append-only entries (19:05, 19:18, 19:30) correcting the false QA claims and describing the actual code. |
| **S8** docs abort paths | **Partly fixed** | `docs/playbooks.md:128-137` added; misses the new third path — **F5**. |
| **S9** `fail` mid-play | **Fixed** for the sleep-hook dir (`:92-98`); **not** for the GNOME schema — **F4**. |
| **S10** proxy inconsistency | **Addressed by argument** | `PLAN.md:137-141` distinguishes the policy question from the measurable fact; `docs/playbooks.md:133-134` documents the abort. Accepted. |
| **Nit** `rm` qualified | **Fixed** | `/usr/bin/rm` at hook `:80`, `:106`, `:150`. |
| **Nit** `STALE_COUNT_AFTER` derivation | **Fixed** | Hook `:39-44` states "chosen, not measured", both bounds, and the residual (an abort within 600s of the previous request falls to layer 3). |

## Checked and clean

- **DECISIONS.md extraction — zero content loss.** Programmatic comparison of the removed
  `PLAN.md` block against `DECISIONS.md`: 86 non-blank lines, **0** not present verbatim. All
  four decisions, their headings, their `**Date**` lines and the supersession notes survive.
  `PLAN.md:63-71` links it; `qa-docs.bash` rc 0 (63 files, links + anchors).
- **Test quality — 15 tests, none tautological.** Each pins a decision that could have gone the
  other way: `ucsi-sink` excluded, `BAT0` excluded, trailing newline from sysfs, the empty-host
  regression, the exact COVERAGE strings. The gap is coverage of `cli.py`, not the quality of
  what is there (nit above).
- **Helper conventions.** Namespace package, no `__init__.py`, stdlib only, pure `core` + thin
  `cli` executor, invoked with `command:`+`argv:`+`chdir: root_dir` — identical to
  `helpers/sshd_ports`, `helpers/pyenv`, `helpers/gnome`. `__pycache__` gitignored
  (`.gitignore:12`).
- **`qa-helper-tests.bash` discovers the new module.** 14 → 15 test modules across the range;
  240 tests, `OK`, rc 0. `find`-based enumeration, so no Plan 00076 discovery hole.
- **Placement in the IaC graph.** No new play; the verification lives in the play that owns the
  udev rule. Handler chain order still correct and documented. Handlers confirmed to execute in
  **definition** order at the flush (probe playbook).
- **Fail-fast.** Only two annotated exemptions in the range, both trailing-form and both
  genuinely probe-then-fail with the rc consumed. No `|| true`, no skip-and-warn, no
  `creates:`-style existence-as-generated guard.
- **Public-repo safety.** The range's added lines were scanned for home paths, emails, RFC1918
  addresses and UUIDs → no hits. The newly committed round-3 report carries no personal data;
  device names are generic ACPI identifiers.
- **`argv:` + `args: chdir:` works**, proven by execution, not by reading — this was the one
  runtime unknown, since the check-mode run skips the task.
- **Conditional gates — checked, and stated.** `qa-helper-tests.bash` **required** (helpers/ +
  tests/helpers/ changed) → run, green. `check_extension_compat` and ESLint **not** required —
  no `extensions/` change. No `files/var/local/claude-yolo/**` change ⇒ no `CCY_VERSION` /
  `REQUIRED_CONTAINER_VERSION` bump required.

## Mechanical gates

Run individually — `qa-all.bash` still exits at `:43` on `qa-python`'s rc=2 (pre-existing ruff
pin, raised in round 3, not this range's).

| gate | rc | note |
| --- | --- | --- |
| `qa-ansible.bash` | **0** | was 1 in round 3 — B1 cleared |
| `qa-ansible-syntax.bash` | 0 | 77 playbooks |
| `qa-patterns.bash` | 0 | 200 files |
| `qa-docs.bash` | 0 | 63 files |
| `qa-bash.bash` | 1 | **only** `CLAUDE/Plan/00079-…/unit-test-selection.bash` SC2154 — pre-existing, outside this range |
| `qa-python.bash` | 2 | ruff pin 0.16.0 vs 0.16.3 — pre-existing; ruff run manually on the new files: **clean** |
| `qa-helper-tests.bash` | 0 | 15 modules, 240 tests, OK |
| `plan-qa --sweep` | 1 | 0 block / 2 advise, both repo-wide staleness nags; 00104 in neither |
| `--syntax-check` | 0 | `play-suspend-and-lid-policy.yml`, `play-podman.yml`, `playbook-main.yml` |
| `--check --diff` full play | 0 | `ok=13 changed=4 failed=0 skipped=6` |
| `udevadm verify` on the rule | 0 | Success 1, Fail 0 |

## Deploy call

**Do not deploy yet — but the reason is defect count, not danger.**

I checked whether any finding can hurt this machine, and none can:

- **F2 cannot fire here.** All three targets (`AC`, both `ucsi-source-psy-USBC000:00{1,2}`) have
  a `power/wakeup` attribute and read `enabled`. Measured, just now.
- **F1's abort is loud, not silent** — you would see the run fail — but it would leave you with
  layer 1 written and **layers 2 and 3 not deployed**, i.e. the hot-bag scenario still fully
  live. Low probability, bad outcome, and it is a two-task move to remove entirely.
- **F3, F4, F5** are text and plan accuracy.

Against the standard set for this round — good, safe **and free of defects** — this is not
clean. **F1 and F2 are both small and both worth doing before the deploy**; F3–F5 are a few
lines each.

**Once fixed, watch on first deploy:**

1. `TASK [Verify the power-delivery wakeup policy applied]` must pass and
   `TASK [Report wakeup policy coverage]` must print
   `COVERAGE: 3 of 3 power-delivery devices disarmed`. Confirm out of band by reading
   `power/wakeup` on `AC` and both UCSI ports — all three read `enabled` right now, so this is a
   real before/after.
2. Confirm `/usr/lib/systemd/system-sleep/resuspend-aborted-suspend` and
   `/etc/udev/rules.d/99-suspend-wakeup-policy.rules` both exist afterwards.
3. `journalctl -t resuspend-aborted-suspend --no-pager` across the first few deliberate suspends
   — especially a wake from the external keyboard inside 10s with the lid shut, the
   false-positive case the window bets against.
4. `/run/resuspend-aborted-suspend.count` should be absent after any normal long sleep.
5. Phase 4's reproduction (suspend → unplug dock within ~3s → lid closed) remains the only thing
   that closes this plan. Nothing in this range has run on the host.
