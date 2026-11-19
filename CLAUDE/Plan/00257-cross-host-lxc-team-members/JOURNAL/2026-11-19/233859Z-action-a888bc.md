## 23:38 · action · —

B.12 follow-ups and the B.12 review (lxc-no-autostart-review.md) worked through:

- PLAN.md: the v1-freeze Non-Goal records the owner's no-autostart order as its override, and the coordinator's ruling that lxc.start.auto is the container owner's choice.
- The v2 unit-down why names `teamchat-admin lxc restart --member <container>/<seat key>` (SPEC-n3 row 09).
- v2 placeUnit stops a boot-started session before removing its wants link, so a failed stop is retried.
- The no-autostart acceptance script lists members through plan_lxc_online_named (offline never fails), treats an unanswered probe as exit 2, prints coverage, checks is-active after a reboot, is plan_mode deploy and ends in plan_finish.
- Docs: localhost.yml.dist and agent-bus-lxc-members.md say the session runs only on demand.
- lxc.start.auto is neither required nor read for a member, in either generation.
- TestT9AFloodAtTheCapHoldsNoSlot waits on a server-side admission hook: red 20 of 20 on the old admit, green 50 of 50 under -race.

Open: item F (ccy-teams Start line only for this host's service members), handed off (untracked/scratch/noauto-F-handoff.md). Report: subagent-reports/noauto-followup-report.md.
