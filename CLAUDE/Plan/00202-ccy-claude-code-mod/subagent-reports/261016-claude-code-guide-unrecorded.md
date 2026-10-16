# Claude Code extension points: reference for a containerised plugin

Docs are at https://code.claude.com/docs/en/. Items I could not confirm in the docs are marked **UNVERIFIED**.

**Two corrections to the brief**
- `claude plugin eval` has a public page: https://code.claude.com/docs/en/plugin-evals. The embedded note says none exists yet. Each eval run and judge grader is a real model call, so it needs network access and credentials.
- The docs do not list a status line as a mod surface. The mods render sites are Pane, AbovePrompt (the band above the prompt), Spinner, ToolUse, and others. I could not verify the "status line" item from the plugin-authoring text you quoted. **UNVERIFIED**.

## 1. Plugins
Sources: https://code.claude.com/docs/en/plugins/manifest-reference and https://code.claude.com/docs/en/plugins/components
- Manifest is `.claude-plugin/plugin.json`. `name` is the only required field, in kebab-case. Components are namespaced under it, e.g. `deploy-tools:reviewer`.
- Default layout: `skills/<name>/SKILL.md`, `commands/`, `agents/`, `output-styles/`, `hooks/hooks.json`, `.mcp.json`, `.lsp.json`, `monitors/monitors.json`, `themes/`, `workflows/`, `bin/` (on the Bash PATH while enabled), `settings.json`. Everything except the manifest sits at the plugin root.
- Manifest keys: `commands`, `agents`, and `outputStyles` replace their default folder. `skills` adds to it. `hooks`, `mcpServers`, and `lspServers` merge with their default files.
- Plugin `settings.json` (or manifest `settings`): **only `agent` and `subagentStatusLine` take effect**. Other keys are dropped. A plugin cannot set `statusLine`, `permissions`, `model`, `env`, or hooks this way.
- `userConfig` types: string, number, boolean, directory, file. `sensitive: true` moves the value to secure storage. Reference it as `${user_config.KEY}`, or read `CLAUDE_PLUGIN_OPTION_<KEY>` in hooks. Shell-form hooks, monitor commands, and `headersHelper` cannot take `${user_config.*}`.
- Variables: `${CLAUDE_PLUGIN_ROOT}` is the versioned install directory and changes on update, so do not write state there. `${CLAUDE_PLUGIN_DATA}` is `~/.claude/plugins/data/<id>/`, survives updates, and is deleted on last uninstall unless `--keep-data`. `${CLAUDE_PROJECT_DIR}` is the project root. None of these are in the environment of Bash tool commands.
- Limits: component paths must start with `./` and stay inside the plugin root. A `CLAUDE.md` at the plugin root is not loaded; use a skill. `claude plugin validate` is the authoritative check.
- `--plugin-dir <dir|zip>` loads in place for one session, as `<name>@inline`. Directories are never copied.
- `enabledPlugins` precedence, lowest to highest: `--add-dir`, user, project, local, flag, managed. Managed `true` force-enables and `false` blocks. Source: https://code.claude.com/docs/en/plugins/loading

## 2. Marketplaces, sources, updates
Sources: https://code.claude.com/docs/en/plugins/marketplace-reference, https://code.claude.com/docs/en/plugins/loading, https://code.claude.com/docs/en/plugins/org
- `.claude-plugin/marketplace.json` requires `name`, `owner`, and `plugins`.
- Plugin entry `source`: relative path (`./plugins/x`, resolved from the marketplace root; works only for git, `file`, and `directory` marketplaces), `github`, `url`, `git-subdir`, `npm`, `archive` (https zip with `sha256`), or `command`.
- Marketplace sources: `github`, `git`, `url` (a marketplace.json link), `file`, `directory`, and `settings` (inline). Policy lists also accept `hostPattern`, `pathPattern`, and `skills-dir`.
- Settings keys: `extraKnownMarketplaces` (any scope) registers marketplaces. `strictKnownMarketplaces` (managed) is the allowlist, and `[]` locks out everything. `blockedMarketplaces` (managed) is checked first. `enabledPlugins` (managed) force-enables plugins.
- Cache: `~/.claude/plugins`, overridable with `CLAUDE_CODE_PLUGIN_CACHE_DIR`. Contents are `cache/<marketplace>/<plugin>/<version>/`, `marketplaces/<name>/`, `installed_plugins.json`, and `known_marketplaces.json`.
- Version resolution: manifest `version`, then entry `version`, then the source. For `github`, `url`, and `git-subdir`, the source is the 12-character commit SHA. For `archive`, it is the sha256. For `npm`, and for a local directory that is not a git repo, it is `unknown`.
- A pinned `version` means no re-copy while it is unchanged. Update prints `already at the latest version`. Pushing commits without a bump changes nothing for copied plugins.
- A relative-path plugin in a marketplace added from a local path loads in place. Edits appear at the next session start or `/reload-plugins`, with no bump needed.
- **Offline behaviour:** plugins load at session start from `installed_plugins.json` and the cache, without network. The network is touched only for a declared-but-uncached marketplace (clone), auto-update, and install or update. A `directory` or `file` marketplace is not refreshed before install.
- **Seed for images:** `CLAUDE_CODE_PLUGIN_SEED_DIR` points at a pre-built tree. Build it with `CLAUDE_CODE_PLUGIN_CACHE_DIR=<path> claude plugin marketplace add` and `... claude plugin install`. Seed marketplaces are read-only, have auto-update forced off, and overwrite a same-name user marketplace on startup. You must still enable the plugins. The docs say to combine a seed with `directory` or `file` sources and `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` for fleets with no outbound git.
- **Auto-update:** default on for Anthropic's official marketplaces, off for others. It runs after the first message with a random delay of up to ten minutes. `DISABLE_AUTOUPDATER`, `DISABLE_UPDATES`, and `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC` turn it off. `FORCE_AUTOUPDATE_PLUGINS=1` keeps plugin updates on.
- `claude plugin update <plugin>` takes `--scope` and `--json`. The new version applies in the next session or after `/reload-plugins`.

## 3. Skills, commands, sub-agents, output styles
Sources: https://code.claude.com/docs/en/skills and https://code.claude.com/docs/en/sub-agents
- `SKILL.md` frontmatter: `name`, `description`, `when_to_use`, `argument-hint`, `arguments`, `disable-model-invocation`, `user-invocable`, `allowed-tools`, `disallowed-tools`, `model`, `effort`, `context: fork`, `agent`, `background`, `hooks`, `paths`, `shell`. `metadata`, `license`, and `compatibility` are ignored by Claude Code. Description plus `when_to_use` is truncated at 1,536 characters.
- Commands are merged into skills. `commands/` still works, and a skill wins a name clash. Plugin skills are invoked as `/<plugin>:<skill>`.
- Plugin agents are `agents/*.md`, invoked as `<plugin>:<name>`. In plugin agents, `permissionMode`, `hooks`, `mcpServers`, and `initialPrompt` are ignored.
- Output styles are `output-styles/<name>.md`, selected via `/output-style` as `<plugin>:<name>`.

## 4. Hooks
Source: https://code.claude.com/docs/en/hooks. I read the event list from the reference table as summarised by the fetch tool. Please verify it against the page.
- **Events (33):** SessionStart, Setup, UserPromptSubmit, UserPromptExpansion, PreToolUse, PermissionRequest, PermissionDenied, PostToolUse, PostToolUseFailure, PostToolBatch, Notification, MessageDisplay, SubagentStart, SubagentStop, TaskCreated, TaskCompleted, Stop, StopFailure, TeammateIdle, InstructionsLoaded, ConfigChange, CwdChanged, DirectoryAdded, FileChanged, WorktreeCreate, WorktreeRemove, PreCompact, PostCompact, PreModelSwitch, PostModelSwitch, Elicitation, ElicitationResult, SessionEnd.
- **Types:** `command`, `http`, `mcp_tool`, `prompt`, `agent` (agent is experimental). Not every event accepts every type. SessionStart accepts only `command` and `mcp_tool`.
- **Outputs:**
  - Universal: `continue`, `stopReason`, `systemMessage`, `terminalSequence`.
  - PreToolUse: `hookSpecificOutput.permissionDecision` (`allow`/`deny`/`ask`/`defer`), `permissionDecisionReason`, `updatedInput` (replaces the whole input), `additionalContext`. Multiple hooks resolve as deny > defer > ask > allow. Deny and ask rules are still evaluated, so `allow` does not override them. `defer` is honoured only with `-p`.
  - PostToolUse: `decision: "block"`, `additionalContext`, `updatedToolOutput` (changes only what Claude sees).
  - PermissionRequest: `decision.behavior` (`allow`/`deny`), `updatedInput`.
  - SessionStart: `additionalContext`, `initialUserMessage` (`-p` only), `sessionTitle`, `watchPaths`, `reloadSkills`.
  - Stop and SubagentStop: `decision: "block"`, `additionalContext`.
  - `additionalContext` is capped at 10,000 characters.
  - Exit 2 blocks. Other non-zero exits do not block unless `onFailure: "block"`.
- **UI:** hooks cannot draw UI. They can show `systemMessage` and emit `terminalSequence` (notifications, window title, taskbar progress). No hook output sets the status line.
- **Plugin vs settings hooks:** plugin hooks merge with user and project hooks while the plugin is enabled. Matching hooks run in parallel. An identical handler in several settings files runs once, but plugin copies stay separate. **No explicit precedence order between plugin and settings hooks was documented in what I read. UNVERIFIED beyond that.**
- `allowManagedHooksOnly` blocks user, project, local, and plugin hooks, except plugins force-enabled in managed `enabledPlugins`. `disableAllHooks` in user settings cannot disable managed hooks.

## 5. Status line
- `statusLine` is settings-only. Its scopes are user, project, local, and managed. A plugin cannot ship it. Sources: https://code.claude.com/docs/en/statusline and https://code.claude.com/docs/en/settings-reference
- A plugin can set `subagentStatusLine` through its `settings.json`.

## 6. MCP, channels, monitors, LSP
- `.mcp.json` or manifest `mcpServers` (path, inline map, or `.mcpb`/`.dxt` bundle). `${CLAUDE_PLUGIN_ROOT}` works in stdio `command`, `args`, and `env`. For a repo-shipped plugin, MCP servers need per-server approval, and bundle files are skipped.
- **Channels** (https://code.claude.com/docs/en/channels) push events into a running session, and can be two-way. The manifest `channels` key binds to an MCP server. Requirements: launch with `--channels plugin:<name>@<marketplace>`, claude.ai login or a Console API key, and not Bedrock, Vertex, or Foundry. Team and Enterprise orgs must enable `channelsEnabled`. It is a research preview with an allowlist. Custom channels need `--dangerously-load-development-channels`. Channels need an external service, so they are not offline.
- **Monitors** (`experimental.monitors`): persistent background processes that run only in interactive sessions, and not on Bedrock, Vertex, or Foundry. `when` is `always` or `on-skill-invoke:<skill>`.
- **LSP** (`.lsp.json` or `lspServers`): the server binary must exist on the machine, so bake it into the image.

## 7. Settings and permissions
- A plugin cannot change `permissions` or the permission mode through its settings.
- Hooks can allow, deny, or ask, but cannot override deny or ask rules.
- Mods can approve a call that an `ask` rule would prompt for. They cannot approve one that a `deny` rule refuses, unless managed `allowModsToOverrideDenyRules` is set. Mods cannot change the permission prompt.
- Managed keys: `allowManagedHooksOnly`, `allowManagedModsOnly`, `allowManagedPermissionRulesOnly`, `disableSideloadFlags` (rejects `--plugin-dir`), `strictPluginOnlyCustomization`, `disableCommandPluginSources`. Source: https://code.claude.com/docs/en/plugins/org
- Interaction with `bypassPermissions`: **UNVERIFIED**.

## 8. Newer features
- **Mods** (https://code.claude.com/docs/en/plugins/mods/overview, https://code.claude.com/docs/en/plugins/mods/reference). A plugin can ship one. Declare it in `hooks/hooks.json` as `"modules": ["./register.js"]`, and export `register(on, options)` from a JS or TS file. Mods are on by default in the terminal (v2.1.287 or later) and the Desktop app (v2.1.286 or later). Their hooks run in terminal, Desktop, VS Code, `claude -p`, the Agent SDK, Remote Control, and cloud sessions. Drawing happens only in terminal, Desktop, and VS Code, and not in WSL. Mods are not sandboxed and run with the user's permissions. They are switched off by `disableAllHooks`, `--safe-mode`, and `allowManagedModsOnly`. Limits include 10 s per hook, 4 MiB for `$.fs` reads and writes, and 30 s per `$.http.fetch`. The `$.ui.toast` method gives toasts.
- **Agent teams** (https://code.claude.com/docs/en/agent-teams): hooks TeammateIdle, TaskCreated, and TaskCompleted exist. Whether a plugin can ship teammate definitions is **UNVERIFIED**.
- **Background work:** command hooks support `async: true`. Monitors are described in section 6.
- **Dependencies:** manifest `dependencies` lists other plugins. Each entry is `name`, `name@marketplace`, or an object with `version`.
- **Evals:** `claude plugin eval` (https://code.claude.com/docs/en/plugin-evals). Suites live in `evals/`, results in `evals/results/<timestamp>/`, and it needs model access. Use `claude plugin validate` for structure and `claude plugin test` for mods.

## Cannot, summarised
- Set `statusLine`, `permissions`, or the permission mode from a plugin.
- Ship a `CLAUDE.md` that loads (use a skill).
- Put file-based hooks in a marketplace entry (inline only).
- Work offline for anything that calls a model or a remote service. Claude itself needs its model API, which is an inference, not stated in these pages.

## Offline container checklist
1. Bake the plugin into the image. Use a `directory` marketplace with relative-path plugins for in-place loading, or a seed via `CLAUDE_CODE_PLUGIN_SEED_DIR` plus managed `enabledPlugins`.
2. Set `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1` to stop background fetches and auto-update.
3. Install npm dependencies at build time. The lockfile must be npm or Bun, with no git dependencies.
4. Bump `version` on each image build, or rely on in-place loading for local marketplaces.
5. Pin `CLAUDE_CODE_PLUGIN_CACHE_DIR` and `CLAUDE_PLUGIN_DATA` deliberately.