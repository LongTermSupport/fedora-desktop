# Upgrade Hooks Daemon

Upgrade the Claude Code Hooks Daemon and commit the result atomically.

## Agent Workflow

1. **Run the upgrade**:

   ```claude-code
   /hooks-daemon upgrade                              # latest
   /hooks-daemon upgrade 3.14.0                       # specific version
   /hooks-daemon upgrade --skip-config-optimisation    # opt out of step 9
   /hooks-daemon upgrade --skip-reading-confirmation=<digest>   # after reading what the gate listed
   /hooks-daemon optimise                              # step 9 on its own
   ```

   **The pre-deploy gate (MANDATORY to act on).** Once the new version is
   checked out, and before anything is deployed into the project, the upgrade
   prints `REQUIRED READING`: the upgrade guides this upgrade crosses, and every
   pre-upgrade task whose `**Detect**` pattern found a call site in this
   project, at `file:line`. When there is anything to read, the run stops
   with `UPGRADE STOPPED before anything was deployed`, puts the daemon
   checkout back on the installed version when it can tell which that is (the
   stop says so, and prints the command to run, when it cannot), and exits
   non-zero:

   - **Exit 3: read, act, re-run.** Read every listed document. Carry out each
     listed pre-upgrade task in the project as its file says (its `## How to handle`), and commit that work separately. Then re-run the same command with
     `--skip-reading-confirmation=<digest>`, the digest the stop printed. Pass
     it only after doing this. The digest belongs to that listing: a bare flag,
     or the digest of another listing, stops again.
   - **Exit 4: the owner must approve.** The upgrade needs the project owner:
     a MAJOR version, a manifest declaring `breaking: true`, a `critical`
     pre-upgrade task that found call sites, or an installed version the gate
     cannot read. Report the reasons and the approval command the gate printed
     to the user and STOP. The approval needs the owner's own terminal and a
     typed phrase, so you cannot record it; do not try. After the owner has
     approved, re-run with the same `--skip-reading-confirmation=<digest>`.

   Any other non-zero exit is an upgrade failure; report it with the printed
   error (exit 1 before deploying can also mean no Python 3.11+ in a system
   location, which the user installs, or a checkout the stop could not put
   back; see "The pre-deploy gate" in the daemon clone's `CLAUDE/LLM-UPDATE.md`). No metadata
   block is emitted on any stop. If the output says `THE UPGRADE DID NOT COMPLETE`, the upgrade stopped even though the command exited 0: an old
   installed upgrade script ran it. Run the command printed below that line.

   Run the upgrade with no environment variable set in front of it. An
   agent that sets `PATH`, `HOOKS_DAEMON_PYTHON` or another variable the
   upgrade reads on an upgrade command is denied (`upgrade_approval_guard`),
   and so is checking out, pulling or resetting `.claude/hooks-daemon` by
   hand. If the upgrade genuinely needs `HOOKS_DAEMON_PYTHON` (no Python
   3.11+ it can find), ask the user to run it themselves.

2. **Parse the metadata block** emitted on stdout between the
   `<<<UPGRADE_METADATA` and `UPGRADE_METADATA>>>` sentinels. Fields:
   `from_version`, `to_version`, `python_version`, `python_path`,
   `venv_path`, `host`, `daemon_dir`, `project_root`, `modified_files`,
   `config_diff_summary`.

3. **Verify daemon RUNNING**:

   ```bash
   .claude/hooks-daemon/bin/hooks-daemon status
   ```

4. **Reconcile project docs with truth-changes** (skip when
   `from_version == to_version`, a reinstall of the same release). Some statements that were
   true about working in this project may have changed across the upgrade.
   Load the truth-changes for the range you just crossed:

   ```bash
   .claude/hooks-daemon/bin/hooks-daemon check-truth-changes \
       --from ${from_version} --to ${to_version}
   ```

   Exit code `0` means nothing to do — skip to the next step. Exit code `1`
   means there is reconciliation work, and what you receive is a **bounded
   summary**, never the entries: counts, the path of the full report
   (`untracked/truth-changes/v<from>-to-v<to>/REPORT.md`) and one line per
   **chunk file** (`chunk-NN-<topic>.md` beside it). The full report is
   deliberately not printed — an unbounded one is delivered head-and-tail
   with the middle silently dropped, and it grows with every release crossed.
   Do NOT read the full report into your own context; delegate the chunks:

   - **Dispatch one subagent per chunk file, in parallel.** A chunk is one
     topic, and chunks are disjoint in the documents they touch, so parallel
     subagents cannot race for a file. A chunk marked `SEQUENTIAL` (entries
     with no topic) runs alone, AFTER every other chunk has returned.
   - The subagent's brief is the chunk file: tell it to read that path and
     follow it. The file carries the rules below and its own entries only.
   - **Each subagent returns ONLY what it changed** — the files it edited,
     one line each, plus which entries no doc asserted. Never the entries it
     read: you hold paths and counts, not the report.
   - A small range (a few chunks of one or two truths each) you may reconcile
     yourself from the chunk files; the rules are the same.

   The rules every chunk carries, for **each** entry:

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
   - An entry marked `revised in vX, vY` is the CURRENT form of a truth that
     also changed in those earlier releases; their entries are deliberately
     not shown. Reconcile any earlier form of the statement to the same `now`.

   Stage and commit any project-doc edits **separately** from the daemon
   upgrade commit below (they touch project files, not daemon-owned paths). You
   can re-run `check-truth-changes` any time to re-reconcile; `--full` prints
   the whole report inline for a human reader, and `--report-dir` moves the
   files.

5. **Surface newly-available / recommended config options** (skip when
   `from_version == to_version`, a reinstall of the same release). Some releases add opt-in
   protections or flip a default; this step reports what is now available or
   recommended for the range you crossed so a new feature never ships dormant:

   ```bash
   .claude/hooks-daemon/bin/hooks-daemon check-config-migrations \
       --from ${from_version} --to ${to_version}
   ```

   Exit code `0` means nothing to surface — skip to the next step. Exit code `1`
   means there are suggestions. What you receive is a **bounded summary**: the
   actionable lines inline, and the path of the full advisory
   (`untracked/config-changes/v<from>-to-v<to>/ADVISORY.md`) for every
   description, note and example. Read the summary:

   - Anything under **🆕 Recommended — enable these** is a feature the daemon
     recommends turning on. The line shows the key, the recommended value, and
     your current value. To adopt one, set that key/value in
     `.claude/hooks-daemon.yaml`.
   - A line marked "has a migration Note" (e.g. "migrate existing memory into
     tracked docs first") needs that migration performed **before** enabling —
     read the Note for that key in the full advisory. A post-upgrade task it
     references is one of the tasks step 6 lists; carry it out there.
   - **💡 New Options Available** is a count; the options are informational and
     listed with examples in the full advisory — adopt if useful. `--full`
     prints the whole advisory inline instead.
   - Anything under **⚠️ Stale handler keys** is a `handlers.<event>.<key>`
     entry the installed daemon does not register for that event: it names
     the event or pseudo-event the handler lives under now, or says the
     handler no longer exists. Move or delete the key as the line says
     (`audit-handler-keys` re-runs this check on its own, any time).

   This is advisory — enabling is your choice; the daemon never edits your
   config for you, except that the upgrade merge moves a key whose handler
   RELOCATED to a pseudo-event (the two nitpick detectors) to its new home,
   keeping `enabled`/`priority`, and lists the move in `config_diff_summary`.
   Stage and commit any `.claude/hooks-daemon.yaml` edits separately from the
   daemon upgrade commit below.

6. **Carry out the post-upgrade tasks for every version you crossed** —
   MANDATORY (skip only when `from_version == to_version`, a reinstall of
   the same release). A release can ship work that a clean
   upgrade does not do for you: auditing files a previous version's bug
   damaged, migrating a value or an interface this project consumes,
   retiring a workaround. That work lives in each crossed upgrade guide's
   `post-upgrade-tasks/`, and nothing runs it except this step. It is
   separate from step 5's migration Notes: an upgrade that changes no config
   key can still carry tasks. List them:

   ```bash
   .claude/hooks-daemon/bin/hooks-daemon check-post-upgrade-tasks \
       --from ${from_version} --to ${to_version} --project-root "$PWD"
   ```

   Exit code `0` means there are no tasks, so skip to the next step. Exit
   code `1` lists every task file, oldest guide first, with its severity and
   type. For **each** task, in order:

   - Read the whole file.
   - Skip it only if its `Applies to` does not cover the version you
     upgraded from.
   - Follow `How to detect if this applies to you`. If it does not apply,
     record that and move on.
   - Otherwise follow `How to handle`, then `How to confirm`. Adapt the
     sample commands to this project; never run them blind, and ask the user
     wherever the task says to.
   - **NEVER** edit anything under `.claude/hooks-daemon/`.

   Report every task's outcome to the user grouped by severity, `critical`
   first. A `critical` task you could not complete means the upgrade is not
   finished: say so rather than report success. Stage and commit any project
   edits **separately** from the daemon upgrade commit below.

7. **Stage daemon-owned paths ONLY** with explicit `git add` — other
   working-tree changes are not part of this commit. Never `git add .`:

   ```bash
   git add .claude/hooks-daemon/ .claude/hooks-daemon.yaml \
           .claude/skills/hooks-daemon/ .claude/hooks/ \
           .claude/settings.json
   ```

8. **Commit** with the metadata block in the body:

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
first (`.claude/hooks-daemon/bin/hooks-daemon logs`).

9. **Run the config-optimisation review** (Plan 00308) — mandatory unless
   `--skip-config-optimisation` was passed to this upgrade. Step 5 above
   surfaces recommended config KEYS via `check-config-migrations`; this step
   is the full per-handler review that decides which ones to enable, applies
   them on your confirmation, and records the run so the
   `config_optimisation_reminder` SessionStart advisory does not re-nag next
   session:

   ```claude-code
   /hooks-daemon optimise
   ```

   Run it in THIS session, immediately after the commit in step 8 above (it
   may itself edit `.claude/hooks-daemon.yaml` and restart the daemon — that
   is a separate, later commit, same discipline as steps 4-6's
   project-doc/config edits). The upgrade is not finished until it has run:
   do not defer it to a later session, and do not report it back as an
   optional follow-up. If `--skip-config-optimisation` was passed, skip this
   step and tell the user to run `/hooks-daemon optimise` themselves when
   ready.
