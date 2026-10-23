# Plan 00222 research: how to keep tools from a worker

The question: a team's workers should, by default, not have a set of Claude Code tools, above
all `AskUserQuestion`, which only the coordinator should have; the set should be optional per
team. Which mechanism, how it learns the role, and how a worker's sub-agents inherit it.

Sources, in order of weight: the vendored Claude Code docs under the hooks daemon's
`remote-docs/` tree (`code.claude.com/docs/en/permissions.md`, `settings-reference.md`,
`sub-agents.md`, `tools-reference.md`, `hooks.md`, fetched 2026-10-17; and
`docs.claude.com/en/docs/claude-code/settings.md`), cited as `<file>:<line>`; this repository's
member kit (`helpers/pingbus/seat.py`, `files/opt/claude-yolo/optional/agent-bus/`,
`files/var/local/claude-yolo/entrypoint.sh`, `files/var/local/claude-code/cc-teams.bash`); and
the v2 design of Plan 00209 (chapter B 1.5). The CLI reference page is not vendored; what is said
of `--disallowedTools` comes from the permissions and settings pages, which describe it.

## 1. What the session already has

Every team session, whatever launches it, runs Claude Code with the member kit's plugin and a
`--settings` file the kit writes: ccy's entrypoint (`entrypoint.sh:592`), `cc --teams`
(`cc-teams.bash:143`) and `agent-bus-claude` (`agent-bus-claude:96`). The plugin's
`hooks/hooks.json` already registers SessionStart, UserPromptSubmit, PostToolUse (Bash),
Stop and SessionEnd hooks, each guarded so a session holding no seat starts nothing. ccy runs
Claude Code in bypassPermissions mode.

A member's role is per team (a session can be coordinator in one team and worker in another),
and it changes while the session runs (Plan 00203's hot control: `ccy-teams session role`,
`set-role`). The status line reads it on every refresh from the team record the role notice
reads (docs/agent-bus.md, the status line), so a live, readable source of the role exists in
every session.

## 2. The options

### 2.1 `permissions.deny` in the kit's `--settings` file (pure Claude Code config)

- A bare tool name in `deny` removes the tool from Claude's context entirely, so the model
  never sees it (`permissions.md:76`).
- Deny rules block in every mode, bypassPermissions included (`settings-reference.md:1706`);
  a tool denied at any level cannot be allowed by another (`permissions.md:730`).
- `--settings <file>` applies above user, project and local settings for that session
  (`settings.md:576`, `:643`). The kit already passes one, so the launcher has the hook point:
  `seat.session_settings` would add `"permissions": {"deny": [...]}` for a worker seat.
- Sub-agents inherit the parent conversation's permission rules (`sub-agents.md:41`).
- **But it is fixed at launch.** The kit's own note records that Claude Code reads a
  `--settings` file once, at start (`seat.py:448`, "the M2 spike"); the docs' live reload
  covers user, project, local and managed settings (`settings.md:594-596`), not the flag file.
  A hot promotion (worker to coordinator) would leave the new coordinator without
  `AskUserQuestion` until a restart, and since a deny cannot be lifted by a later allow, no
  hook could give it back. A hot demotion would leave a worker with it. Plan 00203's promise is
  that role changes need no restart.
- A session on several teams: the deny would have to be computed once from the launch roles.

Verdict: the strongest enforcement, and wrong after the first hot role change. Not chosen as
the mechanism; recorded as the alternative (owner decision OB2).

### 2.2 `--disallowedTools` on the command line

The same per-session deny rules (`settings-reference.md:1586`, "adds deny rules for one
session"; it survives settings reloads, `:1403`), passed as an argument instead of a file. It
has 2.1's launch-time defect, and it needs a change to three launchers' argument lists where
2.1 needs one function. Not chosen.

### 2.3 Environment variables

No Claude Code environment variable restricts tools: the settings reference names none (a
search of the vendored pages for one finds only the settings keys and the flag). The settings
file's `env` key sets the session's environment, not its permissions. An environment variable
is useful only as the signal a hook reads, and one set by the launcher (`PINGBUS_ROLE=worker`)
is as stale after a hot role change as 2.1. Not chosen; the kit's existing variables
(`PINGBUS_MEMBERSHIP`, `PINGBUS_HOME`) are what a hook uses to find the live records.

### 2.4 A hooks-daemon handler keyed on the bus role

- The hooks daemon is installed per project: a team session in a checkout without it (any
  repository the owner has not set it up in, an LXC guest's checkout) would have no
  restriction. The member kit is in every team session (section 1).
- The daemon would have to learn the role: by reading pingbus's per-session team records (it
  would need their path and format, which are v1's and go with v2) or by running
  `pingbus status` per call.
- What it would need upstream: a handler set that ships with a plugin rather than a project
  (the plugin handler API the v2 design already waits on, hooks-daemon #119), and a way for a
  handler to ask a provider for a session attribute ("this session's bus role") instead of
  reading another tool's files. Neither is needed by the recommendation, and nothing is filed.

Verdict: wrong place. Its strength, a deny with a message, the kit's own hook has too.

### 2.5 A PreToolUse hook in the member kit (chosen)

- `hooks.json` gains a PreToolUse entry whose matcher is the fixed catalogue of section 3
  (`AskUserQuestion|PushNotification|...`), so the hook runs on those tool calls only, never
  on Bash, Read or Edit. PreToolUse matches `AskUserQuestion` and other built-in tools by name
  (`hooks.md:1760`) and its `permissionDecision: "deny"` blocks the call with a reason the
  model reads.
- It reads the role live, on each call, from the same team records the status line reads, so
  a hot role change applies at the next call with no restart, in both directions.
- It applies in every team session whatever the checkout (ccy, `cc`, `agent-bus-claude`),
  because the kit is what makes a session a team member.
- The model still sees the tool; a call is denied with a redirect ("ask the coordinator with
  `pingbus say --to-coordinator`"), and the SessionStart worker line names the tools, so a
  well-told worker rarely tries. That costs at most a turn per attempt, against 2.1's wrong
  answer after a role change.
- Cost: one process per matched call. Built as a Go binary beside `pingbus-status-line` (the
  same role reader, milliseconds to start), not a Python `pingbus hook`, so it follows the
  rule that new tooling is Go and the kit's existing Go carve-out.

## 3. The default set for workers

| Tool                                  | Blocked for a worker by default | Why                                                                                                                                                                                                                                                                                    |
| ------------------------------------- | ------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `AskUserQuestion`                     | yes (the owner's)               | it waits on a terminal that, for a worker, nobody watches (questions stay open until answered, `tools-reference.md:143`), and a worker's questions to humans go through the coordinator, who alone is in the questions room (Plan 00211; `check-rooms` calls a worker there a problem) |
| `PushNotification`                    | yes                             | it reaches the human directly, past the coordinator, on the desktop or a phone (`tools-reference.md`, the tool table); a worker tells a human through the coordinator or `pingbus report`                                                                                              |
| `SendUserFile`                        | yes                             | the same: a file sent to the human's device past the coordinator                                                                                                                                                                                                                       |
| `EnterPlanMode`                       | yes                             | plan mode ends with `ExitPlanMode`, which needs the human's approval (`tools-reference.md`, "Permission required: Yes"), so an unattended worker stops there. `ExitPlanMode` itself is not blocked, so a session already in plan mode is never trapped                                 |
| `RemoteTrigger`                       | yes                             | it creates and runs cloud routines on the owner's account, work outside the team and its room                                                                                                                                                                                          |
| `Artifact`, `SendMessage`, cron tools | no                              | not role questions: `Artifact` is publishing policy (projects guard it), `SendMessage` also resumes sub-agents and a bare deny would remove that, and the cron tools keep a session alive (the hooks daemon's failsafe cron uses them)                                                 |

`EndConversation` cannot be denied while any other tool remains (`permissions.md`, the
permissions overview), and is not in the set.

## 4. Sub-agents of a worker

- `AskUserQuestion` is removed from every sub-agent by Claude Code itself, even when listed in
  its `tools` (`sub-agents.md:443-446`). Forks skip that filter and receive the main
  conversation's exact tool pool (`sub-agents.md:443`), so a worker's fork could call it.
- Background sub-agents keep only a short list of built-in tools, which excludes the rest of
  section 3's set (`sub-agents.md:454`); foreground ones inherit the parent's tools.
- A plugin's PreToolUse hook is the session's hook, so it judges a fork's or sub-agent's call
  as it judges the main conversation's. That is to be proven on the host (Plan Task 1.1) before
  the build relies on it; if it does not hold, the fallback is to add 2.1's launch-time deny for
  `AskUserQuestion` alone and accept a restart on promotion.

## 5. v1 and v2

The guard reads v1's per-session team records. v2 (Plan 00209) rebuilds the kit as `teamchat`
with its own hooks (B 1.5, B25: every hook answers one JSON line from fixed templates) and
reads the role from the roster. The decision function (roles per team, the team's policy, the
tool, the answer) is a pure Go package with no v1 file in it, so v2's hook imports it and only
the reader is v1's. The per-team opt-out travels as admin room state in v1 and as a roster key
in v2 (Plan PLAN.md, Dependencies).
