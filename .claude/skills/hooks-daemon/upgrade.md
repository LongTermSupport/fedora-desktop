# Upgrade Hooks Daemon

Upgrade the Claude Code Hooks Daemon and commit the result atomically.

## Agent Workflow

1. **Run the upgrade**:

   ```bash
   /hooks-daemon upgrade           # latest
   /hooks-daemon upgrade 3.14.0    # specific version
   /hooks-daemon upgrade --force   # reinstall current
   ```

2. **Parse the metadata block** emitted on stdout between the
   `<<<UPGRADE_METADATA` and `UPGRADE_METADATA>>>` sentinels. Fields:
   `from_version`, `to_version`, `python_version`, `python_path`,
   `venv_path`, `host`, `daemon_dir`, `project_root`, `modified_files`,
   `config_diff_summary`.

3. **Verify daemon RUNNING**:

   ```bash
   $PYTHON -m claude_code_hooks_daemon.daemon.cli status
   ```

4. **Reconcile project docs with truth-changes** (skip on `--force`
   reinstall, where `from_version == to_version`). Some statements that were
   true about working in this project may have changed across the upgrade.
   Load the truth-changes for the range you just crossed:

   ```bash
   $PYTHON -m claude_code_hooks_daemon.daemon.cli check-truth-changes \
       --from ${from_version} --to ${to_version}
   ```

   Exit code `0` means nothing to do — skip to the next step. Exit code `1`
   means there are `was → now` entries to reconcile. For **each** entry:

   - **Semantically** search the PROJECT'S OWN docs for the `was` statement —
     `CLAUDE/`, `docs/`, `README*`, `AGENTS*`, and any project instruction
     files. It is a natural-language statement, not a literal string; match on
     meaning.
   - **NEVER** edit anything under `.claude/hooks-daemon/` — that is the
     upstream daemon clone and is overwritten on upgrade.
   - If `now` is present: update the project's doc to assert the `now` truth
     instead. Minimal edits — change only the stale statement.
   - If `now` is empty / "remove all reference": delete the stale guidance. Remove
     only the specific statement; if it is embedded in a larger section, ask
     before removing the whole section.
   - If a doc does not assert the `was` truth, there is nothing to do for it
     (the step is idempotent — re-running is a no-op).

   Stage and commit any project-doc edits **separately** from the daemon
   upgrade commit below (they touch project files, not daemon-owned paths). You
   can re-run `check-truth-changes` any time to re-reconcile.

5. **Stage daemon-owned paths ONLY** with explicit `git add` — other
   working-tree changes are not part of this commit. Never `git add .`:

   ```bash
   git add .claude/hooks-daemon/ .claude/hooks-daemon.yaml \
           .claude/skills/hooks-daemon/ .claude/hooks/ \
           .claude/settings.json
   ```

6. **Commit** with the metadata block in the body:

   ```
   hooks daemon upgrade: ${from_version} → ${to_version}

   <<<UPGRADE_METADATA
   from_version=...
   to_version=...
   python_version=...
   python_path=...
   venv_path=...
   host=...
   daemon_dir=...
   project_root=...
   modified_files=...
   config_diff_summary=...
   UPGRADE_METADATA>>>
   ```

If the daemon is not RUNNING after upgrade, do NOT commit — investigate
first (`$PYTHON -m claude_code_hooks_daemon.daemon.cli logs`).
