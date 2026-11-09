# Plan 00236: subagents cannot speak for their seat

**Status**: Not Started (decided by the owner, 2026-10-28; nothing built)
**Created**: 2026-10-28
**Owner**: joseph
**Priority**: Medium

## Overview

The owner saw a sub-agent act as its team's coordinator. A sub-agent inherits its parent
session's seat on the agent bus: its `pingbus say` or `send` goes out under the coordinator's
handle, its `ccy-teams` calls change the team, and the sub-agents it starts inherit the seat in
turn. Nothing tells the team that the sender is not the session that holds the seat.

The owner's decision, 2026-10-28 ("all recommended" to the unblocking list, item A1): a
hooks-daemon rule that applies only inside a sub-agent refuses the bus commands that speak, team
changes through `ccy-teams`, and starting further sub-agents. Read-only pingbus stays allowed. A
sub-agent's only channel is then its final reply to the coordinator, and coordinators keep their
sub-agents. The rejected alternative was no sub-agents for coordinators at all, with every worker
a separate LXC or ccy session.

## Goals

- A PreToolUse project handler under `.claude/hooks/handlers/pre_tool_use/`, registered in
  `.claude/hooks-daemon.yaml`'s `plugins` list beside `system_paths.py` and
  `ansible_enforcement.py`, that acts only on a sub-agent's call (one carrying an agent id) and
  denies:
  - a Bash command that runs a pingbus speaking command (`say`, `send`, `report`, `ask-human`,
    `thread`), however it is reached (a path, `env`, a chain or a subshell);
  - a Bash command that runs a `ccy-teams` mutation;
  - the Agent tool.
- Each denial's reason tells the sub-agent to put what it wanted to say or do in its final
  reply to the coordinator.
- Read-only pingbus (`status`, `history`, `questions` and the other reads) and read-only
  `ccy-teams` (`--list`, `for-checkout`) stay allowed in a sub-agent.
- The main session is never affected.
- Coordinators are told not to use fork sub-agents for worker tasks.

## Non-Goals

- Changing pingbus, agent-bus or the seat model. The rule is a hook, not a bus permission.
- Reproducing the owner's incident: the owner gave no session or time, so the handler's tests
  are its proof.

## Tasks

### Phase 1: The rule, test first (Defence Before Fix)

- [ ] ⬜ **Task 1.1**: Read the daemon's sub-agent-scoped handlers first:
  `subagent_full_qa_blocker` (R-SUBAGENT-FULL-QA) and `subagent_cron_delete_blocker`
  (R-SUBAGENT-CRON-DELETE) in the daemon's `handlers/pre_tool_use/`. They take the sub-agent test
  from the handler scope (`HandlerScope.SUB`: admitted only when `agent_id` is present and the
  event is not synthetic) rather than reading `agent_id` themselves. Use the same scope if a
  project plugin can declare it; if it cannot, say so here and take the test from one helper,
  never a second copy of the rule.
- [ ] ⬜ **Task 1.2**: Red: tests in `.claude/hooks/handlers/pre_tool_use/tests/`, written before
  the handler and seen to fail. Denied in a sub-agent: each of the five speaking commands, by
  bare name, by path, after `env`, in a `&&` chain and in `$( )`; each `ccy-teams` mutation; the
  Agent tool. Allowed in a sub-agent: every read-only pingbus and `ccy-teams` command, and a
  command that only mentions the words (`grep 'pingbus say' file`, an `echo`). Allowed in the
  main session: every one of the denied calls. Each denial's reason names the final reply to
  the coordinator.
- [ ] ⬜ **Task 1.3**: Fail closed on the command lists: a pingbus or `ccy-teams` subcommand on
  neither the speaking or mutating list nor the read-only list is denied in a sub-agent, and a
  test reads each tool's own subcommand list (pingbus's argument parser, ccy-teams' dispatcher)
  so a new subcommand fails the test until it is classified.
- [ ] ⬜ **Task 1.4**: Green: the handler, registered in `.claude/hooks-daemon.yaml`'s `plugins`;
  the daemon restarted through the `hooks-daemon` skill; its rule documented where the daemon's
  generated handler guidance picks it up, else in `CLAUDE/AgentNotes.md`. Red and green
  journalled.

### Phase 2: What coordinators are told

- [ ] ⬜ **Task 2.1**: One sentence in pingbus's `role_coordinator` text
  (`helpers/pingbus/hooks.py`): a coordinator never uses fork sub-agents for worker tasks; a
  worker is a team member, and a sub-agent's only channel is its final reply. It goes here
  rather than in `CLAUDE/AgentNotes.md`, because the role text reaches every coordinator in
  every project at session start, and AgentNotes reaches only this repository's sessions. Test
  first in pingbus's hook tests; it reaches the sessions through pingbus's usual deploy
  (`play-agent-bus.yml` and the ccy image, with their version bumps).

### Phase 3: Review

- [ ] ⬜ **Task 3.1**: qa-reviewer over the branch diff; every BLOCK and FIX-BEFORE-MERGE
  resolved.

## Success Criteria

- [ ] In a sub-agent, every speaking pingbus command, every `ccy-teams` mutation and the Agent
  tool are denied with a reason naming the final reply; the reads pass.
- [ ] In the main session, nothing changes.
- [ ] The tests were red before the handler and green after; a new unclassified subcommand
  fails them.
- [ ] A coordinator's start text says not to use fork sub-agents for worker tasks.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00236-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan only, from the owner's answer (item A1 of the unblocking list).
