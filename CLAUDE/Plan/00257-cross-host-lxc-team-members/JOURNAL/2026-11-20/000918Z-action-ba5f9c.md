## 00:09 · action · —

Item F of the B.12 follow-ups is done. ccy-teams offers "Start its session in its LXC container"
only on an LXC member row of this host (its record holds the member as joined or stopped)
whose present agent_bus_lxc_members entry has service: true. Other hosts' members,
service: false members and deferred or left ones get no line. A record or declaration that
cannot be read keeps the line, so choosing it says why. `declare.LxcMember` gained
`Service`; the check is `lxcsession.Starter.Offers`, read once per picker pass. The tests and
red proof are in subagent-reports/noauto-followup-report.md.
