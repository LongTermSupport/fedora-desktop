# QA Review — `c44c56c..201e115` (DisplayLink background refresh + three inert-recovery fixes)

**Verdict**: BLOCK

Two of the three fixes in this diff are correct and well evidenced, and the new
`Action.REFRESH_BACKGROUND` is placed correctly both in the decision ladder and in
the IaC graph. It is blocked on its executor: `_refresh_background()` blanks a user
setting and restores it on a best-effort basis with three unguarded failure paths,
and its docstring asserts it has none.

Commits reviewed:

- `9a79dd7` — DisplayLink: recover the black desktop background, and fix three faults
- `201e115` — Plan 00109: record T5.4 delivery and the three inert-recovery faults

---

## Blocking

### 1. `_refresh_background()` can permanently leave the user with no wallpaper — and the docstring says it cannot

`helpers/displaylink_recovery/run_recovery.py:250-259`

The docstring at `run_recovery.py:235-236` claims:

> The restore is in a `finally` so an exception mid-toggle cannot leave the user
> with no wallpaper.

That covers exactly one of the four ways this can end badly, and it is the least
likely one. Three paths are unguarded.

**(a) `finally` does not run on SIGTERM.** Measured — a Python child that raises
SIGTERM at itself between a "BLANKED" print and a `finally` that prints "RESTORED":

```
stdout: ['BLANKED']   rc: -15
```

`RESTORED` never printed. Python's default SIGTERM disposition terminates the
process without unwinding, so `finally` is skipped entirely.
`displaylink-dock-recovery.service` is a `Type=oneshot` unit with no
`TimeoutStartSec` override, and the process can also be SIGTERMed by a unit
stop/restart or by system shutdown. The window is the two `sudo`+`gsettings` round
trips between the blank and the restore — small, but this is an automatic,
unsupervised code path, and the outcome is a user setting destroyed with no record
of its previous value and nothing that retries.

**(b) The restore is `check=True` inside the `finally`.** If it fails for any reason
— session bus gone, user logged out during the window, `sudo` refused — it raises
*from within* `finally`, the wallpaper stays blank, the original exception (if any)
is masked, and the unit fails with no repair path.

**(c) There is no read-back.** This entire commit exists because a `gsettings` write
exited zero and applied nothing. The restore has precisely that shape and is not
verified. If the bus address is ever wrong again, `_refresh_background` will blank
the wallpaper, "restore" it into a dead bus, exit zero, and report success.

**Fix:**

- After the restore, re-read the key with `gsettings get` and assert it equals the
  captured value; fail loudly, and `_notify()` the user naming the key and the value
  to re-apply, if it does not.
- Retry the restore once before giving up, rather than a bare `check=True` in a
  `finally`.
- Install a `signal.signal(SIGTERM/SIGINT, ...)` handler around the toggle that
  performs the restore, so the process cannot be killed mid-toggle.

`CLAUDE/AgentNotes.md` — *"A confirmed 'no reboot-free fix exists' finding must be
reported, never routed around"* — was written about this exact package, for the
principle that an automated action must not leave the user's session in a state they
did not ask for and cannot easily undo. A blanked `picture-uri` is a mild version of
the same thing, and it is currently reachable.

### 2. `.strip("'")` corrupts the restored value — `gsettings get` returns GVariant, not a string

`helpers/displaylink_recovery/run_recovery.py:257`

`gsettings get` returns the GVariant **printed** form. Stripping the outer quotes
produces a string that is not valid GVariant at all. Measured with GLib on this
host:

```
orig = file:///home/u/plain.jpg
  gsettings get shows : 'file:///home/u/plain.jpg'
  after .strip("'")   : file:///home/u/plain.jpg
  g_variant_parse(s)  : PARSE FAILS — g-variant-parse-error-quark: 0-4: unknown keyword (15)

orig = file:///home/u/back\slash.jpg
  gsettings get shows : 'file:///home/u/back\\slash.jpg'
  after .strip("'")   : file:///home/u/back\\slash.jpg
  g_variant_parse(s)  : PARSE FAILS

orig = file:///home/u/tab<TAB>here.jpg
  gsettings get shows : 'file:///home/u/tab\there.jpg'
  after .strip("'")   : file:///home/u/tab\there.jpg
  g_variant_parse(s)  : PARSE FAILS
```

The stripped value fails to parse for **every** case, including the plain one. The
verified-working host run therefore proves the restore is relying entirely on
`gsettings`' literal-string fallback for unparseable input. That fallback stores the
argv bytes verbatim — so for any wallpaper URI containing a backslash or a control
character, the restored value gains the GVariant escape as literal text
(`back\\slash`, `tab\there`) and the wallpaper is lost.

The `.strip("'")` is not merely unnecessary, it is the thing that breaks the round
trip. Passing `current` unmodified round-trips exactly, measured:

```
UNSTRIPPED round-trip exact: True   'file:///home/u/back\\slash.jpg'
UNSTRIPPED round-trip exact: True   'file:///home/u/plain.jpg'
UNSTRIPPED round-trip exact: True   'file:///home/<user>/.config/background'
```

**Fix:** delete `.strip("'")` and pass `current`. The `current == "''"` guard at line
248 stays correct, and the restore becomes total rather than fallback-dependent.

(The same probe shows an empty string also fails to parse, so the blank write at line
252 depends on that fallback too. That is acceptable once the restore no longer
does.)

---

## Should fix

### 3. The refresh will almost never fire on resume — the trigger where the fault most plausibly occurs — and the run reports `none`

`helpers/displaylink_recovery/run_recovery.py:194-222`, with
`playbooks/imports/optional/hardware-specific/play-displaylink.yml:283`

Measured on the host:

```
gsettings get org.gnome.desktop.screensaver lock-enabled → true
gsettings get org.gnome.desktop.screensaver lock-delay   → uint32 0
```

GNOME locks immediately on suspend. `displaylink-suspend.service` runs on resume
after `ExecStartPre=/bin/sleep 5`, at which point the session reports
`LockedHint=yes`, so `_session_locked()` returns True and `decide()` returns `NONE`.
One of the two triggers this feature was wired to is therefore disabled by default
on a stock GNOME desktop. The udev trigger has the same hole whenever the dock is
plugged before the user unlocks — the normal docking order for a laptop.

The lock guard itself is correct and must stay. The defects are:

- **Nothing states the gap.** The run prints `RECOVERY-STATE: action=none`, which is
  byte-identical to "nothing was needed". This is the
  [partial-result-read-as-complete](../../../AgentNotes.md) shape: the guard exists, and
  the population it excludes is invisible in the output.
- **PLAN.md marks T5.4 ✅** — "Recover the background after a monitor
  reconfiguration" — without recording that the resume path is suppressed by the lock
  guard. The plan is otherwise scrupulous about what is unverified (see *Checked and
  clean*), which is why this omission stands out.

**Fix:** emit a distinct marker when the refresh is withheld —
`RECOVERY-SKIP: background refresh withheld, session locked` — so a skip is
distinguishable from a no-op, and record the gap in PLAN.md T5.4. Deferring the
refresh until unlock is a follow-up task, not this diff's job, but the gap must be
written down rather than left to be rediscovered.

### 4. The `RECOVERY-BACKGROUND` marker claims a refresh that may not have happened

`helpers/displaylink_recovery/run_recovery.py:260`

The `print` sits at the `for user` level — outside the `for key` loop and outside the
`try`. Every user returned by `_graphical_sessions()` gets
`RECOVERY-BACKGROUND: refreshed desktop background for <user>` even when both keys
hit the `continue` at lines 248-249 and nothing was written at all. That is exactly
what happens for an SSH-only user, or whenever the session bus is unreachable.

This bears directly on the verification already performed: "a real run prints
`refresh_background` then `none` ... and the wallpaper keys are unchanged afterwards"
cannot distinguish *toggled and correctly restored* from *did nothing*. Both produce
that output.

**Fix:** count the keys actually toggled and print
`RECOVERY-BACKGROUND: toggled n of 2 key(s) for <user>`, suppressing the line when
`n == 0`. A number cannot be misread; a fixed sentence can.

### 5. `_graphical_sessions()` is not graphical, and does not return sessions

`helpers/displaylink_recovery/run_recovery.py:156-171`

It parses `who`, i.e. utmp, which lists tty and SSH logins alongside graphical ones.
Measured here:

```
who
  joseph   seat0   2026-09-11 11:16
  joseph   tty2    2026-09-11 11:16
```

Two lines, deduped to one `(user, uid)` pair — so the return value is *logged-in
users*, not sessions, and nothing about it is graphical. Consequences:

- A user logged in only over SSH has no `Display`, so `_session_locked()` returns
  True and the feature is disabled for **everyone** for as long as that login lasts.
- `_refresh_background()` and `_notify()` then shell out against
  `/run/user/<uid>/bus` for a user who may have no session bus at all.

**Fix:** rename to `_logged_in_users()`, which is what it computes. If graphical is
genuinely what is wanted, derive it from `loginctl` and filter on session `Type` in
`{wayland, x11}` rather than inferring it from utmp.

### 6. `_session_locked()` checks one session per user; the write reaches all of them

`helpers/displaylink_recovery/run_recovery.py:205-213`

`loginctl show-user <uid> --property=Display` names a single session. Measured:

```
loginctl show-user 1000 --property=Display --property=Sessions
  Display=3
  Sessions=3 1
```

Here session 1 is `Class=manager` (the systemd user manager), so the common case is
fine and the guard works. But `picture-uri` is a **per-user** key: the resulting
`bg-changed` reaches every gnome-shell that user is running. With two real graphical
sessions (fast user switching, or a second VT login), `Display` names one, the
other's `LockedHint` is never read, and the ~57 MB/monitor leak the guard exists to
prevent fires in the unchecked one.

**Fix:** enumerate `--property=Sessions` and require *every* session with `Type` in
`{wayland, x11}` to report `LockedHint=no` — the same conservative
unreadable-means-locked handling, applied to the whole population rather than one
member of it.

### 7. The three new side-effecting functions have no tests, and the test docstring says they are covered

`tests/helpers/displaylink_recovery/test_run_recovery.py:5-8`

> Most of run_recovery.py shells out and is covered by recovery.py's pure tests.
> `edid_byte_count` is the exception and earns a test of its own

Not true as written. `_as_user` is also tested, in that same file. And
`_session_locked`, `_graphical_sessions` and `_refresh_background` are neither
trivially-shelling-out nor covered by `recovery.py`'s tests — those take
`session_locked` as an **input**. Nothing anywhere tests the producer.

`_session_locked()` is the sole guard against the leak its own docstring cites, and
every one of its four early returns is subprocess-output parsing —
`unittest.mock.patch("subprocess.run")` covers all of them in a dozen stdlib-only
lines. The same is true of `_refresh_background`'s skip/blank/restore ordering, which
is where findings 1, 2 and 4 all live.

`CLAUDE/AgentNotes.md` row 14: *"A test that does not execute the production path is a
partial result wearing a passing verdict."*

**Fix:** add `TestSessionLocked` (every branch, including both unreadable cases) and
`TestRefreshBackground` (asserting the restore argv is byte-identical to what the read
returned, and that the marker line is not printed when nothing was toggled), then
correct the module docstring to say what is and is not covered.

### 8. The `-1` EDID sentinel is untested and undeclared at its consumer

`helpers/displaylink_recovery/run_recovery.py:68` returns `-1`;
`helpers/displaylink_recovery/recovery.py:69` decides on `edid_bytes == 0`.

`recovery.py:42` declares `edid_bytes: int` with no mention of a sentinel, and
`wedged_heads`'s docstring says only "connected but never got an EDID". Every
`HeadState` in the test file is built with `edid_bytes=0` or `256` — 15 occurrences,
never `-1`. So the one property `-1` exists to guarantee — that an unreadable
connector is not armed as a wedge — is asserted nowhere. A future tidy of `== 0` to
`<= 0` or `not h.edid_bytes` re-arms the recovery ladder against unreadable heads,
silently, with every test still green. That is the defect this commit just fixed, one
refactor away from returning.

**Fix:** name it (`EDID_UNREADABLE = -1` in `recovery.py`, imported by
`run_recovery.py`), document it on `HeadState.edid_bytes`, and add
`test_unreadable_edid_is_not_wedged` to `TestWedgedHeads`.

---

## Nits

### 9. `qa-helper-tests.bash` does not surface skips

`scripts/qa-helper-tests.bash:38-40` prints the module count and lets unittest print
`Ran 273 tests / OK`. On this host the new sysfs tests do run — verified with a
verbose run of the new module: 10 tests, **0 skips**. In the CCY container and in CI
they skip, and the only signal is unittest's own `OK (skipped=2)`.

A nit rather than a finding, because the class docstring at
`test_run_recovery.py:82-90` is explicit and honest about the skip and unittest does
print the count. But per AgentNotes, *"state coverage as a number, never imply it
from a list"* — the runner should echo the skip count as part of its pass line, so a
permanently-skipped test is visible in ordinary green output rather than only to
someone who reads the tail closely.

### 10. The playbook comment carries incident narrative

`playbooks/imports/optional/hardware-specific/play-displaylink.yml:225-230`

The first two lines are current-state rationale and belong there:

> `copy:` does NOT create missing parent directories for a file destination, so
> without this every recovery deploy below fails and the play aborts

The remaining four lines — "which is what happened on this host: the helper tree
never existed, the udev rule and dock-recovery unit that follow were never reached,
and displaylink-suspend.service sat enabled pointing at a WorkingDirectory that was
not there, failing silently on every resume" — are past-tense history. That is the
hooks daemon's own `R-COMMENT-CHANGELOG` rule and CLAUDE.md's "Comments explain WHY,
not what". It is already recorded, better and at more length, in the 13:05 journal
entry, which is its correct home.

### 11. Docstring drift inside the changed files

- `helpers/displaylink_recovery/recovery.py:8-9` still opens with "a USB-C
  DisplayLink dock can wedge in **two** independent ways" and enumerates 1 and 2.
  There are now three faults and six actions; the third is documented only on
  `needs_background_refresh`.
- `helpers/displaylink_recovery/run_recovery.py:17` advertises `--dry-run` as a
  manual diagnostic. On a healthy, unlocked desktop it now prints
  `would execute refresh_background` instead of `none` — a reader following that
  docstring will read a healthy system as faulty.
- `play-displaylink.yml:220-221` still describes the ladder as "service restart ->
  USB reauth -> module reload", with no mention of the background action.

`docs/playbooks.md:1071` was checked and does **not** drift — it documents driver
installation only and never described the recovery helper.

### 12. Open question: is the blank step needed at all?

`helpers/displaylink_recovery/run_recovery.py:233-235` asserts, as the entire
justification for the blank-and-restore dance:

> dconf suppresses a write that does not change anything and no signal would be
> emitted

I did not verify this and could not, read-only. If it is wrong, the blank step — the
source of findings 1 and 2 — is unnecessary, and re-applying the current value would
do. A `dconf watch /org/gnome/desktop/background/` in one terminal while re-applying
`picture-uri`'s existing value in another settles it in ten seconds, and is worth
doing before hardening a mechanism that may not be needed.

### 13. The two `copy:` tasks name the helper modules by path

`play-displaylink.yml:239-253` enumerates `recovery.py` and `run_recovery.py` as two
separate tasks. A third module added to `helpers/displaylink_recovery/` will not
deploy, and nothing will say so. `play-container-watch.yml:34-43` has the same shape
with a `loop:`. This is AgentNotes row 9 in miniature — "when a program grows a second
file, every check that names the first one by path is now partial". Deriving the set
(`with_fileglob`) would make it right tomorrow as well as today. Low priority; both
packages are two files.

---

## Checked and clean

- **IaC placement — correct, and minimal.** The new `file: state: directory` task is
  the exact pattern `play-container-watch.yml:27-32` uses for the same kind of helper
  tree; it sits immediately before the first `copy:` that needs it and inherits
  `become: true` from the play header (`play-displaylink.yml:6`). No new play was
  created — this concern is owned by `play-displaylink.yml` and stayed there.
- **Swept the tree for the same missing-parent-directory defect elsewhere.**
  Enumerated every `dest:` in `playbooks/**/*.yml` outside standard system paths and
  cross-checked each against every `file: path:`. All of `/opt/claude-yolo/**`,
  `/usr/local/lib/ccy-helpers/**`, `/var/local/claude-yolo/**`,
  `/root/.bashrc-includes` and `/var/local/claude-code` are created by a
  `state: directory` task. **No second instance.** Worth noting that no gate catches
  this class at all — a 20-line version of that sweep would, and the failure it
  prevents (a play that aborts and silently leaves half a subsystem undeployed) is
  precisely what this diff had to fix by hand.
- **Fail-fast.** No new `failed_when: false` or `ignore_errors: true`. Every new
  `subprocess.run` passes an explicit `check=`. The two `check=False` calls
  (`_session_locked`, and the read in `_refresh_background`) inspect the returncode
  on the immediately following lines, which is the exemption `helpers/CLAUDE.md`
  grants. `_notify`'s `check=False` retains its rationale comment.
- **Decision ordering is correct and pinned by tests.** `mutter_corruption_detected`
  (`recovery.py:79`) and `wedged_heads()` (`recovery.py:82`) both pre-empt
  `REFRESH_BACKGROUND`, asserted by `test_mutter_corruption_still_outranks_it` and
  `test_wedged_head_is_fixed_before_the_background`. `attempted_background_refresh`
  makes it fire at most once per process, asserted by
  `test_not_repeated_once_attempted`. The pre-existing
  `test_healthy_system_with_prior_attempts_recorded_is_still_none` was correctly
  updated rather than left passing for the wrong reason.
- **Loop safety.** Writing `picture-uri` cannot re-trigger either invoker: the udev
  rule matches `ACTION=="add", SUBSYSTEM=="usb"` on the dock's vendor/product ID, and
  `displaylink-suspend.service` is `WantedBy=suspend.target`. No feedback loop.
  Concurrent invocations are serialised by the pre-existing `flock` on
  `/run/displaylink-dock-recovery.lock`. The rule matches `ATTR{idVendor}`, which only
  the top-level USB device node carries, so one plug is one trigger, not one per
  interface.
- **The three fixes themselves are right.** Reading the sysfs attribute rather than
  stat-ing it is correct, and the journal carries the per-connector measurements
  proving it. Passing `DBUS_SESSION_BUS_ADDRESS` through `sudo ... env` is the correct
  fix for sudo's environment sanitising, and applying it to `_notify()` too was the
  right call. `TestEdidByteCountAgainstRealSysfs` is a genuine could-have-caught-it
  test: it asserts `os.path.getsize()` reports 0 while the read does not, which is the
  precise inversion the bug depended on.
- **Public-repo safety.** Grepped the whole diff for usernames, home paths, emails,
  private IP ranges and hostnames — none. The journal's `card1-eDP-1` and
  `#30307171aeae` identify nothing.
- **British English.** Clean — `sanitises`, `behaviour` throughout. The only `color`
  matches are the GNOME API identifiers `primary-color` and `color_texture`, correct
  as spelled.
- **Stderr hygiene.** `RECOVERY-*` marker lines go to stdout per `helpers/CLAUDE.md`;
  the write calls deliberately do not capture, so GLib/dconf diagnostics reach the
  inherited stderr rather than polluting the marker stream. See finding 4 for the
  marker's *content*, which is the real issue, not its stream.
- **Plan Commit Rule.** Code (`9a79dd7`) and plan (`201e115`) landed as a pair in the
  same session; `git status` is clean; no untracked `CLAUDE/Plan/` directories; the
  plan folder is already indexed.
- **Plan honesty — actively good.** PLAN.md T5.4 and the 13:05 journal entry both
  carry an explicit "Not yet verified: that the refresh clears the black background
  when the fault is actually present", and the commit message records the
  self-correction about the earlier hardcoded `PLAY EXIT: 0`. Nothing in this diff
  over-claims verification. The only gap is finding 3 — a coverage fact the plan does
  not yet know, rather than a claim it got wrong.
- **CCY version bump.** Not triggered — nothing under `files/var/local/claude-yolo/`,
  the Dockerfile, the entrypoint or the deployed skills is touched by this diff.

---

## Mechanical gates

| Gate                                    | Trigger                               | Result                                                                                          |
| --------------------------------------- | ------------------------------------- | ----------------------------------------------------------------------------------------------- |
| `scripts/qa-all.bash`                   | always                                | **PASS** — 686 files; ansible 78 playbooks, docs 64, js 7, helper-tests 273, deployed-drift 34  |
| `hooks-daemon plan-qa --sweep`          | always                                | **exit 1, 2 advise, 0 block** — repo-wide staleness/journal-freshness nags; neither names 00109 |
| `ansible-playbook --syntax-check`       | playbook changed                      | **PASS** on `play-displaylink.yml`                                                              |
| `scripts/qa-helper-tests.bash`          | `helpers/` + `tests/helpers/` changed | **PASS** — 17 modules, 273 tests, 0 failures                                                    |
| `unittest -v tests...test_run_recovery` | new test module                       | **PASS** — 10 tests, **0 skips on this host**; 2 would skip in CCY/CI (nit 9)                   |
| `helpers.gnome.check_extension_compat`  | **not triggered**                     | no `extensions/` metadata change in the diff                                                    |
| `extensions/` ESLint                    | **not triggered**                     | no extension JS change in the diff                                                              |
