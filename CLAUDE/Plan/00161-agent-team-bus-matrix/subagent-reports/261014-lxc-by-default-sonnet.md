# LXC admitted by default (Plan 00161 follow-up)

Branch `feat/agent-bus-lxc-by-default` from F44. Not pushed, not merged.

## What changed

- `helpers/agent_bus/bridge.py` (new, test first in `tests/helpers/agent_bus/test_bridge.py`): the one place a team's effective `allow_from` is computed: declared list plus the bridge's subnet, no duplicates, declared order kept. The subnet comes from the bridge's Ansible facts (network and prefix, or address and netmask), canonicalised; nothing is written down. Absent bridge (`null` facts): a one-line note, nothing added. Bridge with no IPv4: exits 1 naming lxc-net.
- `playbooks/imports/play-agent-bus.yml`: variable `agent_bus_lxc_bridge` (default `lxcbr0`); a task calls the helper with `ansible_facts[<bridge>]` and the present teams; a debug task prints the note; the team files are rendered from the helper's output. The installer, and through it the firewall rule, the unit drop-in (`render.py`) and `teamfile.py` validation, therefore all see the same list. `render.py` and `teamfile.py` needed no change.
- `helpers/agent_bus_lxc/members.py`: the "allow_from must cover the bridge" check stays (it reads the installed team file, so it passes by default), but its message now says the play admits `agent_bus_lxc_bridge` itself and offers setting that variable or adding the subnet for another bridge. The existing test still passes.
- Guidance removed or reworded: `docs/agent-bus.md` (create wizard paragraph, step 4, team-file note, table row, LXC paragraph, one line saying VMs are not covered yet), `README.lxc` in the member kit, `localhost.yml.dist` comments, the `ccy-teams create` prompt text (`src-go/internal/teams/create/wizard.go`) and `usage.txt`.

## Checks

`./scripts/qa-changed.bash` green: ruff, unit tests, ansible syntax, ansible patterns, docs, qa-go, and the suites test-ccy-teams, test-ccy-teams-command and test-agent-bus-install. No playbook was run. `qa-all.bash` not run.

## Notes for the owner

- `declare.py` still writes `allow_from: []` for new teams; correct now, as the bridge is added at render time. Its `--allow-from` help is unchanged.
- The helper depends on fact gathering for the bridge (`gathering = smart`, so facts are gathered); the play was verified by syntax check only. A host run of `play-agent-bus.yml` is the first real test.
- Nothing needs adding to `meta-deploy.bash` that is not already run there (`play-agent-bus.yml` is the play that applies this).
