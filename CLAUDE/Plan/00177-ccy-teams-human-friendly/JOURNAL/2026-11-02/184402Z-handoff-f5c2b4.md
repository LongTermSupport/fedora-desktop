## 18:44 · handoff · —   — handover: state at the end of the coordinator session

Handover (coordinator, desktop team).

Task 15.3 (the LIVE lines, so that "active" means listening) is merged, deployed and checked on the host. `agent-bus list` prints LIVE for each member, and ccy-teams shows listening, stale or not listening. 15.4 stays open only for the owner's look at a stale member in `--list` and `--graph`.

During the deploy the host agent found that changed-plays missed `play-agent-bus.yml`: the zipapp is built by a script the play runs, from `helpers/agent_bus`. This was fixed as a class under Defence Before Fix (0fbe01f4e). In a bash file the play runs, a `python3 -m helpers...` module is now an input, and the two self-update rules hold every play to that.

15.2 is left with only the Mermaid render, which needs a viewer on the host.
