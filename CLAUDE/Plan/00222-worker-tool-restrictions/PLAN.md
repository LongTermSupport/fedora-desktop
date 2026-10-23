# Plan 00222: worker tool restrictions

**Status**: Not Started (plan only; owner decisions OB1-OB3 open, defaults stated)
**Created**: 2026-10-23
**Owner**: joseph
**Priority**: Medium

## Overview

The owner: "restricting tools for worker team members - optional, but the default should be a
set of tool calls that are blocked for workers, e.g. the question tool (AskUserQuestion) in
particular - only the coordinator should have access to it. We could use the hooks daemon for
this and provide a special set of worker handlers; maybe we can do it with pure Claude Code
config via env vars?"

A worker that calls `AskUserQuestion` stops and waits on a terminal nobody watches, and its
question bypasses the coordinator, who alone is in the team's questions room. The research
([RESEARCH-mechanisms.md](RESEARCH-mechanisms.md)) compares Claude Code's own deny rules
(`--settings` and `--disallowedTools`), environment variables, a hooks-daemon handler and a
hook in the member kit. The deny rules are the strongest (the tool leaves the model's context)
but are fixed at launch, so they go wrong at the first hot role change (Plan 00203); no
environment variable restricts tools; the hooks daemon is per project, so a team session in a
checkout without it would be unguarded.

**Recommended: a PreToolUse hook in the member kit**, a small Go binary that reads this
session's role in each of its teams live, as the status line does, and denies a worker the
blocked tools with a message that points it at its coordinator. It is in every team session
(ccy, `cc --teams`, `agent-bus-claude`), follows role changes with no restart, and covers a
worker's sub-agents and forks.

## Goals

- By default a worker cannot call `AskUserQuestion`, `PushNotification`, `SendUserFile`,
  `EnterPlanMode` or `RemoteTrigger` (RESEARCH section 3); the coordinator can.
- The denial says why and what to do instead (`pingbus say --to-coordinator`), from a fixed
  template, and the worker's SessionStart context names the tools so it rarely tries.
- A hot role change takes effect at the next tool call, both ways, with no restart.
- A team can opt out, wholly or of single tools, in its `agent_bus_teams` entry.
- A worker's sub-agents and forks are held to the same rule (proven on the host, Task 1.1).

## Non-Goals

- Restricting the coordinator, a session on no team, or a human's own `claude`.
- Tools outside the catalogue of RESEARCH section 3 (a team may narrow the set, not add to
  it; a wider catalogue is a later change that widens the hook's matcher).
- Bash command patterns (`Bash(git push *)`): not a role question.
- A hooks-daemon handler, or any upstream feature request: none is needed (RESEARCH 2.4 notes
  what one would need; nothing is filed).

## Design

- **R1 The rule.** A tool of the blocked set is denied when the session holds at least one
  seat and is a worker in every team it is on. A coordinator in any of its teams is not
  restricted (it is the one that may ask the humans). The blocked set is the union of the
  worker teams' sets.
- **R2 The role source.** Exactly what the status line reads, read on every call: the teams
  from the session's record, or from `PINGBUS_MEMBERSHIP` and its applied file (ccy, `cc`), or
  from `PINGBUS_TEAMS` and `PINGBUS_HOME` (`agent-bus-claude`, which has no membership;
  `agent-bus-claude:78-86`); each team's role from the bundle's cached team record
  (`inbox.cached_role`; Go `statusline/roles.go`). A seated session whose role cannot be read in
  some team is treated as a worker there and denied with the reason (fail closed, fail loud:
  "the role in team T cannot be read; `pingbus status` says why"). In a membership session
  (ccy, `cc`), before the keeper has written `$PINGBUS_HOME/active` and after it is removed,
  the hook entry's guard exits 0 and allows: no seat is held yet, so there is no role to fail
  on. An `agent-bus-claude` session has no membership, so the guard passes it through from its
  first call and it is judged by R1 and this rule from then on. A session with no seat is never
  touched.
- **R3 The hook.** `hooks.json` gains a PreToolUse entry with the catalogue as its matcher and
  the same no-seat guard the other entries use; it runs `pingbus-tool-guard` (a Go command in
  the kit beside `pingbus-status-line`, reusing its record readers), which answers one JSON
  object: nothing for allow, `permissionDecision: "deny"` with the reason for deny.
- **R4 The message.** `agent-bus: <tool> is the coordinator's; in team <team> you are a worker. Ask your coordinator: pingbus say --team <team> --to-coordinator '<question>'. A question only a human can answer goes to the humans through the coordinator.` For
  `EnterPlanMode`: plan in your reply instead; a plan needs approval nobody here can give.
- **R5 Told up front.** The worker role line at SessionStart and in the role notice gains one
  sentence naming the blocked tools; a coordinator's line is unchanged.
- **R6 The per-team policy.** `agent_bus_teams[].worker_blocked_tools`: absent is the default
  set; a list (a subset of the catalogue) replaces it; `[]` opts the team out. Checked by the
  team file's rules. The admin keeps it as the main room's `agent_bus.worker_tools` state event
  (`{"blocked": [...]}`, or no event for the default), outside the team record so an older
  member is not made to distrust the team (the colour's route, Plan 00217 C8); the syncer
  keeps it in the team's state. An event that cannot be read means the default set (never
  less than the default). The key must reach every host_vars reader before any entry carries
  it: `teamfile.py`, `declare.py` (read side and `EDITABLE_KEYS`, for the settings screen) and
  00209 Task 7.1's Go line reader, each of which refuses a key it does not know. In v2 it is an
  informational roster key under A 3.1: an old member that ignores it applies the default set,
  which is stricter than any opt-out, never looser.
- **R7 Pure decision, separate reader.** `Decide(rolesByTeam, policyByTeam, tool)` is a pure Go
  package naming no v1 file, so v2's hook imports it; only the reader is v1's.

## Dependencies

- **Plan 00209 (v2), being built by another seat.** v2 replaces this plan's reader, hook entry
  and room-state route: requirements for 00209 are a PreToolUse guard among B 1.5's hooks
  (B25's one-JSON-line answer, R4's text), the role from the roster, `worker_blocked_tools` as
  a roster key and in `teamchat_teams[]` (C 3.8), and the migration carrying it (D 5). R7's
  decision package carries over unchanged. This plan edits none of 00209's design or plan
  files; its change to stage 1's Go reader (`src-go/internal/bus/declare`) is coordinated below.
- **Plan 00209 Task 7.1 (stage 1, in progress)** is porting `declare.py`'s host_vars reader to
  Go; it must learn `worker_blocked_tools` (R6). Coordinated with its owner seat.
- **Plan 00203 (hot control)**: R1 relies on its live role.
- **Plan 00211 (questions room)**: the reason a worker's human questions go through the
  coordinator.
- **Plan 00213**: the Go kit binaries and their build in the plays.
- **Plan 00221 (labels)** adds `labels:` to `agent_bus_teams` through the same three readers
  and an admin room-state event of the same shape: whichever plan builds first owns the shared
  reader change (its Task 1.3, this plan's Task 3.1), the other depends on it.

## Tasks

Every build task is test first: the failing test is committed or shown before the code.

### Phase 1: Facts first

- [ ] ⬜ **Task 1.1**: A host check, as a plan-local script: in a seated worker session, a
  plugin PreToolUse hook on `AskUserQuestion` fires and its deny holds in bypassPermissions
  (ccy runs `--dangerously-skip-permissions`; the vendored docs say this of deny rules, not of
  a hook's deny), in the main conversation, a foreground sub-agent and a fork (the docs cover
  sub-agents, `hooks.md:296`, not forks); record the Claude Code version.
  If the fork or sub-agent case fails, journal it and take the fallback of RESEARCH section 4.

### Phase 2: The guard

- [ ] ⬜ **Task 2.1**: The decision package (R1, R2, R7): table tests over one and several
  teams, mixed roles, unreadable roles, each policy form, every catalogue tool.
- [ ] ⬜ **Task 2.2**: `pingbus-tool-guard`: the reader over the status line's records, the JSON
  answer and R4's text, with tests on a fixture home; built and placed in the kit by the play
  as `pingbus-status-line` is.
- [ ] ⬜ **Task 2.3**: `hooks.json`'s PreToolUse entry (R3); a test that its matcher equals the
  catalogue and that the no-seat guard starts nothing.
- [ ] ⬜ **Task 2.4**: R5's sentence in the role templates (`helpers/pingbus/hooks.py`), tests
  first.

### Phase 3: The opt-out

- [ ] ⬜ **Task 3.1**: `worker_blocked_tools` taught to every host_vars reader before
  `localhost.yml.dist` shows it: `teamfile.py`, `declare.py` (read and `EDITABLE_KEYS`) and
  00209 Task 7.1's Go line reader, each with a test that an entry carrying it is read, not
  refused (shared with Plan 00221 Task 1.3); then the admin's `agent_bus.worker_tools` event,
  the syncer's copy and protocol spec §8 (R6).
- [ ] ⬜ **Task 3.2**: The ccy-teams settings screen shows the field and its effect (one line,
  with the rest of the team's fields).

### Phase 4: Docs, deploy, review

- [ ] ⬜ **Task 4.1**: docs/agent-bus.md: what a worker cannot do and how a team opts out.
- [ ] ⬜ **Task 4.2**: The entrypoint's list of required kit files gains `pingbus-tool-guard`
  (`files/var/local/claude-yolo/entrypoint.sh`, beside `pingbus-status-line`), with the CCY
  version bump that path needs; the meta-deploy entry (play-agent-bus.yml for the kit and the
  admin; play-claude-yolo.yml; the ccy image takes the kit at its next build).
- [ ] ⬜ **Task 4.3**: Adversarial review of the diff until MERGE, then `qa-reviewer`.
- [ ] ⬜ **Task 4.4**: On the host: a worker's `AskUserQuestion` is denied with R4's message; a
  hot promotion lets it ask at the next call; a team with `worker_blocked_tools: []` is not
  restricted.

## Decisions

### Owner only

| ID  | Decision                                       | Default (recommended)                                                                                                                                                           |
| --- | ---------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| OB1 | The default set beyond `AskUserQuestion`       | `PushNotification`, `SendUserFile`, `EnterPlanMode`, `RemoteTrigger` (RESEARCH section 3, each with its reason)                                                                 |
| OB2 | The mechanism                                  | **the kit's PreToolUse hook** (live role, every team session); the alternative is the kit's `--settings` deny (tool hidden, but fixed at launch, wrong after a hot role change) |
| OB3 | On by default for every team, opt-out per team | yes, as the owner asked (R6)                                                                                                                                                    |

### Coordinator-defaultable

| ID  | Decision                    | Default                                                                  |
| --- | --------------------------- | ------------------------------------------------------------------------ |
| CB1 | A session on several teams  | restricted only if a worker in all of them; the union of their sets (R1) |
| CB2 | A role that cannot be read  | treated as worker, denied with the reason (R2)                           |
| CB3 | What a team's list may name | a subset of the catalogue only (Non-Goals)                               |
| CB4 | The guard's language        | Go, beside `pingbus-status-line` (R3)                                    |
| CB5 | `ExitPlanMode`              | never blocked, so no session is trapped in plan mode                     |

## Success Criteria

- [ ] In a worker session each of `AskUserQuestion`, `PushNotification`, `SendUserFile`,
  `EnterPlanMode` and `RemoteTrigger` is denied with R4's fixed text for that tool, compared
  byte for byte; in the coordinator's session none is denied.
- [ ] A hot role change applies at the next tool call without a restart.
- [ ] A team opts out with one host_vars field.
- [ ] Task 1.1's host check passes for the main conversation, a sub-agent and a fork.
- [ ] `qa-changed.bash` passes on each commit; review at MERGE; `qa-reviewer` passes.

## Delivery & Milestones

- Plan and research: branch `agent-a73de92448810ff4c-98c4ef93`.
