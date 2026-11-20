## 12:52 · decision · — — Owner: explicit wind-down only; plan kept Dormant, not a priority

The owner answered Q34 with option 1: "yes wind down only for now - we can keep this plan around for possible future use but its not a priority right now".

PLAN.md now opens with a scope decision:

- the idle trigger is dropped, along with gates 1 and 2 and any hooks-daemon change;
- the plan keeps the stand-down's end state: the session ends, compose `stop` runs, the record is kept and marked stood-down, and `ccy-sessions wake NAME` brings it back;
- something must ask for the stand-down explicitly: a `ccy-sessions` command, or the coordinator wind-down in #291.

Status is set to Dormant and priority to Low. Gates 3 to 5 stay as recommendations until the plan resumes. Task 2.1 is closed as decided.

Plan QA accepts Not Started, In Progress, Complete, Blocked, Cancelled, Superseded and Dormant. CLAUDE/PlanWorkflow.md still lists an "On Hold" status, which the check rejects; that is logged as an issue.
