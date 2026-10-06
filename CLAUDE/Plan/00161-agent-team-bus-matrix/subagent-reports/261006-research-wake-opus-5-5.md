# Research: waking an idle session when a ping arrives (Plan 00161, Task 1.1)

Scope: the ways an idle Claude Code session can be woken by a ping, judged on what wakes,
latency, failure modes and prompt-injection exposure (issue #59 forbids free text reaching
an agent). Read-only research; nothing was run against a live session.

Sources read: issue #59 (sections 3, 5, 6, 7, 8); `CLAUDE/AgentMailbox.md` and
`scripts/agent-mailbox-watch.bash`; `docs/ccy.md` "The Supervisor" and "Session limits";
`files/var/local/claude-yolo/entrypoint.sh` (settings merge, plugin and skill install,
supervisor wrapper); `files/var/local/claude-yolo/supervisor-plugins/ccy_lifecycle.py`;
`.claude/ccy/claude-supervise.py` (the deployed supervisor); the hooks daemon's
`PROJECT_HANDLERS.md`, `explain-handler hook_registration_checker`, and its Plan 00487
(supervisor plugin API); this repo's Plan 00146 `UPSTREAM-REQUEST-supervisor-plugins-draft.md`;
`.claude/hooks-daemon.yaml`, `.claude/settings.json`; issue #31; the harness's own tool
contracts for background Bash and Monitor.

## The one fact every option shares

A Claude Code session that has ended its turn runs nothing. Only four things start a new
turn: a human typing; the harness re-invoking the agent because a background task it owns
ended or emitted; something typing into the session's terminal (the ccy supervisor owns
the PTY); and a scheduled prompt (session crons). Hooks never start a turn on their own:
they fire on events inside a turn (Stop at its end, UserPromptSubmit at its start). So a
hook can stop a session going idle while pings are pending, but cannot wake one that is
already idle.

## 1. A background `pingbus wait` started by the agent

**What wakes.** The agent runs `pingbus wait` with Bash `run_in_background`. The harness
re-invokes the agent when the command exits, and the agent reads its output. This is the
mechanism the file mailbox already relies on (`agent-mailbox-watch.bash` exits on a waiting
request, the agent handles it and re-arms). The Monitor tool is the streaming variant: each
stdout line becomes a notification, without exiting.

**Time limits (checked against the harness contracts in this session).**

- Bash `run_in_background`: no time limit is stated, and this project's
  `R-UNBOUNDED-LIVENESS-LOOP` rule says the same ("run_in_background has no time limit").
  The mailbox watcher runs up to six hours, then exits 3 to be re-armed.
- Monitor: `timeout_ms` default 5 minutes, capped at 30 minutes; on expiry the agent is
  notified and must re-arm.
- So issue #59 section 5's "30 minutes by default" is the **Monitor** cap, not background
  Bash. Recommendation: `pingbus wait` takes `--timeout` with a default well under 30
  minutes (for example 25) so the same command works under both, exits with a distinct
  "timed out, re-arm" code (as the mailbox's exit 3), and writes a "listening until <t>"
  status that `pingbus status` shows as stale once past. A short default also bounds the
  damage of a waiter orphaned by a crashed or compacted session.

**Latency.** Matrix `/sync` with a long-poll `timeout` returns as soon as an event lands, so
wake latency is the homeserver round trip plus the harness's own notification delay:
seconds at most. No polling interval.

**Failure modes.**

- The agent forgets to arm, or arms and then ends its turn after the waiter exited: the
  session sleeps through pings. This is the main weakness; it depends on agent discipline.
  The Stop hook (section 2) is what closes it.
- The waiter does not survive the session: a ccy restart (`--max-age`, worker restart,
  reboot), `/exit`, or a resumed session leaves no waiter. A SessionStart reminder or the
  Stop check covers it.
- Two syncers for one account (a background `wait` and a Stop-hook `recv` at once) break the
  "one syncer per account" rule in issue section 5. The CLI needs an account lock (`flock`
  on a file in the member's config dir): whoever holds it syncs and writes the durable local
  inbox; `recv` without the lock reads the local inbox only. Then hooks never touch the
  network and stay fast.
- Monitor is stopped by the harness if it emits too much; `wait` should exit on the first
  ping (one notification), not stream.

**Injection exposure.** Whatever `wait` prints lands in the agent's context. It must print
only re-validated pings in the fixed one-line format (sender handle, verb, reference,
note), never raw event content; a ping failing validation is dropped and reported as a
count. The only free-form field left is the optional 80-character note in a restricted
character set; consider printing it only on `pingbus show`, so the wake line carries verb
and reference alone.

## 2. Claude Code hooks: Stop and UserPromptSubmit

**What they do.**

- **Stop** runs when the agent ends its turn. A command hook may block the stop and give a
  reason, and the agent continues with that reason. It is the right place to (a) catch a
  ping that arrived mid-turn and (b) refuse to go idle without a live waiter. It must honour
  `stop_hook_active` (or a per-session counter) so it cannot loop for ever.
- **UserPromptSubmit** adds context to a turn a human (or the supervisor) started: "N pings
  pending", plus their validated one-line forms.
- Neither wakes an idle session. Stop is the guard that makes option 1 dependable.

**Latency.** Stop: zero extra, at the end of every turn. UserPromptSubmit: the next prompt.

**How this project registers hooks.** `.claude/settings.json` registers one daemon wrapper
per event (`bash "$CLAUDE_PROJECT_DIR"/.claude/hooks/<event>`); the daemon dispatches to
built-in handlers (enabled in `.claude/hooks-daemon.yaml` `handlers:`) and project handlers
(listed under `plugins.plugins:` in the same file, code under `.claude/hooks/handlers/<event>/`,
here two `pre_tool_use` handlers). `hook_registration_checker` audits the **project's**
`settings.json` and `settings.local.json`, and requires every `type: command` hook there to
end in `/.claude/hooks/{event}`; a raw `pingbus` command hook in a project's settings would
be flagged as legacy.

**Where a handler for another project would ship from.** Not from fedora-desktop as a
project handler: project handlers live in, and are registered by, each consuming project's
own `.claude/hooks/handlers/` and `hooks-daemon.yaml`, so every team member's repository
would carry a copy (and repositories without the daemon get nothing). Three real choices:

| Route                                                                                                                                                                                          | Reaches                                                    | Cost                                                                                                                                                 |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| Upstream hooks-daemon library handler (Stop + UserPromptSubmit, off by default, enabled per project in `hooks-daemon.yaml`)                                                                    | Every daemon project, ccy or not                           | Cross-repo change on another release cadence; the daemon would own a check for a protocol fedora-desktop versions; each project still edits its yaml |
| **Claude Code plugin shipped in the ccy image** (`hooks/hooks.json` + the `pingbus` skill), installed and enabled by the entrypoint only when the team opt-in is set                           | Every ccy session that opts in, with or without the daemon | Lives with the CLI and protocol it checks; one ccy version bump                                                                                      |
| User-level hooks merged into the container's `settings.json` (the entrypoint already `jq`-merges `env` and `enabledPlugins` into `/root/.claude/settings.json`, a symlink into `.claude/ccy/`) | Same as the plugin                                         | Mutating per-project user settings on each start, and removing them on opt-out, is fiddly; the plugin route packages the same thing                  |

The ccy image already ships a Claude Code plugin (`/opt/claude-yolo/plugins/phpantom-lsp`,
copied into `/root/.claude/plugins/` and enabled through `enabledPlugins`) and installs image
skills on every start (`/opt/claude-yolo/skills`). So the plugin route fits an existing
pattern; it also gives section 8 members (not managed by ccy) a single installable artefact:
the CLI plus the plugin. **To verify in Task 2.4:** that a plugin installed this way (not
through a marketplace) has its `hooks/hooks.json` loaded, and that plugin hooks are not
flagged by `hook_registration_checker` (its documented scope is the two project settings
files only).

**Interaction with the daemon's own Stop handlers.** In daemon projects the daemon's Stop
chain (`R-STOP-NO-REASON`, `R-STOP-GOAL-LEDGER`, cron enforcers) also runs. Claude Code runs
all Stop hooks and combines the blocks. The pingbus Stop hook must give a short,
fixed-template reason ("2 pings pending: run `pingbus recv`", "no waiter armed: run
`pingbus wait` in the background") so the two do not produce a confusing loop.

**Failure modes.** The hook must be fast and offline: read the local inbox and the waiter
status file only, never sync (see the lock in section 1). A missing or broken CLI must fail
loudly in the hook output, not silently allow the stop (fail-fast rule), but must not block
every stop for ever: block once, then report. A team-opted-in session whose credential is
missing should say so at SessionStart.

**Injection exposure.** The reason and context are text the agent reads. Use fixed templates
plus counts and validated one-line pings only; never event bodies.

## 3. The ccy supervisor seam (claude-supervise.py, plugin API, ccy_lifecycle)

**What it is.** Since issue #31, ccy execs the hooks daemon's PTY supervisor in place of
`claude` (on by default when a project has `.claude/ccy/claude-supervise.py`; armed via
`ccy.env`). It types into the session's terminal at an "injection choke point": idle, input
box empty, after compact/continue. It polls every 2 seconds (`_DEFAULT_POLL_SECONDS = 2.0`,
idle floor 2 s). This is the only mechanism that wakes a truly idle session with no action by
the agent.

**Can a plugin inject a prompt?** Not free text, by design. The plugin API (hooks daemon Plan
00487, drafted in this repo's Plan 00146) lets a worker plugin's `on_idle(tick)` return
`Notify(<template constant>, <closed-set values>)` or `ExitForRestart(...)`. "A general type
this text action" is an explicit non-goal: "Only fixed supervisor templates reach the chat."
`ccy_lifecycle.py` shows the pattern: `on_idle` returns
`self._api.Notify(self._api.RESTART_SOON, minutes)` or
`self._api.Notify(self._api.DEADLINE_REACHED)`, and the supervisor renders the
provenance-marked text ("machine-generated, NOT a human instruction"). The plugin ships in the
image under `/opt/claude-yolo/supervisor-plugins/`, root-owned, outside the project mount, and
the entrypoint adds `--plugin name=path` only when the feature is configured, after checking
the supervisor's `_PLUGIN_API_MAJOR` matches the plugin's `PLUGIN_API`.

**What a pingbus wake would need.**

- A new template in the supervisor (for example `PINGS_PENDING` with an integer count),
  i.e. an upstream hooks-daemon change and possibly an API minor bump. The typed text would
  be "N pings pending: run `pingbus recv`", which is the ideal shape for issue #59: zero
  ping content reaches the chat, the agent fetches and re-validates itself.
- A `pingbus_wake.py` plugin in the image beside `ccy_lifecycle.py`, added to the wrapper by
  the entrypoint only when the team opt-in is set, that reads the **local** inbox count in
  `on_idle`. Hooks run on a short-budget thread and must not do network I/O, so a syncer
  must exist to fill the inbox: either a long-lived `pingbus sync` started by the entrypoint
  beside `claude`, or the agent's background `wait`. It must remember what it already
  announced, so it notifies once per new batch.

**Latency.** Ping to inbox (syncer) plus up to about 2 to 4 seconds of supervisor tick and
idle floor. Never mid-turn and never into a box a human is typing in.

**Failure modes and limits.**

- Not available yet here: the deployed `.claude/ccy/claude-supervise.py` has no plugin API
  (no `_PLUGIN_API_MAJOR`; the local daemon clone is at v3.68.0, and Plan 00487 says the API
  merges after it). ccy already refuses `--max-age` with an older supervisor, and a pingbus
  plugin would need the same check.
- Only ccy sessions with the supervisor (not `--no-supervise`, not section 8 members).
- Injection caps: the supervisor caps each notice family per process; a busy bus could
  exhaust it, so the plugin must coalesce (one notice per idle period, count only).
- A plugin that fails is disabled with one notice; the session continues without wakes, so
  the Stop-hook check still matters as the second line.

**Injection exposure.** None from ping content: fixed template and an integer.

## 4. The dormant file mailbox (`CLAUDE/AgentMailbox.md`)

**What it is.** Markdown files under `untracked/agent-mailbox/` in one shared checkout,
between a ccy agent and a desktop `cc` agent. `scripts/agent-mailbox-watch.bash` polls every
5 seconds, exits 0 with the waiting path when there is work, exits 3 after
`WATCH_MAX_SECONDS` (default six hours) to be re-armed. Woken by the background-command
exit (option 1).

**Latency.** Up to 5 seconds.

**Failure modes.** Needs a shared filesystem, so it works only for agents in the same
checkout; it is not a transport between containers of different repositories. Same
"forgot to re-arm" weakness as option 1, mitigated only by written discipline.

**Injection exposure.** High: messages are free-form markdown the other agent acts on. It is
acceptable there because both ends are agents of one owner in one checkout, with a
whitelist of what may be run. It must not be reused as the bus transport. What it does prove
is the wait-exit-re-arm loop, so `pingbus wait` should copy its contract: exit 0 with the
item, a distinct "nothing, re-arm" code, usage exit 64.

## 5. Session crons (noted for completeness)

A session cron (`CronCreate`; the hooks daemon already declares persistent crons) fires a
fixed prompt into an idle session on a schedule. A cron that says "run `pingbus recv`"
carries no ping content, but latency is the cron interval, and every tick costs a model turn
even when the inbox is empty (the daemon already backs its failsafe cron off for that
reason). Not recommended as a wake path; acceptable as a last-resort sweep for orchestrators.

## Comparison

| Mechanism                 | Wakes an idle session   | Latency             | Needs agent discipline | Ping content reaching chat       | Availability                                                   |
| ------------------------- | ----------------------- | ------------------- | ---------------------- | -------------------------------- | -------------------------------------------------------------- |
| Background `pingbus wait` | Yes, when armed         | Seconds             | Yes (arm, re-arm)      | Validated one-line pings         | Any Claude Code session                                        |
| Monitor + `wait`          | Yes, when armed         | Seconds             | Yes; 30-minute cap     | Same                             | Any                                                            |
| Stop hook                 | No; stops it going idle | End of turn         | No                     | Fixed template + validated lines | Any, via plugin                                                |
| UserPromptSubmit hook     | No                      | Next prompt         | No                     | Same                             | Any, via plugin                                                |
| Supervisor plugin         | Yes, always             | 2 to 4 s after sync | No                     | Fixed template + count           | ccy with a plugin-API supervisor only; needs upstream template |
| File mailbox              | Yes, when armed         | 5 s                 | Yes                    | Free text                        | Shared checkout only; unsuitable                               |
| Session cron              | Yes                     | Cron interval       | No                     | Fixed prompt                     | Any; costly                                                    |

## Recommendation

**Primary: background `pingbus wait`, made dependable by a Stop hook, both shipped as a
Claude Code plugin in the ccy image.**

- `pingbus wait --timeout` (default under 30 minutes so it also works under Monitor),
  exiting on the first ping with the validated one-line format and a distinct "timed out,
  re-arm" code; an account lock so only one process syncs, and a durable local inbox that
  `recv` and the hooks read offline.
- A Stop hook that blocks once, with a fixed-template reason, when the local inbox has
  pending pings or no live waiter is recorded; a UserPromptSubmit hook that lists pending
  pings; both honour `stop_hook_active`.
- Delivery: a ccy-image Claude Code plugin (hooks plus the `pingbus` skill) installed and
  enabled by the entrypoint only for a team opt-in, following the phpantom-lsp and image
  skills pattern. It works with or without the hooks daemon, ships with the CLI it calls,
  and is the same artefact section 8 members install. Not a fedora-desktop project handler
  (does not reach other repositories), and not an upstream daemon handler for the first
  version (wrong owner of the protocol, slower cadence).

**Fallback, and later upgrade: a supervisor plugin.** When the hooks daemon releases the
plugin API, add `pingbus_wake.py` beside `ccy_lifecycle.py` and request one upstream
template (`PINGS_PENDING`, count only). It wakes an idle ccy session with no agent action
and no ping content in the chat, which covers the main weakness of the primary path (a
waiter that was never armed or did not survive a restart). It needs a syncer outside the
agent (`pingbus sync` started by the entrypoint), and remains ccy-only. Raise the template
request on the daemon's tracker as part of Task 1.2's design review rather than block the
first version on it.

**Do not use** the file mailbox as transport, or session crons as the wake path.

## Corrections for the plan and issue

- Issue #59 section 5's "30 minutes by default" is the Monitor tool's cap; background Bash
  has no stated limit. Keep the short default timeout anyway (it bounds orphaned waiters
  and serves Monitor).
- Issue section 6's "Integrate with the ccy supervisor seam (#31)" depends on the plugin API
  and one new fixed template upstream; the deployed supervisor here predates the API.
- Task 2.4 should list: the plugin-install verification above, the account lock, and the
  SessionStart check for a missing credential.
