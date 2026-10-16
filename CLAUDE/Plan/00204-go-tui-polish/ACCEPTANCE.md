# Plan 00204: host acceptance

The owner's steps at a host terminal, after `play-claude-yolo.yml` has built and installed
`ccy-teams` and `ccy-sessions` (queued in `CLAUDE/Plan/meta-deploy.list`). The freeze tools
are rebuilt by their optional plays (`play-lxcfreeze.yml`, `play-podfreeze.yml`), so H7 waits
for the next run of whichever is installed. Nothing here is scripted: what is checked is what
a person reads.

## Phase 1, the context heading (D4)

Every `ui` screen starts with a heading: the path that led to it, with a wizard's step, then a
rule, then a line naming what it acts on (InteractiveScripts.md rule 15). On a terminal the path
is bold in the accent colour; with `NO_COLOR=1` it is plain.

| Step | Do                                                                                                                        | See                                                                                                                                                                                                         |
| ---- | ------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| H1   | `ccy-teams`                                                                                                               | Above the key legend: `Teams`, then how many teams there are (and which are not up).                                                                                                                        |
| H2   | Enter on a team row, then on a seat row                                                                                   | `Teams › <team>` over `team <team> · <state>`; `Teams › <team> › seat <n>` over what the row is.                                                                                                            |
| H3   | On a team's menu, *Add an LXC container's agent*; answer each step                                                        | `Teams › <team> › Add LXC member · step 1/4 Container` over `into team <team>`; each later step adds the answer given (`container …`, `user …`, `checkout …`); the last question is `… › Review`.           |
| H4   | The Running sessions row, Enter on a session, *Add to a team*; choose a team, a seat and a role, then Esc at the question | `Teams › Running sessions › <session> › Add to a team · step 1/3 Team`, then `step 2/3 Seat` and `step 3/3 Role` each over `session <session> · team <team>`: the team being joined is named on every step. |
| H5   | In a checkout, `ccy --teams` with no value; mark two teams (Tab), Enter                                                   | The list is headed `New session › Teams`; each team's seat question `New session › Team 1 of 2: <team> · step 1/2 Seat`, then `Team 2 of 2: <team>`.                                                        |
| H6   | `ccy-sessions`, then Ctrl-X on a session and Esc                                                                          | `Sessions` over `<n> running · new sessions start in <dir>`; the question is headed `Sessions › <session> › End`.                                                                                           |
| H7   | `lxcfreeze` (or `podfreeze`) with no target; choose a group                                                               | The menu headed with the tool and how many groups; the drill-down with `<tool> › <group>` and how many containers are in it.                                                                                |
