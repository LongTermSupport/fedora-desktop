# Plan fact check: 00268 Task 1.9, cap default 5G

Scope: the claims the diff adds or changes in PLAN.md (Task 1.9).

## REFUTED

None.

## Claims

| #   | Claim                                                                      | Verdict           | Evidence                                                                                                                                                                                                                                                                 |
| --- | -------------------------------------------------------------------------- | ----------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1   | The play's `agent_bus_lightning_memory_max` default is 5G                  | VERIFIED          | `playbooks/imports/optional/common/play-agent-bus-element.yml:77`: `agent_bus_lightning_memory_max \| default('5G')`; also lines 25, 74, 152                                                                                                                             |
| 2   | `agent_bus_lightning_memory_max` is home-level and `false` removes the cap | VERIFIED          | play line 78 `agent_bus_lightning_memory_cap_on: ... is not sameas false`; `localhost.yml.dist:262-263` "false removes the cap" (a host_vars key)                                                                                                                        |
| 3   | host_vars documents the default as 5G                                      | VERIFIED          | `environment/localhost/host_vars/localhost.yml.dist:263`: `# agent_bus_lightning_memory_max: 5G`                                                                                                                                                                         |
| 4   | docs/agent-bus.md and meta-deploy.list agree on 5G                         | VERIFIED          | `docs/agent-bus.md:86` "capped at 5 GB"; `CLAUDE/Plan/meta-deploy.list:162` "memory at 5G with no swap". No remaining "2G" or "2 GB" in either file or the play                                                                                                          |
| 5   | The 2.2 GB figure is the busy-day peak of the older builds with no runaway | VERIFIED          | `JOURNAL/2026-11-26/115314Z-finding-28f2ed.md:15` ("two earlier builds, whose peak was 2.2 GB on busy days"); play comment line 75                                                                                                                                       |
| 6   | Owner at 11:58Z, verbatim: "on the 2.2 basis id cap at 5GB"                | UNVERIFIABLE-HERE | The sentence appears only in PLAN.md:133. `subagent-reports/261126-task-1-9.md:73` says only "the owner raised the cap from 2G at 11:58Z" and gives no quote. The chat transcript (the owner's message) would settle the wording. Substance (5G at 11:58Z) is consistent |
| 7   | The earlier quote (Q20, 11:40Z) mentions a "2GB cap"                       | UNVERIFIABLE-HERE | Unchanged by the diff, so not re-checked                                                                                                                                                                                                                                 |
