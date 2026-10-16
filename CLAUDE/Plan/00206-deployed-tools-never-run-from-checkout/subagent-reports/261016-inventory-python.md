# Inventory: Python run or imported from the live checkout at run time

Slice: Python. Read-only audit of the repository tree; nothing on a host was inspected.
All paths are repo-relative unless they are a generic deployed location.

## How the checkout is named

| Name                                     | Where                                                                                  | What it is                                                                                                                                                |
| ---------------------------------------- | -------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `root_dir`                               | every play's `vars:` (`lookup('ansible.builtin.config', 'CONFIG_FILE') \| dirname`)    | The checkout Ansible runs from. The only Ansible name; no `repo_root` / `fedora_desktop_root` / `playbook_dir` use exists in plays, tasks, files or vars. |
| `RootDir`                                | `src-go/cmd/ccy-teams/main.go:13-15`, `src-go/internal/teams/config/config.go`         | Go var baked in at build: `go_command_ldflags: "-X main.RootDir={{ root_dir }}"` (`playbooks/imports/play-claude-yolo.yml:863`).                          |
| `ROOT_DIR`                               | `files/home/.local/bin/fedora-desktop-health.j2:33`                                    | `ROOT_DIR="{{ root_dir }}"` rendered into a deployed command.                                                                                             |
| `VMTEST_CHECKOUT`                        | `playbooks/imports/optional/common/play-vm-test-lab.yml:322` (lab.env)                 | Checkout as a spool DATA directory for the vmtest bridge, not as code.                                                                                    |
| `REPO_PATH`                              | `playbooks/imports/optional/common/play-ftp-camera.yml:262` (`/etc/ftp-camera/config`) | Bash slice (vault helper), noted only.                                                                                                                    |
| `CCY_TEAMS_HOST_VARS` / `CCY_TEAMS_PLAY` | `src-go/internal/teams/config/config.go:38-40`                                         | Derived from `RootDir`: host_vars file and plays in the checkout.                                                                                         |

Deployed Python library roots that already exist: `/usr/local/lib/ccy-helpers/helpers/{containerwatch,github443,vmtest}`,
`/usr/local/lib/displaylink-recovery/helpers/displaylink_recovery`, `/usr/local/lib/agent-bus/{agent-bus,pingbus}.pyz`,
`/usr/local/share/agent-bus/kit/`, `/var/local/claude-yolo/lib/ssh_agent_filter.py`, `/opt/claude-yolo/...` (image build context),
and the self-update deploy clone `/var/lib/fedora-desktop/deploy`.

## Counts

| Class                 | Findings |
| --------------------- | -------- |
| RUNTIME-FROM-CHECKOUT | 6        |
| DEPLOYED-OK           | 11       |
| REPO-BY-DESIGN        | 9 groups |

The six RUNTIME-FROM-CHECKOUT findings touch five helper packages: `host_health`, `play_ledger`, `self_update`,
`version_pins` (all imported by the health surface), `agent_bus` + `agent_bus_lxc` + `pingbus` (imported by `declare.py`),
and `github_scopes`.

## RUNTIME-FROM-CHECKOUT (must move)

### R1. `host-health.service` (user unit, login-time report)

- Repo: `files/home/.config/systemd/user/host-health.service.j2:23-24`, deployed by
  `playbooks/imports/optional/common/play-host-health-login-report.yml:149-151`.
- Mechanism: `ExecStart=/usr/bin/python3 -m helpers.host_health.login_report` with `WorkingDirectory={{ root_dir }}`; `-m`
  puts the cwd (the checkout) first on `sys.path`.
- Run by: systemd user manager, `WantedBy=graphical-session.target`, on every graphical login.
- Imports from checkout: `helpers.host_health.*`, `helpers.play_ledger.*`, `helpers.self_update.published`,
  `helpers.version_pins.check_pins` (`helpers/host_health/login_report.py:39-58`).
- Deployed location: none (only the unit is deployed).
- Risk: a WIP merge to F44 that renames a module, changes a CLI flag or breaks an import in any of four packages is live at
  the next login with no play run. Exit 1 is a real unit failure (SuccessExitStatus=3 only), so the user sees a failed unit
  and no report. The checkout is also its DATA (git fetch for play-freshness, ledger); that part is legitimate and can stay as
  an argument (`--repo-root`) while the code moves.

### R2. `host-health-collect.service` + timer (silent refresh)

- Repo: `files/home/.config/systemd/user/host-health-collect.service.j2:23-24`, timer `host-health-collect.timer.j2`,
  deployed by `play-host-health-login-report.yml:257-267`.
- Mechanism: identical to R1 (`python3 -m helpers.host_health.login_report --no-notify`, `WorkingDirectory={{ root_dir }}`).
- Run by: systemd user timer on desktop and server; also restarted by the play-ledger callback after every play run
  (`helpers/play_ledger/health_refresh.py`), so it runs the checkout's code immediately after any play from any branch.
- Deployed location: none.
- Risk: same as R1 but on a timer, so a broken F44 merge turns the panel's status document stale/red within one tick on every
  host whose checkout is on F44. On a server this is the ONLY collector.

### R3. Login-shell snippet `~/.bashrc-includes/host-health-report.bash`

- Repo: `files/home/bashrc-includes/host-health-report.bash.j2:32-33,48`, deployed by `play-host-health-login-report.yml:302-304`.
- Mechanism: `PYTHONPATH="{{ root_dir }}" /usr/bin/python3 -P -m helpers.host_health.login_message --once-a-day`, guarded by
  `[ -r "{{ root_dir }}/helpers/host_health/login_message.py" ]`.
- Run by: every interactive bash shell of the user (sourced from `~/.bashrc`).
- Imports from checkout: `helpers.host_health.{probe,status_document}`, `helpers.play_ledger.{ledger,repo}`.
- Risk: highest blast radius of the set: an import error in WIP code prints a traceback in every new terminal and tmux pane.
  The guard only covers "checkout gone", not "checkout broken".

### R4. `~/.local/bin/fedora-desktop-health` (on-demand report, GNOME panel action)

- Repo: `files/home/.local/bin/fedora-desktop-health.j2:33,106,121-129,145,168`, deployed by
  `play-host-health-login-report.yml:323-325`.
- Mechanism: `ROOT_DIR="{{ root_dir }}"` baked in; `PYTHONPATH="$ROOT_DIR" /usr/bin/python3 -P -m helpers.host_health.play_runner --repo-root "$ROOT_DIR" --validate ...` and `PYTHONPATH="$ROOT_DIR" ... -m helpers.host_health.login_message --on-demand`;
  also execs `"$ROOT_DIR/run.bash" --rerun` (bash slice).
- Run by: the user on demand, and the GNOME panel extension, which opens a terminal on
  `~/.local/bin/fedora-desktop-health` (`extensions/fedora-desktop@fedora-desktop/statusDocument.js:69-74`, `terminal.js:31`).
- Imports from checkout: `helpers.host_health.play_runner` -> `helpers.play_ledger.{freshness,ledger,store}`; plus R3's set.
- Risk: as R3; additionally the panel's "re-run" path validates and launches a play using checkout Python. Launching the
  checkout's PLAYS is inherent (they are Ansible); the validator/report Python is not.

### R5. `ccy-teams` (Go binary) -> `helpers.agent_bus.declare` in the checkout

- Repo: `playbooks/imports/play-claude-yolo.yml:855-866` (ldflags `-X main.RootDir={{ root_dir }}`);
  `src-go/internal/declare/declare.go:507-509` (`env PYTHONPATH=<RootDir> python3 -P -m helpers.agent_bus.declare <sub>`);
  `src-go/internal/teams/host/host.go:164-191` (refuses to run if `<RootDir>/helpers/agent_bus/declare.py` is missing;
  builds the declare client with `Root: h.RootDir`).
- Run by: the host user, directly and from deployed launchers on every team-aware session start:
  `files/var/local/claude-yolo/claude-yolo:854` (`ccy-teams choose-teams`), `:1248`, `:1278`, `:3789`;
  `files/var/local/claude-yolo/lib/agent-bus-seats.bash:89`; `lib/agent-bus-membership.bash:49`;
  `files/var/local/claude-code/cc-teams.bash:89-90`. The declare client is used by board, choose, chooser, find, create
  wizard, ops/takedown, picker, login, lxcadd (`src-go/internal/teams/*`).
- Imports from checkout: `helpers.agent_bus.{bridge,registry,teamfile}`, `helpers.agent_bus_lxc.{derive,members}`,
  `helpers.pingbus.{bundle,protocol}` (`helpers/agent_bus/declare.py:117-119`).
- Deployed location: none. NOTE: `declare.py` cannot simply run from the existing `agent-bus.pyz`: the bundle carries only
  `helpers/pingbus` and `helpers/agent_bus` and EXCLUDES `helpers/pingbus/bundle.py` (`helpers/pingbus/bundle.py:39-41`), and
  declare imports both `agent_bus_lxc` and `pingbus.bundle`.
- Risk: the Go binary is rebuilt only when the ccy play runs, but `declare.py` is live, so a WIP merge changing declare's JSON
  output or subcommands breaks the Go/Python contract on the next `ccy` start; `ccy-teams choose-teams` failing makes `ccy`
  exit (`|| exit $?`). Version skew in both directions. Also reads/edits `environment/localhost/host_vars/localhost.yml` and
  runs `playbooks/imports/play-agent-bus.yml` from the checkout (config + Ansible: by design, but bound to the same path).

### R6. `gh-aliases.inc.bash` bashrc function -> `helpers.github_scopes.cli`

- Repo: `playbooks/imports/play-github-cli-multi.yml:864-872` (blockinfile into `~/.bashrc-includes/gh-aliases.inc.bash`), the
  rendered line at `:1269` inside `_gh_phpstorm_token_report()`:
  `(cd "{{ root_dir }}" && python3 -m helpers.github_scopes.cli missing)`.
- Run by: the host user, when invoking the generated `gh-<alias>-token-phpstorm` shell functions.
- Imports from checkout: `helpers.github_scopes.{cli,scopes}`, and reads `vars/github-required-scopes.yml` from the checkout.
- Risk: lower frequency, but same class: a WIP change to the CLI or the scopes file is live in the user's shell immediately.

## DEPLOYED-OK (already a copy)

| #   | What                                                                                                                   | Repo refs                                                                                                                                                                                | Runs from                                                                                                                                                                                                                                                                                                                                                     |
| --- | ---------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | `container-watch` CLI + `container-watch.{service,timer}`, panel container report                                      | `files/home/.local/bin/container-watch:21`; `playbooks/imports/optional/common/play-container-watch.yml:30-57`                                                                           | `PYTHONPATH=/usr/local/lib/ccy-helpers`; explicit file list (core, crashloop, containment, restartpolicy, cli) currently complete.                                                                                                                                                                                                                            |
| D2  | `github-ssh-443` toggle                                                                                                | `files/usr/local/bin/github-ssh-443:25`; `playbooks/imports/play-github-cli-multi.yml:98-117`                                                                                            | `/usr/local/lib/ccy-helpers/helpers/github443` (core.py, cli.py).                                                                                                                                                                                                                                                                                             |
| D3  | vmtest lab: `vmtest-bridge-watcher`, `vmtest` Go (`src-go/internal/vmtest/lab.go:37,204-215`), `vmtest-bridge*@` units | `files/home/.local/bin/vmtest-bridge-watcher:17,33`; `play-vm-test-lab.yml:135-154` (fileglob of `helpers/vmtest/*.py`)                                                                  | `/usr/local/lib/ccy-helpers/helpers/vmtest`, run with `-P`. The checkout is used only as the spool data dir (`VMTEST_CHECKOUT`, `--checkout`), not as code. Drift-checked by `src-go/qa/deployeddrift/run.go:181`.                                                                                                                                            |
| D4  | `displaylink-dock-recovery.service`                                                                                    | `files/etc/systemd/system/displaylink-dock-recovery.service:9`; `play-displaylink.yml:297-348` (also an inline copy of the unit at :346-348)                                             | `WorkingDirectory=/usr/local/lib/displaylink-recovery`.                                                                                                                                                                                                                                                                                                       |
| D5  | `agent-bus` admin wrapper, `agent-bus-hs-health@.service`                                                              | `files/usr/local/bin/agent-bus` (`AGENT_BUS_PYZ`), `files/etc/systemd/system/agent-bus-hs-health@.service:13`                                                                            | `/usr/local/lib/agent-bus/agent-bus.pyz` via `python3 -I`.                                                                                                                                                                                                                                                                                                    |
| D6  | `pingbus` command + member kit + Claude Code plugin dir (host)                                                         | `files/usr/local/sbin/agent-bus-install:387-397,441-451,493-498`; `files/opt/claude-yolo/optional/agent-bus/agent-bus-claude:95`; `files/var/local/claude-code/cc-teams.bash:29,141-143` | `/usr/local/bin/pingbus` -> `../lib/agent-bus/pingbus.pyz`; kit at `/usr/local/share/agent-bus/kit` (its own `pingbus`, `settings.json`, `plugin/pingbus/`). `--plugin-dir` points at the KIT copy, never the checkout. Plugin hooks (`plugin/pingbus/hooks/hooks.json`) and the status line (`settings.json`: `pingbus status-line`) call `pingbus` on PATH. |
| D7  | CCY container pingbus + plugin dir                                                                                     | `playbooks/imports/play-claude-yolo.yml:380-399` (zipapp built into `/opt/claude-yolo/optional/agent-bus/pingbus`), `files/var/local/claude-yolo/entrypoint.sh:566,595`                  | Image copy at `/opt/claude-yolo/optional/agent-bus`; `--plugin-dir $_ccy_agent_bus/plugin/pingbus`.                                                                                                                                                                                                                                                           |
| D8  | ccy one-key ssh-agent filter                                                                                           | `files/var/local/claude-yolo/lib/ssh-handling.bash:1042,1178`; `play-claude-yolo.yml:476`                                                                                                | `/var/local/claude-yolo/lib/ssh_agent_filter.py`, `python3 -I`.                                                                                                                                                                                                                                                                                               |
| D9  | ccy supervisor lifecycle plugin                                                                                        | `files/var/local/claude-yolo/lib/session-lifecycle.bash:43`; `Dockerfile:416-418`                                                                                                        | `/opt/claude-yolo/supervisor-plugins/ccy_lifecycle.py` in the image.                                                                                                                                                                                                                                                                                          |
| D10 | `fedora-desktop-self-update` (root timer)                                                                              | `files/usr/local/sbin/fedora-desktop-self-update:155`; units `fedora-desktop-self-update*.service*`                                                                                      | `cd /var/lib/fedora-desktop/deploy && python3 -E -s -B -m helpers.self_update.cycle`: a SEPARATE clone whose HEAD must be signed by the configured principal and clean (no strays), never the dev checkout. A WIP F44 merge reaches it only if signed and on its channel.                                                                                     |
| D11 | `kernel-version-manager.service`                                                                                       | `files/etc/systemd/system/kernel-version-manager.service:8`                                                                                                                              | `/usr/local/bin/manage-kernel-versions.py` (standalone, deployed).                                                                                                                                                                                                                                                                                            |

## REPO-BY-DESIGN (Ansible during a play, operator entry point, QA, tests, plans)

1. **Plays running helpers with `chdir: "{{ root_dir }}"`** (run only during `ansible-playbook`):
   `helpers.agent_bus.{config_restart,bridge,timer_state}` and `helpers.agent_bus_lxc.members` (`play-agent-bus.yml:138,238,275,347,357`);
   `helpers.agent_bus.element` (`play-agent-bus-element.yml:58,107`); `helpers.gnome.{apply_enabled_extensions,verify_extension}`
   (`play-gnome-shell-extensions.yml:159,213`, `play-fedora-desktop-panel.yml:112,155`, `play-displaylink.yml:409`);
   `helpers.pyenv` (`play-python.yml:265`); `helpers.rpm_keys` (`play-browsers.yml:100,141`); `helpers.dconf.staleness`
   (`play-suspend-and-lid-policy.yml:315`); `helpers.suspend_wakeup.cli` (`play-suspend-and-lid-policy.yml:374`);
   `helpers.sshd_ports.cli` (`play-lxc-install-config.yml:177`); `helpers.unifi.*` (`play-unifi-firmware.yml:76,91,106`);
   `helpers.github_scopes.cli audit`, `helpers.github443.cli`, `helpers.github_signing.cli` (`play-github-cli-multi.yml:214,446,673,748,800`).
2. **Zipapp builds at play time** from the checkout: `helpers.pingbus.bundle` via `agent-bus-install software --source {{ root_dir }}`
   (`play-agent-bus.yml:51,180`) and directly in `play-claude-yolo.yml:384-396`. Output is deployed (D5-D7).
3. **`agent-bus-install` executed from `files/usr/local/sbin/` in the checkout by the play** (`play-agent-bus.yml:51,165,180,248,308`);
   the deployed copy in `/usr/local/sbin` serves the timers and ccy-teams. Bash slice; noted because the play-time copy and the
   deployed copy can differ inside one run.
4. **Ansible callback** `callback_plugins/play_ledger.py` (enabled in `ansible.cfg`) imports `helpers.play_ledger`; runs inside
   `ansible-playbook` only. Its `health_refresh` restarts R2.
5. **`run.bash`** (the checkout's own operator entry point): `helpers.play_lock.lock` (`:1891,1897`),
   `helpers.play_ledger.changed_plays` (`:2051,2099`), `helpers.github_scopes.cli` (`:2815,2832,2986`),
   `helpers.github443.cli` bootstrap (`:3080`).
6. **Scripts**: `scripts/gh-account-setup.bash:44` (github_scopes; run by the play and by hand from the checkout),
   `scripts/check-pinned-versions.bash:106` (version_pins).
7. **QA**: `scripts/qa-gates.tsv:116-117` (`helpers.gnome.check_extension_compat`, `check_panel_contract`),
   `helpers/qa_environment`, `src-go/qa/**`.
8. **Tests**: `tests/helpers/**`, `scripts/test-*.bash` (PYTHONPATH=$REPO_ROOT), `src-go/**/_test.go` fakes.
9. **Plan scripts**: e.g. `CLAUDE/Plan/00055-container-process-watchdog/acceptance.bash:150`,
   `CLAUDE/Plan/00161-agent-team-bus-matrix/*.py`, `CLAUDE/Plan/00201-*/m7_check.py`.

Borderline, recorded for the plan owner: the container supervisor `CCY_SUPERVISOR_PATH=/workspace/.claude/ccy/claude-supervise.py`
(`files/var/local/claude-yolo/entrypoint.sh:630,658`; `claude-yolo:3647`) runs the MOUNTED PROJECT's own supervisor. It is
project-owned (hooks-daemon), not a fedora-desktop tool, but when the project is this repository it is checkout code run by
every ccy session in it.

## Per-helper-package runtime summary

| Package                                                          | Runtime caller outside a play                                        | Class                                           |
| ---------------------------------------------------------------- | -------------------------------------------------------------------- | ----------------------------------------------- |
| agent_bus                                                        | `agent-bus.pyz` (D5); `declare.py` via ccy-teams (R5)                | DEPLOYED-OK + RUNTIME-FROM-CHECKOUT             |
| agent_bus_lxc                                                    | imported by `declare.py` (R5); plays                                 | RUNTIME-FROM-CHECKOUT (via R5)                  |
| pingbus                                                          | `pingbus.pyz` (D6, D7); `bundle`/`protocol` imported by declare (R5) | DEPLOYED-OK + RUNTIME-FROM-CHECKOUT (via R5)    |
| containerwatch                                                   | container-watch (D1)                                                 | DEPLOYED-OK                                     |
| host_health                                                      | R1-R4                                                                | RUNTIME-FROM-CHECKOUT                           |
| play_ledger                                                      | imported by host_health (R1-R4); callback; run.bash                  | RUNTIME-FROM-CHECKOUT (via host_health)         |
| self_update                                                      | deploy clone (D10); `published` imported by host_health (R1, R2)     | DEPLOYED-OK + RUNTIME-FROM-CHECKOUT (via R1/R2) |
| version_pins                                                     | imported by host_health login_report (R1, R2); script                | RUNTIME-FROM-CHECKOUT (via R1/R2)               |
| play_lock                                                        | run.bash; imported by self_update in the deploy clone                | REPO-BY-DESIGN / DEPLOYED-OK                    |
| displaylink_recovery                                             | D4                                                                   | DEPLOYED-OK                                     |
| suspend_wakeup                                                   | play only                                                            | REPO-BY-DESIGN                                  |
| github_scopes                                                    | R6; plays; run.bash; gh-account-setup                                | RUNTIME-FROM-CHECKOUT (R6)                      |
| github443                                                        | D2; play; run.bash bootstrap                                         | DEPLOYED-OK                                     |
| github_signing, gnome, dconf, pyenv, rpm_keys, sshd_ports, unifi | plays only                                                           | REPO-BY-DESIGN                                  |
| qa_environment                                                   | QA only                                                              | REPO-BY-DESIGN                                  |
| vmtest                                                           | D3                                                                   | DEPLOYED-OK                                     |
| ssh_agent_filter                                                 | D8                                                                   | DEPLOYED-OK                                     |

## Cross-slice notes (not Python, for the other inventories)

- `fedora-desktop-health` execs `$ROOT_DIR/run.bash --rerun` (R4).
- `/etc/ftp-camera/config` bakes `REPO_PATH={{ root_dir | realpath }}` for the vault helper (`play-ftp-camera.yml:262`).
- `ccy-teams` runs `playbooks/imports/play-agent-bus.yml` and edits host_vars in the checkout (`config.go:38-40`, `ops.go:316`).
- The existing deployed-drift gate (`src-go/qa/deployeddrift/run.go:177-190`) covers vmtest and the agent-bus kit only, not the
  `ccy-helpers` containerwatch/github443 copies or displaylink-recovery.
