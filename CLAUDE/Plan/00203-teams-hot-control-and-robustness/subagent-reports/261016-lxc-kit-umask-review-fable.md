# Review of 63345520 (fix/lxc-kit-umask): MERGE

Reviewed `git diff 114dfa25..63345520` against the six attack points. No blocker.

## 1. Repair safety

`helpers/agent_bus_lxc/members.py` `KIT_SYNC_PY`, `directory()`:

- Order of checks is right: `S_ISDIR` on an `lstat` result (a symlink is never a directory
  here), then owner `== uid 0` and `mode & 0o022 == 0`, and only then the repair. Nothing not
  owned by root is ever chmod'd; nothing group- or other-writable is ever chmod'd.
- The repair opens `O_RDONLY|O_DIRECTORY|O_NOFOLLOW` and `fchmod`s the fd, so a swap for a
  symlink between `lstat` and `open` fails with ELOOP rather than following it.
- Scope: `directory()` runs on `parent` (`/usr/local/share/agent-bus`) and on tar directory
  names, which are validated to start with `kit` and contain no `..`, `.` or empty parts. No
  path outside the kit tree is reachable.
- The agent (uid 1000) cannot plant anything: the containing directory of every path root
  touches is either `/usr/local/share` (root 0755 in any image; was not verified before the
  fix either, unchanged surface) or a kit directory already verified root-owned and not
  others-writable earlier in the same sorted pass. A swap between `lstat` and `open` needs
  write on that containing directory, which only root has. The fd is not `fstat`ed to re-check
  uid after open; given the above that gap is unreachable, noted only.
- `makedirs(parent)` runs only after `lstat(parent)` raised `FileNotFoundError`; a dangling
  symlink there `lstat`s fine and is refused by `directory()`.

## 2. Umask 022 in root scripts

Root writes: kit files (tar modes, public code), the unit file 0644 (template has no
secret: user, paths, tmux command), bin symlinks. No token or env file is written by root
(`FakeGuest.root` asserts that too). Nothing becomes world-readable that should not.

## 3. Umask 077 in agent scripts

`PLACE_PY` and `WRITE_ENV_PY` only create `~/.config` (if missing), `~/.config/pingbus`,
the team dir and files. The system unit runs `User=<agent>`, so the same uid reads them. No
other uid needs them.

## 4. Fully healed on re-run

The bad run stopped at `pingbus suggest-handle`, after `_sync_kit` and before any agent
write or unit write. It left: `parent` 0700 (repaired by `directory()`), kit dirs already
0755 (old code chmod'd them explicitly), kit files with tar modes, bin symlinks. `/usr/local/ share` and `/etc/systemd/system` pre-exist in the image. So a re-run heals it in full.
Residual: an existing `~/.config/pingbus` is never tightened if some earlier run under umask
000 had made it 0777; that run did not happen on the broken container (umask was 077), and
no secret file was ever wider than 0600. Not a blocker.

## 5. Tests

`FakeGuest._run` executes the scripts as real code: `subprocess.run([python, "-I", "-c", KIT_SYNC_PY, ...])` against a temp tree. Red first verified: with `114dfa25`'s `members.py`
substituted, `UmaskTest` gives 5 failures and 1 error (the exact set the report claims).
With the fix: `python3 -m unittest tests.helpers.agent_bus_lxc.test_members tests.helpers.agent_bus_lxc.test_derive` -> 206 tests OK.

## 6. Hygiene

No install-specific names in the diff (placeholders `<user>`, `<container-B>`; `devbox`
is the fixture). `meta-deploy.list` keeps every entry; only the first comment changed.

## Nits (not blocking)

- `PLACE_PY`: `names =("token", ...)` lost its space (inside a string, so no linter sees it).
- A symlink swapped in between `lstat` and `open` surfaces as an `OSError` traceback, not a
  named message; unreachable by the agent uid, so cosmetic.

Verdict: MERGE.
