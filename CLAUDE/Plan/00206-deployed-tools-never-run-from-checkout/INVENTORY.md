# Plan 00206 inventory: what runs code from the checkout at run time

The consolidated, de-duplicated result of the three slice audits:
[Python](subagent-reports/261016-inventory-python.md),
[ccy launcher and container](subagent-reports/261016-inventory-ccy.md) and
[host units, wrappers, links and Go](subagent-reports/261016-inventory-host.md). Those
reports hold the file:line evidence; this page is the list the plan works from. A
finding found by more than one slice appears here once.

The checkout reaches deployed things by one Ansible name, `root_dir` (each play's
`lookup('ansible.builtin.config', 'CONFIG_FILE') | dirname`), and by one Go name,
`main.RootDir` (ccy-teams only, set by `-X main.RootDir={{ root_dir }}`). Deployed files
also carry it as `ROOT_DIR` (fedora-desktop-health), `REPO_PATH` (ftp-camera) and
`VMTEST_CHECKOUT` (vmtest, data only).

**Classes.** RUNTIME: deployed code that loads code from the checkout when it runs, so a
merge into F44 changes it with no play run. DEPLOYED: already runs from a copy a play
installed. BY-DESIGN: runs from the checkout because that is its job (plays, `run.bash`,
`meta-deploy.bash`, plan scripts, QA, tests, the repo's own hooks).

## Counts

| Class      | Findings                               |
| ---------- | -------------------------------------- |
| RUNTIME    | 7 (6 to move, 1 for an owner decision) |
| Borderline | 2 (owner decisions)                    |
| DEPLOYED   | 22                                     |
| BY-DESIGN  | 13                                     |

Not found anywhere: symlinks from a deployed path into the repo, cron or anacron entries,
desktop or autostart entries pointing into the repo, `go run` or a binary executed from
inside the repo tree by a deployed thing, user-global Claude Code hooks pointing into the
checkout, a ccy container bind-mount of a repo directory that the container then executes
(apart from the project mount itself, B2 below).

## RUNTIME (ranked by risk, highest first)

| #   | What                                         | Mechanism                                                                                                                                                                        | Run by                                                                                                                                                                                        | Checkout code loaded                                                              | Risk on a WIP merge                                                                                                                                                                                                                                                 |
| --- | -------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| R1  | `ccy-teams` (Go)                             | `-X main.RootDir` baked in (`play-claude-yolo.yml`); `declare.go` runs `env PYTHONPATH=<RootDir> python3 -P -m helpers.agent_bus.declare`; `host.go` only checks the file exists | the deployed ccy launcher on every `ccy --teams` start and restart (`choose-teams`, `describe-seats`, `record-seats`, `internal restart-args`, `membership-init`), `cc-teams.bash`, the owner | `helpers.agent_bus`, `helpers.agent_bus_lxc`, `helpers.pingbus.{bundle,protocol}` | Highest. The Go/Python contract skews both ways; a broken `choose-teams` makes `ccy` exit. This is how the LXC btrfs fix in `helpers/agent_bus_lxc` went live on merge. `apply`/`remove` also run the checkout's plays on their own, whatever branch is checked out |
| R2  | `~/.bashrc-includes/host-health-report.bash` | `PYTHONPATH="{{ root_dir }}" python3 -P -m helpers.host_health.login_message --once-a-day`, guarded only by "file exists"                                                        | every interactive shell, tmux pane and SSH login                                                                                                                                              | `host_health`, `play_ledger`                                                      | Widest reach: an import error prints a traceback in every new terminal                                                                                                                                                                                              |
| R3  | `host-health.service` (user)                 | `ExecStart=python3 -m helpers.host_health.login_report`, `WorkingDirectory={{ root_dir }}`                                                                                       | user manager at every graphical login                                                                                                                                                         | `host_health`, `play_ledger`, `self_update.published`, `version_pins`             | A failed unit and no report at login; the panel reads it as stale                                                                                                                                                                                                   |
| R4  | `host-health-collect.service` + timer        | as R3 with `--no-notify`                                                                                                                                                         | user timer (the only collector on a server); restarted by the play-ledger callback after every play                                                                                           | as R3                                                                             | As R3, unattended                                                                                                                                                                                                                                                   |
| R5  | `~/.local/bin/fedora-desktop-health`         | `ROOT_DIR="{{ root_dir }}"` baked in; `PYTHONPATH=$ROOT_DIR` for `login_message --on-demand` and `play_runner --validate`                                                        | the owner, and the GNOME panel (report and "Re-run a play")                                                                                                                                   | as R3, plus `play_runner`                                                         | The panel's report text and play validator are WIP code. Launching a play from the checkout (`--rerun`, `--run-play`) is BY-DESIGN                                                                                                                                  |
| R6  | `gh-<alias>-token-phpstorm` shell functions  | `(cd "{{ root_dir }}" && python3 -m helpers.github_scopes.cli missing)` in the `blockinfile` of `play-github-cli-multi.yml`                                                      | the owner                                                                                                                                                                                     | `github_scopes`; reads `vars/github-required-scopes.yml`                          | Low frequency, same class                                                                                                                                                                                                                                           |
| R7  | `ftp-camera --pass`                          | `/etc/ftp-camera/config` has `REPO_PATH={{ root_dir \| realpath }}`; the binary execs `$REPO_PATH/vault.bash get ftp_camera_password` (allowed in `src-go/qa/rules/table.go`)    | the owner                                                                                                                                                                                     | `vault.bash`                                                                      | Medium-low. The vault file only exists in the checkout, so this is the one case where the checkout is arguably the right source: owner decision D6                                                                                                                  |

The four `host_health` entries (R2 to R5) are one package and move together.

## Borderline (owner decisions)

| #   | What                                                                                                                                    | Why it is borderline                                                                                                                                                                                                                                 |
| --- | --------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| B1  | ccy's in-container supervisor, `/workspace/.claude/ccy/claude-supervise(.py)` (`entrypoint.sh`, `claude-yolo`)                          | It is the mounted project's own file, run for every ccy session in that project. For other projects it is theirs; when the project is fedora-desktop it is checkout code, and a broken copy fails every launch here (the parse check fails fast). D7 |
| B2  | The fedora-desktop project image, built from `.claude/ccy/Dockerfile` with the checkout as build context, and `ccy.env`/`ccy.env.local` | Every ccy project builds its own image this way; it is project configuration, not a desktop tool. Recorded so the decision is explicit. D7                                                                                                           |

## DEPLOYED (already a copy; listed so nobody re-audits them)

01. `/var/local/claude-yolo` (ccy launcher and `lib/`): an `ansible.builtin.copy`, not a link; the launcher sources `lib/` relative to itself and the alias starts the deployed path.
02. `/var/local/claude-code` (`cc`, `cc-teams.bash`): loads only from `/var/local`.
03. The ccy base image: `podman build /opt/claude-yolo`, a deployed copy.
04. The pingbus bundle in the image (`/opt/claude-yolo/optional/agent-bus`): built from the checkout at play time, then baked in; the in-container plugin dir points at it.
05. `ccy-sessions` and its user units.
06. Container mounts: project dir, agent and key sockets, membership dir, display sockets, declared mounts; no repo directory.
07. `container-watch` CLI, units and panel report: `PYTHONPATH=/usr/local/lib/ccy-helpers`.
08. `github-ssh-443`: `/usr/local/lib/ccy-helpers/helpers/github443`.
09. vmtest lab (Go `vmtest`, `vmtest-bridge-watcher`, `vmtest-bridge*@` units): helpers in `/usr/local/lib/ccy-helpers/helpers/vmtest`; the checkout is only the data spool.
10. `displaylink-dock-recovery.service`: `/usr/local/lib/displaylink-recovery`.
11. `agent-bus` admin wrapper and `agent-bus-hs*` units: `/usr/local/lib/agent-bus/agent-bus.pyz`.
12. `pingbus` command, member kit and host Claude Code plugin dir: `/usr/local/bin/pingbus` -> `pingbus.pyz`; kit and `--plugin-dir` at `/usr/local/share/agent-bus/kit`, never the checkout.
13. ccy ssh-agent filter: `/var/local/claude-yolo/lib/ssh_agent_filter.py`.
14. ccy supervisor lifecycle plugin: `/opt/claude-yolo/supervisor-plugins/ccy_lifecycle.py` in the image.
15. `fedora-desktop-self-update`: a separate root-owned clone whose HEAD must be signed and clean (Plan 00137), never the working checkout.
16. `kernel-version-manager.service`: `/usr/local/bin/manage-kernel-versions.py`.
17. The other system units under `files/etc/systemd/system/`: every `ExecStart` under `/usr/local` or `/usr/bin`.
18. Inline units in plays (ssh-suspend-guard, NM wait, ydotoold, rescrobbled, rclone mounts).
19. Go commands built by `tasks/build-go-command.yml` (ccy-sessions, ccy-teams, ftp-camera, wsi, vmtest, watermark, lxcfreeze, podfreeze): built from `src-go` at play time and copied to `~/.local/bin` or `/usr/local/bin`. Caveat: built from the working tree as it is, uncommitted edits included, and unversioned (`-buildvcs=false`): see the design's version section.
20. md-viewer: `go install` of a pinned external module.
21. GNOME extensions: all copied.
22. Symlink tasks (five): all link system paths to system or vendor paths.

## BY-DESIGN

01. Plays running helpers with `chdir: "{{ root_dir }}"` while the play runs.
02. Zipapp builds at play time (`agent-bus-install software --source {{ root_dir }}`, the pingbus bundle in `play-claude-yolo.yml`).
03. `agent-bus-install` executed from `files/usr/local/sbin/` by the plays (the timers use the deployed copy).
04. The `play_ledger` Ansible callback (inside `ansible-playbook` only).
05. `run.bash`, play shebangs, `meta-deploy.bash`.
06. Plan scripts.
07. `scripts/` (QA gates, `check-pinned-versions.bash`, `gh-account-setup.bash`); deployed code names `scripts/` only in printed remedies.
08. `tests/`, `scripts/test-*.bash`, Go `_test.go` fakes.
09. Git hooks: `core.hooksPath=scripts/git-hooks`, this repo only; self-update turns them off.
10. The project's Claude Code hooks (`.claude/settings.json`, the repo's hooks daemon).
11. `watchlist` (Go), run with `go run` by the `/watchlist` skill inside the repo; never deployed.
12. `ccy-teams apply`/`remove` and `fedora-desktop-health --rerun`/`--run-play` launching plays (the plays are BY-DESIGN; the helper code around them is R1 and R5, and whether a tool may launch plays from a dirty or non-F44 checkout on its own is D5).
13. The pingbus kit is built twice, from `files/opt/claude-yolo/` and `helpers/pingbus`; both copies are deployed, the drift between them is Plan 00202's.

## Found on the way, not in this plan's class

- `play-git-hooks-security.yml` hardcodes `repo_path: $HOME/Projects/fedora-desktop` instead of `root_dir`, so it configures the wrong tree, or fails, for any other checkout location. Task 6.3.
- `qa-deployed-drift` does not cover the `ccy-helpers` copies of containerwatch and github443, or displaylink-recovery. Task 6.1.
