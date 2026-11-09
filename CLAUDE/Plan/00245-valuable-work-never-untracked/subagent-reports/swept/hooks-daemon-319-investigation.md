# daemon_upgrade_detector reports "installed v3.19.0" -- investigation

Read-only. Nothing was edited, restarted, upgraded or moved.

## Verdict

**A misread, not a real downgrade.** The handler reads the version from the wrong venv.
The venv it picks is a stale, pre-slug leftover that was installed as v3.19.0 on 2026-06-13
and never removed. The authoritative resolver (the one the bash side uses) selects the
current venv, which is v3.68.0 and matches the running process.

## Root cause (file, line, values)

1. Handler: `<repo>/.claude/hooks-daemon/src/claude_code_hooks_daemon/handlers/user_prompt_submit/daemon_upgrade_detector.py`

   - line 135: `python_path = resolve_existing_venv_python(daemon_dir)` (called fresh each turn)
   - lines 139-140: `venv_dir = python_path.parent.parent`; `read_daemon_metadata(venv_dir)`
   - line 145: `parse_install_stamp(metadata.daemon_version).version` -> `3.19.0`
   - line 114: `installed == running` -> `"3.19.0" != "3.68.0"` -> advisory fires.

2. Resolver: `<repo>/.claude/hooks-daemon/src/claude_code_hooks_daemon/daemon/paths.py`, `resolve_existing_venv_python`, lines 958-1040.

   - It has NO lock-hash check. Its only candidates are:
     - the fingerprint-keyed dir `venv-<current fingerprint>` (line 1022), which does not exist here, since the current fingerprint is `workspace_claude_hooks-daemon-py311-2a519198`;
     - then a scan, `sorted(untracked.glob("venv-*"))` (lines 1026-1034), that takes the FIRST dir passing `_venv_slug_eligible`.
   - `_venv_slug_eligible` (line 236) lets through any name with no slug. The legacy name `venv-py311-28fb230b` has no slug, so it is eligible.
   - The host-view-slug venvs are correctly rejected (slug mismatch). `venv-py311-28fb230b` sorts before `venv-workspace_...`, so it wins.

3. The bash-side SSOT, `resolve_existing_venv_python_with_diagnostics` (paths.py ~1043+), has a metadata-authoritative step 2 that matches `lock_hash` against the current `sha256(pyproject.toml + uv.lock)`. It correctly picks the current venv. That is the disagreement: the two resolvers pick different venvs.

## Values on disk (verified)

Current inputs:

- `lock_hash` of `pyproject.toml` + `uv.lock`: `sha256:<container-id>`
- Running `claude_code_hooks_daemon.version.__version__`: `3.68.0`
- Current interpreter fingerprint: `workspace_claude_hooks-daemon-py311-2a519198` (its venv dir does not exist)

`untracked/` venvs (metadata `daemon_version`, `lock_hash` match?, slug):

| venv dir                                            | daemon_version      | lock_hash matches current? | slug               | written    |
| --------------------------------------------------- | ------------------- | -------------------------- | ------------------ | ---------- |
| `venv-py311-28fb230b`                               | **v3.19.0**         | no (`515357...`)           | none (legacy name) | 2026-06-13 |
| `venv-py311-66bbc57c`                               | v3.15.1             | no                         | none               | 2026-05-19 |
| `venv-py314-a5dc9d15`                               | v3.13.0             | no                         | none               | 2026-05-13 |
| `venv-py314-fefc85e6`                               | v3.8.2 (stamp only) | n/a                        | none               | 2026-04-24 |
| `venv-<host-view-slug>-py314-0d115c17`              | v3.25.0             | no                         | other view         | 2026-06-23 |
| `venv-<host-view-slug>-py314-63f74b3d`              | v3.31.1             | no                         | other view         | 2026-07-01 |
| `venv-workspace_claude_hooks-daemon-py311-28fb230b` | v3.38.0             | no (`950b63...`)           | this project       | 2026-07-13 |
| `venv-workspace_claude_hooks-daemon-py311-81c29529` | **v3.68.0**         | **yes**                    | this project       | 2026-10-03 |

Resolver outputs, run read-only from the 3.68.0 venv interpreter with the source on `sys.path`:

- `resolve_existing_venv_python(daemon_dir)` (what the handler uses) returns `.../venv-py311-28fb230b/bin/python`, which gives `v3.19.0`.
- `resolve_existing_venv_python_with_diagnostics(daemon_dir)` (bash SSOT) returns `.../venv-workspace_claude_hooks-daemon-py311-81c29529/bin/python` via "step 2: metadata match OK".

Also, `untracked/venv-py311-28fb230b/.daemon-version` contains `v3.19.0`, and its `pyvenv.cfg` shows `version_info = 3.11.2` with `uv = 0.11.21`.

## Is it a real downgrade?

No. Nothing installed 3.19.0 recently. The newest install (2026-10-03) is 3.68.0. The clone is at tag v3.68.0 (`git describe --tags`), and the running process loaded 3.68.0.

## Why it is noisy now

Interpreter fingerprint changed (the current interpreter's `sys.version` build date differs from the one that keyed `...81c29529`), so the keyed fast path misses. The scan fallback then lands on whichever stale venv sorts first. Removing only `venv-py311-28fb230b` would not be enough: the next eligible scan hit would be `venv-py311-66bbc57c` (v3.15.1), then the `py314` dirs. The scan fallback is wrong for any tree with more than one venv.

## Certainty

High (about 95%) on the mechanism: the file, line and values were reproduced directly, and both resolvers were run. Not run: the live hook process itself, so the exact per-turn path is inferred from the same code path.

## Fixes

### Local (this project, no daemon-clone edits)

- Nothing in project config controls this resolver. `HOOKS_DAEMON_VENV_PATH` would work but is an env override that the upgrade guard forbids agents from setting, and it is not a fix.
- Option A (owner's call): remove the stale venv dirs under `untracked/` whose lock_hash does not match current (all except the 3.68.0 one). This is daemon-owned state and needs the owner's decision; it is whack-a-mole unless all of them go, and `hooks-daemon repair`/cleanup should be the route rather than a manual `rm`.
- Option B: disable the advisory handler in `<repo>/.claude/hooks-daemon.yaml` (advisory only; loses the signal).

### Upstream code fix (for the issue report)

- Primary: `daemon_upgrade_detector._resolve_installed_daemon_version` should select the venv the same way the bash SSOT does: pick the `venv-*` whose `.daemon-metadata.json` `lock_hash` equals `compute_project_lock_hash(daemon_dir)` and is runnable, and return None (fail open) when none matches. This avoids a subprocess on the hot path.
- Also: `resolve_existing_venv_python` scan fallback (paths.py ~1026-1034) should skip venvs whose metadata `lock_hash` mismatches the current one, or be deleted in favour of the diagnostics resolver's step 2. Its docstring and the handler docstring both assume the scan result is the current venv, which is false when stale venvs coexist.
- Regression test: a tree with a stale un-slugged `venv-py311-<hex>` (metadata v3.19.0, mismatched lock_hash) plus a current matching venv must report the current version and emit no advisory.
