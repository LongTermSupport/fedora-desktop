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

## The mechanism that kept all of it invisible

`qa-all.bash` exits at the first failing hard gate (`scripts/qa-all.bash:152` for
`helper-tests`), and **25 gates are declared after that point**. A gate that cannot pass in
an environment therefore does not merely stay red — it stops every gate behind it from
running at all, and the number of checks actually executing falls with nothing reporting
it. Three gates in this plan had never run once in CI before the abort was cleared.

That is Task 4.1's answer and the argument for Task 4.3.
