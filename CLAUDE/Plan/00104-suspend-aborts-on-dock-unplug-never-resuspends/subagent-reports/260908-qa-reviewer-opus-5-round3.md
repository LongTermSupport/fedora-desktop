# QA Review — round 3, `70458b0..80024ce` (144ecd5, 90ba291, 80024ce)

**Reviewer**: qa-reviewer (opus-5) · **Date**: 2026-09-08 · **Branch**: F44
**HEAD**: `80024ced311feb3027d020260f3375b26ef59248`
**Round 1**: [260908-qa-reviewer-opus-5.md](260908-qa-reviewer-opus-5.md) (BLOCK)
**Round 2**: [260908-qa-reviewer-opus-5-round2.md](260908-qa-reviewer-opus-5-round2.md) (FIX-BEFORE-MERGE, do-not-deploy)

**Verdict**: **BLOCK** — **DO NOT DEPLOY**

`git status --short` is empty; HEAD is `80024ce`. Working tree matches the commit, so unlike
round 2 this review is of what would actually deploy.

Round-2 §0 and N1–N5 are all genuinely fixed. Both blockers below are new, introduced inside
this range, and one of them was invisible to every QA run this plan has recorded.

## Blocking

### B1. `qa-ansible.bash` fails on this change — and `qa-all.bash` exits before it ever runs

`playbooks/imports/play-suspend-and-lid-policy.yml:171` is a bare `failed_when: false`. The
justification sits on lines 169–170, *above* it. The gate requires the annotation as a
**trailing same-line comment** — `scripts/qa-ansible.bash:8`: *"it requires a same-line
`# FAIL-FAST-OK:` justification too"*, and `:131-136` confirms the parser reads a trailing
comment on the same physical line. Every compliant instance in the repo is in that form, e.g.
`playbooks/imports/play-network-wait-tuning.yml:98`.

Run directly:

```
$ ./scripts/qa-ansible.bash
  ERROR (fail-fast): playbooks/imports/play-podman.yml:127:      failed_when: false
  ERROR (fail-fast): playbooks/imports/play-suspend-and-lid-policy.yml:171:      failed_when: false
✗ ansible: 2 violation(s) — 2 fail-fast, 0 hygiene, 0 self-ref var, 0 scope/guard, 0 deprecated fact var
rc=1
```

**The second half is worse than the first.** `scripts/qa-all.bash:40-43` runs
`qa-python.bash` third and `exit 2`s on rc=2. The ruff pin mismatch returns exactly rc=2. So
`qa-all.bash` has been short-circuiting at line 43 and **never reaching** `qa-patterns`,
`qa-ansible`, `qa-ansible-syntax`, `qa-js` or `qa-docs`. Confirmed by reading
`qa-all.bash:28-95` and by running the four skipped gates by hand:

| gate | rc when run directly |
| --- | --- |
| `qa-patterns.bash` | 0 — 200 files OK |
| `qa-ansible.bash` | **1 — the violation above** |
| `qa-ansible-syntax.bash` | 0 — 77 playbooks OK |
| `qa-docs.bash` | 0 — 63 files OK |

This plan's entire change surface is a playbook. Every journal line of the form *"`qa-all.bash`
is red only on the two pre-existing items"* (15:55, and by implication 17:32 and 18:30)
describes a run that structurally could not have looked at a playbook. Round 2's own
"Mechanical gates" section made the same claim and was equally blind — my error, corrected
here. This is `CLAUDE/AgentNotes.md`'s *"a gate that is not running is indistinguishable from
a gate that passes"* in the live tree.

The ruff pin is pre-existing and is not attributed to this range. The *consequence* — that
this change's ansible gate has never executed and is red — is this change's problem.

**Fix**: move the justification onto the same line
(`failed_when: false  # FAIL-FAST-OK: probe-then-fail — rc is consumed by the two tasks below`),
keeping the prose block above as the WHY. Separately, `qa-all.bash`'s short-circuit is a
repo-level defect worth its own plan: a missing-tool rc from one gate should not silently
suppress five unrelated ones.

### B2. `assert-wakeup-disarmed` hard-fails on any host that does not have this exact laptop's power-supply devices

`playbooks/imports/play-suspend-and-lid-policy.yml:257-267`, introduced by `80024ce`.

```yaml
cmd: >-
  grep -l '^enabled$'
  /sys/class/power_supply/AC/power/wakeup
  /sys/class/power_supply/ucsi-source-psy-*/power/wakeup
failed_when: wakeup_still_armed.rc != 1
```

Measured exit codes (GNU grep, sandbox fixtures):

| scenario | rc | `failed_when` verdict |
| --- | --- | --- |
| AC armed, ucsi devices absent | **2** | fail — but the message is "No such file or directory", not "AC is still armed" |
| AC disarmed, ucsi devices absent — **policy fully applied** | **2** | **fail (false positive)** |
| no AC and no ucsi at all (a desktop PC) | **2** | **fail (false positive)** |
| all three present and disarmed | 1 | pass |
| live host now (all three still `enabled`) | 0 | fail — correct |

The unmatched glob is passed through literally, so a missing device is indistinguishable from
a real error. The comment at `:254-256` asserts "2 = a real error such as a missing path" and
treats absence as a fault.

Blast radius: `Disarm power-delivery devices as wakeup sources` (`:113-120`) has **no `when:`**,
so on any host's first run the copy is `changed` → `reload-udev-rules` → `trigger-udev` → this
handler. A failing handler under `any_errors_fatal = true` (`ansible.cfg:26`) aborts the
**whole** `playbook-main.yml` run. `/etc/udev/rules.d/99-suspend-wakeup-policy.rules` does not
yet exist on this host, so the chain will fire on the very next deploy.

This is a straight regression inside the range. The form at `90ba291` — four minutes earlier —
handled absence correctly with `[ -r "$f" ] || continue`. The rewrite deleted that guard while
keeping the same claim. It also directly contradicts this commit's own stated purpose: the
header at `:11-17` reads *"EVERY TASK IS GATED ON A MEASURED FACT, NOT ON A PROXY … it runs on
every host — laptop, desktop and headless server"*, and the new handler is gated on nothing and
hardcodes three device paths.

The old loop form was not right either — it printed "all power-delivery wakeup sources
disarmed" on a host with zero devices, which is `AgentNotes.md`'s *blind reported as clean*.
Neither form states its population.

**Fix**: make it enumerate rather than name. Compute the set the udev rule targets
(`SUBSYSTEM=="power_supply"`, `KERNEL=="AC"` or `ucsi-source-psy-*`) from
`/sys/class/power_supply/*`, fail on any member reading `enabled`, and print a coverage line —
`COVERAGE: 3 of 3 power-delivery devices disarmed` /
`COVERAGE: 0 of 0 — no power-delivery wakeup devices on this host`. That is loops +
conditionals + data-munging, so per `playbooks/CLAUDE.md` ("Complex Logic → TDD Helper", in
stone) it belongs in a tested `helpers/` module invoked with `command:` + `argv:`, not in
either a `shell:` loop or a hand-tuned `grep`.

## Should fix

### S1. `deploy.bash --help` still says a reboot is required — N2 is only half fixed

`deploy.bash:14-22` is corrected. But `deploy.bash:44-45`, the `PLAN_USAGE` string printed by
`--help`:

> "Changes suspend, lid and wakeup behaviour on THIS machine. **A reboot is required afterwards
> before the policy is fully in effect.**"

That is the exact sentence N2 flagged, still live, one screen lower. On this host
`/etc/systemd/logind.conf.d/laptop-lid.conf` is already present and the check run reports the
blockinfile task as `ok`, so `warn-reboot-required` will not fire and **no reboot is required
at all**.

### S2. `deploy.bash:20-22` overstates what the assertion does

> "then asserts `power/wakeup` reads `disabled` on all three power-delivery devices"

It does not. It asserts that no listed file *contains* `enabled` — which a nonexistent file
satisfies (rc=2 aside), and "all three" hardcodes this laptop's device count into a general
statement. This is the same claim-without-observation shape N3 was raised about, reintroduced
in the operator-facing header.

### S3. `meta: end_play` should be `meta: end_host`

`playbooks/imports/play-suspend-and-lid-policy.yml:69-71`. `ansible-doc -t module meta`:

> `end_play` … "Note that this **affects all hosts**."
> `end_host` … "is a **per-host** variation of `end_play`."

`host_can_suspend` is a per-host measured fact. Today `environment/localhost/hosts.yml` has one
host in `desktop`, so the two are equivalent — but the whole point of `144ecd5` is that this
play must behave correctly across a heterogeneous host set. Add a second host and one
non-suspending machine silently cancels the policy for the laptop too, with no error.
`ansible-doc`'s own example for `end_host` is this exact shape.

### S4. The preflight that exists to make the play safe everywhere introduces a new unconditional fatal

`:32-35` `slurp: /sys/power/state` runs first, with no guard. Measured:

```
$ ansible localhost -m slurp -a src=/sys/power/definitely_absent
fatal: [localhost]: FAILED! => {"changed": false, "msg": "File not found: ..."}
rc=2
```

With `any_errors_fatal = true` that aborts the entire provisioning run on the first task, on a
host where the honest answer is "this machine cannot sleep — skip". Gate it the way the other
two preconditions are gated: `stat` first, then `slurp` `when: power_state.stat.exists`, and
treat absence as `host_can_suspend: false`.

### S5. The verification only runs on the run that changes the file, and prints nothing when it passes

`assert-wakeup-disarmed` is a handler three links down a notify chain. On a re-run where the
udev rule is unchanged, nothing notifies and **the wakeup state is never checked** — the play
reports green having verified nothing. Confirmed in check mode: `reload-udev-rules` is skipped
(command module in check mode), so the chain never starts and the assert has, to date, never
executed at all.

That is `AgentNotes.md`'s *"a gate whose only visible output is a failure is indistinguishable
from a gate that is not running"*. Make it a task that runs every time, after the udev copy,
and have it emit the `COVERAGE: n of m` line from B2 on success.

Corollary answer to the round-3 brief's check-mode question: `failed_when: wakeup_still_armed.rc != 1`
is **unreachable in check mode**, because its notifier is skipped before it. It is safe today
by accident of ordering, not by design — adding `check_mode: false` to `trigger-udev` later
would expose it.

### S6. PLAN.md has a ticked ✅ subtask that is now false

Task 3.4: *"`sleep-inactive-battery-type=suspend`, **guarded by `provisioning_profile != 'server'`**"*.
It is now guarded by `gnome_power_schema.rc | default(1) == 0` (`:187`). Tasks 3.1–3.3 also
carry no record of the preflight block, the hard `fail` tasks, or the assert handler — the
three largest structural changes in this range.

### S7. `80024ce` changed a deployed playbook with no plan or journal record, and the journal now describes code that does not exist

`git log --stat` for the range: `80024ce` touches `play-suspend-and-lid-policy.yml` only. No
`PLAN.md`, no `JOURNAL/`. The last journal entry (18:30, written for `90ba291`) says the
handler *"reads `power/wakeup` back on all three devices and fails if any still says
`enabled`"* — which describes the deleted loop, not the grep, and is materially wrong about the
grep's behaviour (it also fails when a device is **absent**). Per `CLAUDE/PlanJournalling.md`
the journal is append-only, so this needs a new entry, not an edit.

The same 18:30 entry presents a six-scenario `--check` table as the rework's verification. None
of those scenarios can reach the assert handler (S5), so the table does not vouch for the thing
the entry is about — the round-3 instance of the defect class this plan's own journal has now
logged four times.

### S8. `docs/playbooks.md` does not mention two new ways this play can abort a full provisioning run

`docs/playbooks.md:110-127` is otherwise accurate and the 30s→10s fix landed. But the play now
hard-`fail`s on a missing `/usr/lib/systemd/system-sleep` (`:127-133`), hard-`fail`s on an
unreadable GNOME power schema on any non-server host (`:200-211`), and hard-fails on B2. This
play is on the main provisioning path; a user hitting any of these gets a fatal run and no
documentation. One bullet naming the preconditions would cover it.

### S9. The system-sleep-directory `fail` runs after three mutating tasks

`:127-133` sits between the udev copy and the hook copy. By the time it fires, logind's
drop-in, `UPower.conf` and the udev rule are already written and `restart-upower` is queued. It
is a precondition; put it in the preflight block with the others so the play either aborts
clean or proceeds.

### S10. The play's own thesis is not applied consistently

The header (`:11-17`) condemns `provisioning_profile` as a proxy. The upower task now measures
(`:107` `when: upower_conf.stat.exists`) — good. But `:172` still gates the GNOME probe on
`when: provisioning_profile != 'server'`, and that proxy is what decides whether a non-zero rc
becomes a fatal error (`:209-211`). A desktop-profile host with no GNOME session — provisioning
over SSH before first login, a headless box whose default target is `graphical.target` — now
aborts the whole run with "Fix the user session". That outcome is arguably correct under
fail-fast, but it is being decided by exactly the proxy the header rejects, and it is
undocumented (S8).

## Nits

- **`:57-62` "Report the preflight result"** prints three lines on every `playbook-main.yml`
  run. Informational, not a skip-and-warn, so it is not a rule breach — but it is unconditional
  output on a shared run, which is what `:90-92` argues against for the reboot banner.
- **Unqualified `rm`** in the hook at `:75`, `:101`, `:145`, while `date`, `logger`, `busctl`,
  `systemd-run` and `systemctl` are all absolute. Round 2 said `date` was the last one; it was
  wrong — `rm` was there too. Cosmetic (systemd sets a sane `PATH`), but finish the job.
- **`STALE_COUNT_AFTER=600`** (`:39`) has no derivation, unlike `RESUSPEND_WINDOW=10` which
  carries its F1 measurement. The comment argues *that* an expiry is needed, never why ten
  minutes. State the residual explicitly: after a give-up, a genuine abort occurring within
  600s of the previous suspend request is still uncovered.
- **`RESUSPEND_DELAY=5` vs the sibling hooks** (`displaylink.sh`, `nvidia`, both present in
  `/usr/lib/systemd/system-sleep/`) remains the unverified round-1 residual. Phase 4.

## Verification of round-2 findings — mechanical, not by reading

| # | Status | Evidence |
| - | ------ | -------- |
| **§0** skip-and-warn | **Fixed** | `grep -n "Report that\|debug" play-suspend-and-lid-policy.yml` → only `:58` (preflight info) and `:270` (reboot handler). Both "Report that X was skipped" tasks gone. |
| **§0** gsettings fails on desktop | **Fixed** | `:200-211`. Verified the conditional pair by evaluation, not inspection: `ansible localhost -m debug -e '{"r":{"rc":1,...}}'` → `r is not skipped`=**True**, `r.rc\|default(1)!=0`=**True** ⇒ fires. |
| **§0** server still skips cleanly | **Fixed** | `--check -e provisioning_profile=server` → probe `skipping`, set `skipping`, **fail task `skipping`**, `failed=0`, rc 0. Confirmed mechanically: `-e '{"r":{"skipped":true}}'` → `r is not skipped`=**False**. |
| **§0** the `\| bool` claim | **Correct** | `not ("False"\|bool)`=**True**, `not "False"`=**False**. |
| **N1** 30→10 propagated | **Fixed** | Repo-wide grep for `30s`/`within 30` across `docs/`, `PLAN.md`, `deploy.bash`, hook and play → the only hit is `docs/playbooks.md:134`, which is `NM_ONLINE_TIMEOUT`, unrelated. |
| **N2** deploy.bash reboot claim | **Partly fixed** | Header `:14-22` corrected; `PLAN_USAGE` `:44-45` still contradicts it — **S1**. |
| **N3** `--settle` + assert | **Regressed** | `--settle` present at `:243` ✓. The assert handler is **B2**, and never runs on a re-run or in check mode — **S5**. Exit-code semantics, glob behaviour and check-mode reachability all measured. |
| **N4** COUNT expiry | **Fixed** | `:73-76`. Retry path: pre(t₀) → abort → post(t₀+3) → retry fires t₀+8 → pre reads `previous=t₀`, Δ=8 ≪ 600 ⇒ **counter survives its own burst** ✓. Later incident: give-up at T, next suspend T+2h ⇒ Δ=7200 > 600 ⇒ `rm -f COUNT` ⇒ **fresh burst** ✓. A successful long sleep clears it anyway via `:99-102`. Residual noted as a nit. |
| **N5** /proc before logind | **Fixed** | `:117-134`. ACPI loop first, busctl fallback, `--timeout=2` at `:127`. `/proc/acpi/button/lid/LID/state` reads `state:      open` on this host and matches `*closed*` correctly. Unmatched-glob path falls through to `unknown`, not to `no`. |
| **Nit** explicit `yes)`/`*)` | **Fixed** | `:139-156`, all four arms explicit, `*)` logs and exits 0. |
| **Nit** negative elapsed | **Fixed** | `:94-97`, guarded *before* the window comparison. |
| **Nit** `date` qualified | **Fixed** | `/usr/bin/date` at `:74`, `:77`, `:89`. (`rm` still bare — nit above.) |
| **Nit** handler ordering | **Fixed** | `:219-221` states the constraint and names the chain. |

## Checked and clean

- **Public-repo safety**: `git diff -U0 70458b0..80024ce` grepped for absolute home paths,
  emails, RFC1918 addresses — none. The newly committed round-2 report contains no personal
  data. Hardware IDs are generic ACPI identifiers.
- **Hook syntax and lint**: `bash -n` rc 0; `shellcheck -x` on both the hook and `deploy.bash`
  rc 0. The deployed hook **is** covered by the bash gate — it appears at `results[164]` in
  `/tmp/qa-bash-results.json`, so no discovery hole of the Plan 00076 kind.
- **Fail-fast in the hook**: no `|| true`, no swallowed errors; the two tolerant paths
  (`systemd-run` failure at `:179-186`, unknown lid at `:148-151`) both log and are deliberate.
- **`read_int_or_empty` and the arithmetic**: unchanged since round 2's sandbox exercise; the
  new `previous=` call at `:73` uses the same contract.
- **Playbook hygiene**: shebang present, exec bit set (mode 100755), `scope: general` declared,
  `root_dir` anchored on the config lookup. `qa-ansible-syntax.bash` → 77 playbooks OK.
- **Placement in the IaC graph**: no new play; layers 1–3 correctly consolidated in the one play
  that owns suspend policy, imported from `playbook-main.yml`. Handler chain ordering is correct
  and now documented. Naming is action-oriented throughout; no `: -x` traps in unquoted task
  names.
- **Conditional gates — checked, not triggered**: no `helpers/`/`tests/helpers/` change ⇒
  `qa-helper-tests.bash` not required; no `extensions/` change ⇒ `check_extension_compat` and
  ESLint not required; no `files/var/local/claude-yolo/**` change ⇒ no `CCY_VERSION` or
  `REQUIRED_CONTAINER_VERSION` bump required.

## Mechanical gates

- **`./scripts/qa-all.bash`**: rc=2. Output shows only the two pre-existing items —
  `CLAUDE/Plan/00079-podman-container-control/unit-test-selection.bash` (8× SC2154) and the ruff
  pin 0.16.0 vs 0.16.3. **Confirmed pre-existing; neither file is in this range.** But see
  **B1**: it exits at `qa-all.bash:43` and never reaches the five gates after `qa-python`.
- **Gates `qa-all` skipped, run by hand**: `qa-patterns` 0 · **`qa-ansible` 1 (B1)** ·
  `qa-ansible-syntax` 0 (77 OK) · `qa-docs` 0 (63 OK). The second `qa-ansible` hit,
  `play-podman.yml:127`, dates to `a82a3a1` (2026-09-07) and is outside this range — a second
  casualty of the same short-circuit, not this change's.
- **`hooks-daemon plan-qa --sweep`**: rc=1, **0 block / 2 advise**. Both are repo-wide
  staleness/journal nags; Plan 00104 appears in neither list.
- **`ansible-playbook --syntax-check`**: `play-suspend-and-lid-policy.yml` rc 0;
  `playbook-main.yml` rc 0.
- **Live `--check` runs**: desktop profile rc 0, `ok=12 changed=4 failed=0`;
  `-e provisioning_profile=server` rc 0, `ok=11 changed=4 failed=0`.

## Deploy call — DO NOT DEPLOY

Blocking, in order of what actually bites:

1. **B2** — `assert-wakeup-disarmed` is a regression that aborts the whole provisioning run on
   any host without an `AC` power-supply device *and* at least one `ucsi-source-psy-*`. The udev
   copy has no `when:`, the rules file is absent on this host, so the chain **will** fire on the
   next deploy. `90ba291` had this right; `80024ce` broke it.
2. **B1** — the ansible fail-fast gate is red on this play, and the plan's Success Criteria
   include "QA passes". Also fix the annotation on `play-podman.yml:127` while you are there, and
   raise the `qa-all.bash` short-circuit separately — five gates have been silently unrun.

**On this specific laptop, if you deployed anyway**, what would happen (from the real
`--check --diff` run, not inference):

- `AC` and both `ucsi-source-psy-USBC000:00{1,2}` are present and currently read `enabled`, so
  B2's grep would return **1** and pass — *provided* `udevadm trigger --settle` applies the rule.
  Round 2 proved the write fires via `udevadm test --action=change` against these exact devices,
  so this is likely but has never been executed end to end.
- If it does not apply, the handler fails **after** `laptop-lid.conf`, `UPower.conf`, the udev
  rule and the sleep hook are all already written and `upower` already restarted — a partial
  apply, recoverable only by re-running.
- `IgnoreLid=false → true` will change and `restart-upower` will fire, briefly re-enumerating the
  battery indicator under the live GNOME session.
- `laptop-lid.conf` reports `ok` (unchanged), so `warn-reboot-required` will **not** fire and
  **no reboot is needed** — contradicting `deploy.bash --help` (S1).
- The GNOME probe returned `ok` here, so the new hard-fail at `:200-211` will not trip.
- Behaviour change to accept knowingly: `sleep-inactive-battery-type` `nothing → suspend` at the
  existing 900s.

**Once B1 and B2 are fixed, watch on first deploy:**

1. `RUNNING HANDLER [assert-wakeup-disarmed]` — it must appear and pass. Then confirm out of
   band: `cat /sys/class/power_supply/{AC,ucsi-source-psy-*}/power/wakeup` should read `disabled`
   on all three (all three read `enabled` right now, so this is a real before/after).
2. `journalctl -t resuspend-aborted-suspend --no-pager` across the first few deliberate suspends
   — especially a wake from the external keyboard inside 10s with the lid shut, which is the
   false-positive case the 10s window is betting against.
3. `/run/resuspend-aborted-suspend.count` should be **absent** after any normal long sleep. If it
   ever holds `3`, layer 2 is dormant until either a normal resume or a suspend more than 600s
   after the previous one.
4. The Phase 4 reproduction (suspend → unplug dock within ~3s → lid shut) is still the only thing
   that closes this plan. Nothing in this range has been executed on the host.
