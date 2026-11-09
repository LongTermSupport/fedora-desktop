## 10:07 · decision · —   — the owner decides: sub-agents cannot speak for their seat

The owner answered the unblocking list of 2026-10-28 with "all recommended", typed directly in
the coordinator's session. Item A1, sub-agents acting as coordinator: add a hooks-daemon rule
that applies only inside a sub-agent and refuses the bus commands that speak (`pingbus say`,
`send`, `report`, `ask-human`, `thread`), team changes through `ccy-teams`, and starting further
sub-agents. Read-only pingbus stays allowed; a sub-agent's only channel is its final reply to
the coordinator; coordinators keep their sub-agents. The rejected alternative: no sub-agents for
coordinators, every worker a separate LXC or ccy session.

This plan holds tasks only. The handler is a project PreToolUse handler modelled on the daemon's
sub-agent-scoped handlers (R-SUBAGENT-FULL-QA, R-SUBAGENT-CRON-DELETE, which take the test from
`HandlerScope.SUB`), built Defence Before Fix: red tests first, the command lists fail closed.
The "no fork sub-agents for worker tasks" sentence goes in pingbus's `role_coordinator` text
rather than AgentNotes, since the role text reaches every coordinator at session start. The
owner gave no session or time for the incident, so it is not reproduced first.
