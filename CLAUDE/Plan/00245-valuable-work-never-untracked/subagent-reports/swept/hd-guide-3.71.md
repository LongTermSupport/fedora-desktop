# Upgrade Guide: v3.70.0 to v3.71.0

## Summary

- **Breaking changes:** No. `CLAUDE/UPGRADES/config-changes/v3.71.0.yaml` has `breaking: false`.
- **Config migration required:** No. Four keys are added, one default changes for new configs, and two retired options are removed (they still validate, with a warning).
- **Pre-upgrade tasks:** none. **Post-upgrade tasks:** none.

## What changes for an existing project

- **The relay is on by default for new configs.** An explicit `daemon.transport.relay_enabled: false` is kept. A config with no `transport` block adopts the relay at the upgrade through the example merge. A partial `transport` block is kept whole, so the relay stays off until you add `relay_enabled: true` and `relay_source: auto`; then run `bin/hooks-daemon transport on`.
- **Added keys:** `daemon.chain.max_inflight` (default 16), `daemon.chain.slot_wait_seconds` (default 2), `handlers.session_start.relay_binary_checker` (on by default), `daemon.transport.relay_source: auto`. See the manifest.
- **Removed options (not breaking):** the `expiry_hours` option of `failsafe_cron_blockage_suppressor` and the `marker_expiry_hours` option of `idle_workable_issue_stop_gate`, listed under `removed` in the manifest. A config that sets either still validates and logs a warning, so no config stops loading; delete the key to clear the warning. The awaiting-human lifetime is fixed at 24 hours and the marker file `human-input-blockage-marker.json` is no longer read.
- **Truth change:** `paths:` is the only rules-file frontmatter field Claude Code reads. See `CLAUDE/UPGRADES/truth-changes/v3.71.0.yaml`.
- **ccy wrapper repair:** a wrapper written as `python3 .claude/ccy/claude-supervise.py` (broken by 3.69.0 and 3.70.0) is rewritten to `sh .claude/ccy/claude-supervise` by this upgrade. Read the upgrade output for any `export CCY_PYTHON=...` line it names.

## Steps

Follow the commands in `RELEASES/v3.71.0.md`, "Upgrading Existing Installations". If the gate exits 3, read what it lists and re-run with the `--skip-reading-confirmation=<digest>` it printed.

## Verification

```bash
.claude/hooks-daemon/bin/hooks-daemon restart
.claude/hooks-daemon/bin/hooks-daemon status
.claude/hooks-daemon/bin/hooks-daemon config-validate
```

`status` prints a `WARNING:` line if the relay is enabled but its binary is missing; `bin/hooks-daemon transport on` provisions it.

## Rollback

Restore the backup of `.claude/hooks-daemon.yaml`, then upgrade to the previous tag with the same procedure and `v3.70.0` as the target.

## Callouts

Every release-note callout behind this release is kept, with its plan number, in [release-notes/](release-notes/).
