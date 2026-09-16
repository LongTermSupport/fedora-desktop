# QA Review — commit `a90c88de` (Plan 00109 ledger 2.19 fix) + the two CI-only test failures

**Verdict: FIX-BEFORE-MERGE** (already landed — `a90c88de` is 14 commits back on `F44`, pushed).
No blockers: the fix is correct and was verified independently, not taken on the commit's word.

---

## JOB 1 — commit `a90c88de`

### The central claim holds — re-derived, not repeated

`ansible` **is** importable here via the pipx interpreter
(`/root/.local/pipx/venvs/ansible/bin/python`, ansible-core **2.19.13**). A real playbook was
loaded through `Play.load` and the **production** `callback_plugins/play_ledger.py:_source_position`
called on the result:

```
type(play._ds): <class '...._AnsibleTaggedDict'>
legacy ansible_pos: NO-ATTR
play._origin: Origin(path='/workspace/playbooks/imports/play-claude-code.yml', line_num=3, col_num=3)
PLUGIN _source_position -> ('/workspace/playbooks/imports/play-claude-code.yml', 3, 3)
play_source -> /workspace/playbooks/imports/play-claude-code.yml
COPY _source_position -> (same)
```

**Tagged-dict trap: clean.** Nothing rebuilds the mapping anywhere. `_origin` is read off the play
object, set at `playbook/base.py:157` (`Origin.get_tag(ds)`), initialised at `:113`, and
**preserved by `Base.copy()` at `:440`** — which is load-bearing, because callbacks receive
`new_play = play.copy()` (`executor/task_queue_manager.py:346`), not the parsed play. The `COPY`
line above is that path measured.

`Origin` is a dataclass with `path | description | line_num | col_num`; `_post_validate` requires
`path` to be absolute. `Origin.UNKNOWN` carries `path=None`, so it falls through to the legacy
shape and then to `None`, and `play_source` refuses — a recorded hole, never a guess.

### Should fix

**1. Clearing the sentinel silently un-declares an open question the pin check depends on**
`helpers/play_ledger/check_freshness.py:190`, consumed at `helpers/host_health/login_report.py:228`
and `helpers/version_pins/check_pins.py:288`.

`plays_run_here`'s own docstring says the sentinel **is** the declaration that the question is
open, and that a set read from the ledger anyway "would silently suppress every ABSENT verdict
whose row is in the hole, with nothing saying so". `--clear-broken` deletes that declaration.
Measured, on a ledger holding one row plus a sentinel:

```
with the sentinel   , plays_run_here -> None
after --clear-broken, plays_run_here -> {'playbooks/imports/play-x.yml'}
```

`None` keeps every ABSENT pin reported; the partial set makes `ran_here()` False for every other
pin, so `check_pins.py:288` skips them.

*Failure scenario:* a transient write failure holes a full run (nothing is written, because
`write_records` only fires at `v2_playbook_on_stats` and `_broken` short-circuits it). The
operator later runs one play, then clears the sentinel. `evdi_version (pinned 1.15.0, nothing
installed)` is now silently skipped for ever, and `ledger_presence.findings` is silent too because
the ledger is non-empty (`helpers/play_ledger/ledger_presence.py:69-70`). The printed
"NOT recovered" sentence is the only record of the hole, and it is terminal output.

*Fix:* make the clearing durable — append a `cleared-hole` row to `runs.jsonl` (the genesis record
already exists to let a reader date its silences), or leave a `CLEARED` marker that
`plays_run_here` reads as `None` for rows before that stamp.

**2. The channel the operator actually sees still does not name the remedy**
`helpers/play_ledger/check_freshness.py:149`.

`record_failure` (`plugin_support.py:121-127`) gained the command, but that line prints during an
`ansible-playbook` run and stops the moment the cause is fixed. The **persistent** surface is
login. Measured output of `login_report.main` with a sentinel present:

> play-freshness could not give an answer, so no play was judged: … reason: … ValueError: the play
> carries no source position; clear it deliberately once the cause is fixed; it never clears itself.

`--clear-broken` appears nowhere in it (asserted). The same non-actionable line lands in the
handoff file and the status document. This is AgentNotes' *"Generalise a fix past the file you were
reading"*: one of the two places that tell the operator the ledger is broken was fixed. One-line fix.

**3. Nothing tests `main`'s `--clear-broken` wiring**
`helpers/play_ledger/check_freshness.py:218-219`.

`grep` for `check_freshness.main` across `tests/` returns nothing — no test drives `main` at all.
All six new cases call `check_freshness.clear_broken(base=…, stdout=…)` directly. A flag-name typo,
an inverted branch, or a dropped `if` leaves all 23 tests green. This is the same shape as the
defect the commit fixes: `store.clear_broken` was tested with no caller; now
`check_freshness.clear_broken` is tested and *its* caller is untested. (The CLI does work today —
it was run end to end.)

**4. Both new test classes sit AFTER `if __name__ == "__main__": unittest.main()`**
`tests/helpers/play_ledger/test_check_freshness.py:329`, `tests/helpers/play_ledger/test_plugin_support.py:301`.

```
python3 tests/helpers/play_ledger/test_check_freshness.py   -> Ran 17 tests  OK
python3 -m unittest tests…test_check_freshness              -> Ran 23 tests  OK
test_plugin_support.py: 40 direct vs 42 by module
```

`qa-helper-tests.bash` uses module names, so CI collects them — but the `__main__` block exists for
direct execution, and direct execution silently drops the six `TestClearBroken` cases and prints
`OK`. Across all 63 `tests/**/test_*.py` these are the **only two** files whose `__main__` block is
not at the end. *Fix:* move the appended classes above it.

**5. No gate catches the next Ansible rename**

The regression that motivated this commit would ship silently again; the plugin↔helper seam is
covered only by `_FakeOrigin`. A real gate is roughly twenty lines and was demonstrated while
reviewing: resolve the interpreter from `command -v ansible-playbook`'s shebang (works both in the
CCY container, where `import ansible` fails under `python3`, and in CI, which pip-installs
ansible), `Play.load` one tracked playbook, assert
`plugin_support.source_position(play._origin, getattr(play._ds, "ansible_pos", None))[0]` equals
that file. Task 1.2's open HOST/VM item is not a substitute — it runs by hand.

**6. The documented remedy fails from any cwd but the repo root**
`helpers/play_ledger/plugin_support.py:125-127`.

Measured from `/tmp`:
`python3 -m helpers.play_ledger.check_freshness --clear-broken` →
`ModuleNotFoundError: No module named 'helpers'`. From the repo root it works. The same bare
command is now the fix instruction in the public issue #46 comment. *Fix:* `cd <repo> && python3 -m …`.

**7. Nothing pins that `--clear-broken` clears ONLY the sentinel**

A mutation that also `os.unlink`s `runs.jsonl` cleared all 23 tests with no failure. The code is
correct today — `helpers/play_ledger/store.py:96-100` unlinks `<base>/BROKEN` only, and a real CLI
run left `runs.jsonl` at 2 lines before and after — but the suite would not notice a regression in
the one operation in this plan that deletes host state on an operator's instruction. Two-line test.

### Nits

**8.** `test_a_non_string_origin_path_is_not_a_path` only passes `path=None`, so dropping
`isinstance(path, str)` for a plain `is not None` check survives it (measured: only
`test_a_blank_origin_path_is_not_a_path` failed under that mutation). The test is named for the
half it does not exercise.

**9.** "Verified end to end against real 2.19 objects" (commit message and
`JOURNAL/00109-Journal-26-09-15.md`) carries no recorded output — the quoted probe block is the
*pre-fix* diagnosis. The claim is true; nothing in the tree shows it. Per AgentNotes,
*"measured" is a claim with a scope*.

**10.** `clear_broken` puts its no-op line and its "the reason could not be read" diagnostic on
**stdout**, while `_emit` puts the same subject on stderr. The report-command exception makes
stdout defensible; the inconsistency is not.

### Checked and clean

- **Mutation controls, baseline green first** (per AgentNotes, *a control experiment that surprises
  you is usually a broken control*): legacy-only → 3 failures including
  `test_the_result_feeds_play_source_directly`; legacy-wins-over-origin → 1; return-legacy-verbatim
  → 1; drop the "NOT recovered" line → 2; never-unlink → 2. All caught.
- **`--clear-broken` destroys nothing it should not, and a normal run still refuses**: real run
  measured `exit=2`, sentinel intact, then `--clear-broken` → sentinel gone, `runs.jsonl` intact.
- **Fail-fast**: no `failed_when`/`ignore_errors` in the diff (no YAML); the single
  `except OSError` (`check_freshness.py:186`) reports into the printed reason and carries its
  rationale; no new `subprocess` call, so no `check=` to audit.
- **Stderr hygiene**: `record_failure`'s line reaches the operator through `_warn`
  (`callback_plugins/play_ledger.py:106-110`) → stderr.
- **Placement in the IaC graph**: a flag on the CLI that already exists, not a new entry point —
  correct. `login_report` calls `check_freshness.run` directly and can never reach the clearing path.
- **Version bumps**: no `files/var/local/claude-yolo/**` change, so no `CCY_VERSION` /
  `REQUIRED_CONTAINER_VERSION` bump is owed.
- **Plan Commit Rule**: PLAN.md and JOURNAL are in the same commit; Task 1.2 stays `🔄` with its
  HOST/VM item open. Nothing marked ✅ that was not done. Tree clean, nothing unpushed.
- **Public-repo safety**: the `Origin(path=…)` evidence is elided in both the commit message and the
  journal; the issue #46 comment carries no host identifiers.

### Mechanical gates

- `./scripts/qa-all.bash`: **green**, 906 files checked (`helper-tests: Ran 1402 tests`). The
  `⚠ shellcheck: 169 issues` and `⚠ patterns: 15 file(s) parsed only in part` lines are
  pre-existing advisories, not this diff.
- `.claude/hooks-daemon/bin/hooks-daemon plan-qa --sweep`: exit 1 — **0 block, 2 advise**, neither
  in 00109 (a stale path reference in 00046, journal-freshness for 12 other plans).
- `ansible-playbook --syntax-check`: **not triggered** — no playbook in the diff. `qa-all`'s
  `ansible-syntax` stage reported 82 playbooks OK, and `callback_plugins/play_ledger.py` imports
  cleanly under the real 2.19 interpreter (the probe above), which is the check that matters since
  `ansible.cfg` loads it on every run.
- `qa-helper-tests.bash`: run inside `qa-all`. `check_extension_compat` and eslint: **not
  triggered** — no `extensions/` change.

---

## JOB 2 — the two CI-only failures: both determined, both reproduced byte-exactly

Neither is about the ledger location. The `HOME` / `XDG_STATE_HOME` / `XDG_DATA_HOME` hypothesis was
correctly falsified and is not revisited here.

### 2a. `test_login_message.py::TestTheEntryPointALoginShellCalls::test_it_exits_zero_and_prints_nothing_when_clean` — **defective TEST**

`tests/helpers/host_health/test_login_message.py:40` hardcodes `KERNEL = "7.2.4-200.fc44.x86_64"`
and `document()` stamps it into the status document. `login_message.main` reads the **real** running
kernel — `helpers/host_health/login_message.py:261`, `probe.running_kernel()` = `os.uname().release`.
Every other test in that file passes `running_kernel=KERNEL` explicitly through the `render()`
helper; `main` is the one entry point that cannot be given it, so it reads host state. On a runner
the kernel differs → `status_document.is_boot_stale` True → a reboot finding → non-empty output.

Reproduced by faking only the kernel:

```
real kernel (this container): (0, '')
CI-like kernel 6.11.0-1018-azure:
(0, 'fedora-desktop: this machine needs attention\n  Not checked — these are NOT clean results,
     nothing is known about them:\n  - these results were collected under kernel
     7.2.4-200.fc44.x86_64 and this host is now running 6.11.0-1018-azure, so the post-boot checks
     describe a different boot and nothing has looked at the kernel you are on\n')
```

That is the reported CI string.

**A second, independent failure mode sits in the same test.** `NOW = "2026-09-14T18:00:00Z"` is the
document's stamp, while `main` calls `repo.utc_now()`. Faking only the clock, with the kernel
matching:

```
today   : ''
+14 days: '… the host status was last collected 14 days ago, so nothing here describes this machine as it is now'
```

So it goes red **everywhere on 2026-09-28**, kernel fix or not. `STALE_AFTER_DAYS = 14`
(`login_message.py:52`).

*Fix:* inject both. `main` already has `--state-dir`; give it a seam for the running kernel and the
clock, or assert through `read_and_render`, which takes both as keyword arguments.

### 2b. `test_handoff.py::TestTheHandoffCanBeSuppressedForTriage::test_the_findings_are_still_reported_either_way` — **defective TEST**

`tests/helpers/host_health/test_handoff.py:190-194` drives `login_report.main` against the **live
host** and asserts the prose `"could not run"`. The sibling case at `:180` states the dependency
outright: *"This container always has findings — no dkms, no systemd bus — which is what makes it a
usable fixture for the write path."*

Measured in this container:

```
the system-scope failed-unit probe could not run: systemctl: System has not been booted with systemd as init system (PID 1)…
the user-scope failed-unit probe could not run: systemctl: Failed to connect to bus…
no play run has ever been recorded on this host, so the ledger is empty — …
```

Both `"could not run"` lines come from `probe_results._unit_outcome_findings`
(`helpers/host_health/probe_results.py:186`) failing **because this container has no systemd**. A
GitHub `ubuntu-latest` runner has systemd as PID 1 and no failed units, and has neither `dkms` nor
`/var/lib/dkms` — which takes the deliberate silent branch at `probe_results.py:213-223`. Emulating
exactly that (systemctl probes ok, dkms missing, `DkmsRegistry(present=False)`):

```
STDOUT: 'no play run has ever been recorded on this host, so the ledger is empty — every check that
         reads it is answering from nothing while reporting as though it had looked\n'
contains 'could not run': False
```

Byte-identical to the CI `printed` quoted in the dispatch. `test_by_default_the_handoff_file_is_written`
survives on CI only by luck: the ledger-empty finding happens to be present, so `written` is True.

*Fix:* inject `probe.run_probe` / `probe.dkms_registry` for this class — both are already documented
seams (`probe.collect(runner=…, registry=…)`) — and assert on `Finding.checked` rather than on
prose, which is what `TestTheSplitDoesNotGuessFromWording` in the same file already argues for.

### Conclusion for Plan 00125 Task 1.3

Both failures are **tests reading unowned host state**, not production paths misbehaving.
Production is doing its job in both cases: reporting a kernel mismatch and reporting that systemd
could not be asked is exactly what these modules exist for. Two tests, two host facts
(`os.uname().release`; whether systemd is PID 1), plus one calendar bomb due **2026-09-28**. That
accounts in full for `qa-all.bash` being green here and red on a runner.
