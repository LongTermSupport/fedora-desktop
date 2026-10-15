# Plan 00202 research: a ccy mod for Claude Code

Research behind [PLAN.md](PLAN.md). Section 1 maps what ccy puts into a session today.
Section 2 covers Claude Code's extension points, centred on **mods**. Section 3 is the
feature brainstorm, and section 4 the architecture. Sources are cited inline. The docs are
read at Claude Code v2.1.301, which is the build in the ccy image as this is written.

## 1. What ccy puts into a session today

Full trace: [subagent-reports/261016-explore-unrecorded.md](subagent-reports/261016-explore-unrecorded.md).

### 1.1 How each piece arrives

| Piece                                                                    | How it reaches a session                                                                                                                                                      | Versioned by                                             | Notes                                                                                                                         |
| ------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| Launcher `claude-yolo` and its `lib/`                                    | Deployed to the host by `play-claude-yolo.yml`. Never in the image                                                                                                            | `CCY_VERSION` (3.98.11), self-hash                       |                                                                                                                               |
| Image (Dockerfile, `entrypoint.sh`, Claude Code)                         | **Baked in**. Claude Code is self-updated in place at most once a day                                                                                                         | `REQUIRED_CONTAINER_VERSION` = Dockerfile `LABEL` (2.66) | The runtime gate hashes the **Dockerfile alone**                                                                              |
| `files/opt/claude-yolo/` (docs, `skills/browsing`, `optional/agent-bus`) | **Baked in** through the build context                                                                                                                                        | Container version, bumped by hand                        | **Not covered by the pre-commit COPY-source gate**, which resolves sources under `files/var/local/claude-yolo/` only. See 1.3 |
| pingbus zipapp                                                           | Built at deploy time from `helpers/pingbus`, then **baked in**                                                                                                                | By hand                                                  | Not a repo file, so no gate sees it                                                                                           |
| Container user `settings.json`                                           | **Written at launch** by an `entrypoint.sh` heredoc or `jq` merge. `/root/.claude` is a symlink into the project's `.claude/ccy/`                                             | —                                                        | Adds `ENABLE_LSP_TOOL` and `enabledPlugins.phpantom-lsp`                                                                      |
| phpantom LSP plugin                                                      | **Copied at launch**, once, into `~/.claude/plugins/phpantom-lsp`                                                                                                             | `plugin.json` 1.0.0                                      | The enable key has no `@marketplace`. Hand-wired                                                                              |
| Browsing skill                                                           | **Copied at launch**, on every start                                                                                                                                          | —                                                        |                                                                                                                               |
| pingbus plugin                                                           | **`--plugin-dir` plus `--settings`** added to the `claude` argv by the entrypoint, only when the host passes `PINGBUS_SEATS`                                                  | `plugin.json` 1.2.3, bumped by hand, format-checked only | Id `pingbus@inline`                                                                                                           |
| pingbus status line                                                      | The kit `settings.json` `statusLine: pingbus status-line`, which chains the checkout's own status line first                                                                  | —                                                        | Takes the single `statusLine` slot through `--settings`                                                                       |
| pingbus inject frame                                                     | `helpers/pingbus/hooks.py` `TEMPLATES["inject_frame"]`, emitted as SessionStart context                                                                                       | Inside the zipapp                                        | Not in the plugin files                                                                                                       |
| Project settings and hooks                                               | Through the `/workspace` mount: `.claude/settings.json` points every hook event at `.claude/hooks/*` (the hooks-daemon), the `statusLine` at the daemon, and `enabledPlugins` | hooks-daemon `expected_version`                          | Same file for cc on the host                                                                                                  |
| ccy supervisor                                                           | `.claude/ccy/claude-supervise.py`, tracked and refreshed by the daemon, wrapped by the entrypoint when `ccy.env` arms it. `ccy_lifecycle.py` is **baked into** the image      | Daemon version; plugin API major                         |                                                                                                                               |

### 1.2 cc on the host

`files/var/local/claude-code/cc` sources ccy's libraries and runs `claude update` on every
launch. It has no image, no entrypoint, no supervisor and no version of its own. `cc --teams`
(`cc-teams.bash`) builds the same `--plugin-dir` and `--settings` arguments against a
**second copy of the kit** that `agent-bus-install` puts at `/usr/local/share/agent-bus/kit`.
It merges deny rules in with `jq` and adds `--append-system-prompt`.

### 1.3 Duplication and hand-wiring

- **The kit is installed twice**, into the image and onto the host. Its file list is
  spelled out **four times**: the playbook, `agent-bus-install`, `src-go/qa/deployeddrift`
  and the contract test. Its "required files" check is spelled out three times.
- **Three launchers** wire the pingbus plugin by hand: the entrypoint, `cc-teams.bash` and
  `agent-bus-claude`.
- **Two plugins, two mechanisms:** phpantom is copied and enabled with `jq`, while pingbus
  uses `--plugin-dir`. Neither uses a marketplace.
- **The version gate has a gap:** a change under `files/opt/claude-yolo/`, or to
  `helpers/pingbus`, needs no container bump at commit time, and the runtime check does
  not notice it. A mod placed under `files/opt/claude-yolo/` would inherit this gap.
  Closing it is task 1.3 of PLAN.md, and overlaps Plan 00093.

### 1.4 What the pingbus plugin covers, and what it does not

**It covers:**

- settings-style hooks for SessionStart (seat, inject frame), UserPromptSubmit and Stop
  (the pending count), PostToolUse (relay mode) and SessionEnd;
- the pingbus skill;
- through the kit, the status line and `crossSessionInbound: accept`.

**It does not cover:**

- any UI beyond the one status line;
- commands that run without a turn, since every action is the model running `pingbus`;
- a roster or room view;
- container or ccy awareness, doctor, deploy or plan helpers;
- a place to grow non-team features.

**Live delivery is already in-process.** It is not in the plugin. With
`PINGBUS_DELIVERY` unset, which is the default, `pingbus watch` writes each item **into
the session's Claude Code inbox socket** (`CLAUDE_CODE_MESSAGING_SOCKET`, authenticated
with the session token). The kit's `crossSessionInbound: accept` exists for exactly this.
Supervisor typing (Plan 00194) is the `relay` mode, which the launch sets explicitly.
Sources: `docs/agent-bus-protocol.md` around lines 641 to 680, and
`helpers/pingbus/notify.py`. Deliveries therefore already arrive as cross-session peer
messages, with a Claude Code `origin` and `from`.

The docs say "a session inside a container and a session on the host can't reach each
other" ([cross-session messaging](https://code.claude.com/docs/en/cross-session-messaging)).
That does not bite here: the watcher runs inside the same container and posts to the
local socket. It does mean host cc and a ccy session never reach each other through this
channel.

## 2. Claude Code's extension points, centred on mods

### 2.1 What a mod is

A mod is a plugin that holds a **hooks module**: a JavaScript or TypeScript file of event
handlers that Claude Code calls **inside its own process**
([Mods overview](https://code.claude.com/docs/en/plugins/mods/overview)). A small mod has
three files:

- `.claude-plugin/plugin.json`, the ordinary plugin manifest. Mods add no required fields.
- `hooks/hooks.json` with `"modules": ["./register.ts"]`, one path. The same file can still
  hold settings-style `hooks` beside it.
- `hooks/register.ts`, which exports `register(on, options)`.

A mod may also have `types/index.d.ts`, named in the manifest by `types`, when it keeps
`$.state` values or adds a namespace to the API. Test files end in `.test.ts`
([Mods reference: Files](https://code.claude.com/docs/en/plugins/mods/reference#files)).

Every hook has the shape `($, e, next)`, a middleware chain:

- `$` is the mods API.
- `e` is the event, deeply frozen.
- `next(e)` runs the mods after this one, then Claude Code's own behaviour.

A hook observes (it returns `next(e)`), rewrites (it calls `next({...e, x})`), or answers
(it returns without calling `next`). The module has no Node and no DOM. Everything outside
it goes through `$`, which is why `claude plugin validate` can list what a mod calls before
it runs ([Events](https://code.claude.com/docs/en/plugins/mods/events)).

**Version and status.** Mods are on by default from v2.1.287 in the terminal. The old
`CLAUDE_CODE_ENABLE_FUNCTION_HOOKS` switch is ignored. The overview tells you to remove it
if you set it "during early access", so **early access is over**. The live risk is
different: the API is **version-gated and still growing**. The reference marks fields as
needing v2.1.289, .290, .292, .299 and .300, and says the GitHub copy of the types "can be
older than the Claude Code version you have installed… trust the copy Claude Code writes
for your version"
([Reference](https://code.claude.com/docs/en/plugins/mods/reference)). Claude Code writes
that copy at `<mod>/.claude-plugin/types/` on load. It has about 22,000 lines at v2.1.301.

### 2.2 How a mod differs from a plugin

A mod **is** a plugin. Everything a plugin has (skills, commands, agents, settings-style
`hooks.json` hooks, MCP and LSP servers, monitors) can sit in the same folder. What the
hooks module adds is code that runs in-process:

|                            | Mod (hooks module)                                                                                           | Settings-style hook (`hooks.json` / settings)                | Skill                | MCP server              |
| -------------------------- | ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------ | -------------------- | ----------------------- |
| Runs                       | In Claude Code's process, as functions                                                                       | A shell command, HTTP call, prompt or agent per event        | Text the model reads | An external process     |
| Can draw UI                | **Yes**                                                                                                      | No                                                           | No                   | No                      |
| Can change                 | Tool calls, prompts, commands, turns, system prompt sections, every transcript row, what the interface draws | Allow or block, a tool's arguments and result, added context | What Claude knows    | Which tools Claude has  |
| Keeps state between events | Yes: module variables, `$.state`, and `$.store` across sessions                                              | No: each run is a new process                                | No                   | Yes, in its own process |

Source: [Mods overview: Compare](https://code.claude.com/docs/en/plugins/mods/overview#compare-mods-settings-hooks-skills-and-mcp-servers).

### 2.3 UX a mod can extend that nothing else can

These are from the [Interface](https://code.claude.com/docs/en/plugins/mods/interface)
page, the [API](https://code.claude.com/docs/en/plugins/mods/api) page, and the
render-sites table in the [Reference](https://code.claude.com/docs/en/plugins/mods/reference#render-sites).

- **A pane** with `$.ui.open({ id, title })`. It docks as a sidebar in fullscreen, or
  inline above the prompt. It holds tabs, buttons, inputs and selects. A pane opened
  without the user asking is only placed from 144 columns (110 once the user has opened
  it).
- **A band above the prompt**, the `AbovePrompt` site, such as a team roster or an
  "update needed" strip.
- **A status line entry per mod** with `$.ui.status(text)`. It is one line under the
  prompt, which Claude Code prefixes with `⚠ <mod-name>:`. **It is not the settings
  `statusLine`.** That one stays a command in settings and only one can be set. The mod's
  line sits beside it, so a mod adds a line without taking the slot.
- **Toasts** (`$.ui.toast`), **dim transcript lines the model does not read**
  (`$.ui.log`), and **native notifications** (`$.ui.notify`).
- **Redraw of Claude Code's own sites:** the spinner, tool-call rows, user and assistant
  messages, `CommandOutput`, `AskUserQuestion`, `PromptHint`, `SessionMode`, `InfoNotice`
  and `TurnDuration`. **The permission prompt cannot be redrawn**
  ([Admin](https://code.claude.com/docs/en/plugins/mods/admin#know-which-controls-still-apply)).
- **`UserMessage` rows know their origin.** `e.props.origin.kind` tells a typed prompt
  from a task notification, or from another agent, session or channel. `e.props.from.name`
  names the sender. A teammate's message can therefore be drawn as a distinct row
  (bundled reference, "Drawing: ui.render").
- **Slash commands that run code with no Claude turn,** `$.command.register` plus
  `command.run`. With `immediate: true` a command runs even while Claude works. Its
  `{ text }` reply is printed and Claude reads it. `{}` prints nothing.
- **A tool the model calls** with no MCP server: `$.tool.register`, served by `tool.call`.
  It is listed as `mcp__<plugin>__<name>`.
- **Start a turn in this session when idle:** `$.prompt.submit({ text })` queues a turn
  once the session is idle. It does not wake another session. Claude reads the text after a sentence that names the mod. `asUser: true`
  drops that sentence.
- **Append rows Claude reads without starting a turn:** `$.session.append` adds a meta
  user row, which the model reads at the loop's next step, or a notice the model never
  reads.
- **Observe and filter deliveries:** `session.receive` sees each inbound message before
  Claude reads it, and may consume it with `{ consumed }`. The API page lists the origin
  kinds `peer`, `peer-send-message`, `task-notification` and `scheduled-trigger`. The
  v2.1.301 types add `coordinator`. `session.send` sees outbound messages. The API page:
  "The sender's name on a received message is whatever the sender wrote, so don't base a
  decision on it."
  ([API: Send and receive](https://code.claude.com/docs/en/plugins/mods/api#send-and-receive-messages-between-sessions)).
- **Timers** with `$.clock.every` and `$.clock.after`, started in `session.start`. They run
  between turns and do not start one.
- **Ask the user** with `$.ui.ask`, using Claude's own question dialog.
- **Read the session:** `$.session.usage()` gives context use and plan limits,
  `$.agent.list()` lists subagents and teammates with their status, and
  `$.session.messages()` returns the transcript.
- **System prompt sections** (`prompt.compose`, `prompt.section`), the first message's
  context, reminders (`prompt.attachment`) and skill text (`skill.prompt`).

### 2.4 Packaging, install, versions and updates, offline and in a container

- **Install** a mod as a plugin, from a marketplace: `claude plugin install <name>@<mkt>`
  ([Overview: Install](https://code.claude.com/docs/en/plugins/mods/overview#install-or-update-a-mod)).
- **For one session:** `claude --plugin-dir <dir>` (repeatable), or
  `CLAUDE_CODE_PLUGIN_DIRS` taken from the environment or from the `env` block of
  `~/.claude/settings.json`. The id is `<name>@inline`, and the folder is watched and
  hot-reloaded in an interactive session
  ([Loading](https://code.claude.com/docs/en/plugins/loading#find-where-a-plugin-came-from)).
- **A directory marketplace loads in place.** A plugin listed by a relative path in a
  marketplace added from a local folder loads from that folder at every session start or
  `/reload-plugins`, "whatever its version string says". It needs no copy, no reinstall
  and no network ([Loading: In-place and copied](https://code.claude.com/docs/en/plugins/loading#in-place-and-copied-plugins)).
- **First session:** a marketplace that settings declare but `known_marketplaces.json`
  lacks is registered **in the background after the session starts**, and plugins then
  reload ([Loading](https://code.claude.com/docs/en/plugins/loading#plugins-and-marketplaces-that-arent-on-disk-at-session-start)).
  In ccy `~/.claude` is per project, so `known_marketplaces.json` is too. Every new
  project's first session could therefore start without the mod. The entrypoint must run
  `claude plugin marketplace add /opt/claude-yolo/mods` before `exec claude`. That step
  uses a directory source, needs no network, and is idempotent. A test must cover the
  first-session case.
- **Seed directory for images:** `CLAUDE_CODE_PLUGIN_SEED_DIR`. Install into it at build
  time with `CLAUDE_CODE_PLUGIN_CACHE_DIR=<seed>`. The seed is read-only, `autoUpdate` is
  forced off, and its plugins still need `enabledPlugins`
  ([Org: Seed containers and CI](https://code.claude.com/docs/en/plugins/org#seed-containers-and-ci)).
- **Versions:** for a copied plugin, the manifest `version` wins. Then the marketplace
  entry's version applies, then a git SHA. With an unchanged version string an update is
  a no-op. A plugin loaded in place ignores the version for loading
  ([Loading: Versions](https://code.claude.com/docs/en/plugins/loading#versions-and-updates)).
- **Offline:** loading from `installed_plugins.json` and the cache uses no network. Auto
  update is off by default for third-party marketplaces, and
  `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC` or `DISABLE_AUTOUPDATER` turn it off
  entirely.
- **Name conflicts:** a managed `enabledPlugins` id wins over everything. After that a
  `--plugin-dir` copy silently replaces a same-named installed plugin. Only one plugin per
  manifest name loads a hooks module
  ([Loading: Name conflicts](https://code.claude.com/docs/en/plugins/loading#name-conflicts)).
- **An "organization" mod,** which runs first (`prependPlugins`, tier `prepend`), must be
  enabled by managed settings. It must also come from a directory marketplace named by
  absolute path in managed settings and be listed by a relative path. A copied plugin
  always counts as a user's mod
  ([Admin: Install your organization's mods](https://code.claude.com/docs/en/plugins/mods/admin#install-your-organizations-mods)).
  **The ccy image owns its own `/etc/claude-code/managed-settings.json`** (none exists
  today), so a ccy mod could be a managed, prepend-tier mod inside the container. See
  decision D4 in PLAN.md.
- **Hot reload while writing:** saves reload the module. A reload re-runs `register` and
  `session.start`, and drops module variables and timers. `$.state` and `$.store`
  survive.
- **Test:** `claude plugin test <dir>` runs `*.test.ts` against the engine with stubs. It
  needs no session, no sign-in and no network, and exits 1 on a failure, so it works in
  CI. `claude plugin validate [--strict] [--json]` lists the events a mod hooks and the
  API calls it makes ([Test](https://code.claude.com/docs/en/plugins/mods/test)).

### 2.5 Security model and limits

From the [Overview: Decide whether to trust a mod](https://code.claude.com/docs/en/plugins/mods/overview#decide-whether-to-trust-a-mod)
and the [Admin](https://code.claude.com/docs/en/plugins/mods/admin) page:

- A mod is **not sandboxed**. It runs as the user. Through the API it reads and writes
  files, runs programs (`$.process.run` takes an argv and uses no shell), fetches over
  the network, and reads the environment and settings. It sees every prompt and tool call,
  can rewrite them, can submit a prompt as the user, can approve a tool call before the
  user is asked (`tool.check`), and can spend usage through `$.model.*`.
- In a ccy container the session already runs with bypassed permissions. A mod therefore
  adds **no reach** beyond what the session has. It does add **in-process influence**:
  rewriting prompts, rows and tool results. That influence is the real new attack
  surface, and the design must keep it small (section 4.5).
- Without managed settings and without a Team or Enterprise sign-in, **no built-in
  guard loads** (`sec-default`), so nothing orders or polices user mods.
- **Limits** ([Reference: Limits](https://code.claude.com/docs/en/plugins/mods/reference#limits)):
  - A hook gets 10 s of its own CPU time, and a `.catch` handler 1 s. All `session.end`
    hooks share the SessionEnd budget, 1.5 s by default.
  - `$.process.run` has a 30 s default timeout, up to 10 min.
  - `$.store` holds 4 MiB, shared by every session on the machine.
  - `$.fs` reads or writes at most 4 MiB per file, and writes are not atomic.
  - Status and toast text is cut at 2,000 characters in the terminal.
  - A hook that throws is **skipped** unless its `.catch` answers, so guards must fail
    closed by hand.
  - If the hooks worker crashes three times, every non-built-in mod is off for the
    session.
- **What turns a mod off:** `--safe-mode`, `--bare` and `disableAllHooks` turn installed
  mods off. `disableAllHooks` also stops settings hooks and the custom status line.
- **Where mods run:** hooks run in `claude -p` and the Agent SDK, but **nothing draws
  there**. A mod must fall back to text where nothing draws.
- **Trust prompt first:** in an untrusted folder no mod loads until the trust prompt is
  answered.

### 2.6 The rest of the plugin surface, briefly

The `claude-code-guide` sub-agent's cited report is
[subagent-reports/261016-claude-code-guide-unrecorded.md](subagent-reports/261016-claude-code-guide-unrecorded.md).
That report marks the mod status line as UNVERIFIED. The
[API page](https://code.claude.com/docs/en/plugins/mods/api#show-something-without-starting-a-turn)
confirms it: `$.ui.status` is "one line under the prompt", per mod.
The short form:

- **Skills and commands:** `SKILL.md` with frontmatter. Commands and skills are merged.
- **Sub-agents:** `agents/*.md` files, or `$.agent.register`.
- **Settings-style hooks:** every event (`SessionStart` to `SessionEnd`). Each is also
  hookable from a mod as `classic.<Event>`.
- **MCP and LSP servers.**
- **Output styles.**
- **Monitors:** background processes declared in a plugin. They do not load for
  project-scope plugins.
- **`userConfig`:** fields the user sets in `/config`, which `register` receives as
  `options`.
- **The settings `statusLine`** is settings-only and single. A plugin cannot set it except
  through a settings file passed with `--settings`, which is how pingbus does it today.

## 3. Brainstorm: what a ccy mod could give a session

Each feature lists:

- **Tag:** **mod-native** needs the hooks module. **plugin/hooks** works with a skill or a
  settings-style hook. **not-a-mod** belongs elsewhere.
- **Value**, and **Effort** (S, M or L).
- **Risk:** sessions run with bypassed permissions, so the risk that matters is new
  influence over the session, or a new route into it.
- **Extension point** used.

### 3.1 Teams (absorbing pingbus)

| #   | Feature                                                                                                                                                                                                                                                                                                    | Tag          | Value                                                                        | Effort | Risk                                                                                                                                                                                                                                                                                                                                                                                                                                | Extension point                                                      |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------ | ---------------------------------------------------------------------------- | ------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------- |
| T1  | **Team roster band**: seat, team(s), coordinator, unread count, members' last-seen                                                                                                                                                                                                                         | mod-native   | See the team at a glance without running `pingbus`                           | M      | Low: read-only, from `pingbus status --json`                                                                                                                                                                                                                                                                                                                                                                                        | `AbovePrompt`, `$.clock.every`, `$.process.run`                      |
| T2  | **Delivery role: none, by default.** Deliveries already reach the model in-process through the inbox socket (§1.4). The mod must not add `$.prompt.submit` or `$.session.append` as a second route. The only open question is whether the mod needs any delivery role at all, and the default answer is no | mod-native   | None: the socket already does it                                             | —      | **High** if ever built: it would be a second injection route                                                                                                                                                                                                                                                                                                                                                                        | not used                                                             |
| T3  | **Team messages drawn distinctly**: a human's message, a peer's note and a ping each get their own row style                                                                                                                                                                                               | mod-native   | The trust model becomes visible to the owner reading the transcript          | S      | Medium, as a spoof surface: a peer can write text that *renders* as HUMAN. The style may key **only** on `e.props.origin.kind` and on fields pingbus itself verified (the watcher writes the envelope from the team record), never on `from.name` or on free text. Display only: it never changes what the model read                                                                                                               | `ui.render` on `UserMessage`                                         |
| T4  | **`/team` commands**: `/team status`, `/team say`, `/team ping`, `/team who`, run as code with no turn                                                                                                                                                                                                     | mod-native   | Fast and deterministic, with no model tokens                                 | M      | Medium. A `command.run` `{ text }` reply "prints in the transcript and Claude reads it", with no HUMAN/PEER envelope. So `/team status` and `/team who` **return `{}`** and show any peer-authored text (names, topics, last messages) only in the band, a pane or `$.ui.log`, which Claude does not read. Only operator-authored, fixed-format facts may go in `{ text }`. `say` posts as the seat, so it needs the CLI's refusals | `$.command.register`, `command.run`, `$.process.run(['pingbus', …])` |
| T5  | **Team pane**: a scrollable room view with a reply input                                                                                                                                                                                                                                                   | mod-native   | Read the room without leaving the session                                    | L      | Medium: an input posts to Matrix. It must run through `pingbus` and never call Matrix directly                                                                                                                                                                                                                                                                                                                                      | `Pane`, `Input`, `ui.input`                                          |
| T6  | **Shutdown coordination UX**, **blocked** on feat/teams-shutdown, which has no commits yet: a "team shutting down: save state" band, a `/team shutdown` that asks with `$.ui.ask`, and a hold on new work while draining                                                                                   | mod-native   | Clean team stop with a visible state                                         | M      | Medium: it must not block the session's own exit. `session.end` has 1.5 s in total                                                                                                                                                                                                                                                                                                                                                  | `AbovePrompt`, `$.ui.ask`, `session.end`                             |
| T7  | **pingbus status line** moved from the settings `statusLine` to `$.ui.status`                                                                                                                                                                                                                              | mod-native   | Frees the single `statusLine` slot, which the kit's `--settings` takes today | S      | Low                                                                                                                                                                                                                                                                                                                                                                                                                                 | `$.ui.status`                                                        |
| T8  | The pingbus **skill**                                                                                                                                                                                                                                                                                      | plugin/hooks | Already exists                                                               | —      | —                                                                                                                                                                                                                                                                                                                                                                                                                                   | `skills/`                                                            |

### 3.2 Container awareness

| #   | Feature                                                                                                                          | Tag                                   | Value                                                  | Effort | Risk                                                                                                                                                                                                                                    | Extension point                                    |
| --- | -------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------- | ------------------------------------------------------ | ------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------- |
| C1  | **Status entry**: `ccy <ver> · image <ver>`, network mode, and whether the session is in a team                                  | mod-native                            | Always know where you are                              | S      | Low                                                                                                                                                                                                                                     | `$.ui.status`, `$.fs.read` of an image-baked JSON  |
| C2  | **Update-needed band**: the host's ccy is newer than this container's image. Relaunch with `ccy` (or `ccy --restart`)            | mod-native                            | Stops stale-image sessions                             | S      | Low, but **needs a launcher change first**. No such host-written file or mount exists, and the launcher never passes `CCY_VERSION` into the container. That means a launcher change, a `CCY_VERSION` bump and a new read-only mount. M2 | `AbovePrompt`                                      |
| C3  | **`/ccy mounts`**: what is mounted, read-only or read-write, and what is deliberately absent (ssh keys, vault)                   | mod-native                            | Answers "why can't I see X" without a turn             | S      | Low: it lists paths and never prints contents                                                                                                                                                                                           | `command.run`, `$.fs.read('/proc/self/mountinfo')` |
| C4  | **Relaunch guidance**: when a tool needs something the container lacks, the command prints the exact `ccy` flag to relaunch with | mod-native                            | Fewer dead ends                                        | M      | Low                                                                                                                                                                                                                                     | `command.run`                                      |
| C5  | **System-prompt section** saying "you are in ccy, here is what you can and cannot do"                                            | mod-native (or a `SessionStart` hook) | The model stops asking for host actions it cannot take | S      | Low, but it costs cache and context. Keep it short and stable                                                                                                                                                                           | `prompt.compose`                                   |

### 3.3 Host bridge requests

| #   | Feature                                                                                                                              | Tag           | Value                                                    | Effort | Risk                                                                                                                                                                                                                                | Extension point |
| --- | ------------------------------------------------------------------------------------------------------------------------------------ | ------------- | -------------------------------------------------------- | ------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------- |
| H1  | **`/ccy deploy-needed`**: shows `CLAUDE/Plan/meta-deploy.list` and the one owner instruction, "run `./CLAUDE/Plan/meta-deploy.bash`" | mod-native    | One consistent hand-over, as the CLAUDE.md rule requires | S      | Low: read-only                                                                                                                                                                                                                      | `command.run`   |
| H2  | **Deploy-pending band** while the queue is not empty                                                                                 | mod-native    | The owner sees pending host runs                         | S      | Low                                                                                                                                                                                                                                 | `AbovePrompt`   |
| H3  | A mod that **runs anything on the host**                                                                                             | **not-a-mod** | —                                                        | —      | **Prohibited**: the container must never gain a host-execution route. The host runs `meta-deploy.bash` itself. A host-side request daemon (Plan 00197's seat broker pattern) is the only acceptable shape, and it lives on the host | —               |

### 3.4 Plan workflow helpers

| #   | Feature                                                                                        | Tag           | Value                         | Effort | Risk                                                                                                                              | Extension point |
| --- | ---------------------------------------------------------------------------------------------- | ------------- | ----------------------------- | ------ | --------------------------------------------------------------------------------------------------------------------------------- | --------------- |
| P1  | **Active plan in the status entry**: the goal-ledger plan and its status                       | mod-native    | Context at a glance           | S      | Low                                                                                                                               | `$.ui.status`   |
| P2  | **`/plan` pane**: the Active Plans list, the selected plan's tasks and its next unchecked task | mod-native    | Navigate plans without tokens | M      | Low: read-only                                                                                                                    | `Pane`, `$.fs`  |
| P3  | Plan QA, number allocation and journal stamping                                                | **not-a-mod** | —                             | —      | These belong to the hooks-daemon and `mkplan.bash`, which already enforce them. Duplicating them in a mod would split enforcement | —               |

### 3.5 Session lifecycle

| #   | Feature                                                                                                                                    | Tag           | Value                                             | Effort | Risk                                                                              | Extension point                                        |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------ | ------------- | ------------------------------------------------- | ------ | --------------------------------------------------------------------------------- | ------------------------------------------------------ |
| L1  | **`/handover`**: writes a structured handover (branch, tip, open plan tasks, uncommitted state) to a tracked plan file, or to `untracked/` | mod-native    | Clean restarts                                    | M      | Low: writes only under the repo                                                   | `command.run`, `$.fs.write`, `$.model.fork` (optional) |
| L2  | **Resume card** after a restart (Plan 00135, ccy-sessions survive a reboot): the band shows "resumed after reboot: last task …"            | mod-native    | Orientation after a reboot                        | S      | Low                                                                               | `AbovePrompt`, `session.start`                         |
| L3  | **Context and usage meter**: context percent and 5-hour and weekly plan usage                                                              | mod-native    | Replaces scraping rate-limit headers (Plan 00101) | S      | Low                                                                               | `$.session.usage()`, `$.ui.status`                     |
| L4  | The restart and relaunch mechanics themselves                                                                                              | **not-a-mod** | —                                                 | —      | The launcher and supervisor own them (`ccy_lifecycle.py`, `restart-request.bash`) | —                                                      |

### 3.6 Secrets hygiene guards

| #   | Feature                                                                                                  | Tag           | Value                                  | Effort | Risk                                                                                                                                                            | Extension point |
| --- | -------------------------------------------------------------------------------------------------------- | ------------- | -------------------------------------- | ------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------- |
| S1  | **Install-specific name mask** on transcript display (show `container-A`)                                | mod-native    | Fewer leaks in screen recordings       | M      | Medium: display-only masking gives false comfort. It must not be presented as protection                                                                        | `ui.render`     |
| S2  | Blocking reads of vault or secret paths, and blocking commits of secrets                                 | **not-a-mod** | —                                      | —      | The hooks-daemon already enforces these (R-SECRET-\*, the pre-commit scanner). A second guard in a mod would be bypassable and drift. Leave them where they are | —               |
| S3  | **Outbound bus redaction check** before `/team say` posts: refuse text matching the gitignored term list | mod-native    | Stops leaking names into a Matrix room | S      | Low                                                                                                                                                             | inside T4       |

### 3.7 Status line segments

| #   | Feature                                                                                                              | Tag        | Notes                                                                                                                                                       |
| --- | -------------------------------------------------------------------------------------------------------------------- | ---------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| SL1 | One `$.ui.status` line for the mod, composed from segments: ccy/image, team, unread, deploy-pending, plan, context % | mod-native | `$.ui.status` gives one line per mod, so the mod composes the segments itself. It is prefixed `⚠ ccy:`, which may look alarming. **Verify the glyph** in M1 |
| SL2 | Leave the settings `statusLine` to the hooks-daemon, whose own status line exists                                    | not-a-mod  | Avoids a fight over the single slot                                                                                                                         |

### 3.8 Diagnostics

| #   | Feature                                                                                                                                                                                                                                                                                                                      | Tag        | Value                                         | Effort | Risk                                                   | Extension point                        |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- | --------------------------------------------- | ------ | ------------------------------------------------------ | -------------------------------------- |
| D1  | **`/ccy doctor`**: versions (ccy, image, Claude Code, mod), the mounts, pingbus seat and watcher health, whether the inbox socket is bound and the session token is exported (never its value), hooks-daemon health, supervisor presence, whether mods loaded, the network mode. Each check prints PASS or FAIL with the fix | mod-native | One command answers "is this session healthy" | M      | Low: read-only. It must print no secrets or env values | `command.run`, `$.process.run`, `$.fs` |
| D2  | **Doctor pane** with re-run buttons                                                                                                                                                                                                                                                                                          | mod-native | Nicer than D1                                 | M      | Low                                                    | `Pane`                                 |

### 3.9 cc on the host compared with ccy in a container

| Aspect                 | ccy (container)       | cc (host)                                                                         |
| ---------------------- | --------------------- | --------------------------------------------------------------------------------- |
| Permission mode        | bypass                | normal prompts                                                                    |
| Supervisor             | yes (`ccy_lifecycle`) | no                                                                                |
| Mod reach              | the container only    | **the owner's whole user account**                                                |
| Features that apply    | all                   | teams (T1 to T7), plans (P1, P2), doctor (part), usage (L3)                       |
| Features to switch off | —                     | C1 to C4 (container awareness), the deploy band (the host *is* where deploys run) |

The mod must detect where it runs and load only the matching modules. It should use an
**image-baked marker that the Dockerfile writes**, not `/run/.containerenv`, which
podman writes and docker does not. On the host it runs as the owner, so it must
add no capability there that ccy does not already need. Decision D6.

### 3.10 What should NOT go in a mod

- **Enforcement** (fail-fast guards, secrets, plan QA, destructive-git blocks) stays in
  the **hooks-daemon**, which already owns it, has tests and runs as settings hooks. Mod
  guards fail open on a throw unless written with `.catch`, and they are skipped under
  `--safe-mode`.
- **Image contents and tool installs** stay in the **Dockerfile and Ansible**.
- **Launch-time decisions** stay in the **launcher and entrypoint**: seats, team
  membership, network mode, mounts. A mod must never choose a seat or team; D62 in Plan
  00161 already refuses that from session-writable files.
- **Process supervision and restart** stay in the **ccy supervisor**.
- **Anything run on the host** stays with the **host and `meta-deploy.bash`**.
- **The pingbus CLI and protocol** stay in **pingbus**. The mod only calls it.

## 4. Architecture

### 4.1 Shape: one mod with modules, beside a small local marketplace

**Recommended: one plugin, `ccy`, holding one hooks module.** The module imports one
feature file per area: `core` (status, doctor, container), `teams`, `plans` and
`lifecycle`. The reasons:

- Only one hooks module per plugin is allowed, and `$.ui.status` gives **one line per
  mod**. Several mods would mean several `⚠` lines and several `session.start` hooks
  racing for the band.
- Modules share state (team unread count, deploy-pending, plan) for one status line and
  one band.
- Each feature is switched by `userConfig` options (`teams`, `plans`, `container`),
  which shows as `/config` rows.

The **pingbus skill and its settings-style hooks** move into the same plugin in a later
milestone (M3), so the one name `ccy` holds them all. Until then `pingbus` stays its own
plugin, so running teams keep their contract (section 4.6).

The alternative was a marketplace of `ccy-core`, `ccy-teams` and `ccy-plans`. It is
rejected: it brings several status lines, ordering through `dependencies`, and three
version tracks, for no isolation gain, since all three run in one process anyway.

### 4.2 Delivery: one source, a directory marketplace, loaded in place

- **Source:** `files/opt/claude-yolo/mods/` holds a marketplace:
  `.claude-plugin/marketplace.json` lists `ccy` at `./ccy`. That is one tree in the
  repo.

- **Into the image:** the Dockerfile copies `/opt/claude-yolo/` already. The entrypoint
  registers the directory marketplace and enables `ccy@ccy-local`, through either:

  - (a) `extraKnownMarketplaces` and `enabledPlugins` written into
    `/root/.claude/settings.json` by the entrypoint's existing `jq` merge (the phpantom
    precedent); or
  - (b) image-owned `/etc/claude-code/managed-settings.json`, which also makes the mod
    "organization" tier, so it runs first and cannot be shadowed by a `--plugin-dir`
    copy.

  Both load **in place, with no network and no copy**. Either way, the entrypoint also
  runs `claude plugin marketplace add /opt/claude-yolo/mods` before `exec claude`, so the
  first session in a new project loads the mod (§2.4). Decision D4.

  **The integrity trade-off:** the container settings file is session-writable, because
  `/root/.claude` is a link to `/workspace/.claude/ccy`. Under (a) the session controls
  whether its own mod loads, which gives no integrity guarantee. Under (b) the mod's load
  is outside the session's control, since the image layer owns the managed file. Option
  (b) also loads the built-in `sec-default` guard, which is harmless for a mod that never
  approves tool calls. Recommendation: (a) for M1, and move to (b) by M2.

- **To cc on the host:** an Ansible task registers the same directory, deployed to
  `/opt/claude-yolo/mods/` on the host by the existing claude-yolo play, as a marketplace
  in the owner's `~/.claude/settings.json`. It is one tree with two consumers and no copy
  in git.

- **Not `--plugin-dir`:** the id becomes `@inline`, and every launcher would need the
  flag. Not the seed directory either: a seed suits copied, versioned caches, and the
  in-place directory marketplace is simpler.

- **Versioning:** `plugin.json` `version` follows SemVer and is bumped on every change.
  In-place loading ignores it, but `/plugin` and `/ccy doctor` show it. Because the mod
  ships in the image, a mod change **is** an image change: bump `REQUIRED_CONTAINER_VERSION`
  and the Dockerfile label, and add a ccy changelog line. A contract test asserts that
  the mod version appears in `docs/ccy-changelog.md` (section 4.4).

- **Close the gate first:** the gate resolves each Dockerfile COPY source only under
  `files/var/local/claude-yolo/`. The repo path and the build-context path differ per
  file, and the playbook is the only map between them, so "also try
  `files/opt/claude-yolo/`" alone is not enough. Gate 2 needs one of two things. One
  option is a second context root, `files/opt/claude-yolo/`, tried per source. The other
  is an **explicit list** of repo paths that require a container bump, with a test that
  checks the list against the playbook's copy tasks. Either way the gate must also
  cover `helpers/pingbus/**`, because the zipapp built from it is baked into the image.
  This lands in its own commit, first, before the mod exists.

- **One host copy, with one open question:** the host's `/opt/claude-yolo/mods` is the
  only copy on a ccy host. But `agent-bus-claude` members (LXC, VM, other hosts) get the
  kit from `agent-bus-install`, not from `/opt/claude-yolo`. If M3 moves the pingbus
  hooks and skill into `ccy`, those members need `ccy` too. Either the kit carries a
  second copy of the mod, or the `pingbus` plugin stays for them permanently. **Decide
  this before M3** (D11).

- **Claude Code version floor, with no pin:** the image does not pin Claude Code. It
  self-updates from npm at most once a day (`docs/ccy.md`, "Claude Code auto-update",
  and Plan 00169), and cc runs `claude update` on every launch. So the guard is at
  runtime: `session.start` checks `$.session.version()` against the floor the types were
  generated for, and degrades to a toast if it is older. `tsc` against new types catches
  API drift when the floor is bumped, not before a session runs.

### 4.3 Relationship to the hooks-daemon and the supervisor

| Concern                                                                           | Owner                                                                                                                                                |
| --------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| Enforcement: blocks, denials, fail-fast rules, secret guards, plan QA, stop gates | **hooks-daemon** (settings hooks). Unchanged                                                                                                         |
| The settings `statusLine` slot                                                    | **hooks-daemon** (or the user)                                                                                                                       |
| UI: band, pane, `/ccy` and `/team` commands, the mod's status entry               | **ccy mod**                                                                                                                                          |
| Team protocol and CLI                                                             | **pingbus**; the mod calls `pingbus … --json`                                                                                                        |
| Live delivery into the session                                                    | **pingbus watch** writes to the inbox socket by default. The supervisor types it only in `relay` mode (Plan 00194). **The mod has no delivery role** |
| Restart and relaunch                                                              | **supervisor and launcher**                                                                                                                          |

The rules that keep them apart:

- **The mod never denies a tool call.** It has no `tool.call` or `tool.check` guards.
  `claude plugin validate` must show neither event, and a contract test asserts it. This
  keeps one enforcement owner.
- **The mod never rewrites prompts or transcript rows.** It has no `prompt.submit`
  rewrite and no `session.append` hook. Display-only `ui.render` changes are allowed.
- **The mod never touches deliveries.** It has no `session.receive` hook (which could
  consume a delivery, or skip it silently when it throws) and no `session.send` hook.
- **The mod never sits in front of the daemon.** It has no `classic.*` hooks, which
  would run ahead of the hooks-daemon's settings hooks.
- **The mod owns only `/ccy` and `/team` command names.** A test checks that they are
  not built-ins. `$.command.register` throws for a taken name, which skips the whole
  `session.start` hook, so commands are registered last or inside `try`.
- The hooks-daemon keeps the settings hooks. The mod may *read* hooks-daemon health
  (`bin/hooks-daemon status`) for `/ccy doctor`.

### 4.4 Testing

- **`claude plugin test files/opt/claude-yolo/mods/ccy`** runs the `*.test.ts` files beside
  the module. Each fires `session.start` and the commands, and mounts the band and pane
  on `terminal` and `desktop` with stubbed `$.process.run` and `$.fs`. It needs no
  network and no login. Wire it into `qa-all.bash` and `qa-changed.bash`, and add the
  `claude` binary as a QA dependency through IaC if the CI image lacks it.
- **`claude plugin validate --strict --json`** feeds a **contract test**
  (`tests/helpers/ccy_mod/test_mod_contract.py`, modelled on
  `tests/helpers/pingbus/test_plugin_contract.py`). `validate` prints `hooks:` and
  `calls:` as separate lines, and the test pins each separately:
  - **`hooks:`** must contain none of `tool.call`, `tool.check`, `prompt.submit`,
    `session.append`, `session.receive`, `session.send` or `classic.*`;
  - **`calls:`** must contain none of `$.http.fetch`, `$.env.set`, `$.settings.read`,
    `$.prompt.submit`, `$.session.append` or `$.session.send`, and is otherwise an
    exact allow-list;
  - `env reads:` is an exact allow-list;
  - the version is in the changelog;
  - no install-specific text appears.
- **Argv lint, not a pin:** `validate` shows `$.process.run`, not its arguments. A check
  that greps `register.ts` for literal argv arrays (`pingbus`, `git`,
  `bin/hooks-daemon`) is a lint on our own source, and a variable defeats it. Keep it,
  but the real controls are the root-owned source and enforcement staying outside the
  mod.
- **The mod's own tests** are `claude plugin test` fixtures, not pytest. The Python test
  only reads `validate --json`.
- **Types:** `tsc -p` against the per-build declarations is the API-drift check. It runs
  when the version floor is bumped (§4.2), from M2. `claude plugin test` runs TypeScript
  itself, so M1 does not need `tsc`.

### 4.5 Security model

- **No new injection route, ever by default.** The mod reads and displays. It does not
  use `$.prompt.submit`, `$.session.append` or `$.session.send`, and does not hook
  `session.receive`. The inbox socket is the one delivery route (§1.4).
- **"Read-only" includes what the model reads.** A command's `{ text }` reply goes to the
  model. Peer-authored text never goes into `{ text }`; it is shown in the band, a pane
  or `$.ui.log`. `/ccy doctor` may reply in `{ text }`, because its output is local,
  fixed-format facts with no secrets and no env values.
- **No network:** no `$.http.fetch` (the contract test enforces it). Everything goes
  through local CLIs that already exist.
- **No settings and few env reads:** `$.settings.read` is forbidden, because settings
  can hold tokens. Env reads are limited to a named allow-list (`PINGBUS_SEATS`, ccy
  markers). The test pins both through `validate`'s `calls:` and `env reads:` lines.
- **No host execution.** In the container the mod can reach only the container. On the
  host the same code runs, but the host-only modules are off and the allow-listed argv
  are read-only commands.
- **Root-owned source:** the mod files come from the image layer under
  `/opt/claude-yolo/mods`. In the bypass container the session could still edit them,
  but it could equally run anything, so this adds nothing. On the host the directory is
  root-owned through Ansible, so a session cannot rewrite the mod that cc loads.
- **Fail closed** is not needed, because the mod guards nothing. A thrown hook only loses
  a display.

### 4.6 Migration from the pingbus plugin

1. **M1** ships `ccy` beside `pingbus`, unchanged. There is no overlap, since M1 is the
   version gate, then the marketplace, the status entry (C1) and `/ccy doctor` (D1).
2. **M2** adds the teams read-only UI (T1, T3, T4 status and who). The mod's team
   module turns on only when `PINGBUS_SEATS` is set and `pingbus` is on PATH. The
   pingbus plugin keeps its hooks and its skill. M2 also adds C2, H1 and H2, `tsc`, cc on
   the host (D5), and moves the phpantom `jq` wiring onto the same directory marketplace.
3. **T7 waits for D5 on the host.** Once T7 drops `statusLine` from the kit settings,
   `cc --teams` and `agent-bus-claude` lose the bus segment unless they also load `ccy`.
   So T7 lands after `ccy` reaches the host. The kit keeps `statusLine` for
   `agent-bus-claude` until its members have `ccy` (D11).
4. **M3** moves the pingbus skill and settings-style hooks into `ccy`, after D11 is
   decided. The `test_plugin_contract.py` assertions move with the files. Running teams
   are untouched until their container is relaunched on the new image, because the image
   carries both plugins until M3 lands.
5. **M4 is a question, not a feature:** does the mod need any delivery role at all? The
   default answer is no, because the socket already delivers in-process.
