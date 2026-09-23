# Plan 00137 — qa-reviewer round 2 fixes

These are the fixes for [260923-qa-reviewer-round2-opus-5.md](260923-qa-reviewer-round2-opus-5.md). Tests were written first. The red run was 112 passed and 24 failed, and one of the failures showed that the old wrapper wrote `__pycache__` into the clone.

## BLOCK: ignored files in the clone reach root

- **Wrapper** (`files/usr/local/sbin/fedora-desktop-self-update`).
  - After the clean-status check, it runs `git ls-files --others --exclude=/environment/localhost/host_vars/localhost.yml`. Without `--exclude-standard`, that lists ignored files, and a nested repository as `dir/`. Anything listed is exit 20, and the message names the files.
  - Root then runs `python3 -E -s -B -X pycache_prefix=${STATE}/pycache`. `-I` is not usable, because it drops the cwd that `-m` imports from.
  - The header comment is corrected.
- **`update.py --anchor`.**
  - `_require_no_strays` (`ls-files --others -z`) runs before HEAD moves, and again after `checkout -B` together with the clean-branch check.
  - `--allow-untracked PATH` (repeatable, only valid with `--anchor`) names the exception.
  - `run`/`_update` still tolerate ignored files, so `test_an_ignored_file_is_not_dirt` stands.
- **Play.** The clone task has `recursive: false`. The anchor runs with `python3 -B` and `--allow-untracked environment/localhost/host_vars/localhost.yml`, needed because re-runs happen after the copy.
- **Child plays' Python.** Nothing is added. The children run as the user, who cannot write the clone. The wrapper has just proved the clone holds no strays, and root writes no pyc. The reasoning is in DESIGN-cycle.md.
- **Tests.**
  - Anchor units: an ignored pyc, a nested repository, the one allowed file, and a stray found before anything moves. There is also a CLI case for `--allow-untracked`.
  - e2e:
    - no `__pycache__` or other file untracked by git is left after a cycle;
    - a planted ignored pyc gives 20 from `run --dry-run` and from `status`, and 11 from the anchor;
    - a nested repository gives 20;
    - the host_vars copy is allowed by both the wrapper and the anchor.

## SHOULD: search paths by rule

- `cycle.search_path_environment()` is factored out of `play_environment`. `cycle.home_search_paths(dump, home, cwd)` is a pure function:
  - it judges every list element of a `*PATH*` setting, and of `DEFAULT_HOST_LIST`;
  - `~` expands to the home, and a relative path resolves against the clone;
  - strings are data and are not judged;
  - the `{"GALAXY_SERVERS": …}` entry is skipped;
  - any other shape raises ValueError.
- `RealHost.check_toolchain` requires a trusted `ansible-config` beside `ansible-playbook`. It runs `ansible-config dump --format json` as the user, from the clone, with the user env plus the pinned search env. A failure, an unreadable dump or any finding is returned as the toolchain error, and the cycle maps that to exit 70 config-invalid.
- Real ansible-core 2.19: the unpinned dump has 18 findings (every `~/.ansible` default), and the pinned dump has none.
- Tests: faked dumps in `TestSearchPathsUnderHome` (11 cases). The e2e stub `ansible-config` adds `FUTURE_WIDGET_PLUGIN_PATH` under `$HOME` when a flag file exists, and the cycle gives 70, with no calls and config-invalid recorded.

## Nits

- **Wrapper.** It checks that ALLOWED_SIGNERS is not a symlink, is a regular file, is owned by uid 0 or the caller, is not group- or world-writable, and sits in a directory that is equally unwritable. Otherwise it exits 70, naming "allowed-signers". The e2e test uses `chmod 666` to get 70.
- **`acceptance.bash` [0].** A failed `sudo -n true` passes only on "a password is required" or on a not-allowed message ("is not allowed to", "not in the sudoers file", "may not run sudo"). Anything else is `unknown`. Check [2] adds `ansible-config`.
- **`test-run-bash-single-play.bash`.** The copy probe now searches TMPDIR, HOME and XDG_RUNTIME_DIR, and the claim names exactly those three.

## Not done

- The `sudo -n -l … run --config /nonexistent` extra-argument check in [0] still passes on any refusal. It was not in scope.

## QA

- `./scripts/qa-all.bash`: every gate green (45), run with a temporary `extensions/node_modules` symlink (removed afterwards) and `ANSIBLE_VAULT_PASSWORD_FILE` pointing at a dummy file.
- `shellcheck -x` is clean on the four edited bash files.
- `visudo` is still absent from the container, and nothing sudoers-related changed.
