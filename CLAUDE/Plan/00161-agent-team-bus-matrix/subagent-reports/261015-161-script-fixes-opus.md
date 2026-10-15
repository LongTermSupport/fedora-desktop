# Plan 00161: triage and deploy script fixes after the host run at 446634e8

## Root causes

1. triage.bash H1/H2: triage only tested the bus address when `--bus-address=` was given, and
   meta-deploy runs it with none; it never read `agent_bus_address` as deploy and acceptance do.
2. deploy.bash: the throwaway team file named `forge_api: https://api.example.com`, which
   agent-bus-install now refuses (D66: `https://api.github.com` only).
3. The sudo hint after a failed installer run printed on every failure, whatever the cause.

## Fixes

- `triage.bash` sources `_bus-address.inc.bash` and calls `resolve_bus_address` (flag, else
  host_vars, else fatal 64 before the log opens) unless `--claude-only`; `--reach-only` no
  longer needs the flag. No install-specific value is tracked.
- `_deploy-steps.inc.bash` passes `-` for forge_api (team-file's GitHub default) and keeps org
  `example`. The installer only stores the org in the team policy and contacts no forge; the
  team has no members. Verified: the generated file passes `helpers.agent_bus.teamfile.parse_team_file`.
- The sudo hint prints only when `sudo -n true` fails after the installer fails.
- `meta-deploy.list` re-queues the plan folder (triage, deploy, acceptance) with a description.

## Verification

The plan's six test modules pass, shellcheck is clean on the four touched or sourced scripts,
and `triage.bash --help` runs.
