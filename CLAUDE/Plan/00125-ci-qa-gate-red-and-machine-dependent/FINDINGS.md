# Plan 00125 — the two causes, and the evidence for each

Supporting document for [PLAN.md](PLAN.md). This holds the established facts and the
reasoning behind them; `PLAN.md` holds the task tree and current state, and `JOURNAL/`
holds the dated narrative of how each was arrived at.

Both causes were observed on run `35027739399` (`b81832cb`) and reproduced on
`34995414256` (`9b6c8d55`), so neither was new on the day the plan was filed.

## Cause A — the docs gate

Eight findings, all one shape. Eight tracked `.claude/rules/*.md` files link to
`../hooks-daemon/CLAUDE/DirectoryRoles.md`. `.claude/hooks-daemon/` is gitignored
(`.gitignore:53`, `.claude/.gitignore:3`), so in a clean checkout the target cannot exist.
`helpers/docs/link_check.py` already lists `.claude/hooks-daemon/` in `_EXCLUDE_PREFIX`,
but that excludes files **in** that tree from being scanned — it does not exempt links
**into** it from the existence check. The link is correct on an installed machine and
impossible in CI.

Those pointer files arrived in `0015c886` on 2026-08-31, five days *after* the last green
run, which was first read as meaning they cannot be the original breakage. **That
inference was wrong** (Task 1.2): CI only observes commits that are pushed, and nothing was
pushed to `F44` in those five days. `0015c886` is the *very next run* after `1fc1c5fe`, and
it failed on **docs alone** — 7 findings, all `target does not exist`, with all 203 helper
tests passing. Cause A is the original breakage and for eleven days it was the only one.

This is the one cause still open, because fixing it is Task 2.1's decision, not a repair.

### Two of Task 2.1's three options are not actually available

The three options were recorded as a genuine three-way choice. Checked rather than left
that way, and the choice is narrower than it looked:

**The eight findings are all daemon-generated content, and the correspondence is exact.**
Eight of the fifteen tracked `.claude/rules/*.md` files carry
`<!-- hooks-daemon-rule-version: 1.0.0 -->`, and they are the same eight the gate reports.
Their source is the daemon's own installer,
`.claude/hooks-daemon/src/claude_code_hooks_daemon/install/directory_role_rules.py`, which
renders the link from config via `directory_roles_link()` and re-deploys the files through
`sync_directory_role_rules()`. Nothing this repository hand-wrote contributes a single
finding.

- **(c) is not durable, for a stronger reason than "it would regress".** The installer
  re-renders the link on every sync, so an edit is reverted at the next upgrade — and the
  daemon's `docs_qa` `rules-file-shape` check *enforces* the pointer-only contract, so the
  edit would also be fighting a check while it lasted. The daemon's module docstring
  records the design decision deliberately: `DirectoryRoles.md` is not seeded into the
  client tree because a normal client install "clones the WHOLE daemon repository into
  `.claude/hooks-daemon/`", so the target exists "at a fixed, predictable path the moment
  the daemon itself is installed". The link is not wrong. Its premise — *the daemon is
  installed* — is simply false in CI.

- **(b) is the shape `CLAUDE.md` prohibits, and it is prohibited by name.** "Missing
  Dependencies — Fail Fast, Fix in IaC" rules out exactly "make the script tolerate the
  missing tool (skip-if-absent, `|| true`, advisory-only mode)", and a link check that
  stops checking when the tree is absent is that, one level of indirection away. It would
  also pass on a genuinely broken link in the only environment that cannot tell.

That leaves **(a)**: the daemon is a real dependency of the docs graph and CI does not have
it. `.github/workflows/` contains no reference to the daemon at all today, so this is an
addition rather than a repair. It is still the owner's call, because it makes every QA run
depend on an external repository's installer — a cost the rules do not decide.

## Cause B — tests that read the machine they were written on

Five at first; a sixth surfaced once the abort stopped hiding it.

| Test                                                                                                                                      | Status       |
| ----------------------------------------------------------------------------------------------------------------------------------------- | ------------ |
| `tests/helpers/displaylink_recovery/test_run_recovery.py::TestEdidByteCountAgainstRealSysfs::test_a_connected_display_reports_edid_bytes` | fixed (T3.1) |
| `…::TestEdidByteCountAgainstRealSysfs::test_stat_disagrees_with_reading_which_is_the_whole_point`                                         | fixed (T3.1) |
| `tests/helpers/gnome/test_apply_enabled_extensions.py::TestMain::test_falls_back_to_dbus_run_session_without_a_bus`                       | fixed (T3.2) |
| `tests/helpers/host_health/test_handoff.py::TestTheHandoffCanBeSuppressedForTriage::test_the_findings_are_still_reported_either_way`      | fixed (T1.3) |
| `tests/helpers/host_health/test_login_message.py::TestTheEntryPointALoginShellCalls::test_it_exits_zero_and_prints_nothing_when_clean`    | fixed (T1.3) |
| `scripts/test-freezelib.bash` — the `assert_on_host` case                                                                                 | fixed (T3.5) |

All six are **defective tests**, not production paths misbehaving. Run `35034834651`
(`cedc9426`) confirmed the first pair of fixes on a real runner: `failures=5` became
`failures=3`, with the two `host_health` entries gone and nothing else changed.

### The DisplayLink pair

It deliberately asserts against real sysfs — its own docstring says a tempfile cannot
reproduce the defect, which is true: sysfs binary attributes report `st_size 0` with
content present, so a tempfile-only suite passes just as happily with `os.path.getsize()`.

It scanned every `card*-*` connector and asserted "connected and advertising modes ⇒ has
EDID bytes". That inference holds for a connector with a **physical display link**, where
the EDID comes from the monitor across it, and is simply false for a `Virtual` connector,
whose modes are invented by the driver and which has no monitor to read from. A runner is
a VM whose one connected connector is `card1-Virtual-1`. Production never looks at these
at all — `_drm_head_states()` globs `card*-DVI-I-*` — so the unsound inference was the
test's alone.

### The dbus fallback

The first diagnosis was **wrong**, and it is corrected here. The test *does* isolate
`DBUS_SESSION_BUS_ADDRESS`: `mock.patch.dict(..., clear=True)` unsets it, measured as
`None` inside the very patch the test uses. The real cause is one candidate further down.
`runtime_dirs` always appends `/run/user/<uid>` and deliberately never drops it — it is
the path derived from who the process actually is, so no environment change can remove it.
On a runner (uid 1001, a live user session) that socket is reachable,
`resolve_session_bus` returns `source="runtime-socket"`, and the fallback the test names
is never reached. It passed in the container only because the container runs as a uid with
no session.

### The freeze library's host guard

`assert_on_host` ORs three container signals, and the test drove it by the suite
*happening* to run inside a container — with an `else` branch that failed outright rather
than skipping. A deliberate fail-fast choice, and also what made the gate impossible to
satisfy on a runner. Only `$container` was injectable; the two marker paths now are too.

**The host consequence, and why "the behaviour is identical" understated it.**
`qa-deployed-drift.bash:219` covers `files/home/.local/lib/freeze/*` and compares with
`cmp -s`, so a comment-only change drifts. Its abort is `qa-all.bash:137`, which sits
*before* `helper-tests` — so an undeployed host has a red `qa-all.bash` that **stops 28
hard gates short** (enumerated: 29 `exit 1` lines after `:137`, less the final run summary
at `:620`). That is this plan's own Task 4.1 mechanism aimed at the owner's workstation, and
`CLAUDE.md` makes a local `qa-all.bash` the pre-commit requirement, so it is not cosmetic.

**Both freeze plays are required, not either.** `tasks/deploy-freeze-lib.yml` deploys only
`freeze-common.bash`. The two binaries are deployed by their own plays —
`play-podfreeze.yml:75` and `play-lxcfreeze.yml:91` — and each play merely *includes* the
shared-library task. All three files changed in this plan, so running one play leaves the
other binary drifted and the gate still red. `qa-deployed-drift.bash:194` prints the owning
play per drifted file, so the gate names the right play itself; what was stale was this
plan's own instruction, which said "either freeze play".

## The mechanism that kept all of it invisible

`qa-all.bash` exits at the first failing hard gate, and **at the masked commit `29ceee97`,
25 gates were declared after the `helper-tests` abort** (`qa-all.bash:152` there; `:160`
today, with 27 behind it, because this plan added two aborts). A gate that cannot pass in
an environment therefore does not merely stay red — it stops every gate behind it from
running at all, and the number of checks actually executing falls with nothing reporting
it. Three gates in this plan had never run once in CI before the abort was cleared.

That is Task 4.1's answer and the argument for Task 4.3.

Three causes compounded to keep it that way:

1. **The first red was a gate that cannot pass in CI by construction** — a link into a
   gitignored tree. It was never a regression anyone could fix by fixing code, so nobody did.
2. **A permanently-red run carries no information**, so each later regression joined it
   invisibly. Red → red is not an event.
3. **Local `qa-all.bash` was green throughout**, and `CLAUDE.md` names it the pre-commit
   requirement — so the contributor's own signal said green every single time.

### The suite already contains both designs, and that is why the two causes hid differently

Counted mechanically, `qa-all.bash` runs its stages two ways:

| Design                  | Count | Behaviour on failure                                                 |
| ----------------------- | ----- | -------------------------------------------------------------------- |
| jq-merged, accumulating | 7     | `\|\| rc=$?`, `FAILED++`, **run continues**; all reported at the end |
| hard gate               | 30    | `exit 1` immediately; everything declared after it never runs        |
| missing-tool abort      | 7     | `exit 2`; same effect, and prints no `QA FAILED` line                |

**These counts are as of this plan's HEAD, and this plan moved them** — Task 4.4 added two
aborts of its own. Numbers describing what CI *was* masking are the counts at the masked
commit and are labelled as such below. Mixing the two is how a citation quietly stops being
true, which this plan has already had to correct once.

The seven accumulating stages are `bash`, `python`, `patterns`, `ansible`,
`ansible-syntax`, `js` and `docs`; they merge into one JSON document and are reported
together by `qa-all.bash:613-620`.

**This is the explanation the plan was missing.** The two causes were masked differently
because they fell on opposite sides of that line:

- **Cause A (`docs`) is an accumulating stage.** It has been red since 2026-08-31 and
  masked nothing at all — every gate behind it kept running. That is why the current CI log
  shows `✗ docs` followed by 25 passing gates and only then `✗ QA FAILED`.
- **Cause B landed in hard gates.** At `29ceee97`, `helper-tests` aborted at `:153` and
  enumerating the `exit 1` lines after it gave 27 — less the final run summary (`:602`) and
  less `helper-tests` itself — **25 gates that never ran in CI**. Clearing it unmasked
  `panel-sections`, then `freezelib`, one at a time.

It also narrows **Task 4.3**. Its option (1) — run every gate, report all verdicts, exit
non-zero at the end — is not a new design to weigh: it is the design already in force for
seven stages of this same script, and the one the final summary was written for. The
question is whether to extend it to the other 28, not whether to invent it. The repo's own
recurring lesson applies to the plan that is documenting it: the right answer already
existed one directory over — in this case, sixty lines up.
