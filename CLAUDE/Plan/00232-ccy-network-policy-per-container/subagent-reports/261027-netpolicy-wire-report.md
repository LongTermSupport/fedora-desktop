# Plan 00232: Tasks 3.3 and 3.4 wired

Branch `feat/00232-netpolicy-build`, commit `032903341` (not pushed).

## Wired

- `lib/network-policy.bash` (new): `ccy_network_policy` (the command, or 1 naming the play when
  it is missing), `ccy_network_policy_launch` (the session's run), `ccy_network_change` (the
  both-at-once refusal, connect or disconnect, then `refresh`). `ccy_agent_container` calls the
  launch; the launcher's three connect/disconnect blocks are one call. A new library was needed
  because the bash-limits gate refused the inline wiring (agent-choice.bash over 90 statements;
  the launcher over its ratchet record, now tightened to 2700).
- `play-claude-yolo.yml`: `nftables`, and `ccy-network-policy` built into `~/.local/bin`;
  `tasks/claude-yolo-image.yml` lists the new library.
- CCY 3.101.0 (container unchanged), `docs/ccy-changelog.md`, `docs/ccy.md` "Network policy" and
  the security table.
- difftest: `ccy-network-policy` is an observed pass-by-name tool; goldens re-recorded from the
  bash; `go test ./internal/ccylaunch/...` passes. `scripts/test-ccy-agent-choice.bash` passes.
- `deploy.bash` (the play), `acceptance.bash` (renamed), `_acceptance-sessions.inc.bash`,
  `meta-deploy.list` entry. `./scripts/qa-changed.bash` passes.

## Departure from the brief

acceptance.bash, not deploy.bash, launches the two throwaway sessions (np232a, np232b), because
meta-deploy passes acceptance.bash no arguments and a session started by deploy.bash would
outlive the only script that can remove it. The team is the one this checkout already has a
seat in (`agent-bus seat list`), or `--team=`; none is exit 2. The bus check now reads each
session's own seat bundle (it read the first `member.json` under the team) and runs pingbus with
the session home the entrypoint uses.

## Open

- The `qa-reviewer` agent has not been run (Task 3.4's last step).
- The tests in `test-ccy-agent-choice.bash` were written with the change, not seen red first.
- The sessions' stream-json fifo with no turn sent follows Plan 00201's acceptance; whether
  claude keeps its container up with no input is first proven by the host run.
- The headless launch takes the checkout's saved Quick Launch (token, keys); none is exit 2.
