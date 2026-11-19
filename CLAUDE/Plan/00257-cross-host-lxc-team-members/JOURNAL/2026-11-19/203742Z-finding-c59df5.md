## 20:37 · finding · B.12 — no LXC member session starts with its container

Owner's order (verbatim): "there is no need for claude to start as soon as the LXC container
boots - in fact that is AWFUL - remove that completely." Done in both bus generations.

- v1 (helpers/agent_bus_lxc/members.py): the agent-bus-claude-<user> unit has no [Install] and
  Restart=no; the play never enables or starts it; an enabled unit is disabled and stopped
  (`disable --now`); a changed unit is only reloaded, so an on-demand session keeps running.
  RED: 5 of the new ServiceTest cases failed against the old code; GREEN after.
- v2 (host/lxc unit.go, apply.go): the session unit has no [Install] and Restart=no; apply
  makes no wants link and never calls Start or Restart; a wants link an earlier apply made is
  removed and the session stopped (Stop, then Reload). RED: TestRenderNeverStartsAtBootOrByItself
  and five apply/fault tests failed against the old code; GREEN after.
- On demand: v1 gets a ccy-teams menu line, Start its session in its LXC container
  (teams/lxcsession, lxcfreeze.Lister.StartUnit; systemctl gained lxcfreeze as a second owner in
  the project_seam_commands table, as nmcli did). v2 keeps `teamchat-admin lxc restart --member`,
  whose Restart starts an inactive unit that is not enabled. ccy-teams shows no v2 LXC rows,
  so no ccy-teams start for v2 was built: a decision for the coordinator.
- Detector for the class (a member session unit that starts at boot): rendering tests in both
  generations that refuse [Install], WantedBy= and the other install keys, and any Restart= but
  no. No Go analyzer: the v2 text is built from non-constant parts, and v1 is Python.
- Host proof: lxc-no-autostart-acceptance.bash, queued after play-agent-bus.yml in
  meta-deploy.list. Its reboot question is put at the terminal even under meta-deploy, because
  plan_confirm takes PLAN_ASSUME_YES (which meta-deploy exports) as yes, and a container is
  never rebooted unasked.
