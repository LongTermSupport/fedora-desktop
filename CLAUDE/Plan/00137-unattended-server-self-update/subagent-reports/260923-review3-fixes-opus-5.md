# Plan 00137 — round 3 review fixes, opus-5, 2026-09-23

Fixes for `260923-qa-reviewer-round3-opus-5.md` (FIX-BEFORE-MERGE).

## Should fix

1. **Dirty-clone remedy.**
   - `files/usr/local/sbin/fedora-desktop-self-update`: the "differs from its signed HEAD"
     and stray-file refusals now say that re-running the play cannot clear them. They
     tell you to run it once with `self_update_enabled: false`, which removes the clone,
     then once with it true. The unsigned-HEAD refusal keeps "re-run the play", which is
     correct there: the anchor moves HEAD.
   - `docs/configuration.md` has the operating note, `docs/playbooks.md` points at it,
     and DESIGN-cycle.md records why.
   - e2e: both refusals are checked for the remedy text. A new case covers a local edit to
     a tracked file.
2. **Exact-path host_vars exception.**
   - The wrapper reads `git ls-files --others -z` in a NUL-safe loop and excuses only the
     exact path. A failure of `git ls-files` itself is caught with `wait "$!"` on the
     process substitution and refused (20).
   - e2e: a directory at `environment/localhost/host_vars/localhost.yml` holding a file.
     RED against the old wrapper (exit 0, file not named), GREEN now (20, file named).
3. **No name filter on search paths.**
   - `cycle.home_search_paths` judges every list-valued setting.
   - A bare `~` is no longer read as the home. `INVENTORY_IGNORE_EXTS` and
     `MODULE_IGNORE_EXTS` hold it as a suffix, and ansible expands `~` in every path-typed
     value before dumping it (`config/manager.py`, `resolve_path` → `unfrackpath`), so
     without this change the cycle would have refused on every host. `~/…` is still the
     home.
   - Tests: an unnamed list holding a home path is caught (RED before the fix). Both
     ignore-ext lists with a bare `~` are clean. Name and pattern lists are clean.
   - Checked against a real `ansible-config dump --format json` (unpinned, HOME=/root):
     exactly the 18 `~/.ansible` search paths, and no false findings.
   - Path-typed STRING settings are still not judged. `DEFAULT_LOCAL_TMP` and the rest
     are under the home by default, so judging them would refuse every cycle. That
     exception is recorded in DESIGN-cycle.md as open.

## Nits

- `acceptance.bash` \[0\]: the extra-argument refusal is PASS only when all four exact lists
  were allowed. Otherwise it is COULD NOT ESTABLISH.
- DESIGN-cycle.md: the pycache prefix does not stop a sourceless `.pyc`. Only the
  stray-file check does.
