I traced every path in the list. The biggest finding is a version-gate gap: edits to the pingbus plugin and skill aren't caught by the pre-commit check, and they only reach a session if someone bumps the container version by hand.

## 1. How ccy reaches a session

The playbook `playbooks/imports/play-claude-yolo.yml` deploys the host side and builds the image:

- **Host side.** The launcher `files/var/local/claude-yolo/claude-yolo` and its `lib/*.bash` go to `/var/local/claude-yolo/`. The playbook also copies `helpers/ssh_agent_filter/ssh_agent_filter.py` into that `lib/`. These files are never in the image.
- **Image build context.** The playbook copies everything into `/opt/claude-yolo/` and runs `podman build --build-arg DOCKERFILE_HASH=...`. The parts are:
  - `Dockerfile` and `entrypoint.sh`
  - `agent-browser-*`
  - `supervisor-plugins/ccy_lifecycle.py` and `plugins/phpantom-lsp/` (from `files/var/local/claude-yolo/`)
  - `ccy-startup-info.txt`, `docs/`, `skills/browsing/` and `optional/` (from `files/opt/claude-yolo/`)
  - the pingbus zipapp, built at deploy time from `helpers/pingbus` by `python3 -m helpers.pingbus.bundle` into `optional/agent-bus/pingbus`
- **Baked in (Dockerfile COPY, around lines 311–468).** All of the above, plus Claude Code itself (`npm install -g @anthropic-ai/claude-code`). Claude Code is then updated in place at most once per 24 hours (`docs/ccy.md`, "Claude Code auto-update").
- **Bind-mounted at launch** (`claude-yolo` lines 2510–2511):
  - `$PWD` as `/workspace`
  - a read-only temp config directory as `/tmp/claude-config-import`
  - the tracked `.claude/ccy/mounts` file, plus `CCY_EXTRA_MOUNTS`
- **Written at launch by `entrypoint.sh`:**
  - `/root/.claude` becomes a symlink to `/workspace/.claude/ccy`, so session state lives in the project.
  - `settings.json` there is created, or merged with jq, to add `ENABLE_LSP_TOOL` and `enabledPlugins.phpantom-lsp`.
  - phpantom is copied into the plugins directory once only.
  - `skills/` is copied over unconditionally on every start.
  - `.claude.json` gets its onboarding and trust flags.
  - `ccy.env` and `ccy.env.local` are sourced.
  - child-claude is opt-in.
  - then the agent-bus block, the supervisor wrap and the lifecycle plugin, described below.

**Versioning.** There are two version numbers:
- `CCY_VERSION="3.98.11"` (`claude-yolo:17`), backed by a self-hash check (`CCY_HASH`).
- `REQUIRED_CONTAINER_VERSION="2.66"` (`claude-yolo:~77`), which must equal `LABEL claude-yolo-version="2.66"` (`Dockerfile:36`).

The runtime gate is `validate_container_version` in `lib/common.bash:~934`. It rebuilds when the image label differs from the required version, or when the image's `claude-yolo-dockerfile-hash` label differs from the md5 of the **Dockerfile alone**. Nothing in the build context besides the Dockerfile is hashed.

## 2. The pingbus plugin (`files/opt/claude-yolo/optional/agent-bus/`)

**Files:**
- `plugin/pingbus/.claude-plugin/plugin.json` (hidden directory): `{"name":"pingbus","version":"1.2.3",...}`
- `plugin/pingbus/hooks/hooks.json`: SessionStart, UserPromptSubmit, PostToolUse(Bash, relay mode only), Stop and SessionEnd (timeout 6). Each runs `pingbus hook <event>`.
- `plugin/pingbus/skills/pingbus/SKILL.md`
- `settings.json`: `crossSessionInbound: accept`, plus `statusLine: pingbus status-line` with `refreshInterval` 1
- `agent-bus-claude`: the launcher for members that are not ccy sessions
- `README.{docker,host,lxc,podman,vm}`

**How it is enabled.** Only through `--plugin-dir` plus `--settings`. There is no `enabledPlugins` entry and no marketplace. Three launchers each wire it by hand:
1. **ccy:** `entrypoint.sh:581–597`. Only when the host passes `-e PINGBUS_SEATS` (`lib/agent-bus-seats.bash:84`), the entrypoint links `pingbus` into `/usr/local/bin` and runs `set -- claude --plugin-dir /opt/claude-yolo/optional/agent-bus/plugin/pingbus --settings .../settings.json`, with `pingbus seat exec --` in front. It refuses to start if `ccy.env` touches any `PINGBUS_*` variable.
2. **cc --teams:** `files/var/local/claude-code/cc-teams.bash:136–146`. It uses the host kit at `/usr/local/share/agent-bus/kit`, adds `--append-system-prompt`, and passes a settings file that merges the kit settings with `cc-teams-settings.json` (Element/keyring deny rules) using `jq -s '.[0] * .[1]'`.
3. **agent-bus-claude:** the same arguments, reading `~/.config/pingbus/env`.

**The "inject frame"** is not in the plugin files. It is `TEMPLATES["inject_frame"]` in `helpers/pingbus/hooks.py:136`, added as SessionStart context when injecting (line 513). That means it ships, and is versioned, inside the pingbus zipapp, not with `plugin.json`.

**The status line.** `helpers/pingbus/statusline.py` (Plan 00201 S-C6) runs the checkout's own `statusLine` first and appends the bus segment. It looks in the project's `settings.local.json`, then the project's `settings.json`, then the user's `settings.json`. Settings given on the command line outrank all of these.

**Versioning.** `plugin.json` is bumped by hand (1.2.0 → 1.2.3 across Plans 00194 and 00201), and nothing checks it except a semver regex in the test. Changes are called out in commit subjects such as "CCY 3.98.11, container 2.66, plugin 1.2.3".

**Duplication.** The same kit is installed in two places:
- `/opt/claude-yolo/optional/agent-bus` in the image, via the playbook's file list and its own zipapp build.
- `/usr/local/share/agent-bus/kit` on the host, via `files/usr/local/sbin/agent-bus-install`'s `KIT_FILES` array and `install_kit`.

The kit file list is spelled out four times: the playbook loop, `KIT_FILES`, `src-go/qa/deployeddrift/run.go:169–189`, and the test. The "required files" check is spelled out three times: `entrypoint.sh:582`, `cc-teams.bash:153`, and `agent-bus-claude`. The playbook's optional/ copy only ever adds files, so anything removed from the repo needs a manual `state: absent` task.

## 3. Settings, hooks, hooks-daemon, supervisor and status lines

- **Project `.claude/settings.json` (tracked):**
  - every hook event points to `.claude/hooks/<event>`
  - `statusLine` is `bash $CLAUDE_PROJECT_DIR/.claude/hooks/status-line`, which forwards to the daemon socket
  - `enabledPlugins` is `defence-before-fix@defence-before-fix`
  - `permissions.deny`

  It reaches ccy sessions through the `/workspace` mount.
- **Hooks-daemon.** The install itself, `.claude/hooks-daemon/`, is gitignored and installed per checkout by `.claude/provision.sh`. It is absent in this worktree. Its config is `.claude/hooks-daemon.yaml` with `expected_version: "3.69.0"`. The hook scripts under `.claude/hooks/` are files the daemon owns.
- **The ccy supervisor:**
  - The supervisor itself is `.claude/ccy/claude-supervise.py` (`__version__ = "3.69.0"`, kept in step with the daemon). It is tracked and refreshed by the daemon (`ccy.deploy_supervisor`), along with the `.claude/ccy/claude-supervise` launcher.
  - `.claude/ccy/ccy.env` turns it on with `--arm`. `entrypoint.sh:625–662` uses it by default when the file exists.
  - The lifecycle plugin, `supervisor-plugins/ccy_lifecycle.py`, is baked into the image. Its plugin API version is checked against `_PLUGIN_API_MAJOR` in the supervisor.
- **Container user settings.** The settings file in the project's `.claude/ccy/` directory is written at launch, as above. Its only template is the heredoc in `entrypoint.sh:361`.
- **cc's settings.** cc writes the host user's `~/.claude`, plus `LAST_TOKEN` for the daemon's status line (`cc:212`).

## 4. The cc host launcher

`files/var/local/claude-code/cc` (383 lines) is installed to `/var/local/claude-code/cc` and called through a `.bashrc` alias. It is deployed by the same play, because it sources the ccy libs `common-pure`, `token-management`, `tmux-session` and `session-registry`. It also sources `cc-teams.bash`.

How it differs from ccy:
- It runs `claude update` on every launch (native install, `latest` channel).
- It has no image, no entrypoint and no supervisor wrap.
- Its "Desktop" token option uses the host's own login.
- `--teams` builds the plugin command line itself and points it at the host kit.

It has no version number of its own.

## 5. Tests

- `tests/helpers/pingbus/test_plugin_contract.py` checks:
  - the manifest's keys and semver format
  - `settings.json`, exactly
  - the exact list of kit files
  - every `hooks.json` command is a real `pingbus hook` subcommand, with the matcher, guard and timeout rules
  - the skill's commands parse, its rules are present, and its quoted phrases match the hook templates
  - the skill's exit-code table matches the CLI's
  - the launcher's arguments and its refusals
- Other related tests:
  - `tests/helpers/pingbus/test_hooks.py`
  - `tests/helpers/ccy_lifecycle/test_plugin.py`
  - `scripts/test-ccy-agent-bus.bash`, which runs the AGENT-BUS block of `entrypoint.sh`
  - `scripts/test-agent-bus-install.bash`
  - `scripts/test-ccy-container-version-hook.bash`
  - `scripts/test-ccy-teams.bash`
  - `tests/helpers/agent_bus/test_member_docs.py`

## 6. The `feat/teams-shutdown` branch

`origin/feat/teams-shutdown` does not exist. A local `feat/teams-shutdown` does exist and is checked out in another worktree. It sits at `0e53ac69`, the same commit as HEAD, so `HEAD..feat/teams-shutdown` is empty and it adds nothing yet. Its last 15 commits are all Plan 00201 sign-in, stamp-group and final-review merges. No plan or doc mentions "teams-shutdown".

## 7. Docs

- `docs/ccy.md`: "Sessions Survive a Reboot", "The Supervisor", "--teams", "Two version numbers", "Claude Code auto-update"
- `docs/ccy-changelog.md`, `docs/ccy-debug-mounts.md`
- `docs/agent-bus.md` (section "A session's status line", line 560), `docs/agent-bus-protocol.md`, `docs/agent-bus-lxc-members.md`
- `files/opt/claude-yolo/docs/CCY-GUIDE.txt` and `CUSTOM-DOCKERFILES.txt`, which are inside the image
- `CLAUDE/ContainerRules.md`, `.claude/rules/ccy-version-bump.md`

## 8. Related plans

| Plan | Status |
|---|---|
| 00048 cc token-source parity | used by cc |
| 00093 ccy version gate covers two files of eight | Not Started (partly overtaken by the container gate, but see the version-bump gap below) |
| 00135 ccy sessions survive a reboot | In Progress |
| 00146 token switch through the supervisor | Not Started (waiting on decisions) |
| 00154 freeze and thaw | Not Started |
| 00158 take over | In Progress |
| 00161 agent team bus (Matrix) | In Progress |
| 00169 image keeps its tools current | In Progress (Phase 3 next) |
| 00177 ccy-teams human-friendly | In Progress (waiting on host tests 5.1 and 9.5) |
| 00181 session limits | Not Started (although `ccy_lifecycle.py` already ships) |
| 00186 LXC reboot warning | Not Started |
| 00194 agent-bus live delivery supervisor plugin | In Progress |
| 00196 team coordinator role | In Progress |
| 00197 seat broker daemon | Not Started |
| 00201 many teams per session | In Progress (M1–M7 deployed, waiting on owner acceptance) |
| 00202 ccy Claude Code plugin | Not Started (empty template created 2026-10-16) |

## Version bump mechanics and the gap

`scripts/git-hooks/pre-commit` has two gates:
- **Lines 123–212:** a staged launcher, `lib/*.bash` or `helpers/ssh_agent_filter/*.py` file requires a `CCY_VERSION` bump.
- **Lines 217–291:** the Dockerfile label must equal `REQUIRED_CONTAINER_VERSION`. Any staged file matching a Dockerfile COPY source requires a label bump.

The second gate resolves COPY sources against `files/var/local/claude-yolo/`. But `skills/`, `optional/`, `docs/` and `ccy-startup-info.txt` live under **`files/opt/claude-yolo/`** in the repo. So edits to the pingbus plugin, skill, kit settings, the browsing skill and child-claude are not gated.

The pingbus zipapp built from `helpers/pingbus/*.py` isn't a repo file at all, so it is not gated either. Because the runtime check hashes only the Dockerfile, every change in either of these places reaches sessions only if someone bumps the container version by hand.