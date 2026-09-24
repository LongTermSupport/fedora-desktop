# qa-reviewer: 00109 T5.4a unlock extension (806c8b36, cfcb5fe6), opus-5, 2026-09-24

Saved by the coordinator. Two rounds.

## Round 1, on 806c8b36: FIX-BEFORE-MERGE

1. **`extension.js` said the recovery's action is "usually none".** It is not, when docked:
   every docked unlock repaints the background, and a head that looks wedged sends the
   recovery through its root steps (driver restart, USB re-authorisation, evdi reload). The
   polkit rule's comment said it "grants nothing else", and DESIGN-panel.md §12 agreed.
2. **The HOST check could not tell a refresh from a refusal.** A run that logged
   `action=none` because the lock hint still said locked also met it.
3. **The play never told the operator to log out** after deploying the extension.
4. **The test could not catch a missing `STDERR_PIPE` flag**, and the stub's flag values did
   not match Gio's.
5. **`run_recovery.py` named two callers**, not the three there now are.

Nits: the panel-sections gate header, no syntax check of the polkit rule, a duplicated argv
test.

Checked and clean in round 1:

- **The polkit rule** is limited to one unit, the verb `start`, the templated user, and a
  session that is local and active.
- **A start with no dock does nothing.**
- **The extension's lifecycle** was checked against the shell source: the signal order,
  `disable()` cleanup, the cancel on re-lock and the dedupe.
- **The extension is added to the declared enabled list.**
- **The placement is right.**

## Round 2, on cfcb5fe6: PASS

All five findings are fixed, not reworded, and the three nits are dealt with.

- **Plain wording:** `extension.js`, the rule comment and §12 now say that every docked
  unlock repaints and that a wedged-looking head runs the root ladder.
- **The HOST check** requires `action=refresh_background` and a
  `RECOVERY-BACKGROUND: refreshed` line, plus a separate undocked check.
- **The logout line** is printed when the extension copy changed.
- **Gio flag values** were verified from `libgio-2.0` itself: `stdout-pipe 4`,
  `stderr-pipe 16`. The test asserts the flag.
- **The polkit test** renders the shipped rule and runs it against a stand-in `polkit`:
  - start is allowed;
  - restart, stop, another unit, another action, another user, and a remote or inactive
    session are refused.

Optional note: the test fills the template with string replacement rather than a Jinja
render; a leftover-braces assert makes that safe.

Gates: `test-panel-sections.bash` passed 78, ESLint OK, extension-compat 6/6, helper tests
OK. `ansible-syntax` failed in the worktree only for the missing vault file.
