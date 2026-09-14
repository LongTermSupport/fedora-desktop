# QA Review — Plan 00109, commits `55b8ef4d..fd74e09a`

**Reviewer**: `qa-reviewer` (Opus 5) · **Date**: 2026-09-14 · **Verdict**: **BLOCK**

## Timing caveat — read first

`qa-all.bash` was **green (847 files)** at `fd74e09a`, the assigned scope. It was **RED at
the time of writing**: 23 ERRORs across `test_probe.py` / `test_probe_results.py`, all
`TypeError: build_report() got an unexpected keyword argument 'extra'` — an in-flight edit
removing `extra` while `probe.collect:103` still passed it. `54eae25c` (Phase 4 design)
also landed mid-review; it is out of scope and changes none of the findings below.

Every finding is labelled **CONFIRMED** (proved with a command whose output is quoted) or
**PLAUSIBLE** (reasoned, not reproduced in this container).

---

## Blocking

### B1. `SuccessExitStatus=0 1` declares a crashed health surface a success

`files/home/.config/systemd/user/host-health.service.j2:26`

**CONFIRMED.** Two proofs:

```
$ cd /tmp && python3 -m helpers.host_health.login_report --no-notify
/usr/bin/python3: Error while finding module specification for
'helpers.host_health.login_report' (ModuleNotFoundError: No module named 'helpers')
EXIT=1

$ python3 -c "raise RuntimeError('a crash inside main')"
UNCAUGHT_EXCEPTION_EXIT=1
```

A wrong `WorkingDirectory`, a syntax error, a renamed helper, or any uncaught exception in
`main` all exit 1 — the same status the unit declares to mean "there are findings". So a
permanently dead login surface reports `Result=success`, `is-failed` says nothing, and the
user is never told. That is the plan's own incident reproduced inside the code written to
prevent it.

The unit's comment (l.23-25) and `docs/playbooks.md:771` both assert the opposite:
*"only a crash is a unit failure"*.

**Fix**: `EXIT_FINDINGS = 3` in `login_report` (and `probe`, for consistency),
`SuccessExitStatus=3` in the unit, leaving 1 and 2 as genuine failures.

### B2. `handoff._split` files 6 of 13 reachable "not checked" findings under "What is wrong"

`helpers/host_health/handoff.py:29,33`

**CONFIRMED** by executing `_split` over every finding string the three producers can
emit, re-verified against the then-current working tree, including the untrustworthy
message rewritten during this session:

```
UNCHECKED FINDINGS THAT LAND UNDER 'What is wrong': 6 of 13
  probe_results.py:149         the dkms probe output could not be read: ...
  login_report.py:218 (NEW)    play-freshness could not give an answer, so no play was judged: ...
  fetch_clock.py:78            play-freshness has never successfully reached the remote on this host...
  fetch_clock.py:84            play-freshness cannot tell when it last reached the remote...
  fetch_clock.py:91            play-freshness last reached the remote in the future...
  fetch_clock.py:98            play-freshness has not reached the remote for 9 days...
```

`_UNCHECKED_MARKER = "could not run"` plus a literal `"could not be checked"` covers seven
wordings and misses six. The comment on l.28 — *"The shape every 'could not run' finding
takes, across all three checks"* — is false.

This is the file whose entire thesis is that *"this was not looked at"* must not read as
*"this is broken"*, and the section header it renders says of the misfiled group that they
**are** known-wrong. The sharpest instance: `login_report.py:218` is the message rewritten
today to carry its reason, and the consumer one directory over still misclassifies it —
AgentNotes' *"generalise a fix past the file you were reading"*, live.

**Fix**: stop substring-matching prose. Each producer returns `(text, checked: bool)`, or
at minimum every unchecked finding carries one machine-readable prefix. `DESIGN-panel.md`
l.41/58/67 is about to consume this same split for its `unavailable` state, so the blast
radius grows next phase.

### B3. Top-level fact alias — banned by AnsibleStyle, in a spot the gate structurally cannot see

`playbooks/imports/optional/common/play-host-health-login-report.yml:87,88,115`

Three uses of bare `getent_passwd`. `CLAUDE/AnsibleStyle.md:139-158`: *"Facts: always
`ansible_facts[...]`, never the top-level `ansible_<fact>` alias … removed in ansible-core
2.24, at which point every such reference becomes an undefined variable — an error
mid-play, on the machine, after earlier tasks have already changed state."*

**CONFIRMED from the installed ansible source:**

- `config/base.yml:1805-1812` — *"Unlike inside the `ansible_facts` dictionary where the
  prefix `ansible_` is removed from fact names, these will have the exact names that are
  returned by the module"* → the injected name is `getent_passwd`, with no prefix.
- `constants.py:48` — `_ACTION_WITH_CLEAN_FACTS = _ACTION_SET_FACT + _ACTION_INCLUDE_VARS`;
  `getent` is not in it, so `task_executor.py:668-670` runs and wraps each value in
  `_deprecate_top_level_fact`.
- `qa-ansible.bash:436` — the pattern is `\bansible_(<FACT_NAMES>)\b`. Because the injected
  name has no `ansible_` prefix, no extension of `FACT_NAMES` can ever catch this. The gate
  is structurally blind to every `getent_*` fact.

Nine other sites in the repo use `ansible_facts['getent_passwd'][user_login][1]`
(`play-podman.yml:44`, `play-systemd-user-tweaks.yml:174`, `play-rclone.yml:209`, …). This
is the only one that does not.

**Fix**: `ansible_facts['getent_passwd']` in all three places. Secondary: add a second
pattern to Check 5 for un-prefixed injected fact names (`getent_passwd`, `getent_group`,
`getent_shadow`, …), or the next one will be just as invisible.

---

## Should fix

### M4. `_declared_pins` drops the reason its own subprocess failed, duplicating logic that does not

`helpers/host_health/login_report.py:219-235`

**CONFIRMED** side by side against the same induced failure:

```
WHAT _declared_pins SAYS TO THE USER:
  the installed-vs-pinned check could not run: Command '['/usr/bin/python3', '-c',
  'import json,sys,yaml_missing_on_purpose; ...']' returned non-zero exit status 1.

WHAT WAS THROWN AWAY (error.stderr):
  ModuleNotFoundError: No module named 'yaml_missing_on_purpose'

FOR CONTRAST — check_pins._run on the same failure:
  /usr/bin/python3: ... ModuleNotFoundError: No module named 'yaml_missing_on_purpose'
```

`check_pins.main:192-206` already does this conversion correctly, via `_run`, which folds
stderr into the message. `_declared_pins` is a second copy of the same subprocess with the
diagnostics removed. `probe.run_probe:74-76` carries the comment explaining exactly why:
*"A silent failure still has to say something: 'could not run: ' with nothing after it
tells the user precisely nothing."*

**Fix**: promote and call `check_pins._run`; delete the second `subprocess.run`.

### M5. PyYAML is an undeclared runtime dependency of the login unit

`playbooks/imports/optional/common/play-host-health-login-report.yml:48-54`

The play declares `libnotify` with the comment *"Per the missing dependency rule this is
declared here rather than tolerated at runtime"*, and declares nothing for PyYAML, which
`_declared_pins` needs from `sys.executable` (= `/usr/bin/python3` under
`ExecStart=/usr/bin/python3`).

**PLAUSIBLE, not confirmed.** `run.bash:1662` states that `./run.bash` provisions ansible
through pipx — a pipx venv's PyYAML is not importable by `/usr/bin/python3` — while
`docs/installation.md:97` shows the package-manager route, which does pull
`python3-pyyaml` into the system interpreter. This container has it
(`/usr/lib/python3/dist-packages/yaml`), so the failure could not be reproduced here.
Repo-wide, `grep -rn "pyyaml\|PyYAML"` finds only comments asserting *"the PyYAML that
Ansible itself depends on"* — an assumption, never a declaration.

Combined with M4 the failure mode is a permanently dead pin check reporting an unreadable
message — on the one drift axis the whole plan exists for.

**Fix**: add `python3-pyyaml` to the play's existing package task, per `CLAUDE.md`
"Missing Dependencies".

### M6. `libnotify` is declared in the wrong place in the IaC graph

**CONFIRMED**: `grep -rn libnotify playbooks/ vars/ environment/` returns exactly one hit —
this new opt-in play. But `grep -rln notify-send` shows three consumers:

- `helpers/displaylink_recovery/run_recovery.py`
- `files/usr/local/bin/manage-kernel-versions.py`
- `helpers/host_health/login_report.py` (the new one)

The first two are reached from core paths and have depended on `notify-send` being present
by luck. The play that owns desktop notification for the machine should own the package,
not an opt-in play a host may never run.

### M7. `triage.bash` claims READ-ONLY and it is not

`CLAUDE/Plan/00109-.../triage.bash:15-16` — *"READ-ONLY. It removes nothing, registers
nothing and runs no playbook."*

Line 106 runs `python3 -m helpers.host_health.login_report --no-notify`, which:

1. **runs `git fetch` in the operator's checkout** — `git_history.py:28-35`, reached
   whenever the host has a ledger with at least one play recorded;
2. **writes `last-fetch`** into the ledger directory — `fetch_clock.py:54-58`;
3. **writes `host-health-findings.md` at 0600** — `handoff.py:88-101`, unconditional
   whenever there are findings.

**CONFIRMED**: the container run performed during this review produced
`~/.local/state/fedora-desktop/play-ledger/host-health-findings.md`. `plan_require_host`
guarantees this happens on the host, and Task 0.2's whole premise is that there *are*
findings there.

**Fix**: state what it writes, or give `login_report` a flag that suppresses the handoff
write so triage can stay what its header says it is.

### M8. The pin axis has no coverage floor, and the login report never states its coverage

**CONFIRMED**:

```
9 untracked pins -> findings: []
The report says how many of the 9 were compared: False
```

`check_pins.check:132-134` skips every untracked pin and returns nothing. Today 1 of 9 pins
is tracked (`qa-all` prints `9 pin(s), 1 with install state tracked, 8 declared
untracked`). If that one were flipped to `untracked`, the login-time pin check would
compare **zero** pins, emit zero findings, and `qa-version-pins.bash` would print
`0 with install state tracked` and exit 0 — a gate whose two sides derive from the same
shrinkable population.

Worse for the user-facing surface: the login report says **nothing** about the 8 pins it
never looked at, while `handoff.py`'s entire thesis is that unchecked must be visible.
`DESIGN-version-pins.md:46-47` — *"the gate prints … and the report says so out loud"* —
is true of the gate and false of the login report, which is the report a human reads.

**Fix**: emit `COVERAGE: n of m pins compared` on the login surface (AgentNotes: *"state
coverage as a number, never imply it from a list"*), and fail `qa-version-pins.bash` when
`tracked == 0`.

### M9. Three tracked documents describe a merge mechanism that no longer exists

**CONFIRMED**: `extra` has been removed from both `probe_results.build_report` and
`probe.collect` in the working tree, yet these all still describe it:

- `DESIGN-version-pins.md:85` — *"its findings merge into the one report through
  `probe_results.build_report`'s `extra`"*
- `DESIGN-host-health.md:69` — *"Phase 2's findings arrive through a single `extra`
  argument rather than a second report"*
- `PLAN.md:181` — *"findings merge through one `extra` argument"*

Before the removal, `grep -rn "extra=" tests/ helpers/` showed the parameter had **no
production caller at all** — only `test_probe_results.py:221,229` and `test_probe.py:152`.
`login_report.collect` always built its own list. So five statements (three documents plus
two docstrings) described a mechanism kept alive solely by its own tests.

**Fix**: correct all three documents, and finish the removal so QA goes green.

### M10. `_FETCH_TIMEOUT_SECONDS`' stated rationale is wrong for a `Type=oneshot` unit

`helpers/play_ledger/git_history.py:29-33` (uncommitted) — *"Without it the backstop is
systemd's 90s default `TimeoutStartSec`, which kills the unit"*.

**PLAUSIBLE**, from systemd.service(5): `TimeoutStartSec=` defaults to
`DefaultTimeoutStartSec=` except for `Type=oneshot`, where the timeout is disabled by
default. No systemd man pages are present in this container, so not reproduced. If that
reading is right there is no 90s backstop at all — a hung fetch would hold
`graphical-session.target` in `activating` indefinitely, which makes the Python-side
timeout *more* necessary than the comment claims, not less.

**Fix**: correct the comment and add an explicit `TimeoutStartSec=` to the unit so neither
reading is load-bearing.

---

## Already fixed in the uncommitted tree — fixes reviewed, all three correct

These were CONFIRMED defects at `fd74e09a` and are addressed in the working tree.

1. **A shared sink made diagnostics into findings.** Proved at `fd74e09a`: a `git fetch`
   failure *within* the staleness bound — which `DESIGN-host-health.md:139` decided must be
   silent — produced the user-facing finding
   `play-freshness: git fetch failed, judging on the refs on hand: fatal: unable to access …`.
   Now two `_Sink`s, with diagnostics re-emitted to real stderr.
2. **The untrustworthy branch discarded the reason and then pointed at it.** Proved at
   `fd74e09a` with a BROKEN ledger: the sole finding was
   `"… (see its stderr output above)"`, real stderr was `''`, and the BROKEN reason
   (`disk full`), the *"clear it deliberately; it never clears itself"* instruction and any
   `cannot judge <play>` detail were all dropped. Now the reason is folded into the finding.
3. **Inflated finding count.** Proved at `fd74e09a`: one stale play with three commits plus
   the offline diagnostic rendered as `5 findings`, with each commit subject its own bullet
   in the notification and its own bullet under "What is wrong". `_fold_detail_lines` now
   folds detail into its headline and keeps an orphan detail line rather than dropping it.

The seven new seam tests in `TestTheFreshnessSeamKeepsItsChannelsApart` drive the real
entry point through an injected `run`, which is the right shape. The stamp-write separation
in `check_freshness.py:100-111` is a genuine catch this review had not found — a stamp that
could not be written was reported as a failed fetch *and* as "never reached the remote",
two false statements about a fetch that had just succeeded.

**None of it was committed** when this review closed, and `PLAN.md` / `JOURNAL/` do not yet
reflect it — the Plan Commit Rule applies when it lands.

---

## Nits

- **`test_handoff.py:124-127` cannot fail.** `test_nothing_here_launches_anything` asserts
  `isinstance(handoff.offer("/x"), str)`. Adding a `subprocess.run` and returning the same
  string keeps it green. Its own docstring and `PLAN.md:223-226` both claim it would catch
  that. Fix: patch `subprocess.run` and assert not-called, or assert on the module source.
- **`test_handoff.py:68-74` tests exactly the one phrase that matches** — six lines from
  B2's defect. Table-drive it over every string the three producers emit.
- **The `is defined` half of the uid assert cannot fail** —
  `play-host-health-login-report.yml:87`. `ansible-doc -t module ansible.builtin.getent`
  gives `fail_key … default: 'yes'`, and `ansible/modules/getent.py:187` shows the `None`
  path is the `fail_key: false` branch only. So the preceding task fails outright when the
  key is missing and the assert is never reached. (`| int > 0` does still fail for uid 0.)
  Drop the dead clause, or set `fail_key: false` so the fail_msg becomes the accurate report.
- **Stale test counts in `PLAN.md`.** Line 203 claims a login_report count of nineteen; it
  was 23 at `fd74e09a` and 30 with the uncommitted work. Line 139 claims 47 across
  `freshness.py` / `git_history.py` / `check_freshness.py`; they ran 20+14+16 = 50 at
  `fd74e09a`. Both went stale inside the same day — `fb9bbc8c` added four login_report tests
  and `4d76e961` three check_freshness tests, neither touching a plan file.
- **`docs/playbooks.md:771`** states *"only a crash is a unit failure"* — user-facing and
  false per B1.
- **The sibling defect was diagnosed and left in place.** `play-container-watch.yml:125`
  still carries `XDG_RUNTIME_DIR: "/run/user/{{ user_login_uid | default(1000) }}"`, the
  exact pattern this play's comment (l.75-78) and the 16:29 journal entry describe as
  silently pointing at the wrong user's runtime directory. It is the last remaining
  instance — nine other sites use the getent form. AgentNotes: *"when a diff fixes an
  instance, ask what else in the repo has the same shape."*
- **A "measured" claim with no artefact.** The 16:29 entry's *"systemd's own source is
  `return streq(state, "running") ? EXIT_SUCCESS : EXIT_FAILURE` … Measured from the
  source, not recalled"* leaves no trace: no systemd source, man page or vendored document
  exists anywhere in this repo. The conclusion is right — systemctl(1) documents
  `is-system-running` as returning success only when the system is fully up and running,
  specifically not degraded — so the decision to delete the probe stands. The provenance
  claim does not.
- **A sweep narrower than its wording.** The 16:18 entry's *"The real run here proves the
  split works in the direction that matters"* over-reaches. Reproduced exactly: this
  container's four findings happen to be four of the seven phrasings that *do* match, and
  the handoff it wrote has no "What is wrong" section. It proves the split for 4 of 13
  reachable cases and is silent about B2's six.
- **`PartOf=graphical-session.target` on a `Type=oneshot`** with no `RemainAfterExit` is
  inert — there is nothing to stop-propagate to once it has exited. Not proven harmful;
  `After=` plus `WantedBy=` carry the behaviour.
- **The activation pattern has no precedent here, and it is the part that decides whether
  the report ever fires.** `DESIGN-host-health.md:105` says the unit is *"on the pattern
  `play-container-watch.yml` already establishes"* — true of `After=graphical-session.target`
  only. That unit has no `[Install]` section at all and is timer-activated
  (`container-watch.timer` → `WantedBy=timers.target`). `WantedBy=graphical-session.target`
  is new in this repo and unverifiable from a container. Task 3.1's open HOST item should
  explicitly assert that `list-dependencies graphical-session.target` names the unit — the
  same class as the deployed-but-not-enabled defect already caught once today.
- **`git_history.py:85-86`** collapses any `CalledProcessError` into the GONE signal
  (*"no longer exists at HEAD"*). Largely unreachable, because `changes_since` runs first
  with `check=True` uncaught, so a broken repo becomes UNTRUSTWORTHY before this is
  reached. Worth a comment saying so.
- **`login_report.main` mixes three kinds of line on stdout** — findings, the handoff offer
  and write/notify failure lines — with no marker distinguishing them. Low impact:
  `DESIGN-panel.md:41` has the panel calling `login_report.collect` directly rather than
  parsing stdout.
- **`check_freshness.py`'s stamp-failure message is a stderr diagnostic, not a finding.**
  Defensible (it does not affect this run's verdict) but it does mean the next run will
  report a false long-gap and nobody will have been told on a channel they read.

---

## Checked and clean

- **CCY version bump**: not required. No commit in `55b8ef4d..fd74e09a` touches
  `files/var/local/claude-yolo/` — verified from `git log --name-status`.
- **Public-repo safety**: no username, hostname, checkout path, container or project name,
  container ID, network name, private IP, account ID or non-`example.com` email in any
  tracked file in the diff. `WorkingDirectory` is templated from `root_dir`; `ansible.cfg`
  sets no `ansible_managed`, so only the default `Ansible managed` string reaches the
  deployed unit; `vars/version-pins.yml` carries only public project names and versions.
  `triage.bash` prints a `$HOME`-derived ledger path, but run logs are gitignored
  (`CLAUDE/Plan/.gitignore` → `*-runs/`), so nothing install-specific reaches a tracked file.
- **`ansible_managed | comment` at line 1 of a systemd unit**: valid. Rendered it —
  `'#\n# Ansible managed\n#'` — and comments before the first section are legal.
- **`getent_passwd[user_login][1]` really is the uid**: `ansible/modules/getent.py:177`
  stores `record[1:]`, so `[0]` is the password field and `[1]` the uid.
- **Subprocess hygiene**: no `shell=True`, no `os.system` anywhere in the new helpers; every
  call an argv list with an explicit `check=`; both `check=False` sites (`probe.py:58`,
  `check_pins.py:98`) inspect the returncode on the following lines. `git_history` is
  genuinely fetch-only — no merge, pull, checkout, reset or rebase verb in any argv.
- **Fail-fast**: no new `failed_when: false` or `ignore_errors: true` anywhere in the diff
  (the previous round removed the only one, along with its annotation). No `|| true`, no
  `2>/dev/null` silencing. `triage.bash`'s `probe()` captures the status in the `else`
  branch — `if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi` — which is exactly the shape
  AgentNotes requires and avoids the `$?`-after-terminator trap.
- **`triage.bash` against PlanScriptStandards R1–R14**: R1 marker walk verbatim with the
  load-bearing `.git` bound; R2 `plan_require_host` with a reason; R7 `plan_mode gather`;
  R4 `plan_start_log auto`; `plan_finish`; no `git rev-parse`; no hardcoded `/workspace`;
  both `# shellcheck` source directives present. It uses no `plan_gather_leg` and offers no
  `--help`, but 13 of 21 `triage.bash` scripts in this repo do the same — prevailing
  practice, not a deviation, so not flagged. R9: the two hedged characterisations
  ("may already be moot", "which is the clean case") are borderline but not verdicts.
- **Playbook mechanics**: shebang matches `scripts/make-playbooks-executable.bash`'s
  `SHEBANG` exactly; exec bit set; scope guard plus `provisioning_profile` assert present;
  `ansible.builtin.systemd` matches the repo's dominant spelling (20+ files vs 3).
- **Placement in the IaC graph**: `optional/common/` with no `playbook-main.yml` import
  follows the `play-container-watch.yml` precedent exactly. Worth naming as a design
  observation rather than a defect: this plan's own Overview says the 43 optional plays
  *"are run by hand, once, and then forgotten"*, and its login surface is now one of them —
  a host that never runs it gets no health surface and no complaint, by Task 1.3's
  deliberate silence-for-never-run rule.
- **`manifest.parse` cannot under-count**: it raises rather than dropping a row
  (`manifest.py:148-151`) and its loop has no `continue`, so `qa-version-pins.bash`'s row
  count cannot silently fall below the YAML's list length. That gate's three controls are
  real, and l.92-93 captures the status outside `if ! …` — the AgentNotes `$?` trap done
  right, with a comment saying so.
- **The plan's container smoke-run claim is accurate.** `PLAN.md:192-193` says *"3 findings,
  3 lines, exit 1"*; reproduced verbatim.

---

## Mechanical gates

| Gate | Result |
| --- | --- |
| `qa-all.bash` at `fd74e09a` | **PASS** — 847 files checked (`untracked/scratch/qa-all-review.txt`) |
| `qa-all.bash` on the live working tree | **FAIL** — 23 helper-test ERRORs, in-flight `extra` removal (`untracked/scratch/qa-all-2.txt`) |
| `hooks-daemon plan-qa --sweep` | 2 findings, **0 block**, 2 advise — both pre-existing and unrelated (a Plan 00046 path reference; journal-quiet plans) |
| `ansible-playbook --syntax-check` on the changed play | **PASS** |
| `qa-helper-tests.bash` | Triggered by this diff. Green at `fd74e09a` (1114 tests); red on the live tree, see above |
| `helpers.gnome.check_extension_compat` | **Not triggered** — no `extensions/` metadata change in range |
| `extensions` ESLint | **Not triggered** — no extension JS in range (`54eae25c` added only `DESIGN-panel.md`) |

`--syntax-check` note: it passes, and it cannot see B3. The bare `getent_passwd` is defined
today, so neither `--syntax-check` nor `qa-ansible.bash` can reject it — the same blind
spot the self-defaulting-var grep was added for.

## Disclosure

This review ran `python3 -m helpers.host_health.probe` and
`python3 -m helpers.host_health.login_report --no-notify` in the container to reproduce
behaviour. The latter wrote `host-health-findings.md` (0600) under the container's own
`~/.local/state/` — which is itself the evidence for M7. No repository file was modified by
this review; only this report was written.
