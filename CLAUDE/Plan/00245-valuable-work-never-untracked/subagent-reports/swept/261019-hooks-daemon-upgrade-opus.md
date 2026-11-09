# Hooks daemon upgrade: v3.69.0 to v3.70.0

Not plan work. Done in <repo> on branch F44. Nothing pushed.

## Versions

- Old version: v3.69.0. New version: v3.70.0.
- Python 3.11 venv rebuilt. One stale venv of this view was removed by the upgrade.

## Commits (F44, not pushed)

- `7c617e40`: the daemon made this one by itself on restart ("Auto: hooks daemon regenerated CLAUDE.md handler guidance"). It drops the two retired upgrade-approval rule rows from the `<hooksdaemon>` block.
- `7fae4916` hooks daemon upgrade: v3.69.0 → v3.70.0. It holds the 16 daemon-owned files, each staged by explicit path, and the metadata block. Host ID and paths are replaced by placeholders.
- `98552f1d` hooks daemon: enable review_round_advisor after the v3.70.0 optimise review.

## The gate and the breaking changes

- The pre-deploy gate stopped once with exit 3 (reading confirmation). I read the one listed upgrade guide in full and ran its pre-upgrade grep. The only hits were daemon-generated files. Then I re-ran with `--skip-reading-confirmation=0c92c224bd53`. There was no owner-approval stop: this release removes that tier.
- Breaking change 1: the owner-approval tier is gone. That covers `approve-upgrade`, gate exit 4, the `upgrade-approvals/` markers, the `upgrade_approval_guard` handler, and R-UPGRADE-APPROVAL-AGENT / R-UPGRADE-APPROVAL-ENV-BYPASS. Exit 3 (reading confirmation) is now the only stop.
- Breaking change 2: `prune-venvs --all-except-current` is removed. Use `--stale` instead.
- Neither change affects this project. No project doc, script or config used these names, and the config never named `upgrade_approval_guard`.
- Truth-changes: 1 entry (the approval tier). No project-owned doc stated it, so no doc edits were needed.
- `handler_changes` is empty. The effective handler set was not changed by the upgrade.

## Post-upgrade tasks

- 01, clear unproven venvs (recommended). `list-venvs` shows only the current venv. One host-view venv (py314, another checkout view's slug) was kept and not removed. It belongs to the host's view of this shared checkout, and the container cannot confirm it is unused.
- 02, commit expected_version and provision.sh (recommended). Done in `7fae4916`: `expected_version: "3.70.0"` and the new `.claude/provision.sh`.
- 03, tune the plan fact-checker (optional). Not done. It needs project knowledge the owner should supply. The generic agent `.claude/agents/hooks-daemon-plan-fact-checker.md` now ships and is committed.

## Host-side and desktop sessions (from the release notes)

- `.claude/ccy/claude-supervise.py` changed (release note 038: Ctrl+O and Ctrl+T no longer delay the resume after a compact). A ccy supervisor that is already running keeps the old code. New ccy sessions pick up the fix. To get it in an existing session, restart that session.
- The host's daemon runs from its own venv (py314, at v3.69.0). It should notice the version change and rebuild on its next hook event: there is a stamp check, and release note 072 says a pulled upgrade syncs itself in the background at session start. If a host or desktop agent session misbehaves, run `.claude/hooks-daemon/bin/hooks-daemon restart` from the host checkout.
- No plugin restart is needed.

## Optimise decisions

The checklist scored 169 of 172 relevant handlers enabled, with 3 recommendations. All 14 handlers the config advisory recommends are on by default and already enabled.

- Applied: review_round_advisor. It only gives advice, from the 4th review dispatch on one plan or branch. This project runs repeated qa-reviewer and re-review rounds.
- Declined: write_protected_paths. It does nothing until `options.paths` lists files, and choosing which files are read-only is the owner's call.
- Declined: github_issue_assignment_guard. It changes how issue work is claimed, and it needs an approved-author policy. That is the owner's call.
- The run is recorded with `record-config-optimisation-run` at v3.70.0.
- No safety handler was disabled. No Bash allowlist was touched.

## Health

- The daemon was restarted with the skill. Status: RUNNING, config validation PASSED, 176 handlers loaded.

## QA (qa-changed)

- Upgrade commit: every check passes except **bash-limits**. `CLAUDE/Plan/mkplan.bash` is daemon-owned and was replaced by the upgrade. It grew past its ratchet record: code lines 622 > 619, functions 19 > 18.
- I could not fix this within the rules:
  - The file must not be edited, because the next upgrade overwrites it.
  - Raising the record is prohibited, and the gate refuses it.
  - I tried an exemption, as CLAUDE/QA.md suggests for daemon-owned files. The gate refuses to drop the ratchet entry of an exempt file that still exists, so I reverted that attempt and nothing of it was committed.
- **qa-all will fail on bash-limits until the owner decides.** The options are to raise the record knowingly, or to change the gate's policy for daemon-owned plan tooling.
- Optimise commit: qa-changed passes.

## Other findings

- The host was heavily loaded (load average 20-34). During the session many hook calls were denied with "dispatch pool saturated" or socket timeouts. Running commands one at a time got through.
- The upgrade's worktree-seed drift report lists `.claude/hooks-daemon.env` as MISSING. The config already marks that entry `optional: true`, so it should not abort worktree creation. The report seems to ignore `optional`, which may be a daemon reporting defect.
- The same report lists `.claude/settings.local.json` as not seeded. I left it alone because it is the owner's local settings.
- The daemon's start-up retention sweep logged a FileNotFoundError on an agent-report file that had already been deleted. The sweep carried on, so this is noise only.

## What the owner must do

1. Decide how bash-limits should treat the daemon-grown `CLAUDE/Plan/mkplan.bash` (above) before the push gate can pass.
2. Optionally, restart ccy sessions to get the new supervisor, and restart the host daemon if a host session misbehaves.
3. Optionally, tune the plan fact-checker (post-upgrade task 03), and choose paths for write_protected_paths.
