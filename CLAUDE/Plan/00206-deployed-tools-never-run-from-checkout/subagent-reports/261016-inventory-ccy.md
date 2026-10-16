# Inventory: CCY launcher and container side — runtime code taken from the checkout

Read-only audit. Scope: `files/var/local/claude-yolo/`, `files/var/local/claude-code/`, the
play that deploys them (`playbooks/imports/play-claude-yolo.yml`), the Go commands it builds
(`ccy-sessions`, `ccy-teams`), the in-container entrypoint, project-level `.claude/`
config, and git / Claude Code hooks.

The checkout path is never named. In the plays it is `root_dir`
(`"{{ lookup('ansible.builtin.config', 'CONFIG_FILE') | dirname }}"`,
`play-claude-yolo.yml:14`); in Go it is `main.RootDir` (ccy-teams) and
`config.Config.RootDir`; in `play-git-hooks-security.yml` it is `repo_path`.

## Classes

- **RUNTIME-FROM-CHECKOUT**: a deployed tool reaches back into the live checkout at run time.
  It must move.
- **DEPLOYED-OK**: copied or built out of the checkout by a play. At run time it uses only
  the deployed copy.
- **REPO-BY-DESIGN**: plays, plan scripts, tests, QA, and dev hooks scoped to the repo, plus
  CCY's per-project contract, which reads files from whichever project `$PWD` is.

## Counts

| Class                 | Count                                                           |
| --------------------- | --------------------------------------------------------------- |
| RUNTIME-FROM-CHECKOUT | 1 (ccy-teams, with three related uses of the checkout: R1a–R1c) |
| DEPLOYED-OK           | 9                                                               |
| REPO-BY-DESIGN        | 6                                                               |

---

## RUNTIME-FROM-CHECKOUT

### R1. `ccy-teams` has the checkout path baked in and runs checkout code

- **Build**: `playbooks/imports/play-claude-yolo.yml:857-866`, with
  `go_command_ldflags: "-X main.RootDir={{ root_dir }}"` at line 863, which passes through
  `tasks/build-go-command.yml:80`. The binary is installed to `~/.local/bin/ccy-teams`.
- **Variable**: `src-go/cmd/ccy-teams/main.go:13-26` (`var RootDir string`; if it is empty the
  command refuses to run). `src-go/internal/teams/config/config.go:26-47` derives these paths
  from it.
- **Who runs it**: the desktop user, interactively (`ccy-teams` board, create wizard,
  apply, takedown, lxc add, Element login).

The binary uses the checkout in three ways:

- **R1a. It runs `helpers/agent_bus/declare.py` out of the checkout** (most serious).
  - Code: `src-go/internal/declare/declare.go:65-78,509` runs
    `env PYTHONPATH=<RootDir> python3 -P -m helpers.agent_bus.declare <sub> ...`.
  - Wiring: `src-go/internal/teams/host/host.go:190-192`.
  - Guard: `NeedCheckout` at `host.go:163-171` refuses if the file is gone. It never checks
    which version is there.
  - Used by the board (`board/collect.go:161`, `Declare.Overview`) and by every
    create/apply/remove path.
  - Mechanism: whatever branch is checked out at that moment decides the Python that runs.
    No deployed copy exists.
  - **Risk on a WIP merge into F44**: the code that runs is whatever is on disk, not the
    commit that was last deployed. A WIP branch (or a half-finished merge) can change the
    `declare` CLI contract under a Go binary built from an older tree. The result is either
    a hard failure or wrong data on the board, and it happens with no deploy step in between.
    If the checkout is moved or deleted, the command breaks.
- **R1b. It reads and writes `environment/localhost/host_vars/localhost.yml` in the
  checkout.** See `config.go:38` (overridable by `CCY_TEAMS_HOST_VARS`).
  - host_vars is inventory and lives in the checkout by design, so editing it is
    legitimate. The coupling is the problem: the editor is the declare.py from R1a, which
    also comes from the checkout.
  - When this moves, the data file can stay where it is, but the code that edits it must
    be deployed.
- **R1c. It runs plays from the checkout**: `playbooks/imports/play-agent-bus.yml` and
  `playbooks/imports/optional/common/play-agent-bus-element.yml`.
  - Paths: `config.go:40-41`. Called at `ops/ops.go:101-110` and `ops/ops.go:291-305`.
  - `checkPlay` at `ops.go:312-322` requires the play to be inside `RootDir`.
  - Running a play from the checkout is how IaC works in this repo, so on its own this is
    REPO-BY-DESIGN. It is listed here because a deployed tool triggers it implicitly. On a
    WIP branch, `ccy-teams apply` deploys the WIP plays without the operator choosing to
    deploy anything. The plan should decide whether that is acceptable or whether to warn
    when the checkout is not on F44 or is dirty.

Other uses of `RootDir` in ccy-teams are messages only. `teamtest/world.go:113` is test code.

---

## DEPLOYED-OK

| #   | Item                                                                   | Repo source                                                                                                                                                                                | Deployed to                                                                                                    | Evidence                                                                                                                                                  |
| --- | ---------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | `ccy` launcher                                                         | `files/var/local/claude-yolo/claude-yolo`                                                                                                                                                  | `/var/local/claude-yolo/claude-yolo` (copied by `ansible.builtin.copy`, not a symlink)                         | play `:536-546`                                                                                                                                           |
| D2  | Launcher libs (13 `*.bash` plus `ssh_agent_filter.py` from `helpers/`) | `files/var/local/claude-yolo/lib/`, `helpers/ssh_agent_filter/`                                                                                                                            | `/var/local/claude-yolo/lib/` (explicit copy list)                                                             | play `:444-476`                                                                                                                                           |
| D3  | Launcher sources its libs relative to itself                           | `claude-yolo:20` `SCRIPT_DIR=$(dirname BASH_SOURCE)`; `:29-74` sources `$SCRIPT_DIR/lib/*.bash`; `:110-120` hashes the same directory; self re-exec at `:1171`, `:3843` uses `$SCRIPT_DIR` | deployed dir, provided it is started from the deployed path                                                    | `alias ccy='/var/local/claude-yolo/claude-yolo'` in `files/home/bashrc-includes/claude-yolo.bash.j2` (deployed to `~/.bashrc-includes/`, play `:548-563`) |
| D4  | `cc` host launcher plus `cc-teams.bash` and `cc-teams-settings.json`   | `files/var/local/claude-code/`                                                                                                                                                             | `/var/local/claude-code/`                                                                                      | play `:646-673`; `cc:34-82` sources only `/var/local/claude-yolo/lib/*` and `/var/local/claude-code/cc-teams.bash`                                        |
| D5  | Base image build context                                               | `files/var/local/claude-yolo/{Dockerfile,entrypoint.sh,agent-browser-*,supervisor-plugins/,plugins/}`, `files/opt/claude-yolo/**`                                                          | `/opt/claude-yolo/` (copied). Build: `{{ container_engine }} build ... -t claude-yolo:latest /opt/claude-yolo` | play `:116-435`, `:1128-1140`; every `COPY` in the Dockerfile (`:311-470`) is relative to `/opt/claude-yolo`                                              |
| D6  | pingbus zipapp                                                         | `helpers/pingbus` (`python3 -m helpers.pingbus.bundle --source {{ root_dir }}`, chdir root_dir)                                                                                            | `/opt/claude-yolo/optional/agent-bus/pingbus`, then baked into the image                                       | play `:382-405`; the checkout is used at play time only                                                                                                   |
| D7  | In-container pingbus hooks and settings                                | `files/opt/claude-yolo/optional/agent-bus/{settings.json,plugin/pingbus/hooks/hooks.json}`                                                                                                 | in image under `/opt/claude-yolo/optional/`; the hooks call `pingbus` on PATH                                  | play `:362-375`                                                                                                                                           |
| D8  | Session lifecycle supervisor plugin                                    | `files/var/local/claude-yolo/supervisor-plugins/ccy_lifecycle.py`                                                                                                                          | image `/opt/claude-yolo/supervisor-plugins/` (root owned, outside the project mount)                           | `entrypoint.sh:686`                                                                                                                                       |
| D9  | `ccy-sessions` Go binary and its systemd units                         | `src-go/cmd/ccy-sessions`, `files/home/.config/systemd/user/ccy-sessions-*.service*`                                                                                                       | `~/.local/bin/ccy-sessions`, `~/.config/systemd/user/`                                                         | play `:842-850` (no ldflags; the comment at `:838` says it needs no checkout path); units `ExecStart=%h/.local/bin/ccy-sessions ...`                      |

Bind mounts from the host into the container contain nothing specific to fedora-desktop.
The full list:

- `$PWD:/workspace` (`claude-yolo:2516`)
- the config import temp dir (`:2517`)
- the membership dir (`lib/agent-bus-membership.bash:54`)
- the ssh-agent socket and staged keys (`lib/ssh-handling.bash:956-1477`)
- `ccy.env.local` (`lib/common.bash:716`)
- the declared project mounts (`lib/common.bash:468`)
- Wayland/X11 sockets (`claude-yolo:3565-3575`)

No checkout `helpers/`, `scripts/` or hook directory is mounted into containers that run
other projects.

`declare -f` / `typeset -f` used to ship function bodies: these appear only in test code
(`scripts/test-ccy-session-registry.bash:440,997`). They do not occur in runtime code in
this slice.

No play installs hooks into user-global Claude config (`~/.claude/settings.json`). No play
mentions `hooks-daemon`, and the only `settings.json` the play deploys is the in-image
agent-bus one (D7).

---

## REPO-BY-DESIGN

Each of these is used when the project being worked on is fedora-desktop itself. Each one
reads that project's own tree, as CCY does for any project.

| #   | Item                                                                                    | Mechanism                                                                                                                                                                        | Note on WIP risk                                                                                                                                                                                                            |
| --- | --------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| B1  | Project image build `.claude/ccy/Dockerfile`                                            | `claude-yolo:2071` (`PROJECT_DOCKERFILE=".claude/ccy/Dockerfile"`), built at `:2173-2181` with context `.`, the project dir. The Dockerfile `COPY .qa-versions` (`:88`, `:162`). | This is the per-project dev image. A WIP branch's Dockerfile rebuilds the image for sessions on this project only. That is the expected contract.                                                                           |
| B2  | `.claude/ccy/ccy.env` / `ccy.env.local` sourced inside the container                    | `entrypoint.sh:449-453`                                                                                                                                                          | This is the per-project config contract. It is sourced in the container, never on the host.                                                                                                                                 |
| B3  | PTY supervisor `/workspace/.claude/ccy/claude-supervise(.py)` run as the claude wrapper | `ccy.env` sets `CCY_CLAUDE_WRAPPER=/workspace/.claude/ccy/claude-supervise --arm --`; default path at `entrypoint.sh:630-659`                                                    | A tracked copy owned by the hooks daemon (`ccy.deploy_supervisor`). The project tree decides which supervisor runs, so a WIP or broken copy breaks the launch. The parse probe fails fast. Scoped to the project by design. |
| B4  | Claude Code project hooks                                                               | `.claude/settings.json:5-146`, every one `bash "$CLAUDE_PROJECT_DIR"/.claude/hooks/<event>`, which goes to the hooks daemon clone under `.claude/hooks-daemon/`                  | Hooks for working in this repo. Not installed globally.                                                                                                                                                                     |
| B5  | Git hooks                                                                               | `playbooks/imports/play-git-hooks-security.yml:49-63` sets `core.hooksPath=scripts/git-hooks` at `scope: local` (relative path)                                                  | Only affects git operations in this repo. `fedora-desktop-self-update` deliberately disables them with `-c core.hooksPath=/dev/null` (`files/usr/local/sbin/fedora-desktop-self-update:116`).                               |
| B6  | Plays run from the checkout, including the R1c play runs considered on their own        | IaC                                                                                                                                                                              | This is how the repo works. R1c is the case where a deployed tool triggers a play run implicitly.                                                                                                                           |

---

## Recommendation for the plan (this slice)

Only ccy-teams needs work:

- Deploy `helpers/agent_bus/declare.py`, together with the `helpers` modules it imports, to
  a fixed root-owned location, as `ssh_agent_filter.py` already is (D2). Then point
  `declare.Client` at that location instead of `PYTHONPATH=<RootDir>`.
- Alternatively, port `declare` into the Go binary.
- Keep `RootDir` only for the two things that genuinely belong to the checkout: the
  host_vars data file and the plays. Consider guarding play runs by refusing, or warning,
  when the checkout is not on the deploy branch or has uncommitted changes.
