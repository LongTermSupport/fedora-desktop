# QA — Quality Assurance Scripts

## Primary Rule

**ALWAYS run QA before committing changes to Bash, Python, or Ansible files.**

**ALWAYS and ONLY use this single command:**

```bash
./scripts/qa-all.bash
```

**NEVER use individual scripts directly** (`qa-bash.bash`, `qa-python.bash`, `qa-patterns.bash`) — always use `qa-all.bash`.

---

## What qa-all.bash Runs

`qa-all.bash` runs **forty** gates. Seven merge their JSON into
`/tmp/qa-results.json`; the other thirty-three run separately (see below). Those seven emit
**eight** named verdict lines — `qa-bash.bash` prints `bash` and `shellcheck` — so a run
shows 41 stage names for 40 gates. A missing **required** tool makes a stage (and the whole
run) exit `2`; a real analyser crash (e.g. ruff/shellcheck exit ≥ 2) is a hard failure,
never silently treated as "0 issues".

**This inventory is derived, not maintained.** `helpers/docs/link_check.py` parses the
gate invocations out of `qa-all.bash` and fails the docs gate for any that has no row
below, so a gate added without a row is caught on the same commit. It is derived because
it kept going stale: the counts above were wrong by eleven and the table was missing ten
rows, having been "corrected" more than once by swapping one hand-written list for a
fresher hand-written list.

| Script                   | Checks                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  | Files                                                                                                                                                 |
| ------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| `qa-bash.bash`           | `bash -n` (always) + shellcheck (**required** — exits 2 if absent, Plan 00075). Exits 2 if discovery finds **0 files**, and (Plan 00076) if it misses **any tracked shell script**. **shellcheck `error` AND `warning` findings GATE** (raised in Plan 00075 — SC2155 is this repo's own defect class); `info`/`style` advisory.                                                                                                                                                        | Repo-owned bash (excludes `roles/vendor`, `.claude/hooks-daemon`, `.claude/ccy`, `.claude/skills`)                                                    |
| `qa-python.bash`         | `python3 -m py_compile` + ruff (ruff exit ≥ 2 = hard fail; no `--fix` mutation in the check path). Exits 2 if discovery finds **0 files**, and (Plan 00081) if it misses **any tracked Python file**. A `.j2` with a Python shebang is **rendered then compiled** — `{{ … }}` → `None`; a `{% … %}` statement is a hard failure, never a skip                                                                                                                                           | Repo-owned Python files — discovered by extension **or shebang, regardless of file mode**; plus Python `.j2` templates, syntax-checked but not linted |
| `qa-patterns.bash`       | Semgrep rules from `.semgrep/bash-conventions.yml` (`\|\| echo` and other error-hiding patterns). Scans a temp mirror so coverage does not depend on file mode, and exits 2 if any discovered file is absent from `.paths.scanned` (Plan 00076)                                                                                                                                                                                                                                         | Repo-owned bash                                                                                                                                       |
| `qa-ansible.bash`        | Fail-fast grep (`failed_when: false`/`ignore_errors` without same-line `# FAIL-FAST-OK:`, case-insensitive), **self-default vars** (`x: "{{ x \| default(…) }}"` — the 2.19 recursive-loop footgun `--syntax-check` can't see), **deprecated fact vars** (both `ansible_<fact>` and the un-prefixed injected names like `getent_passwd`, which no `ansible_`-anchored pattern can reach), **a guessed uid in a `/run/user/{{ … }}` path**, **plus** playbook shebang + exec-bit hygiene | `playbooks/ tasks/ vars/ environment/ roles/` (excludes `roles/vendor`), `*.yml`/`*.yaml`                                                             |
| `qa-ansible-syntax.bash` | `ansible-playbook --syntax-check` on every playbook — a file with a top-level `- hosts:` **or `- import_playbook:`** (Plan 00081 F9/F14: deriving from `hosts:` alone dropped `playbook-main.yml`). Parse-only — safe in the CCY container. The pass line states the breakdown, so a coverage change is visible                                                                                                                                                                         | **Repo-wide**, not a fixed path list; excludes vendor/upstream trees. Includes playbooks under `CLAUDE/Plan/**`                                       |
| `qa-js.bash`             | `node --check` on repo JS + `eslint .` in `extensions/`                                                                                                                                                                                                                                                                                                                                                                                                                                 | Repo-owned `.js` (excludes vendor/node_modules) + `extensions/`                                                                                       |
| `qa-docs.bash`           | Link targets exist; every `#anchor` matches a real heading; every play imported by `playbook-main.yml` is named in both `docs/playbooks.md` and `docs/architecture.md`; every `CLAUDE/*.md` has an index row (Plan 00070)                                                                                                                                                                                                                                                               | Core docs only — `docs/`, `CLAUDE/*.md`, `README.md`, `*/CLAUDE.md`, `.claude/rules/`. **Not** `CLAUDE/Plan/**`                                       |

Thirty-three further gates run inside `qa-all.bash` as **hard, non-structural** checks —
they are deliberately not jq-merged stages, so they cannot disturb the positional
`.[0]..[6]` JSON merge. Any one of them fails the whole run immediately:

| Gate                                        | Checks                                                                                                                                                                                                                                                                                                                           |
| ------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `qa-nokill-containerwatch.bash`             | the container-watch watchdog has gained no process-termination call site                                                                                                                                                                                                                                                         |
| `qa-deployed-drift.bash`                    | every repo-owned `files/home/.local/bin/` script matches its deployed `~/.local/bin/` copy                                                                                                                                                                                                                                       |
| `qa-helper-tests.bash`                      | the `helpers/` unit suite (Plan 00081 F11); `--counts-file` reports its size and skip count as data                                                                                                                                                                                                                              |
| `test-secret-scan.bash`                     | the pre-commit secret scanner's own unit suite (Plan 00092)                                                                                                                                                                                                                                                                      |
| `test-planlib.bash`                         | the `_planlib.inc.bash` regression suite behind every plan script (Plan 00092)                                                                                                                                                                                                                                                   |
| `test-ccy-rootless-guard.bash`              | ccy's rootless-engine verdict (Plan 00072); pure function, no podman needed                                                                                                                                                                                                                                                      |
| `test-ccy-token-mode.bash`                  | `select_token`'s per-mode answer to an unusable token pool (Plan 00048, CCY 3.50.0)                                                                                                                                                                                                                                              |
| `test-ccy-ssh-handling.bash`                | ccy's SSH key and agent handling into the container                                                                                                                                                                                                                                                                              |
| `test-ccy-selinux-verdict.bash`             | ccy's SELinux verdict, including the states that must refuse                                                                                                                                                                                                                                                                     |
| `test-ccy-gpu-device.bash`                  | ccy's GPU device passthrough decision                                                                                                                                                                                                                                                                                            |
| `test-ccy-host-hostname.bash`               | `ccy_host_hostname` — the RFC 1123 grammar guarding `CCY_HOST_HOSTNAME` (Plan 00121)                                                                                                                                                                                                                                             |
| `test-ccy-session-registry.bash`            | the session registry a boot-time restore acts on unattended: partial-write refusals, restore-flag reconstruction, the flag classification derived from the launcher's own parser, and the launcher's unattended `read` guard (Plan 00123)                                                                                        |
| `test-ccy-session-restore.bash`             | `ccy-sessions-restore`, the boot-time service deciding which recorded sessions come back: every retirement reason, the live-boot guard, and that a record is consumed before its session starts (Plan 00123)                                                                                                                     |
| `test-ccy-sessions-status.bash`             | `ccy-sessions restore-status` and the pre-reboot audit: every installation state including the two "could not tell" ones, and the audit's ready/no-CLI table, partial-warning refusal and listing-failure branch — driven through stub systemctl/loginctl/tmux, because the broken states are unreachable otherwise (Plan 00123) |
| `test-vmtest-host-only-gate.bash`           | `host_only_preflight`, the host-CLI gate on a credential-bearing VM scenario (Plan 00121)                                                                                                                                                                                                                                        |
| `test-vmtest-reboot-dispatch.bash`          | `reboot_guest`/`guest_prepare` — a run judged on the wrong boot has no other symptom (Plan 00109)                                                                                                                                                                                                                                |
| `test-panel-sections.bash`                  | the panel's own decisions on boot-stale, malformed and `state`-disagreeing documents (Plan 00109)                                                                                                                                                                                                                                |
| `test-vmtest-prepare-record.bash`           | the fixture→checker record seam — one metacharacter unset every key after it (Plan 00109)                                                                                                                                                                                                                                        |
| `test-vmtest-kernel-selection.bash`         | `select_second_kernel` — the one step of that route no machine here can execute (Plan 00109)                                                                                                                                                                                                                                     |
| `test-run-bash-headless-localhost-yml.bash` | the headless `localhost.yml` writer (Plan 00119)                                                                                                                                                                                                                                                                                 |
| `test-run-bash-ssh-agent-teardown.bash`     | `hl_ssh_agent_stop`, including an agent that SURVIVES the kill (Plan 00063 Task 3.4)                                                                                                                                                                                                                                             |
| `test-run-log-scrub.bash`                   | the run-log secret scrubber, driven by a deliberately incomplete redaction (Plan 00121)                                                                                                                                                                                                                                          |
| `test-freezelib.bash`                       | the freeze library both freeze tools source — every decision under BOTH state vocabularies (Plan 00122)                                                                                                                                                                                                                          |
| `test-lxcfreeze.bash`                       | `lxcfreeze`'s decisions — a state or a config it could not read must not resolve to a fact (Plan 00122)                                                                                                                                                                                                                          |
| `test-podfreeze.bash`                       | `podfreeze`'s decisions, pinned before Plan 00122 Task 4.2 extracted a library out of it                                                                                                                                                                                                                                         |
| `test-host-health-login-snippet.bash`       | the server login snippet's interactive guard — an unconditional print breaks `scp` (Plan 00109)                                                                                                                                                                                                                                  |
| `test-qa-ansible-failfast.bash`             | the fail-fast directive regex in `qa-ansible.bash`, read from it rather than copied                                                                                                                                                                                                                                              |
| `test-qa-helper-summary.bash`               | the three readers in `lib/qa-helper-summary.bash` that produce every stage line below                                                                                                                                                                                                                                            |
| `test-qa-docs-exit-codes.bash`              | `qa-docs.bash`'s three exit codes, driven against fixture trees — a crash must never read as clean                                                                                                                                                                                                                               |
| `helpers.gnome.check_extension_compat`      | every extension declares the GNOME Shell major this branch's Fedora ships                                                                                                                                                                                                                                                        |
| `helpers.gnome.check_panel_contract`        | the panel's constants, document keys and section ids agree with the producer (Plan 00109)                                                                                                                                                                                                                                        |
| `qa-vmtest-manifest.bash`                   | `vars/vm-test-scenarios.yml` parses and is coherent (Plan 00110); a broken control must be rejected first                                                                                                                                                                                                                        |
| `qa-version-pins.bash`                      | `vars/version-pins.yml` parses, and every row still names a playbook that declares that var (Plan 00109)                                                                                                                                                                                                                         |

`qa-helper-tests.bash` and `check_extension_compat` were **documented here as gates and
not run by `qa-all.bash`** until Plan 00081. Following this document's own "ALWAYS and
ONLY use `qa-all.bash`" rule, a `helpers/` change earned `✓ QA passed` with its unit
suite never executed. The fix was to run them rather than to soften the rule.

The `test-*` suites are the same shape of hole, closed later: each guards a defect
class whose regression is **silent by construction**. A leak the secret scanner stopped
catching produces no signal on any commit, and a `_planlib.inc.bash` regression surfaces
only when someone next runs a plan's host script — which may be months, and on a plan
nobody is working on. Being exercised incidentally is not the same as being tested.

`test-ccy-rootless-guard.bash` was the sharpest case, because it was worse than
unrun: it ran in `.github/workflows/qa.yml` and nowhere locally, so this page's
"ALWAYS and ONLY use `qa-all.bash`" was false for anyone touching ccy's engine
guard — green here, red in CI. **When adding a suite, add it to `qa-all.bash`
first; CI runs `qa-all.bash`, so a separate CI step is a divergence, not a
belt-and-braces.** `scripts/test-ccy-ssh-probe.bash` is deliberately not a gate:
it needs a real host and a `gh` token, so it is a host diagnostic.

### The same command does not reach the same verdict everywhere

`qa-all.bash` is the authority, but some stages read what the *machine* supplies rather
than what the repository ships. A local run and a CI run disagreeing is a fact about the
stage, not a flaky gate — find which input differs before touching anything.

| Gate                     | What it needs from the machine                                                                                      | Where that is missing                                                    |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
| `qa-ansible-syntax.bash` | a vault password file to **exist** (never read — `--syntax-check` does not decrypt)                                 | a clean checkout, and a linked worktree                                  |
| `qa-js.bash`             | `extensions/node_modules` — its own message says no playbook installs it. Exits **2**, which still aborts the run   | a linked worktree, and any checkout where `npm install` has not been run |
| `qa-deployed-drift.bash` | deployed copies under `~/.local/bin` to compare the repo against                                                    | the CCY container and a clean checkout — it self-skips **and names why** |
| `qa-helper-tests.bash`   | one pair asserts against real `/sys/class/drm` and skips where no connector with a physical display link is present | a VM whose only connector is virtual                                     |

`qa-deployed-drift.bash` is the shape to copy: it states the dependency, skips only for a
reason it prints, and the reason is checkable.

`qa-js.bash` is the shape NOT to copy, and it is in this table because a reviewer hit it,
not because anyone predicted it: exiting 2 makes it a gate whose absence removes every gate
behind it, which is the mechanism this page's own subject was hiding inside for three weeks.
A *failing* gate no longer does that; a gate that cannot RUN still does, which is why this
row matters more than it did. On a fresh clone `qa-all.bash` reaches `qa-js.bash` and stops
there — including before `qa-docs.bash`, the gate Plan 00125 repaired.

**`qa-docs.bash` used to head this table and no longer belongs in it.** It needed
`.claude/hooks-daemon/` on disk, because eight tracked `.claude/rules/*.md` files are
GENERATED by that repository's installer and link into it — so the same commit was green
here and red in CI for three weeks. It now classifies a link target three ways instead of
two, and the verdict no longer depends on what is installed:

| The target is…                    | Verdict                                                 |
| --------------------------------- | ------------------------------------------------------- |
| tracked by this repo              | checked as before; missing is a **failure**             |
| inside a declared vendored repo   | **warned on, never failed** — three outcomes below      |
| untracked, and vendored by nobody | **failure** — a link to something no clean checkout has |

The third row is what makes the second safe. "Ignored, so skip it" would have quietly
exempted a link into `untracked/` as well, trading a false failure for a silent skip — this
repository's recurring defect, in the fix for an instance of it.

**The third row says *untracked*, not *ignored*, and the two are different populations.** A
tracked file that happens to match an ignore rule moved **out** of that row; a file that is
present, unignored and simply never committed moved **in**, and that one is the finding —
green here, red in a clean checkout. `git check-ignore` answers "does `.gitignore` name this
path"; `git ls-files` answers "does this repository carry it". Only the second is a question
every checkout answers the same way.

**A vendored target is still looked at**, in the one case where looking means something:

| The vendored repo is… | The target is… | Outcome                                        |
| --------------------- | -------------- | ---------------------------------------------- |
| present               | there          | `verified` — silent                            |
| **absent** (CI)       | unknowable     | `unverifiable` — soft; nothing here could say  |
| present               | **missing**    | `broken` — its own `⚠` line, listing the links |

`broken` means the link is demonstrably wrong, which usually means that repository moved the
file and our generated pointers are stale — worth saying loudly, and still not ours to fix.
So it warns and does not fail: **the gate's exit code must never depend on what is
installed**, which is the whole point. What the gate SAYS may, and should — a machine that
can see the repo can say more about it, and saying more never flips a verdict. Anchors into
a vendored repo are not followed even when it is present: their headings are theirs to
rename, and going red on another repo's churn would be a dependency on it for a defect we
could not fix.

That `⚠` line is composed by `vendored_warning_lines()` in the checker, not by
`scripts/qa-docs.bash`, and it returns a list the script prints with **no condition of its
own**. The state that fills that list needs a vendored repo present *and* a stale pointer
into it, so a `if broken > 0` around the formatting would be a branch whose first execution
is the one nobody is watching. Empty list, empty output, formatting code exercised on every
run — the same reason a gate that only prints on failure gets a pass line.

**The question asked is trackedness** — `git ls-files`, plus the directories it implies,
since a link to `docs/` is a link to something this repo plainly owns. The index ships in
every clean checkout, so CI asks the same question.

Existence is checked **first** and trackedness second, deliberately. A typo'd link is both
absent and untracked, and `target does not exist` is the message that helps. Both are
findings either way, so a present-but-untracked target fails here and fails in CI for the
other reason: same exit code, different sentence.

That ordering is also the fix for the first version, which asked `git check-ignore`. That
question is machine-independent but narrower than the finding it raised: a file sitting on
one disk, never `git add`ed and matching no ignore rule, is not ignored — so it passed there
and failed in CI. Cause A's own shape, inside the classification built to remove it.

**The TARGETS ask trackedness; the DOCUMENTS are whatever the tree walk finds**, so the scan
population is still what is on this disk. An in-scope markdown file nobody committed is
scanned here and simply absent in CI. Excluding it would be the wrong fix — a broken link in
a file you have not committed yet is a true finding, and catching it before the commit is
what a local gate is for — so the stage line carries the denominator instead:
`71 files (71 tracked)`, the same shape as `helper-tests`' `65 modules (65 tracked)`. Equal
numbers mean the two machines are reading the same set; unequal numbers name the gap.

The vendored roots are DECLARED in `_VENDORED_ROOTS` (`helpers/docs/link_check.py`) rather
than detected, because in CI there is nothing on disk to detect — probing for a `.git` would
answer one way here and another there. They are declared as parents (`roles/vendor/`, not
each role), so vendoring under an existing root needs no code change; a new root is a
one-line edit, and the finding that prompts it names the nested repository when the machine
can see it. **Nothing checks that a declared root really is a vendored repository** — adding
one exempts every link under it, and only review stops that. The stage line carries all
three counts for the same reason `version-pins` prints `COVERAGE: 9 of 9` — an exemption
nobody counts reads exactly like a check that ran and found nothing.

A tree git cannot answer for — no checkout at all — makes the gate exit 2 rather than assume
nothing is tracked. Assuming would be a confident verdict derived from a check that did not
run, and it would condemn every link in the repository.

**A skip is not a pass, so the `helper-tests` line carries the skip count.** `unittest`
counts a skipped test inside `testsRun`, so `Ran N tests` is byte-identical whether a test
asserted or skipped itself — two machines then report the same verdict over different
executed populations, which is this page's own subject appearing in the line used to detect
it. The count differing is the signal; the skip *reason* names what was ignored and is
printed by `python3 -m unittest -v <module>`, not by the suite at its default verbosity.

**That count is read from a file, never scraped from the run's output.**
`qa-helper-tests.bash --counts-file PATH` has
`helpers/qa_environment/unittest_counts.py` take the numbers from unittest's
`TestResult` object and write them there; `helper_counts_summary` in
`scripts/lib/qa-helper-summary.bash` reads the file and **fails** rather than reporting
zero if it cannot. Four readers that parsed the text instead were each defeated by a test
printing unittest-shaped output — a decoy crosses the summary in either direction
depending only on Python's 8KB stdout buffering, and an `atexit` handler writes to stderr
after it. A test can put anything on either stream in any order, so the streams are not a
source of truth for this; the result object is. Both halves are covered by
`test-qa-helper-summary.bash`, whose last case runs the real runner end to end so a format
change on one side alone turns it red.

The file is **harder to reach than a stream, not unreachable**: its path travels in `argv`, so
a test that reads `sys.argv` could overwrite it — and this module's own tests are collected by
the runner they test. What protects it there is **write ordering**: the runner writes after
every test has finished, so a mid-run forgery is overwritten. A write landing *after* the
runner's — `atexit`, or a surviving thread — beats that, and nothing detects it.
`--counts-token` covers a different case only: a file written by something that never read
this run's `argv`, such as a stale file or a concurrent run. It is not a lock, because the
token rides in the same `argv` as the path. An **empty** file is refused with its own message,
which matters because a test calling `os._exit(0)` skips the write while the process still
exits 0 — leaving exactly the zero-byte file `mktemp` created. Existence is not generation.

`qa-all.bash` also **captures** the run's stdout and requires it to be empty. That stream is
the one `verdicts.py` parses for stage lines, so a single `print()` anywhere in the suite
could forge or split this suite's own verdict; a precondition that broad is a gate rather
than a comment. (This sentence carried the test count until it had rotted twice and the two
copies of it disagreed. A number that must be re-measured to stay true does not belong in
prose — the stage line prints the live one on every run.)

### The stage-line readers are shared, and tested against the gates they read

`scripts/lib/qa-helper-summary.bash` holds all three, and between them they produce the
summary in **every** stage line `qa-all.bash` composes — 29 of them. `qa_pass_line` prints
it, and prints nothing when that gate has already failed, so a gate cannot report both
outcomes. Only `deployed-drift` builds its own line, because there the line IS the gate's
output rather than a summary of it.

| Function                  | Used by                              | Degrades to                       |
| ------------------------- | ------------------------------------ | --------------------------------- |
| `helper_counts_summary()` | `helper-tests`                       | **nothing — it fails the gate**   |
| `qa_gate_case_count()`    | 22 gates that print `passed: <n>`    | the word `passed`, never a number |
| `qa_gate_detail()`        | 6 gates whose summary is not a count | the literal `summary unreadable`  |

(The `()` is load-bearing, not decoration: `check_qa_gate_inventory` reads any row whose
first cell is a backticked bare name as a **gate this document claims**, so writing them
plain made the table assert three gates that do not exist — caught by that check's own test.)

Only the first hard-fails, and the asymmetry is deliberate: its number distinguishes two
machines, so a blind read there is the defect this page exists to remove. The other two read
a gate that has *already* reported pass or fail through its exit status, so the stage line is
detail rather than verdict — but it must still never be a **wrong** number, which is why
neither ever substitutes a plausible-looking one.

Each gate used to inline its own reader. `grep -oE 'passed: [0-9]+'` prints EVERY match, so
an earlier `passed: <digits>` in the capture made the stage line two lines, and `verdicts.py`
reads the first as the stage and loses the rest — one defect, found once, then found again in
21 untested copies, and again in 2 more that had a different regex. `vmtest-manifest`
interpolated its whole capture and emitted a **three-line** stage line on every run, dropping
two coverage measurements into nothing.

**Adding a gate: do not write a reader.** Call one of the three. If your gate's summary is not
a case count, use `qa_gate_detail` with a pattern matching what it actually prints — and note
that `test-qa-helper-summary.bash` reads every `qa_gate_detail` pattern out of `qa-all.bash`
and runs it against the real gate, so a call site whose capture variable is not registered
there FAILS — **and so does a registration no call site reads**, which is what a removed call
site leaves behind. That is deliberate: `nokill-containerwatch` read `[0-9]+ call site[s]? checked` from a gate that has only ever printed `N container-watch file(s) clean` — zero
matches for its entire life, behind a `||` fallback that asserted `no forbidden kill call sites` on every run. A pattern and a gate that nothing compares will drift, and the drift is
silent.

**Finding the call sites is parsed, not grepped** —
`helpers/qa_environment/gate_call_sites.py`, with its own unit tests. Three greps that had to
agree lived here first (a strict extraction, a looser denominator, and parameter expansions
that re-split each match), and every defect found in them was one drifting from the other
two: a spelling the extraction took and the splitter mis-read, a spelling both greps missed
in lockstep and therefore agreed on, a prose mention only one counted. **Two counts that must
agree can agree while both are wrong.** The parser has no second count — anything that looks
like a call and does not parse comes back in `unparsed` with its line and text, and the gate
fails on it. A silently dropped call site is not a state it can reach.

A count is a **proxy, not a proof**: two machines could skip the same NUMBER of different
tests. With the three conditional skips the suite has today the four machine shapes give
four distinct counts (0, 1, 2, 3), so it is currently exact — but that is a property of
those three sites, not of the mechanism. Adding a fourth conditional skip means checking
that property still holds, or surfacing the skipped tests by name instead.

That argument also assumes the skip count is bounded by the test count, and **it is not**.
`testsRun` counts test *methods*; `skipped` counts skip *events*, and one method registering
several `subTest` skips reports more skips than tests — `Ran 1 test … 3 skipped` is a
faithful line, not a broken one. No skip is raised *inside* a `subTest` block in
`tests/helpers` today — the three conditional skip sites are all outside one — so the
enumeration above holds; putting a skip inside a `subTest` means re-deriving it. Note that
`subTest` itself appears in ~70 places, so grepping for it will not answer this question.

The line also carries **`N modules (M tracked)`**, because a machine that COLLECTED a
different set of test modules is the same defect one level up and the test count alone
cannot show it. The two numbers cover the two directions, and both are needed:

- **tracked but not discovered** — `qa-helper-tests.bash` cross-checks its walk against
  `git ls-files` and **exits 2** if any tracked helper test was not found. `mapfile -t < <(find …)` reports `mapfile`'s status, not `find`'s, and `pipefail` does not reach inside
  a process substitution, so a partly-failed walk would otherwise shrink the run silently.
  Same guard as `qa-bash.bash` and `qa-python.bash`, which each grew it after the same
  defect; this was the third site and got it last.
- **discovered but not tracked** — `find` sees uncommitted files too, so a test nobody
  committed runs here and nowhere else and the counts stop being comparable. That is *not*
  a failure (it is the normal state mid-TDD), so it is reported rather than fatal: `modules`
  above `tracked` in the stage line, and the file names on stderr.

The second number goes in the **stage line** specifically. `qa-all.bash` discards the
child's stderr on a successful run, so a coverage figure reported only there is produced and
never delivered — which is one step short of the class this page is about.

**A stage that cannot pass in an environment is not a strict gate there — it is an absent
one.** Two consequences follow, and the second is the one that bites:

- a permanently-red stage carries no information, because a red run looks exactly like the
  previous red run;
- a failing gate used to **abort the suite**, so a stage that could not pass stopped every
  gate declared after it from running at all. The suite did not merely stay red — the
  number of checks actually executed *fell*, silently, and newly added gates could go their
  whole life without running once in CI. Measured while `helper-tests` was red: the masked
  set grew **5 → 11 → 25**, and 20 of those had never executed in CI.

**Every gate runs now, and the run reports all of them** (Plan 00125). A failing hard gate
records itself with `qa_hard_gate_failed` and the suite carries on; the final line names
every gate that failed rather than the first one that did. Only the seven `exit 2`
missing-tool aborts still stop the run, because a suite that cannot run its tools has
nothing to accumulate.

Two things this buys, and the second is easy to miss:

- **the count of executed checks cannot fall silently** — a gate that fails still prints a
  stage line, so a run's census is complete whatever its verdict;
- **the failing gate appears in that census at all.** `verdicts.py` matches
  `^[✓✗⚠] QA (?:passed\|FAILED):` as a RUN SUMMARY *before* it tries the stage pattern, so
  the old `✗ QA FAILED: <prose>` gave the failing gate no stage line — it erased itself as
  well as everything behind it. Measured under one mutated gate: **11 of 38** stages parsed
  before, **38 of 38** after, and the failing gate present only in the second.

`scripts/test-qa-helper-summary.bash` holds the guard: exactly one `exit 1` may remain in
`qa-all.bash`, the final summary's. Proving the behaviour itself means running the whole
suite against a mutated gate, which is too slow for every commit; noticing the shape coming
back is one grep.

So when a gate needs something an environment lacks, add the dependency (`CLAUDE.md` →
"Missing Dependencies — Fail Fast, Fix in IaC") rather than teaching the gate to tolerate
its absence. A gate taught to skip passes in precisely the environment that could not check
it — and it is the "skip and warn" pattern, one level of indirection away.

Two rules for anything a gate executes: resolve paths relative to the file (`REPO_ROOT`,
`import.meta.url`, `__file__`), never to a fixed absolute root — the repo is checked out at
a different path in the container, on a host and on a runner. And exclude the whole
`.ansible/` tree from discovery, not just `.ansible/roles/`: `ansible-galaxy` populates
`.ansible/collections/` with third-party files, so a stage that misses it counts a
different number of files depending on whether galaxy content has landed.

### All three source gates assert their own coverage (Plans 00076, 00081)

`qa-bash.bash`, `qa-patterns.bash` and `qa-python.bash` share one discovery
library, `scripts/qa-discovery.bash` — one mechanism, one "is this a shell
script / is this Python" predicate. Change discovery there, not in a gate.

The two languages carry **separate exclusion lists** over that one mechanism,
and the difference is deliberate: `QA_PY_EXCLUDE_DIRS` excludes only
`.claude/ccy/plugins` and `.claude/ccy/file-history`, not the whole `.claude/ccy`
tree, because `.claude/ccy/claude-supervise.py` is **tracked** — it is committed,
shared between clones, and sits squarely inside ruff's default scope.
Unifying the lists would have *dropped* a real file from the Python gate — this
section's own defect, committed inside the fix for it.

**Tracked is not the same as ours.** `claude-supervise.py` is **daemon-owned**:
the hooks daemon rewrites it on every install and upgrade, so a local edit is
discarded. So are `.claude/init.sh`, `.claude/hooks/*`, the `*.sh` scripts under
`.claude/skills/hooks-daemon/scripts/`, and `CLAUDE/Plan/mkplan.bash` — all
tracked, none of them ours to edit.

**Which of those the gates actually open is a separate question**, and the two
must not be conflated. `.claude/init.sh`, `.claude/hooks/*`, `mkplan.bash` and
`claude-supervise.py` are all discovered and gated. The skill scripts are **not**:
`.claude/skills` is in `QA_EXCLUDE_DIRS`, so nothing under it reaches a gate —
confirm with `jq '.paths.scanned[]' /tmp/qa-results.json`, which lists zero paths
there. Upstream guarantees each daemon-owned file is clean under its language's
**default** rule set (`ruff --isolated`, shellcheck with no rc), which is why
gating the four costs nothing today. If an upgrade ever lands a finding under a
rule *this repo* chose, the remedy is to exclude the file (or narrow the rule —
see the `BLE` note in `ruff.toml`) and never to edit it, because the next upgrade
overwrites the fix. Report it upstream if it fails under default rules.

`qa-python.bash` joined them in Plan 00081. It had the identical defect and had
not learned from 00076: it discovered by extension **or the execute bit**, so six
tracked Python programs — mode 0644 with a `#!/usr/bin/env python3` shebang,
deployed 0755 by their plays — were never compiled or linted. Widening discovery
took it from 35 files to 41 and surfaced **31 real ruff findings** in ~4,000
previously-unread lines, while the old gate printed `✓ python: 35 files OK`. This
document's own "For Python files that use external libraries" note named
`wsi-stream` as *the* example of Python needing care; it was one of the six.

This exists because the two gates identified bash by **filename extension or file
mode**, and 27 of this repo's scripts have neither a `.sh`/`.bash` extension nor
an execute bit — including the 140 KB ccy launcher. They were never opened by
`bash -n`, shellcheck, or semgrep, while the gates printed `125 files OK`. Zero
coverage was already treated as a broken gate; *partial* coverage was not.

Two things changed, and neither is a file-mode change:

- Discovery keys on a **shell shebang, regardless of mode**. A `read` builtin, not
  `head`, because this runs against every file in the repo.
- Each gate **asserts its own coverage** and exits `2` on a shortfall, naming the
  files. `qa-bash.bash` compares its discovered set against every tracked shell
  script; `qa-patterns.bash` requires every file it handed semgrep to come back in
  `.paths.scanned`.

Semgrep needs the owner execute bit before it will read a shebang, so
`qa-patterns.bash` **copies** each discovered script into a temp mirror at its own
repo-relative path (appending `.bash` where it has no shell extension) and scans
that, mapping findings back to real repo paths. `chmod +x` was rejected as the
fix: `/var/local/colours` and `/var/local/ps1-prompt` are deployed `0644` because
they are sourced libraries, so an execute bit would be a lie told to a linter and
would still miss every future sourced library.

**If a gate reports a shortfall, widen the discovery — never exclude the file.**

> **This is one instance of a general defect class**, and treating it as a
> local quirk of these two gates is why it kept recurring elsewhere — four more
> instances surfaced in Plans 00079/00080. See
> [AgentNotes.md → *A partial result read as a complete one*](AgentNotes.md#a-partial-result-read-as-a-complete-one--guard-the-empty-case-miss-the-partial).

#### `.paths.scanned` is not proof a file was analysed

Semgrep lists a file it could not **parse** as scanned, returns zero findings for
it, and exits `0` — the reason appears only in `.errors[]`. Three of this repo's
scripts were in that state, `ftp-camera` (2,475 lines) among them, and two of
them were being scanned by the *old* gate too. All three pass `bash -n`.

`qa-patterns.bash` therefore checks the error list, and treats its two classes
differently because they mean different things:

- **`Syntax error`** — the file did not parse, and **every rule with a match in
  it lost that match** → **gating**. `SEMGREP_CANNOT_PARSE` is the exception
  list; it is **empty today** and self-expiring in both directions: an unlisted
  unparseable file fails the gate, *and* a listed file that starts parsing fails
  it until the entry is removed. Both former entries were the same construct —
  see below.

- **`PartialParsing`** — parsed apart from named ranges. Reported, not gating,
  and measured to cost **nothing** for this ruleset.

Both counts print on **every** run, pass or fail, above the `✓ patterns:` line.
A summary consisting only of a tick and a file count is the format that let this
sit unnoticed.

#### Semgrep parses a file only when a rule's regex has already matched

This is the fact that explains everything else in this section, and it was
established with a probe rule whose regex matches on every line, so the parse is
always attempted. Across ten measured (file, rule) cells the correlation is
exact:

| rule's raw regex matches in the file | parse attempted | error reported |
| ------------------------------------ | --------------- | -------------- |
| 0                                    | no              | none           |
| ≥ 1                                  | yes             | surfaces       |

So a rule that appears to "parse a file fine" may simply never have been asked.
An earlier revision of this document concluded from that appearance that
**parseability is a property of (file × rule)**; it is not, and the reasoning was
this section's own defect class one level up — an *absence of an error* read as
evidence of coverage.

#### `Syntax error` costs everything; `PartialParsing` costs nothing (today)

Both measured, not argued:

- `rclone-tail` carried a whole-file `Syntax error` and reported **0** findings
  while containing **3 real `|| echo` violations**. `rclone-cache-status` hid a
  fourth. All four were invisible for as long as the files were on the exception
  list.
- `ftp-camera` reports a `PartialParsing` range of lines 585–2486, and a probe
  for `echo` returned **293 findings against 293 raw occurrences** — 262 of them
  on distinct lines *inside* that range.

The asymmetry is because every rule in `.semgrep/bash-conventions.yml` is
`pattern-regex`, and the regex engine reads raw text — the parse tree is never
consulted for a match. A `PartialParsing` range is therefore a note about a
**future** cost: the day an AST `pattern:` rule is added, those lines stop being
covered. The report keeps the magnitude visible so that day is noticeable:

```
files/home/.local/bin/ftp-camera — 1902 of 2486 lines outside the parse tree (first gap from 585, last to 2486)
files/var/local/claude-yolo/claude-yolo — 1 of 3021 lines outside the parse tree (first gap from 1252, last to 1252)
```

Those two lines describe situations three orders of magnitude apart, and a bare
list of filenames rendered them identically. The number is the union of the
skipped ranges, so overlaps are not double-counted. The wording is
"outside the parse tree", not "not analysed" — the earlier phrasing was false in
the *alarming* direction, which is no better than false in the reassuring one.

#### The construct that defeats the bash grammar

Both former `SEMGREP_CANNOT_PARSE` entries reduced to one shape — a heredoc fed
directly to an `if` condition, with `then` on the line after the terminator:

```bash
if ! python3 - "$json" <<'PY' 2>/dev/null
...
PY
then
    echo "ERR|parse failed"
fi
```

That is valid bash (`bash -n` passes) and tree-sitter-bash cannot parse it. Feed
the heredoc to a **command substitution** instead and it parses cleanly:

```bash
if ! parsed=$(python3 - "$json" <<'PY' 2>/dev/null
...
PY
); then
    echo "ERR|parse failed"
else
    printf '%s\n' "$parsed"
fi
```

The rewrite is also better bash — the command's status and its output are
handled separately instead of the output flowing straight through the condition.

#### Two files may not claim one mirror path

The mirror appends `.bash` to a file with no shell extension, so `dir/foo` and
`dir/foo.bash` would both land on `dir/foo.bash`: the second `cp` overwrites the
first, and the mirror→repo map — merged from one object per file — keeps a single
value for that key. The overwritten script is then neither scanned **nor** named
by the coverage assertion, which reads the map's *values*. It would simply cease
to exist, and the gate would report a pass over it.

That is this section's own defect committed inside the fix for it, so a collision
is a hard failure (exit 2) naming both files rather than a silent rename. No
collision exists in the repo today; the check is there so one cannot appear
quietly.

### `qa-deployed-drift.bash` — the repo and the host must agree

This is the one QA check whose subject is the **host** rather than the source
tree. It exists because Plan 00094 fixed `files/home/.local/bin/ftp-camera` in the
repo and never ran the play that deploys it — the repo said "fixed", the machine
ran the old build, and it surfaced weeks later as a camera session that would not
copy. No source-reading check can see that: the source was correct.

It compares each repo-owned script against its deployed copy and, on a mismatch,
names the play to run — derived by searching the playbooks for the file's `src:`
path, not from a hand-maintained table. A file is checked **only when a deployed
copy already exists**, so a machine that never installed a feature is never
nagged. It self-skips in the CCY container and in a clean CI checkout, where
there is no deployed state to compare against.

**The deployed name is not always the repo name** (Plan 00081 F4).
`git-account-helper.j2` deploys as `git-account-helper`, so a basename comparison
looked for a `.j2` in `~/.local/bin`, never found one, skipped the file, and
still printed its pass line — Plan 00094's failure mode reproduced inside the
gate written to catch it. A `.j2` is now verified to have a real playbook `dest:`
under its stripped name (**exit 2** if not), and — since a rendered template
genuinely cannot be byte-compared — is **disclosed in the summary** rather than
silently omitted. The pass line also states how many scripts are not installed on
this host, because "2 match" and "2 match, 35 not installed" are different
sentences and only the first was ever printed.

**This changes when you run QA.** The table below says "before every commit", and
that is still right — but on the HOST this gate makes the repo's documented
`edit → playbook → deploy → test` order (see
[InfrastructureAsCode.md](InfrastructureAsCode.md)) **enforced** rather than
merely recommended: a changed script must be deployed before `qa-all.bash` will
pass. That is the intended sequence, not an obstruction — QA is a pre-*commit*
gate, and the workflow already puts deploy and test ahead of commit. If you are
mid-edit and want the other stages, run the deploy first; do not work around the
gate.

Run it alone with:

```bash
./scripts/qa-deployed-drift.bash
```

---

## Changing a Gate

A gate is what `CLAUDE.md` makes mandatory before every Bash/Python/Ansible
commit. A bug in one is worse than a bug in the code it checks, because it is
silent: the gate returns a confident exit code either way. Two failure modes
have already shipped here, and every change to a gate is measured against them.

**A gate that scans nothing and reports a pass.** `find -path` matches the
**whole** printed path, so an unanchored `*/untracked/*` exclusion once excluded
an entire checkout — this repo is vendored at `untracked/repos/fedora-desktop`
inside another project, and every bash file went unscanned with exit 0
throughout. Anchor repo-root-relative exclusions to `$REPO_ROOT` (the shared
`scripts/qa-discovery.bash` lists them repo-relative for this reason) and leave
only genuinely any-depth names (`.git`, `node_modules`, `__pycache__`, `.venv`,
`venv`) unanchored. **Never remove a zero-file guard** to make a gate "work"
somewhere — zero is the signal.

**A gate that scans the wrong things.** `qa-python.bash` once had two discovery
passes and only one carried the venv exclusions, so it linted third-party `pip`
scripts; `qa-ansible.bash` once matched a *comment* documenting the removal of
`ignore_errors: true`. Keep multi-pass discovery on one shared exclusion list,
and strip comments before matching a source pattern — after checking whether
the annotation you rely on (`# FAIL-FAST-OK:`) itself lives in a comment.

**Prove a change with a control that could have failed.** A gate turning green
proves nothing on its own. Build a fixture and check that the gate
**discriminates**: the bad case flags, the good case does not, and the
near-miss cases (annotated, or with an unrelated trailing comment) each behave
correctly. A uniform failure across unrelated assertions means you broke the
fixture, not that the checks work.

**Most gates have no tests of their own**; their fixes were proven with
hand-built fixtures that were then discarded. If you are changing one, consider
whether the fixture should become permanent. `qa-docs.bash` is the exception
and the model: its logic lives in the stdlib-only helper
[helpers/docs/link_check.py](../helpers/docs/link_check.py) with unit tests at
`tests/helpers/docs/test_link_check.py`, which CI runs. Its slug cases are pinned
against anchors observed working in rendered documents, because that checker's
first implementation was wrong in the same way as the defects it hunts and
under-reported them.

---

## ruff: the Ruleset Is Explicit and the Version Is Pinned

`ruff.toml` enumerates `select` explicitly (`E4`, `E7`, `E9`, `F`) so the
enforced ruleset does **not** drift with ruff's own default set — an
unenumerated default once turned `main` red with no commit behind it.
`/.ruff-version` is the single source of truth for the version, read by
`.claude/ccy/Dockerfile` and `.github/workflows/qa.yml` and **asserted** by
`scripts/qa-python.bash`: a version bump can change how the same selected rules
behave, so it is pinned too. If the assertion fails, match the pin — do not
"fix" findings a different ruff invented. Bumping the pin means owning the
triage of whatever changes and rebuilding the ccy image.

Suppression comments (`# noqa`, `# type: ignore`, `# shellcheck disable`) are
blocked by the hooks daemon. Fix the code, or exempt the file in `ruff.toml`
with a stated reason.

---

## GNOME Shell Extension JavaScript

Run ESLint via the binary directly (NOT `npm run lint` — blocked by hooks):

```bash
cd extensions && node_modules/.bin/eslint speech-to-text@fedora-desktop/extension.js
```

---

## Helper Unit Tests + Extension Version Compatibility

**Both of these now run inside `qa-all.bash`** (Plan 00081 F11) — you do not need
to invoke them separately. The commands below are for running one on its own
while iterating.

Helper packages under `helpers/` are stdlib-only (`helpers/CLAUDE.md`). Their unit
tests are namespace-package modules, so `unittest discover` cannot collect them —
run them with the dedicated runner, which enumerates `tests/helpers/**/test_*.py`
and runs them by explicit module name:

```bash
./scripts/qa-helper-tests.bash
```

A separate **static** gate confirms every `extensions/<uuid>/metadata.json`
declares support for the GNOME Shell major that this branch's Fedora release ships
(`vars/fedora-version.yml`). It is session-free (unlike the runtime
`helpers.gnome.verify_extension`), so it runs in CI on the repo source:

```bash
python3 -m helpers.gnome.check_extension_compat
```

The Fedora→GNOME-Shell map lives in `helpers/gnome/fedora_compat.py`
(`FEDORA_TO_GNOME_MAJOR`). When cutting a new `F<N>` branch, add that release's
GNOME major there — an unmapped Fedora version fails the gate by design, forcing a
human to confirm the GNOME version. Both run automatically in the `helpers` CI job
(`.github/workflows/qa.yml`).

---

## Retired: the CCY ctrl+z patch gate

`./scripts/qa-ctrl-z-patch.bash` and its `scripts/qa-ccy/` npm harness were
deleted in CCY 3.42.0 along with the patch they tested. The gate existed only
because the patch rewrote an anchor inside a minified upstream artifact and had
to be re-proven against each Claude Code release. Suppressing ctrl+z is now the
hooks-daemon PTY supervisor's job, outside Claude Code entirely, so there is
nothing version-coupled left to gate — see
[ContainerRules.md](ContainerRules.md#ctrlz-sigstop-suppression-is-the-supervisors-job--do-not-re-add-a-patch).

---

## When to Run What

| Changed files        | QA command                                                                  |
| -------------------- | --------------------------------------------------------------------------- |
| Bash or Python files | `./scripts/qa-all.bash`                                                     |
| Extension JavaScript | `cd extensions && node_modules/.bin/eslint <file>`                          |
| Ansible playbooks    | `./scripts/qa-all.bash` (runs `qa-ansible.bash` + `qa-ansible-syntax.bash`) |

---

## What QA Catches

- ✅ Bash syntax errors (`bash -n` validation)
- ✅ shellcheck `error` **and `warning`** findings (`info`/`style` advisory). **shellcheck is required** — absent, the stage exits 2 rather than reporting a pass it did not earn (Plan 00075). The bar was raised to `warning` because **SC2155** (`local x=$(cmd)` — `local` becomes the command whose status is reported, discarding the substitution's) **is the discarded-failure-signal class**, and it was sitting in the advisory bucket nobody reads. `info`/`style` stay advisory on purpose: they are dominated by SC2016 and SC2012, where gating would trade signal for noise
- ✅ **Discarded failure signals** (Plan 00075, `.semgrep/bash-conventions.yml`) — the class where a command's failure is silently turned into data that is then trusted:
  - `bash-status-after-block` — `$?` read after `fi`/`done`/`esac`/`}`, which is the *block's* status and so is always 0 after a successful `if`. **shellcheck does not catch this even with `--enable=all`** (verified)
  - `bash-capture-discards-status` — `var=$(cmd 2>/dev/null)` with the status thrown away, so an error written to stdout becomes the value. Scoped to `files/var/local/claude-yolo/**` for now; widening is tracked in Plan 00075. Genuine cases carry a same-line `# FAIL-FAST-OK: <reason>`
- ✅ Python syntax errors (`python3 -m py_compile`)
- ✅ Common Python issues (via `ruff` — **required**; `qa-python.bash` exits 2 with an error if ruff is absent; a ruff crash, exit ≥ 2, is also a hard failure)
- ✅ Error-hiding bash patterns (`|| echo` — Semgrep, `.semgrep/bash-conventions.yml`)
- ✅ Ansible fail-fast violations (`failed_when: false` without `# FAIL-FAST-OK:` annotation)
- ✅ Ansible playbook **syntax** errors (`ansible-playbook --syntax-check`, catches the 2.19 parse hazards)
- ✅ Playbook hygiene (every `- hosts:` playbook has the `ansible-playbook` shebang + exec bit)
- ✅ JavaScript syntax (`node --check`) + ESLint across `extensions/`

## What QA Does NOT Catch (Known Limitations)

- ❌ **Runtime API incompatibilities** — e.g., calling a library method with parameters it no longer accepts
- ❌ **Import errors** — missing dependencies only fail at runtime
- ❌ **Logic errors** — code that runs but produces wrong results
- ❌ **Everything judgement-shaped** — work put in the wrong place in the IaC graph, a
  new playbook that should have been an edit to an existing one, names that describe a
  mood rather than a behaviour, a missing version bump, plan/docs drift, a self-test
  that does not exercise the code path it vouches for, or a real identifier about to be
  posted to a public surface. **Use the `qa-reviewer` agent for these** — see below.

---

## The `qa-reviewer` Agent — Required Before Marking a Plan Complete

`qa-all.bash` is mechanical: syntax, lint, greps, playbook parsing. It passes green on
changes that are structurally wrong. `.claude/agents/qa-reviewer.md` is the holistic
gate for that class of defect.

**Run it as the final step of every plan, and to review any PR or branch diff:**

> Use the qa-reviewer agent to review this plan's changes before I mark it Complete.

It is **read-only** — it reports findings with `file:line` evidence and a verdict
(BLOCK / FIX-BEFORE-MERGE / PASS WITH NITS / PASS); it never edits. Its checklist is
built from this repo's own rules and the mistakes already made here (recorded in
`CLAUDE/AgentNotes.md`), so it grows as new ones are found — when a defect gets past
it, add that case to the agent rather than only fixing the instance.

**For Python files that use external libraries** (like `wsi-stream` using RealtimeSTT):

- After editing, **manually test the script** to verify it works
- Library APIs can change between versions
- Syntax checking alone is not sufficient for integration code

---

## Example Workflow

For a file under `files/home/.local/bin/`, deploy BEFORE running QA — the
deployed-drift gate compares the repo against the host, so it fails by design
while a changed script is still undeployed:

```bash
# 1. Make changes
vim files/home/.local/bin/wsi-stream

# 2. Deploy and TEST the actual script (on HOST, not in CCY container)
./playbooks/imports/optional/common/play-speech-to-text.yml
~/.local/bin/wsi-stream --help  # Verify it imports/runs

# 3. Run QA — now the repo and the host agree
./scripts/qa-all.bash

# 4. Only then commit
git add files/home/.local/bin/wsi-stream
git commit -m "fix: update wsi-stream"
```

For changes that deploy nothing to `~/.local/bin/` (playbooks, `scripts/`,
`helpers/`, extension JS), the order does not matter — run QA whenever, as long
as it passes before the commit.

## Rules Summary

1. **Run `./scripts/qa-all.bash` before EVERY commit** that touches Bash or Python files
2. **Run ESLint before EVERY commit** that touches extension JavaScript
3. **Fix all errors** before committing — QA failures indicate broken code
4. **Do not skip QA** — even for "small" changes
5. **Never add a suppression directive** (`# shellcheck disable`, `# noqa`, `# type: ignore`) — fix the code, or exempt in config with a stated reason
